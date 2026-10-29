package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
	"github.com/golang-jwt/jwt/v5"
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

func TestVMManagedCronAuthenticatedAPI(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded local VM only")
	}
	const root = "/var/lib/fluxo-cron-audit-api-20261029-data"
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("fresh private DB required", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "fluxo.db")
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	scheduler.SetDatabasePath(path)
	if _, err := database.DB.Exec("INSERT INTO users(username,token_hash) VALUES('cron-vm','private-test-only-secret'); INSERT INTO sqlite_sequence(name,seq) VALUES('crons',211902)"); err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "cron-vm", "ver": 0, "exp": time.Now().Add(5 * time.Minute).Unix()}).SignedString([]byte("private-test-only-secret"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, root, true) // deliberately do not start panel/background managers
	request := func(method, url, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/v1/crons", "", false); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	const marker = "/home/fluxo/cron-admission-20261029/api-events"
	body := `{"name":"My independent future job","expression":"0 0 1 1 *","command":"/bin/echo api-run >> /home/fluxo/cron-admission-20261029/api-events","user":"fluxo"}`
	w := request("POST", "/api/v1/crons", body, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		ID int `json:"id"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID != 211903 {
		t.Fatal(created, err)
	}
	c, ok := maintenancetask.QualifyApplication(created.ID, 0, "", "0 0 1 1 *", "/bin/echo api-run >> "+marker, "fluxo")
	if !ok {
		t.Fatal("contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	for {
		_, err = maintenancetask.Inspect(ctx, c, path)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	receipts, reasons, err := maintenancetask.InspectDatabase(ctx, database.DB)
	if err != nil || len(reasons) != 0 || len(receipts) != 1 {
		t.Fatal(receipts, reasons, err)
	}
	t.Log("actual JWT create/list and shared PHP/Node cron inventory accept manually named command")
	if w = request("GET", "/api/v1/crons", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"configuration_state":"configured"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = database.DB.Exec("INSERT INTO node_update_php_barriers(unit,generation_id,payload) VALUES('own-api-barrier','own-api','{}')"); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", "/api/v1/crons/211903/run", "", true); w.Code != 409 {
		t.Fatal("pending maintenance bypassed", w.Code, w.Body.String())
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("API pending barrier launched job", err)
	}
	if _, err = database.DB.Exec("DELETE FROM node_update_php_barriers WHERE unit='own-api-barrier'"); err != nil {
		t.Fatal(err)
	}
	if w = request("POST", "/api/v1/crons/211903/run", "", true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for {
		b, _ := os.ReadFile(marker)
		if string(b) == "api-run\n" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Run Now did not finish")
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Let the durable completion commit before retiring the trigger.
	for {
		err = maintenancetask.Quiescent(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if w = request("DELETE", "/api/v1/crons/211903", "", true); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = os.Lstat("/etc/cron.d/fluxo-cron-211903"); !os.IsNotExist(err) {
		t.Fatal("trigger not retired", err)
	}
	t.Log("actual JWT Run Now/409 hold admission/delete passed through retained production dispatcher; no full installed panel-main claim")
}
