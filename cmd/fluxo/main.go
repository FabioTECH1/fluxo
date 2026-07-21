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
)

var version = "dev"

// main initializes the Fluxo daemon: database, encryption, admin token, and HTTP server.
func main() {
	resetToken := flag.Bool("reset-token", false, "Reset the admin user's token and output a new one")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

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
