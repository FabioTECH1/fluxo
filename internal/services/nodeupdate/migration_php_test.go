package nodeupdate

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
)

func migrationTestFile(t *testing.T, path string) nodetoolchain.LegacyFile {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	return nodetoolchain.LegacyFile{Path: path, Device: uint64(st.Dev), Inode: st.Ino, Mode: uint32(info.Mode())}
}

func TestMigrationPreparationUsesOriginalAndTargetExecutableDescriptors(t *testing.T) {
	source := nodetoolchain.LegacySource{Node: nodetoolchain.LegacyFile{Path: "/usr/bin/node", Inode: 42}}
	g := Generation{Plan: Plan{Migration: &source, After: nodetoolchain.Release{Version: "22.22.3"}}}
	for _, goal := range []string{"before", "after"} {
		g.Goal = goal
		set := g.beforeDrainExecutables()
		if len(set) != 2 || set[0].Path != "/usr/bin/node" || set[0].Legacy == nil || set[0].Legacy.Inode != 42 || set[1].Path != filepath.Join(nodetoolchain.NodeRoot(), "v22.22.3/bin/node") || set[1].Legacy != nil {
			t.Fatalf("migration %s lost an exact pre-drain identity: %+v", goal, set)
		}
	}
	g.Plan.Migration = nil
	g.Plan.Before.Root = filepath.Join(nodetoolchain.NodeRoot(), "v22.22.2")
	set := g.beforeDrainExecutables()
	if set[0].Path != filepath.Join(g.Plan.Before.Root, "bin/node") || set[0].Legacy != nil || set[1].Path != filepath.Join(nodetoolchain.NodeRoot(), "v22.22.3/bin/node") {
		t.Fatal("ordinary official generation changed", set)
	}
}

// Exercise the actual /proc callback with a real Node process. This is child
// identity coverage, not qualification of this host's package provenance.
func TestMigrationPHPChildIdentityStillRejectsForeignPathsAndReusedKeys(t *testing.T) {
	if _, err := os.Stat("/usr/bin/node"); os.IsNotExist(err) {
		t.Skip("requires a host Node executable at the legacy path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func(path string) (int, string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, path, "-e", "setInterval(()=>{},1000)")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		key, err := processKey(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		return cmd.Process.Pid, key
	}
	source := nodetoolchain.LegacySource{Node: migrationTestFile(t, "/usr/bin/node")}
	g := Generation{Plan: Plan{Migration: &source, After: nodetoolchain.Release{Version: "22.22.3"}}}
	accept := phpNodeProcessAt(g.beforeDrainExecutables())
	pid, key := start(source.Node.Path)
	u := phpupdate.MaintenanceRuntime{MainPID: os.Getpid(), PIDs: map[int]string{pid: key}}
	if ok, err := accept(u, pid); err != nil || !ok {
		t.Fatal("authentic legacy child rejected", ok, err)
	}
	u.PIDs[pid] = key + "-wrong-boot-or-start"
	if _, err := accept(u, pid); err == nil {
		t.Fatal("changed boot/start key accepted")
	}
	u.PIDs[pid] = key
	g.Plan.Migration.Node.Inode++
	if _, err := phpNodeProcessAt(g.beforeDrainExecutables())(u, pid); err == nil {
		t.Fatal("changed legacy file identity accepted")
	}
	g.Plan.Migration.Node.Inode--
	foreign := filepath.Join(t.TempDir(), "node")
	in, err := os.Open(source.Node.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(foreign, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	pid, key = start(foreign)
	u.PIDs = map[int]string{pid: key}
	if _, err := accept(u, pid); err == nil {
		t.Fatal("same-version foreign Node child accepted")
	}
}
