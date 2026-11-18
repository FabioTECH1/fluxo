package bridge

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/executiondomain/testfixture"
)

func TestRebootProofRequiresExactHistoricalInstallerReplay(t *testing.T) {
	p := historicalTaskArchiveFixture()
	for _, kind := range []string{activationFresh, activationInstallerHistoricalTasks, activationInstallerHistoricalCombined, activationHistoricalRetirement, activationInstallerTaskBackendRenewal, activationTaskHelperRefresh} {
		p.Kind = kind
		for _, resume := range []string{"", p.ID, "another-operation"} {
			for _, installer := range []bool{true, false} {
				r := activationRequest{Resume: resume, InstallerHistoricalTasks: installer}
				want := installer && resume == p.ID && (kind == activationInstallerHistoricalCombined || kind == activationInstallerHistoricalTasks)
				if r.exactHistoricalTaskInstallerReplay(p) != want {
					t.Fatalf("kind=%s resume=%q installer=%v", kind, resume, installer)
				}
			}
		}
	}
}

// Disk-backed unit fixture. Inject loss of the caller after actual protected
// writes. Host qualification and genuine cron receipts require native acceptance.
func TestHistoricalInstallerInterruptedPublicationReplay(t *testing.T) {
	for _, kind := range []string{activationInstallerHistoricalTasks, activationInstallerHistoricalCombined} {
		for _, cut := range []string{"pointer", "helpers", "scheduler-core", "task-core", "prepared", "trigger", "verified"} {
			t.Run(kind+"/"+cut, func(t *testing.T) {
				if !testfixture.InRoot(t, "../services/maintenancetask/testdata") {
					return
				}
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				const data = "/var/lib/fluxo"
				p, image := stageHistoricalTaskUnitArchive(t, kind)
				archive := filepath.Join(data, "execution-activations", p.ID)
				raw, err := readProtected(installerStagedArchive(data, p.ID)+"/plan.json", 128<<20)
				must(err)
				intent := installerExecutionIntent{Format: 1, Operation: p.ID, Image: p.BinarySHA256, InstallationID: p.InstallationID, Version: p.Version, Phase: "pending", Plan: digest(raw), Service: strings.Repeat("c", 64), WasActive: true}
				pointer := filepath.Join(data, installerExecutionRecord)
				must(writeActivationJSON(pointer, intent))
				floor, err := activationTargetFloor(p, raw, "prepared")
				must(err)
				floorPath := filepath.Join(data, "execution-readers.json")
				lost := errors.New("caller interrupted")
				run := func(interrupt string) error {
					boundary := func(point string) error {
						if interrupt == point {
							return lost
						}
						return nil
					}
					if err := boundary("pointer"); err != nil {
						return err
					}
					if err := prepareInstallerRecovery(data, intent); err != nil {
						return err
					}
					if err := applyExecutionTransitions(t.Context(), p.Files, func(f executiondomain.ActivationFile) error {
						if err := applyActivationFile(f, image); err != nil {
							return err
						}
						switch {
						case f.Image:
							return boundary("helpers")
						case f.Path == "/var/lib/fluxo-scheduler/state.json":
							return boundary("scheduler-core")
						case f.Path == "/var/lib/fluxo-system-tasks/state.json":
							return boundary("task-core")
						case f.Cron:
							return boundary("trigger")
						}
						return nil
					}, func() error {
						if err := writeActivationJSON(floorPath, floor); err != nil {
							return err
						}
						return boundary("prepared")
					}); err != nil {
						return err
					}
					if err := writeActivationJSON(archive+"/verified.json", map[string]any{"operation_id": p.ID, "plan_sha256": digest(raw), "format_readback_verified": true}); err != nil {
						return err
					}
					if err := boundary("verified"); err != nil {
						return err
					}
					floor.Phase = "committed"
					return writeActivationJSON(floorPath, floor)
				}
				if err := run(cut); !errors.Is(err, lost) {
					t.Fatal(err)
				}
				got, err := readInstallerExecutionIntent(pointer)
				must(err)
				if got == nil || *got != intent {
					t.Fatal("lost original installer pointer")
				}
				must(prepareInstallerRecovery(data, *got))
				saved, err := readProtected(archive+"/plan.json", 128<<20)
				must(err)
				if !bytes.Equal(saved, raw) {
					t.Fatal("plan changed")
				}
				prior, err := executiondomain.ReadFloor(data)
				must(err)
				must(validateActivationRecovery(p, got.Operation, got.InstallationID, got.Image, got.Version, prior, saved))
				must(validateActivationFloorTransition(data, p, saved, prior))
				must(validateActivationSources(archive, p))
				for _, f := range p.Files {
					must(validateActivationCurrent(f, p.BinarySHA256))
				}
				must(verifyRecoveryInvocationsLocked(t.Context(), p))
				if cut == "task-core" {
					if prior != nil {
						t.Fatal("floor present at pre-floor interruption")
					}
					for _, f := range p.Files {
						if f.Cron {
							b, err := readProtected(f.Path, 1<<20)
							must(err)
							if digest(b) != f.Before {
								t.Fatal("trigger already replaced")
							}
						}
					}
				}
				must(run(""))
				must(validateCommittedActivationArchive(data, floor))
				for _, f := range p.Files {
					must(validateActivationIntended(f, p.BinarySHA256))
				}
			})
		}
	}
}
