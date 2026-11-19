package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

// Exercise only archive publication in a private root-owned directory, using a
// genuine completed plan/image as input. Full activation independently verifies
// original backups before any core writes. Live journals/floors are never written.
func TestVMInstallerArchivePublicationRecovery(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || host != "fluxo-bridge-noble-stale-proof-20261117" {
		t.Skip("named root fixture and explicit opt-in required")
	}
	intent, err := readInstallerExecutionIntent("/var/lib/fluxo/execution-installer.json")
	if err != nil || intent == nil || intent.Operation == "" {
		t.Fatalf("genuine installer archive required: %v", err)
	}
	source := filepath.Join("/var/lib/fluxo/execution-activations", intent.Operation)
	plan, err := readProtected(filepath.Join(source, "plan.json"), 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	image, err := readProtected(filepath.Join(source, "image"), 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	liveFloor, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"staged", "changed-plan", "changed-image", "missing-stage", "conflicting-final"} {
		t.Run(name, func(t *testing.T) {
			root, err := os.MkdirTemp("/root", "fluxo-installer-archive-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			stage := installerStagedArchive(root, intent.Operation)
			if name != "missing-stage" {
				if err = os.Mkdir(stage, 0700); err != nil {
					t.Fatal(err)
				}
				data := plan
				if name == "changed-plan" {
					data = append(append([]byte(nil), plan...), '\n')
				}
				if err = writeActivationFile(filepath.Join(stage, "plan.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				data = image
				if name == "changed-image" {
					data = []byte("wrong image")
				}
				if err = writeActivationFile(filepath.Join(stage, "image"), data, 0700); err != nil {
					t.Fatal(err)
				}
			}
			final := filepath.Join(root, "execution-activations", intent.Operation)
			if name == "conflicting-final" {
				if err = os.MkdirAll(final, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if name == "staged" {
				staged, inspectErr := inspectInstallerRecoveryArchive(root, *intent)
				if inspectErr != nil || !staged {
					t.Fatal("read-only staged archive inspection failed", inspectErr)
				}
				if _, e := os.Lstat(final); !os.IsNotExist(e) {
					t.Fatal("archive inspection published authority")
				}
				if _, e := os.Lstat(stage); e != nil {
					t.Fatal("archive inspection consumed staging", e)
				}
			}
			err = prepareInstallerRecovery(root, *intent)
			if name != "staged" {
				if err == nil {
					t.Fatal("unexpected archive accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Lstat(stage); !os.IsNotExist(err) {
				t.Fatal("staging was not atomically consumed")
			}
			if err = prepareInstallerRecovery(root, *intent); err != nil {
				t.Fatalf("committed directory retry: %v", err)
			}
			got, err := readProtected(filepath.Join(final, "plan.json"), 128<<20)
			if err != nil || digest(got) != intent.Plan {
				t.Fatal("plan bytes changed")
			}
			if err = os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			if err = prepareInstallerRecovery(root, *intent); err == nil {
				t.Fatal("two archives accepted")
			}
		})
	}
	after, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil || digest(after) != digest(liveFloor) {
		t.Fatal("live floor changed during archive-only tests")
	}
}
