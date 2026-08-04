package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/server"
	backupservice "fluxo/internal/services/backup"
	"fluxo/internal/services/bootstrap"
	"fluxo/internal/services/deploy"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/nodetoolchain"
)

var version = "dev"

func ensureNginxUnknownHostGuard() {
	if err := nginx.EnsureDefaultServer(context.Background()); err == nil {
		return
	} else {
		log.Printf("Warning: failed to install Nginx unknown-host guard: %v", err)
	}

	go func() {
		delay := time.Minute
		for {
			timer := time.NewTimer(delay)
			<-timer.C
			if err := nginx.EnsureDefaultServer(context.Background()); err == nil {
				log.Println("Nginx unknown-host guard installed successfully.")
				return
			} else {
				log.Printf("Warning: Nginx unknown-host guard retry failed: %v", err)
			}
			if delay < 30*time.Minute {
				delay *= 2
				if delay > 30*time.Minute {
					delay = 30 * time.Minute
				}
			}
		}
	}()
}

// main initializes the Fluxo daemon: database, encryption, admin token, and HTTP server.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "node-toolchain" {
		if len(os.Args) != 3 || os.Args[2] != "install" {
			fmt.Fprintln(os.Stderr, "Usage: fluxo node-toolchain install")
			os.Exit(2)
		}
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "Node.js toolchain installation must run as root")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		status, err := nodetoolchain.Install(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Node.js toolchain installation failed:", err)
			os.Exit(1)
		}
		fmt.Printf("Node.js toolchain ready: Node.js %s, npm %s, pnpm %s, Yarn %s, Corepack %s, Bun %s\n",
			status.Version, status.NPM, status.PNPM, status.Yarn, status.Corepack, status.Bun)
		return
	}

	resetToken := flag.Bool("reset-token", false, "Reset the admin user's token and output a new one")
	showVersion := flag.Bool("version", false, "Print version and exit")
	supportsNodeToolchain := flag.Bool("supports-node-toolchain", false, "Report support for managed Node.js toolchains")
	flag.Parse()

	if *supportsNodeToolchain {
		fmt.Println("supported")
		return
	}

	if *showVersion {
		fmt.Println("fluxo version", version)
		return
	}

	server.Version = version

	log.Println("Starting Fluxo daemon...")

	cfg := config.LoadConfig()

	// Use prod database if it exists when resetting token in dev mode.
	if *resetToken && cfg.Env != "prod" {
		if _, err := os.Stat("/var/lib/fluxo/fluxo.db"); err == nil {
			cfg.DBPath = "/var/lib/fluxo/fluxo.db"
			cfg.DataDir = "/var/lib/fluxo"
			cfg.Env = "prod"
			log.Println("Detected production database at /var/lib/fluxo/fluxo.db")
		}
	}

	err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	log.Println("Database initialized successfully.")
	if err := config.InitEncryption(cfg.DataDir); err != nil {
		log.Fatalf("Encryption initialization failed: %v", err)
	}

	// Encrypt existing secrets before any background worker can read them.
	database.EncryptExistingSecrets()

	// Clean up any deployments left in 'running' state on startup
	if _, err := database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Deployment was interrupted by a server restart.' WHERE status = 'running'"); err != nil {
		log.Printf("Warning: failed to clean up running deployments: %v", err)
	}

	// Resume any queued deployments from before the restart
	rows, err := database.DB.Query(`SELECT DISTINCT d.site_id FROM deployments d
		JOIN sites s ON s.id = d.site_id
		WHERE d.status = 'pending' AND COALESCE(s.deletion_status, '') = ''`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var siteID int
			if err := rows.Scan(&siteID); err == nil {
				log.Printf("Resuming queued deployments for site %d", siteID)
				deploy.Enqueue(siteID)
			}
		}
	}

	if *resetToken {
		bootstrap.ResetAdminToken(cfg.DataDir, cfg.Env == "prod")
		return
	}

	bootstrap.InitAdminToken(cfg.DataDir, cfg.Env == "prod")
	bootstrap.InitFluxoUser(cfg.DataDir)
	ensureNginxUnknownHostGuard()
	if err := server.MigrateLegacyUnconfiguredHTTPSConfigs(); err != nil {
		log.Printf("Warning: failed to migrate one or more fallback HTTPS site configs: %v", err)
	}

	// pprof debugging server bound to loopback only — not exposed externally.
	go func() {
		log.Println("Starting pprof server on 127.0.0.1:6060")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof failed: %v\n", err)
		}
	}()

	backupManager := backupservice.NewManager(cfg.DataDir)
	backupManager.Start(context.Background())
	srv := server.NewServer(backupManager, cfg.DataDir, cfg.Env == "prod")
	srv.Start(context.Background())

	// Start SQLite daily backup in background (prod only).
	if cfg.Env == "prod" {
		go database.BackupLoop(cfg.DBPath, cfg.DataDir)
	}

	port := ":" + cfg.Port

	if os.Getenv("FLUXO_USE_HTTP") == "1" {
		log.Printf("Listening on http://0.0.0.0%s (FLUXO_USE_HTTP=1)\n", port)
		httpServer := &http.Server{
			Addr:              port,
			Handler:           srv,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
		return
	}

	tlsConfig, err := config.LoadOrGenerateTLS(cfg.DataDir)
	if err != nil {
		log.Fatalf("TLS initialization failed: %v", err)
	}

	httpsServer := &http.Server{
		Addr:              port,
		Handler:           srv,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Listening on https://0.0.0.0%s\n", port)
	if err := httpsServer.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
