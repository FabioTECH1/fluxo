package nodeupdate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// Real systemd/PHP preparation only: the separate migration acceptance fixture
// must already provide the legacy Node and a staged official same-major target.
// This test never switches global runtimes or qualifies package provenance.
func TestVMNodeMigrationPHPPreparation(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root reusable acceptance VM only")
	}
	version := os.Getenv("FLUXO_NODE_MIGRATION_TEST_TARGET")
	if version == "" || filepath.Base(version) != version || strings.ContainsAny(version, `/\\`) {
		t.Skip("requires the explicit staged same-major migration target")
	}
	target := filepath.Join(nodetoolchain.NodeRoot(), "v"+version, "bin/node")
	for _, path := range []string{"/usr/bin/node", "/usr/bin/php8.4", target} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing prepared migration fixture %s: %v", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := func(binary string, args ...string) string {
		t.Helper()
		out, err := syscmd.Run(ctx, 10*time.Second, binary, args...)
		if err != nil {
			t.Fatalf("%s: %v", binary, err)
		}
		return out
	}
	versionOut, err := syscmd.RunApplicationProbe(ctx, 10*time.Second, "fluxo", "/home/fluxo", "/usr/bin/node", "--version")
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := strings.TrimPrefix(strings.TrimSpace(versionOut), "v")
	if err := nodetoolchain.ValidateMigrationVersion(oldVersion, version); err != nil {
		t.Fatal("prepare the actual same-major fixture before running this test", err)
	}
	m := testManager(t)
	name := "fluxo-daemon-209101.service"
	unit := filepath.Join("/etc/systemd/system", name)
	for _, path := range []string{unit, unit + ".d"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("preserve existing fixture %s", path)
		}
	}
	dir, err := os.MkdirTemp("/home/fluxo", "node-migration-php-preparation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", name)
		_ = os.Remove(unit)
		_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
		_ = os.RemoveAll(dir)
	})
	program := `<?php
$binary=trim(file_get_contents(__DIR__.'/node-path'));
$job=proc_open([$binary,'-e','setInterval(()=>{},1000)'],[0=>['file','/dev/null','r'],1=>['file','/dev/null','w'],2=>['file','/dev/null','w']],$pipes);
file_put_contents(__DIR__.'/ready','ready');proc_close($job);
`
	if err = os.WriteFile(filepath.Join(dir, "artisan"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,php_version) VALUES(209100,'migration-preparation.test',?,'laravel','8.4')", dir); err != nil {
		t.Fatal(err)
	}
	command := "php8.4 artisan reports:generate --format=json"
	if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances) VALUES(209101,209100,'Independent report','',?,?,'fluxo',1)", command, dir); err != nil {
		t.Fatal(err)
	}
	content := "[Unit]\nDescription=Fluxo migration PHP preparation fixture\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + dir + "\nExecStart=/usr/bin/" + command + "\nRestart=always\nRestartSec=1\nTimeoutStopSec=2\nKillSignal=SIGTERM\n[Install]\nWantedBy=multi-user.target\n"
	if err = os.WriteFile(unit, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/chown", "-R", "fluxo:fluxo", dir)
	run("/usr/bin/systemctl", "daemon-reload")
	start := func(binary string) {
		t.Helper()
		run("/usr/bin/systemctl", "stop", name)
		_ = os.Remove(filepath.Join(dir, "ready"))
		if err := os.WriteFile(filepath.Join(dir, "node-path"), []byte(binary), 0644); err != nil {
			t.Fatal(err)
		}
		run("/usr/bin/systemctl", "start", name)
		until := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
				return
			}
			if time.Now().After(until) {
				t.Fatal("PHP child did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	source := nodetoolchain.LegacySource{Version: oldVersion, Node: migrationTestFile(t, "/usr/bin/node")}
	g := Generation{ID: uuid.NewString(), State: "prepared", Plan: Plan{Migration: &source, After: nodetoolchain.Release{Version: version}}}
	g.Head = g.ID
	start(source.Node.Path)
	set, reasons, err := inspectPHPConsumersAt(ctx, g.beforeDrainExecutables(), "", nil, nil)
	if err != nil || len(reasons) > 0 || len(set) != 1 {
		t.Fatal("fixture inspection", err, reasons, len(set))
	}
	g.Plan.Impact = Impact{PHPPolicy: 1, PHP: set}
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	for _, binary := range []string{source.Node.Path, target} {
		start(binary)
		if err = m.preparePHPState(ctx, &g); err != nil {
			t.Fatalf("real preparation rejected %s with empty Before: %v", binary, err)
		}
		retained := false
		for pid, key := range g.PHPState.Attempt[name].PIDs {
			path, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
			if path == binary {
				actual, e := processKey(pid)
				retained = e == nil && actual == key
			}
		}
		if !retained {
			t.Fatal("prepared child boot/start identity not retained", binary)
		}
	}
	// A byte-identical executable at an unreviewed path remains foreign.
	foreignDir := filepath.Join(dir, "foreign")
	if err = os.Mkdir(foreignDir, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(foreignDir, "node")
	in, err := os.Open(source.Node.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(foreign, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	previous := digest(g.PHPState.Attempt)
	start(foreign)
	if err = m.preparePHPState(ctx, &g); err == nil || !strings.Contains(err.Error(), "unauthenticated Node.js child") {
		t.Fatal("foreign same-version child passed preparation", err)
	}
	if digest(g.PHPState.Attempt) != previous {
		t.Fatal("failed preparation discarded historical drain evidence")
	}
}
