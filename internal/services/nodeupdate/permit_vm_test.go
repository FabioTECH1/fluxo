package nodeupdate

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

// No service is created or started: exercise actual release, /run permits and
// persistent admission in a private DB on the explicitly selected test host.
func TestVMNodeReleaseStartPermits(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root reusable acceptance VM only")
	}
	m := testManager(t)
	u := Unit{ID: 209201, Name: "fluxo-daemon-209201.service"}
	barrierDir := filepath.Dir(barrierPath(u))
	for _, p := range []string{barrierDir, filepath.Join("/etc/systemd/system", u.Name)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatal("preserve existing fixture", p, err)
		}
	}
	if err := durableDirectory(barrierDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(barrierPath(u))
		_ = os.Remove(barrierDir)
	})
	for _, condition := range []string{"held", "already released", "missing permit", "missing generation directory", "receipt deletion failure", "foreign content", "foreign row", "foreign barrier", "symlink", "hardlink", "writable", "foreign uid", "fifo", "symlink directory", "writable directory", "foreign directory uid"} {
		t.Run(condition, func(t *testing.T) {
			id := uuid.NewString()
			g := Generation{ID: id, Plan: Plan{Impact: Impact{Units: []Unit{u}}}}
			p := permitPath(u, id)
			dir := filepath.Dir(p)
			if err := durableDirectory(dir, 0755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = os.RemoveAll(dir) // unique UUID directory created by this case
				_ = os.Remove(barrierPath(u))
				_, _ = database.DB.Exec("DELETE FROM node_update_barriers WHERE unit=?", u.Name)
			})
			if err := durableWrite(p, []byte(id+"\n"), 0644, true); err != nil {
				t.Fatal(err)
			}
			if condition != "already released" {
				if err := durableWrite(barrierPath(u), []byte(barrierContent(u, id)), 0644, true); err != nil {
					t.Fatal(err)
				}
				if _, err := database.DB.Exec("INSERT INTO node_update_barriers(unit,generation_id) VALUES(?,?)", u.Name, id); err != nil {
					t.Fatal(err)
				}
			}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch condition {
			case "receipt deletion failure":
				_, err := database.DB.Exec("CREATE TRIGGER reject_permit_release BEFORE DELETE ON node_update_barriers BEGIN SELECT RAISE(ABORT,'release fixture'); END")
				must(err)
				t.Cleanup(func() { _, _ = database.DB.Exec("DROP TRIGGER IF EXISTS reject_permit_release") })
			case "missing permit":
				must(os.Remove(p))
			case "missing generation directory":
				must(os.Remove(p))
				must(os.Remove(dir))
			case "foreign content":
				must(os.WriteFile(p, []byte(uuid.NewString()+"\n"), 0644))
			case "foreign row":
				_, err := database.DB.Exec("UPDATE node_update_barriers SET generation_id=? WHERE unit=?", uuid.NewString(), u.Name)
				must(err)
			case "foreign barrier":
				must(os.WriteFile(barrierPath(u), []byte(barrierContent(u, uuid.NewString())), 0644))
			case "symlink", "hardlink":
				other := filepath.Join(dir, "foreign")
				must(os.Rename(p, other))
				if condition == "symlink" {
					must(os.Symlink(other, p))
				} else {
					must(os.Link(other, p))
				}
			case "writable":
				must(os.Chmod(p, 0666))
			case "foreign uid":
				must(os.Chown(p, 65534, 65534))
			case "fifo":
				must(os.Remove(p))
				must(syscall.Mkfifo(p, 0600))
			case "symlink directory":
				other := dir + ".saved"
				if _, err := os.Lstat(other); !os.IsNotExist(err) {
					t.Fatal("preserve existing directory", other, err)
				}
				must(os.Rename(dir, other))
				t.Cleanup(func() { _ = os.RemoveAll(other) })
				must(os.Symlink(other, dir))
			case "writable directory":
				must(os.Chmod(dir, 0777))
			case "foreign directory uid":
				must(os.Chown(dir, 65534, 65534))
			}
			safe := condition == "held" || condition == "already released" || condition == "missing permit" || condition == "missing generation directory"
			err := m.release(context.Background(), g)
			if condition == "receipt deletion failure" {
				if err == nil {
					t.Fatal("journal failure hidden")
				}
				if pending, e := database.NodeMaintenancePending(); e != nil || !pending {
					t.Fatal("interrupted release lost admission", pending, e)
				}
				for _, path := range []string{p, barrierPath(u)} {
					if _, e := os.Lstat(path); !os.IsNotExist(e) {
						t.Fatal("interruption fixture did not reach receipt deletion", path, e)
					}
				}
				_, err = database.DB.Exec("DROP TRIGGER reject_permit_release")
				must(err)
				err = m.release(context.Background(), g)
				safe = true
			}
			if !safe {
				if err == nil {
					t.Fatal("unsafe permit release accepted")
				}
				if _, err := os.Lstat(p); err != nil {
					t.Fatal("rejected permit removed", err)
				}
				if _, err := os.Lstat(barrierPath(u)); err != nil {
					t.Fatal("rejected barrier removed", err)
				}
				var rows int
				must(database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers WHERE unit=?", u.Name).Scan(&rows))
				if rows != 1 {
					t.Fatal("cleanup failure lost durable admission")
				}
				return
			}
			must(err)
			for _, path := range []string{p, barrierPath(u)} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("release left a start exception/barrier", path, err)
				}
			}
			var rows int
			must(database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers WHERE unit=?", u.Name).Scan(&rows))
			if rows != 0 {
				t.Fatal("release left durable barrier")
			}
			must(m.release(context.Background(), g)) // interrupted release/retry is harmless
		})
	}
}
