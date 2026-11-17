package executiondomain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// Execute real retained-helper fixtures, not a mocked capability response. The
// injected reader changes only fixture ownership admission (unit tests run as a
// normal user); production always uses the protected root-owned reader.
func TestExecutableStaleHelperCannotPersistNewInvocation(t *testing.T) {
	for _, component := range []string{"scheduler", "system-task"} {
		for _, kind := range []string{"old-reader", "missing-execute-role", "stale-profile", "missing-backend-role", "current"} {
			if component == "scheduler" && (kind == "stale-profile" || kind == "missing-backend-role") {
				continue
			}
			t.Run(component+"/"+kind, func(t *testing.T) {
				have := Capabilities()
				switch kind {
				case "old-reader":
					have.SchedulerSupport = 4
					have.SystemTaskSupport = 2
					have.SchedulerSchemas = []int{1, 2, 3}
					have.SystemTaskSchemas = []int{1, 2}
					have.Adapters = []AdapterID{CgroupExitV1}
				case "missing-execute-role":
					delete(have.RoleContracts, component+"-execute")
				case "stale-profile":
					have.RoleContracts["system-task-profile"] = []string{"apt-a0-host-inputs-v2"}
				case "missing-backend-role":
					delete(have.RoleContracts, "scheduler-backend-reader")
				}
				metadata, _ := json.Marshal(have)
				// The old helper advertises its real historical 4/2 reader protocol; it
				// cannot acquire current authority merely because the panel/floor is new.
				code := []byte("#!/bin/sh\n[ \"$1\" = --execution-capabilities ] || exit 2\nprintf '%s\\n' '" + string(metadata) + "'\n")
				directory := t.TempDir()
				helper := filepath.Join(directory, "retained-helper")
				if err := os.WriteFile(helper, code, 0700); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(code)
				expected := hex.EncodeToString(sum[:])
				observe := func(path string) (unix.Stat_t, error) { var s unix.Stat_t; err := unix.Lstat(path, &s); return s, err }
				read := func(path string, _ int64) ([]byte, error) { return os.ReadFile(path) }
				run := func(ctx context.Context, path string) (string, error) {
					return syscmd.Run(ctx, 2*time.Second, path, "--execution-capabilities")
				}
				inspect := func() (ReaderCapabilities, error) {
					return readHelperCapabilities(context.Background(), helper, expected, read, observe, run)
				}
				need, err := InvocationHelperRequirements(component, CgroupExitV1)
				if err != nil {
					t.Fatal(err)
				}
				if component == "system-task" {
					need.RoleContracts["scheduler-backend-reader"] = []string{"ubuntu-noble-cron-v1"}
				}
				marker := filepath.Join(directory, "persisted-authority")
				writes := 0
				err = persistInvocationWithHelper(inspect, need, func() error { writes++; return os.WriteFile(marker, []byte("authority and profile"), 0600) })
				if kind == "current" {
					if err != nil || writes != 1 {
						t.Fatal("current actual helper refused", err, writes)
					}
				} else {
					if err == nil || writes != 0 {
						t.Fatal("stale helper reached durable authority write", err, writes)
					}
					if _, e := os.Lstat(marker); !os.IsNotExist(e) {
						t.Fatal("stale helper persisted authority", e)
					}
				}
			})
		}
	}
}

func TestInvocationHelperRequirementsDoNotDemandUnrelatedExecutionRoles(t *testing.T) {
	need, err := InvocationHelperRequirements("scheduler", CgroupExitV1)
	if err != nil {
		t.Fatal(err)
	}
	have := Capabilities()
	delete(have.RoleContracts, "system-task-execute")
	delete(have.RoleContracts, "system-task-profile")
	delete(have.RoleContracts, "scheduler-backend-reader")
	if err = RequireCapabilities(have, need); err != nil {
		t.Fatal("Scheduler was forced to advertise unrelated execution authority", err)
	}
}

func TestProtectedNonCapabilityExecutableCannotGrantInvocation(t *testing.T) {
	// A real older/non-capability executable which ignores unknown arguments
	// cannot be mistaken for a helper just because its exit status is zero.
	if _, err := ReadHelperCapabilities(context.Background(), "/usr/bin/true", ""); err == nil {
		t.Fatal("successful exit without metadata granted helper capabilities")
	}
}

func TestHelperDigestMismatchIsRefusedBeforeExecutingMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("different helper"), 0700); err != nil {
		t.Fatal(err)
	}
	runs := 0
	_, err := readHelperCapabilities(context.Background(), path, "not-the-recorded-digest", func(path string, _ int64) ([]byte, error) { return os.ReadFile(path) }, func(path string) (unix.Stat_t, error) { var s unix.Stat_t; err := unix.Lstat(path, &s); return s, err }, func(context.Context, string) (string, error) { runs++; return "{}", nil })
	if err == nil || runs != 0 {
		t.Fatal("changed content-addressed helper executed before authentication", err, runs)
	}
}
