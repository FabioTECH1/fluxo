package bridge

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This exercises the actual installed TLS service, never an httptest router.
// Only harmless assertions are retained; credentials and terminal transcripts
// remain in memory. The controller creates the canonical real Laravel fixture.
func TestVMInstalledProductionRouter(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" ||
		(host != "fluxo-bridge-jammy-final-20261117" && host != "fluxo-bridge-resolute-final-20261117" &&
			host != "fluxo-bridge-jammy-fresh-eb4-20261117" && host != "fluxo-bridge-resolute-fresh-eb4-20261117" &&
			host != "fluxo-bridge-noble-delivery-20261117") {
		t.Skip("explicit installed Ubuntu amd64 acceptance fixtures only")
	}
	expected := os.Getenv("FLUXO_BRIDGE_EXPECTED_SHA256")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(expected) {
		t.Fatal("exact expected image digest required")
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := readProtected(path, 128<<20)
		if err != nil {
			t.Fatal("protected acceptance input unavailable", path)
		}
		return b
	}
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	if sum(read("/usr/local/bin/fluxo")) != expected {
		t.Fatal("installed image differs")
	}
	var fixture struct {
		Host   string `json:"host"`
		SiteID int    `json:"site_id"`
		App    string `json:"app"`
		Image  string `json:"candidate_sha256"`
	}
	if json.Unmarshal(read("/root/fluxo-bridge-acceptance/production-fixtures.json"), &fixture) != nil || fixture.Host != host || fixture.Image != expected || fixture.SiteID < 1 {
		t.Fatal("fixture identity differs")
	}
	props, err := exec.Command("/usr/bin/systemctl", "show", "fluxo.service", "--property=MainPID", "--value").Output()
	if err != nil {
		t.Fatal("installed service unavailable")
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(props)))
	image, err := os.ReadFile(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || pid < 2 || sum(image) != expected {
		t.Fatal("running image differs")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(read("/var/lib/fluxo/tls/cert.pem")) {
		t.Fatal("invalid installed TLS certificate")
	}
	tlsConfig := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, Timeout: 20 * time.Second}
	defer client.CloseIdleConnections()
	const origin = "https://127.0.0.1:9595"
	token := ""
	request := func(method, path, requestOrigin string, body any, want int) []byte {
		t.Helper()
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal("request encoding failed")
		}
		r, err := http.NewRequest(method, origin+"/api/v1"+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal("request construction failed")
		}
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if requestOrigin != "" {
			r.Header.Set("Origin", requestOrigin)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal("validated TLS request failed")
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil || resp.StatusCode != want {
			t.Fatalf("production endpoint %s %s: status %d, expected %d", method, path, resp.StatusCode, want)
		}
		return data
	}
	db, err := sql.Open("sqlite", "file:/var/lib/fluxo/fluxo.db?mode=ro")
	if err != nil {
		t.Fatal("admin observation failed")
	}
	defer db.Close()
	var username string
	if db.QueryRow("SELECT username FROM users ORDER BY id LIMIT 1").Scan(&username) != nil || username == "__bootstrap__" {
		t.Fatal("normal claimed admin required")
	}
	secrets := regexp.MustCompile(`(?m)^Fluxo bootstrap token(?: \(reset\))?:\s*(\S+)\s*$`).FindAllSubmatch(read("/var/lib/fluxo/.fluxo_credentials"), -1)
	if len(secrets) == 0 {
		t.Fatal("retained acceptance login secret unavailable")
	}
	password := string(secrets[len(secrets)-1][1])
	var login struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(request("POST", "/auth/login", "", map[string]string{"username": username, "password": password}, 200), &login) != nil || login.Token == "" {
		t.Fatal("normal sign-in failed")
	}
	token = login.Token
	endpoint := fmt.Sprintf("/sites/%d/tinker/sessions", fixture.SiteID)
	request("POST", endpoint, "https://wrong.example.test", map[string]string{"password": password}, 403)
	var grant struct {
		ID     string `json:"session_id"`
		Ticket string `json:"ticket"`
	}
	if json.Unmarshal(request("POST", endpoint, origin, map[string]string{"password": password}, 201), &grant) != nil || grant.ID == "" || grant.Ticket == "" {
		t.Fatal("password-verified grant failed")
	}
	ended := false
	defer func() {
		if !ended {
			request("DELETE", endpoint+"/"+grant.ID, origin, nil, 204)
		}
	}()
	wsURL := "wss://127.0.0.1:9595/api/v1/tinker/ws?session=" + url.QueryEscape(grant.ID) + "&ticket=" + url.QueryEscape(grant.Ticket)
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: 15 * time.Second}
	ws, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{origin}})
	if err != nil {
		t.Fatal("validated production WSS upgrade failed")
	}
	defer ws.Close()
	until := func(marker *regexp.Regexp) []string {
		t.Helper()
		var output []byte
		ws.SetReadDeadline(time.Now().Add(25 * time.Second))
		for {
			kind, data, err := ws.ReadMessage()
			if err != nil {
				t.Fatal("production console ended before assertion")
			}
			if kind != websocket.BinaryMessage {
				continue
			}
			output = append(output, data...)
			if len(output) > 128<<10 {
				t.Fatal("unexpected bounded fixture output")
			}
			if ws.WriteJSON(map[string]any{"type": "ack", "bytes": len(data)}) != nil {
				t.Fatal("render acknowledgement failed")
			}
			if m := marker.FindStringSubmatch(string(output)); m != nil {
				return m
			}
		}
	}
	input := func(php string) {
		t.Helper()
		if ws.WriteJSON(map[string]string{"type": "input", "data": php + "\n"}) != nil {
			t.Fatal("console input failed")
		}
	}
	until(regexp.MustCompile(`> `))
	input(`$bridge_x = 41; echo "BRIDGE_"."ASSIGNED\n";`)
	until(regexp.MustCompile(`BRIDGE_ASSIGNED\r?\n`))
	input(`echo "BRIDGE_VALUE=".($bridge_x+1)."\n";`)
	until(regexp.MustCompile(`BRIDGE_VALUE=42\r?\n`))
	account, err := user.Lookup("fluxo")
	if err != nil {
		t.Fatal("application identity missing")
	}
	input(`echo "BRIDGE_IDENT=".json_encode([app()->version(),app()->environment(),PHP_BINARY,posix_geteuid(),posix_getegid()])."\n";`)
	identity := until(regexp.MustCompile(`BRIDGE_IDENT=(\[[^\r\n]+\])\r?\n`))
	var actual []any
	if json.Unmarshal([]byte(identity[1]), &actual) != nil || len(actual) != 5 || !strings.HasPrefix(actual[0].(string), "12.") || actual[1] != "production" || actual[2] != "/usr/bin/php8.4" || fmt.Sprint(actual[3]) != account.Uid || fmt.Sprint(actual[4]) != account.Gid {
		t.Fatal("Laravel/configured PHP/non-root assertion differs")
	}
	_, resp, replayErr := dialer.Dial(wsURL, http.Header{"Origin": []string{origin}})
	if replayErr == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatal("consumed ticket was not refused")
	}
	resp.Body.Close()
	request("PUT", fmt.Sprintf("/sites/%d", fixture.SiteID), origin, map[string]string{"php_version": "8.4"}, 409)
	input(`$bridge_child=pcntl_fork(); if($bridge_child===0){posix_setsid();fclose(STDIN);fclose(STDOUT);fclose(STDERR);sleep(90);exit(0);} echo "BRIDGE_PID=".$bridge_child."\n";`)
	child := until(regexp.MustCompile(`BRIDGE_PID=([1-9][0-9]*)\r?\n`))[1]
	status, err := os.ReadFile("/proc/" + child + "/status")
	if err != nil {
		t.Fatal("detached child was not actually observed")
	}
	for _, field := range []string{"CapPrm:", "CapEff:", "CapInh:", "CapAmb:"} {
		m := regexp.MustCompile(`(?m)^` + field + `\s*([0-9a-f]+)$`).FindStringSubmatch(string(status))
		if len(m) != 2 || strings.Trim(m[1], "0") != "" {
			t.Fatalf("application child capability assertion %s: %q", field, m)
		}
	}
	if !regexp.MustCompile(`(?m)^Groups:\s*$`).Match(status) {
		t.Fatal("application child retained supplementary groups")
	}
	cgroup, err := os.ReadFile("/proc/" + child + "/cgroup")
	if err != nil {
		t.Fatal("child cgroup not observed")
	}
	unit := regexp.MustCompile(`0::/system.slice/(fluxo-tinker-[0-9a-f-]+\.service)`).FindStringSubmatch(string(cgroup))
	if len(unit) != 2 {
		t.Fatal("child escaped Tinker execution domain")
	}
	request("DELETE", endpoint+"/"+grant.ID, origin, nil, 204)
	ended = true
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, childErr := os.Stat("/proc/" + child)
		events, eventsErr := os.ReadFile("/sys/fs/cgroup/system.slice/" + unit[1] + "/cgroup.events")
		if os.IsNotExist(childErr) && (os.IsNotExist(eventsErr) || eventsErr == nil && strings.Contains(string(events), "populated 0")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("observed detached child or execution domain survived disconnect")
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("PASS: installed TLS login/WSS, real Laravel state, PHP/UID, exact Origin, replay and mutation refusal, zero capabilities/groups, observed detached-child drainage; no transcript retained")
}
