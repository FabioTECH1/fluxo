package maintenancetask

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/releaseinfo"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func historicalRecoveryVMLocks(t *testing.T, ctx context.Context) func() {
	t.Helper()
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root opt-in on retained historical VM only")
	}
	lifecycle, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := packages.Acquire(ctx)
	if err != nil {
		lifecycle()
		t.Fatal(err)
	}
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		pkg()
		lifecycle()
		t.Fatal(err)
	}
	var once sync.Once
	return func() { once.Do(func() { native(); pkg(); lifecycle() }) }
}

func TestVMHistoricalV1Recovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	release := historicalRecoveryVMLocks(t, ctx)
	defer release()
	current := newProfile(PackageCleanup)
	spec, err := inspectCleanupPlatformVersion(ctx, &current, "2.8.3")
	if err != nil {
		t.Fatal("unchanged historical tuple inspection", err)
	}
	if spec.executionQualified || current.External["package:apt"] != "2.8.3" {
		t.Fatal("historical inspection acquired execution selection")
	}
	c, ok := Qualify(991101, 0, "0 0 1 1 *", PackageCleanupCommand, "root")
	if !ok {
		t.Fatal("fixed recovery contract")
	}
	if _, err = qualifyContractProfile(ctx, c); err == nil {
		t.Fatal("historical tuple admitted for fresh execution")
	}
	before, err := legacyV1Observation(ctx, PackageCleanup)
	if err != nil {
		t.Fatal("baseline observation producer", err)
	}
	if compatibleProfiles(before, current, false, false) {
		t.Fatal("fixture did not reproduce added-evidence incompatibility")
	}
	if _, err = qualifyRecoveryProfile(ctx, c, before, false); err != nil {
		t.Fatal("historical recovery inspection", err)
	}
	for _, name := range []string{"changed-file", "missing-file", "directory-member", "package-version", "v2", "empty-current-marker", "unknown-empty-external", "unknown-empty-directory", "unknown-empty-file"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(before)
			var changed profile
			_ = json.Unmarshal(raw, &changed)
			switch name {
			case "changed-file":
				changed.Files["/usr/bin/apt-config"] = strings.Repeat("0", 64)
			case "missing-file":
				delete(changed.Files, "/usr/bin/apt-config")
			case "directory-member":
				changed.Directories["/etc/apt/apt.conf.d"] = []string{"unknown"}
			case "package-version":
				changed.External["package:apt"] = "2.8.2"
			case "v2":
				changed.External["apt-execution-profile"] = "apt-a0-host-inputs-v2"
			case "empty-current-marker":
				changed.External["dpkg-bootstrap-contract"] = ""
			case "unknown-empty-external":
				changed.External["unrecognized"] = ""
			case "unknown-empty-directory":
				changed.Directories["/unrecognized"] = nil
			case "unknown-empty-file":
				changed.Files["/unrecognized"] = ""
			}
			if _, err := qualifyRecoveryProfile(ctx, c, changed, false); err == nil {
				t.Fatal("changed or unsupported observations accepted")
			}
		})
	}
	t.Log("actual baseline APT 2.8.3 observations recovered; fresh execution and changed/missing/mixed observations refused")
}

// This private journal models an already absent historical execution domain.
// Its profile is produced by the retained baseline observer on the real host;
// no cron acknowledgement, production journal or maintenance payload is created.
func TestVMHistoricalV1ReconciliationJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	release := historicalRecoveryVMLocks(t, ctx)
	defer release()
	current := newProfile(PackageCleanup)
	if _, err := inspectCleanupPlatformVersion(ctx, &current, "2.8.3"); err != nil {
		t.Fatal(err)
	}
	before, err := legacyV1Observation(ctx, PackageCleanup)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := bootID()
	if err != nil {
		t.Fatal(err)
	}
	pidText, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "cron.service", "--property=MainPID", "--value")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(pidText))
	if err != nil || pid <= 1 {
		t.Fatal("actual historical cron identity required", err)
	}
	stat, err := os.Stat("/sys/fs/cgroup/system.slice/cron.service")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := processIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(identity, ":")
	if len(parts) != 3 {
		t.Fatal("incomplete actual cron identity")
	}
	backend := scheduler.CronBackendIdentity{Boot: boot, PID: pid, Start: parts[2], GroupInode: stat.Sys().(*syscall.Stat_t).Ino}
	root, err := os.MkdirTemp("/var/lib", "fluxo-history-v1-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	oldRoot := stateRoot
	stateRoot = root
	t.Cleanup(func() { stateRoot = oldRoot })
	if err = atomicFile(root+".initialized", []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = atomicFile(root+".lock", []byte(readyGate), 0600); err != nil {
		t.Fatal(err)
	}
	c, _ := Qualify(991102, 0, "0 0 1 1 *", PackageCleanupCommand, "root")
	unit := "fluxo-system-task-" + uuid.NewString() + ".service"
	r := registration{Contract: c, Database: filepath.Join(root, "unused.db"), Identity: uuid.NewString(), Boot: boot, Backend: backend, Nonce: uuid.NewString(), Helper: filepath.Join(root, "retained-helper"), Predecessor: LegacyContent(c)}
	v := invocation{Unit: unit, Registration: r, Boot: boot, Status: "failed", Profile: before, Error: "private historical unknown-outcome fixture"}
	if err = save(journal{Schema: 1, Entries: map[int]registration{}, Invocations: map[string]invocation{unit: v}}); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(v.Profile)
	release()
	if err = ReconcileInvocations(ctx); err != nil {
		t.Fatal("complete explicit recovery path", err)
	}
	raw, err := readRegular(filepath.Join(root, "history", unit+".json"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	var recovered invocation
	if err = json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(recovered.Profile)
	if string(after) != string(original) || recovered.Status != "interrupted" || recovered.ReconciledFrom != "failed" || recovered.ResultProfile == nil || recovered.ResultProfile.External["dpkg-bootstrap-contract"] != dpkgBootstrapRevision {
		t.Fatal("recovery lost original evidence or claimed a known execution outcome")
	}
	j, err := read()
	if err != nil || len(j.Invocations) != 0 {
		t.Fatal("immutable receipt did not retire only private consumed authority", err)
	}
	t.Logf("preserved private historical reconciliation evidence: %s", root)
}

func TestVMHistoricalV1ComposerAndCertbotObservations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	release := historicalRecoveryVMLocks(t, ctx)
	defer release()
	const composer = "/usr/local/bin/composer"
	if _, err := os.Lstat(composer); os.IsNotExist(err) {
		image, err := readRegular("/opt/fluxo-cron-audit-20261029-r4/composer.fixture", 8<<20)
		if err != nil || digest(image) != releaseinfo.ComposerSHA256 {
			t.Fatal("retained authenticated Composer fixture required", err)
		}
		f, err := os.OpenFile(composer, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := f.Write(image)
		info, statErr := f.Stat()
		closeErr := f.Close()
		if writeErr != nil || statErr != nil || closeErr != nil {
			t.Fatal(writeErr, statErr, closeErr)
		}
		t.Cleanup(func() {
			now, err := os.Lstat(composer)
			data, readErr := readRegular(composer, 8<<20)
			if err != nil || readErr != nil || !os.SameFile(info, now) || digest(data) != releaseinfo.ComposerSHA256 {
				t.Error("Composer fixture changed; retained for investigation")
				return
			}
			if err = os.Remove(composer); err != nil {
				t.Error(err)
			}
		})
	}
	for i, command := range []string{ComposerUpdateCommand, CertificateRenewalCommand} {
		c, _ := Qualify(991110+i, 0, "0 0 1 1 *", command, "root")
		current, err := qualifyContractProfile(ctx, c)
		if err != nil {
			t.Fatal("full current closure", c.Kind, err)
		}
		before, err := legacyV1Observation(ctx, c.Kind)
		if err != nil {
			t.Fatal("baseline producer", c.Kind, err)
		}
		if compatibleProfiles(before, current, false, false) {
			t.Fatal("fixture did not reproduce additive evidence change", c.Kind)
		}
		if _, err = qualifyRecoveryProfile(ctx, c, before, false); err != nil {
			t.Fatal("unchanged baseline root observations", c.Kind, err)
		}
		if c.Kind == ComposerUpdate {
			before.Files[composer] = strings.Repeat("0", 64)
			if _, err = qualifyRecoveryProfile(ctx, c, before, false); err == nil {
				t.Fatal("changed Composer executable accepted")
			}
		}
		t.Log("baseline root observations recovered with full current evidence", c.Kind)
	}
}
