package main

// Test-only live panel for the guarded VM acceptance script. It runs the real
// CLI in this same executable, so binary-binding checks remain meaningful,
// without invoking production startup provisioning or touching hosted sites.
import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
)

func init() {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		return
	}
	mode := os.Getenv("FLUXO_PANEL_VERIFY_FIXTURE")
	if len(os.Args) > 1 && os.Args[1] == "--scheduler-support" {
		if b, err := os.ReadFile(os.Getenv("FLUXO_PANEL_VERIFY_SUPPORT_FILE")); err == nil && string(b) == "0" {
			fmt.Println("0")
			os.Exit(0)
		}
	}
	if mode == "cli" || mode == "old" && len(os.Args) > 1 {
		main()
		os.Exit(0)
	}
	if mode != "panel" && mode != "old" {
		return
	}
	if len(os.Args) > 1 {
		main()
		os.Exit(0)
	}
	directory := os.Getenv("FLUXO_PANEL_VERIFY_APP")
	if !filepath.IsAbs(directory) {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /run", func(w http.ResponseWriter, r *http.Request) {
		release, err := packages.Lifecycle(r.Context(), true)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		defer release()
		out, err := syscmd.RunAsUserInDir(context.Background(), 45*time.Second, "fluxo", directory, "/usr/bin/php8.4", "artisan")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fmt.Fprint(w, out)
	})
	if err := http.ListenAndServe("127.0.0.1:19795", mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}
