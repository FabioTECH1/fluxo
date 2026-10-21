package server

// This fixture is also compiled against the immutable v0.4.35 source in the
// guarded VM acceptance procedure, exercising that release's real Run now guard.
import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/database"
)

func init() {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		if b, err := os.ReadFile("/var/lib/fluxo-legacy-panel-test-version"); err == nil {
			fmt.Println("fluxo version " + strings.TrimSpace(string(b)))
			os.Exit(0)
		}
	}
	root := os.Getenv("FLUXO_TEST_LEGACY_PANEL_DIR")
	if root == "" {
		return
	}
	if err := database.InitDB(filepath.Join(root, "fluxo.db")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := NewServer(nil, root, true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /api/v1/sites/{id}/crons/{cron_id}/run", s.handleRunCron())
	if err := http.ListenAndServe("127.0.0.1:19695", s.phpMaintenanceGuard(mux)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
