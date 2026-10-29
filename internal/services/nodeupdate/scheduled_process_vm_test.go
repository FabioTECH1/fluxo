package nodeupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "system-task-") {
		var err error
		ctx := context.Background()
		switch os.Args[1] {
		case "system-task-execute":
			if len(os.Args) != 3 {
				os.Exit(2)
			}
			err = maintenancetask.Execute(ctx, os.Args[2])
		case "system-task-application":
			if len(os.Args) != 3 {
				os.Exit(2)
			}
			err = maintenancetask.ExecuteApplication(ctx, os.Args[2])
		case "system-task-dispatch", "system-task-receipt":
			if len(os.Args) != 4 {
				os.Exit(2)
			}
			id, e := strconv.Atoi(os.Args[2])
			if e != nil {
				os.Exit(2)
			}
			if os.Args[1] == "system-task-receipt" {
				err = maintenancetask.ReceiveReceipt(ctx, id, os.Args[3])
			} else {
				_, err = maintenancetask.Dispatch(ctx, id, os.Args[3])
			}
		default:
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVMNodeInventoryAuthenticatesScheduledChild(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded local VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	const dbPath = "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	const marker = "/home/fluxo/cron-admission-20261029/node-boundary.json"
	if _, e := os.Lstat(marker); !os.IsNotExist(e) {
		t.Fatal("fresh marker required")
	}
	command := `/usr/local/bin/node -e 'const fs=require("fs");fs.writeFileSync("/home/fluxo/cron-admission-20261029/node-boundary.json",JSON.stringify({pid:process.pid,uid:process.getuid(),systemManagerIsSocket:fs.statSync("/run/systemd/private").isSocket(),systemBusIsSocket:fs.statSync("/run/dbus/system_bus_socket").isSocket()}));setTimeout(()=>{},18000)'`
	c, ok := maintenancetask.QualifyApplication(211930, 0, "", "0 0 1 1 *", command, "fluxo")
	if !ok {
		t.Fatal("contract")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
		t.Fatal(err)
	}
	if err = maintenancetask.Materialize(ctx, c, dbPath, true); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = maintenancetask.Inspect(ctx, c, dbPath)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	if _, err = maintenancetask.RunNow(ctx, c, dbPath); err != nil {
		t.Fatal(err)
	}
	var child struct {
		PID     int    `json:"pid"`
		UID     uint32 `json:"uid"`
		Manager bool   `json:"systemManagerIsSocket"`
		Bus     bool   `json:"systemBusIsSocket"`
	}
	for {
		raw, e := os.ReadFile(marker)
		if e == nil && json.Unmarshal(raw, &child) == nil && child.PID > 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Node child start timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		t.Fatal(err)
	}
	if child.UID != credential.Uid || child.UID == 0 || child.Manager || child.Bus {
		t.Fatal("application privilege/socket boundary", child)
	}
	key, err := processKey(child.PID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := nodeProcessGroup(child.PID)
	if err != nil {
		t.Fatal(err)
	}
	if handled, e := maintenancetask.AuthenticateNodeProcess(ctx, child.PID, key, group); !handled || e != nil {
		t.Fatal("authentic child rejected", handled, e)
	}
	if _, e := maintenancetask.AuthenticateNodeProcess(ctx, child.PID, key+"wrong", group); e == nil {
		t.Fatal("stale PID identity accepted")
	}
	unit := strings.TrimPrefix(group, "/system.slice/")
	raw, err := syscmd.Run(ctx, time.Second, "/usr/bin/systemctl", "show", unit, "--property=MainPID", "--value")
	if err != nil {
		t.Fatal(err)
	}
	guard, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		t.Fatal(err)
	}
	guardKey, err := processKey(guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, e := maintenancetask.AuthenticateNodeProcess(ctx, guard, guardKey, group); e == nil {
		t.Fatal("root guard inherited a child's exemption")
	}
	selected, err := filepath.EvalSymlinks("/usr/local/bin/node")
	if err != nil {
		t.Fatal(err)
	}
	roots := []nodeExecutable{{Path: selected}}
	matches := func(impact Impact) bool {
		prefix := "Node.js process " + strconv.Itoa(child.PID)
		for _, reason := range impact.BlockedReasons {
			if strings.HasPrefix(reason, prefix+" ") || strings.HasPrefix(reason, prefix+":") {
				return true
			}
		}
		return false
	}
	impact := Impact{}
	if err = inspectNodeProcessInventoryAt(ctx, roots, "", false, &impact); err != nil || matches(impact) {
		t.Fatal("production global scan rejected guarded child", impact.BlockedReasons, err)
	}
	// Other original VM apps are deliberately absent from this narrow fixture's
	// consumer inventory. Their blockers must remain; no global bypass is added.
	if len(impact.BlockedReasons) == 0 {
		t.Fatal("unrelated unmanaged Node processes were exempted")
	}
	wrong := Impact{}
	if err = inspectNodeProcessInventoryAt(ctx, []nodeExecutable{{Path: "/opt/fluxo/node/v24.22.1/bin/node"}}, "", false, &wrong); err != nil || !matches(wrong) {
		t.Fatal("wrong runtime was exempted", wrong.BlockedReasons, err)
	}
	lockctx, done := context.WithTimeout(ctx, 500*time.Millisecond)
	release, e := packages.Lifecycle(lockctx, false)
	done()
	if e == nil {
		release()
		t.Fatal("runtime switch could pass live scheduled Node child")
	}
	t.Log("production /proc scan accepted exact scheduled Node child, retained unrelated/wrong-runtime blockers; UID and manager-socket mask verified")
	for {
		_, e := os.Stat(fmt.Sprintf("/proc/%d", child.PID))
		if os.IsNotExist(e) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("natural Node exit timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	release, err = packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenancetask.Quiescent(ctx)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if err = maintenancetask.Retire(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	t.Log("natural scheduled Node completion admitted exclusive maintenance; no actual runtime switch in this scoped test")
}
