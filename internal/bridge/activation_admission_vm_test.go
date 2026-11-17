package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// This test calls the actual exported activation API. Its child has private
// bind mounts for every mutable coordination namespace and the installed image;
// no live floor, journal, DB, binary, lock inode or service is changed.
type activationAdmissionFixture struct {
	Format                           int
	Root, Session, Case, Boot, Image string
}
type activationAdmissionResult struct {
	Error         string
	Before, After map[string]string
}

var activationAdmissionCases = map[string]string{
	"current-scheduler": "minimum-reader floor", "current-task": "minimum-reader floor",
	"prototype-scheduler": "minimum-reader floor", "prototype-task": "minimum-reader floor",
	"absent-core-anchor": "partial or damaged", "old-core-anchor": "initialization anchor",
	"old-task-phase": "initialization is incomplete", "lost-floor-archive": "activation history",
	"prepared-floor": "incomplete",
}

func activationAdmissionHost() bool {
	h, _ := os.Hostname()
	return h == "fluxo-bridge-prod-20261101" || h == "fluxo-bridge-jammy-20261101" || h == "fluxo-bridge-resolute-20261101"
}
func readActivationAdmissionFixture(root string) (activationAdmissionFixture, error) {
	var f activationAdmissionFixture
	if os.Geteuid() != 0 || !activationAdmissionHost() || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		return f, errors.New("guarded root native admission test only")
	}
	session := filepath.Dir(root)
	token := strings.TrimPrefix(filepath.Base(session), "fluxo-activation-admission-")
	if filepath.Dir(session) != "/root" || token == filepath.Base(session) {
		return f, errors.New("admission fixture namespace")
	}
	if _, e := uuid.Parse(token); e != nil {
		return f, e
	}
	raw, e := readProtected(filepath.Join(root, "case.json"), 1<<20)
	if e != nil {
		return f, e
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		return f, e
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return f, e
	}
	if f.Format != 1 || f.Root != root || f.Session != session || f.Case != filepath.Base(root) || activationAdmissionCases[f.Case] == "" || f.Boot != strings.TrimSpace(string(boot)) {
		return f, errors.New("admission fixture identity differs")
	}
	image, e := readProtected(filepath.Join(session, "admission.test"), 128<<20)
	if e != nil {
		return f, e
	}
	if digest(image) != f.Image {
		return f, errors.New("admission fixture image differs")
	}
	return f, nil
}
func activationAdmissionTree(root string) (map[string]string, error) {
	result := map[string]string{}
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		key, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if info.IsDir() {
			result[key] = fmt.Sprintf("dir:%o", info.Mode().Perm())
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("unexpected fixture artifact")
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		result[key] = fmt.Sprintf("%o:%s", info.Mode().Perm(), digest(b))
		return nil
	})
	return result, e
}
func init() {
	if len(os.Args) != 3 || (os.Args[1] != "--fluxo-activation-admission-mount" && os.Args[1] != "--fluxo-activation-admission-inside") {
		return
	}
	root := os.Args[2]
	f, e := readActivationAdmissionFixture(root)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	if os.Args[1] == "--fluxo-activation-admission-mount" {
		// /usr/bin/unshare created this process's namespace before Go started.
		for _, mount := range [][2]string{{filepath.Join(root, "var-lib"), "/var/lib"}, {filepath.Join(root, "run-lock"), "/run/lock"}, {filepath.Join(f.Session, "admission.test"), "/usr/local/bin/fluxo"}} {
			if e = unix.Mount(mount[0], mount[1], "", unix.MS_BIND, ""); e != nil {
				fmt.Fprintln(os.Stderr, "private fixture bind", e)
				os.Exit(2)
			}
		}
		e = syscall.Exec("/usr/local/bin/fluxo", []string{"/usr/local/bin/fluxo", "--fluxo-activation-admission-inside", root}, os.Environ())
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	self, e := os.Executable()
	if e != nil || self != "/usr/local/bin/fluxo" {
		fmt.Fprintln(os.Stderr, "fixture is not the installed-path image", self, e)
		os.Exit(2)
	}
	before, e := activationAdmissionTree(root)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := ActivateExecutionFormats(ctx, "/var/lib/fluxo", "0.0.0-admission-test", "")
	cancel()
	after, e := activationAdmissionTree(root)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	result := activationAdmissionResult{Before: before, After: after}
	if err != nil {
		result.Error = err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(result)
	os.Exit(0)
}

func TestVMFreshActivationAdmissionBeforeAnyWrite(t *testing.T) {
	if os.Geteuid() != 0 || !activationAdmissionHost() || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root known bridge VM private-namespace admission only")
	}
	session := "/root/fluxo-activation-admission-" + uuid.NewString()
	if e := os.Mkdir(session, 0700); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		t.Fatal(e)
	}
	image, e := readProtected(exe, 128<<20)
	if e != nil {
		t.Fatal("stage this test binary under protected root ancestry", e)
	}
	if e = writeActivationFile(filepath.Join(session, "admission.test"), image, 0700); e != nil {
		t.Fatal(e)
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		t.Fatal(e)
	}
	for name, want := range activationAdmissionCases {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(session, name)
			for _, d := range []string{root, root + "/var-lib", root + "/var-lib/fluxo", root + "/run-lock"} {
				if e = os.Mkdir(d, 0700); e != nil {
					t.Fatal(e)
				}
			}
			write := func(path string, b []byte) {
				t.Helper()
				if e = os.MkdirAll(filepath.Dir(root+"/var-lib/"+path), 0700); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(root+"/var-lib/"+path, b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			core := func(component string, schema int) {
				write(component+"/state.json", []byte(fmt.Sprintf(`{"schema":%d,"entries":{},"invocations":{}}`, schema)))
			}
			write("fluxo/fluxo.db", []byte("admission fixture: never open or initialize this sentinel\n"))
			switch name {
			case "current-scheduler":
				core("fluxo-scheduler", executiondomain.CurrentSchedulerSchema)
			case "current-task":
				core("fluxo-system-tasks", executiondomain.CurrentSystemTaskSchema)
			case "prototype-scheduler":
				core("fluxo-scheduler", 3)
			case "prototype-task":
				core("fluxo-system-tasks", 2)
			case "absent-core-anchor":
				write("fluxo-scheduler.initialized", []byte("activation:"+uuid.NewString()+"\n"))
			case "old-core-anchor":
				core("fluxo-scheduler", 2)
				write("fluxo-scheduler.initialized", []byte("activation:"+uuid.NewString()+"\n"))
			case "old-task-phase":
				core("fluxo-system-tasks", 1)
				write("fluxo-system-tasks.lock", []byte("initializing\n"))
			case "lost-floor-archive":
				core("fluxo-scheduler", 2)
				core("fluxo-system-tasks", 1)
				write("fluxo/execution-activations/"+uuid.NewString()+"/plan.json", []byte("retained source evidence\n"))
			case "prepared-floor":
				core("fluxo-scheduler", executiondomain.CurrentSchedulerSchema)
				core("fluxo-system-tasks", executiondomain.CurrentSystemTaskSchema)
				f := executiondomain.ReaderFloor{Format: 1, Phase: "prepared", SchedulerSupport: executiondomain.SchedulerCompiledReaderSupport, SystemTaskSupport: executiondomain.SystemTaskReaderSupport, SchedulerSchema: executiondomain.CurrentSchedulerSchema, SystemTaskSchema: executiondomain.CurrentSystemTaskSchema, Adapters: []executiondomain.AdapterID{executiondomain.CgroupExitV1, executiondomain.PersistentV1}, RoleContracts: executiondomain.Capabilities().RoleContracts, ActivationID: uuid.NewString(), InstallationID: strings.Repeat("a", 32), BinarySHA256: digest(image), PlanSHA256: strings.Repeat("b", 64)}
				b, _ := json.Marshal(f)
				write("fluxo/execution-readers.json", b)
			}
			fixture := activationAdmissionFixture{Format: 1, Root: root, Session: session, Case: name, Boot: strings.TrimSpace(string(boot)), Image: digest(image)}
			b, _ := json.Marshal(fixture)
			if e = os.WriteFile(filepath.Join(root, "case.json"), b, 0600); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/usr/bin/unshare", "--mount", "--propagation", "private", filepath.Join(session, "admission.test"), "--fluxo-activation-admission-mount", root)
			cmd.Env = append(os.Environ(), "FLUXO_PHP_UPDATE_VM_TEST=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("private mount/API fixture: %v %s", err, out)
			}
			var result activationAdmissionResult
			if err = json.Unmarshal(out, &result); err != nil {
				t.Fatalf("fixture result %v %s", err, out)
			}
			if !strings.Contains(result.Error, want) {
				t.Fatalf("wrong API refusal: %q want %q", result.Error, want)
			}
			if !reflect.DeepEqual(result.Before, result.After) {
				t.Fatalf("rejected API created a lock or changed persistent fixture state: before=%v after=%v", result.Before, result.After)
			}
			t.Logf("actual exported API rejected %s before ANY lock/directory/DB write", name)
		})
	}
	t.Logf("private admission evidence retained: %s", session)
}
