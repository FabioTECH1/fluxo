package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func installerRetirementFixture() CronBackendRetirement {
	saved := legacyHost{Boot: "083c861b-86fb-405d-9484-09eada7ce31c", PID: 758, Start: "600", GroupInode: 2818, Backend: "cronie-noble-1", Group: "/system.slice/cronie.service", Configuration: strings.Repeat("a", 64)}
	current := saved
	current.PID, current.Start, current.GroupInode = 961231, "900", 65668
	return CronBackendRetirement{Saved: saved, Current: current}
}

func TestInstallerRetirementRequiresBothOldAuthoritiesGoneAndCurrentDomainEmpty(t *testing.T) {
	for _, name := range []string{"retired", "pid-reused", "old-daemon", "uncertain-pid", "old-domain", "uncertain-domain", "active-child", "changed-generation"} {
		t.Run(name, func(t *testing.T) {
			r := installerRetirementFixture()
			n, domains, empty := 0, 0, 0
			err := verifyInstallerHistoricalRetirement(r, func() (legacyHost, error) {
				n++
				now := r.Current
				if name == "changed-generation" && n > 1 {
					now.GroupInode++
				}
				return now, nil
			}, func() error {
				empty++
				if name == "active-child" {
					return errors.New("pids.current=2")
				}
				return nil
			}, func(int) (string, error) {
				switch name {
				case "old-daemon":
					return r.Saved.Start, nil
				case "uncertain-pid":
					return "", os.ErrPermission
				case "pid-reused":
					return "701", nil
				}
				return "", os.ErrNotExist
			}, func(uint64) error {
				domains++
				if name == "old-domain" || name == "uncertain-domain" {
					return errors.New("old hierarchy remains or cannot be observed")
				}
				return nil
			})
			want := name == "retired" || name == "pid-reused"
			if (err == nil) != want {
				t.Fatalf("retirement=%v, want accepted=%v", err, want)
			}
			if want && (n != 2 || domains != 1 || empty != 1) {
				t.Fatal("incomplete observations")
			}
		})
	}
}

func TestInstallerRetirementRefusesContractChanges(t *testing.T) {
	for _, name := range []string{"new-boot", "provider", "configuration", "group", "unchanged-domain", "unchanged-process", "invalid"} {
		t.Run(name, func(t *testing.T) {
			r := installerRetirementFixture()
			switch name {
			case "new-boot":
				r.Current.Boot = "183c861b-86fb-405d-9484-09eada7ce31c"
			case "provider":
				r.Current.Backend = ""
			case "configuration":
				r.Current.Configuration = strings.Repeat("b", 64)
			case "group":
				r.Current.Group = "/system.slice/cron.service"
			case "unchanged-domain":
				r.Current.GroupInode = r.Saved.GroupInode
			case "unchanged-process":
				r.Current.PID, r.Current.Start = r.Saved.PID, r.Saved.Start
			case "invalid":
				r.Saved.PID = 0
			}
			if ValidateInstallerHistoricalRetirement(r) == nil {
				t.Fatal("unreviewed contract admitted")
			}
		})
	}
}

func TestRetirementSearchFindsHistoricalDomainOutsideCurrentPath(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "unrelated.slice", "retained-child")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	inode := info.Sys().(*syscall.Stat_t).Ino
	if scanRetiredHierarchy(context.Background(), root, inode) == nil {
		t.Fatal("old nested hierarchy overlooked")
	}
	if err = os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err = scanRetiredHierarchy(context.Background(), root, inode); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if scanRetiredHierarchy(ctx, root, inode) == nil {
		t.Fatal("cancelled observation admitted")
	}
	if scanRetiredHierarchy(context.Background(), filepath.Join(root, "missing"), inode) == nil {
		t.Fatal("inaccessible hierarchy treated as retired")
	}
}
