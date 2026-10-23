package phpupdate

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestMaintenanceOctaneHealthRejectsHTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	_, text, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(text)
	inspected := false
	err = octaneHealthWithInspector(context.Background(), Consumer{Port: port}, unitSnapshot{}, func(unitSnapshot) error { inspected = true; return nil })
	if !inspected || err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatal("healthy process/listener bypassed HTTP verification", err)
	}
}

func TestMaintenanceRecoveryAllowsAbsentButRejectsForeignListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = verifyOptionalConsumerListener(context.Background(), unitSnapshot{PIDs: map[int]string{}}, port); err == nil {
		listener.Close()
		t.Fatal("recovery accepted an externally owned listener")
	}
	listener.Close()
	if err = verifyOptionalConsumerListener(context.Background(), unitSnapshot{PIDs: map[int]string{}}, port); err != nil {
		t.Fatal("missing listener blocked identity-based recovery", err)
	}
}
