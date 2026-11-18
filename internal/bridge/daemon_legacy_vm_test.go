package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Real loaded-unit metadata, using a private SQLite model and an unstarted,
// exclusively created fixture unit. No application payload or live row is run.
func TestVMLegacyPHPDaemonReadinessAndShadowRefusal(t *testing.T) {
	host, _ := os.Hostname()
	allowed := host == "fluxo-bridge-noble-stale-proof-20261117" || host == "fluxo-bridge-jammy-current-public-20261118" || host == "fluxo-bridge-resolute-current-public-20261118"
	if !allowed || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("named root fixture and explicit opt-in required")
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == manager {
		t.Fatal("private mount namespace required before writes or mounts", err)
	}
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const name = "fluxo-daemon-990011.service"
	path := filepath.Join("/etc/systemd/system", name)
	out, err := run(ctx, "/usr/bin/systemctl", "show", name, "--property=LoadState")
	if err != nil || properties(out)["LoadState"] != "not-found" {
		t.Fatal("fixture service already exists", err)
	}
	floor, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("[Unit]\nDescription=Unstarted legacy PHP metadata fixture\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=/home/fluxo\nExecStart=php8.4 -v\nRestart=always\n")
	closeErr := f.Close()
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if _, err := run(context.Background(), "/usr/bin/systemctl", "daemon-reload"); err != nil {
			t.Error(err)
		}
	})
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	if _, err = run(ctx, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		t.Fatal(err)
	}
	out, err = run(ctx, "/usr/bin/systemctl", "show", name, "--property=ExecStart,MainPID")
	p := properties(out)
	if err != nil || p["MainPID"] != "0" || !strings.HasPrefix(p["ExecStart"], "{ path=php8.4 ; argv[]=php8.4 -v ; ") {
		t.Fatal("real historical-form metadata not observed", err)
	}
	if strings.Contains(p["ExecStart"], " ; argv[]=/usr/bin/php8.4 -v ; ignore_errors=") {
		t.Fatal("fixture did not reproduce the former checker mismatch")
	}
	db := fixture(t)
	if _, err = db.Exec("INSERT INTO daemons(id,site_id,command,directory,user) VALUES(990011,0,'php8.4 -v','/home/fluxo','fluxo')"); err != nil {
		t.Fatal(err)
	}
	if err = checkDaemons(ctx, db); err != nil {
		t.Fatal("valid legacy unit refused", err)
	}
	shadow := t.TempDir()
	raw, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(shadow, "php8.4"), raw, 0700); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mount(shadow, "/usr/local/bin", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	err = checkDaemons(ctx, db)
	unmountErr := unix.Unmount("/usr/local/bin", 0)
	if err == nil || !strings.Contains(err.Error(), "shadowed") || unmountErr != nil {
		t.Fatal("shadowed legacy interpreter not refused", err, unmountErr)
	}
	after, err := readProtected("/var/lib/fluxo/execution-readers.json", 64<<10)
	if err != nil || digest(after) != digest(floor) {
		t.Fatal("inspection changed the live reader floor", err)
	}
}
