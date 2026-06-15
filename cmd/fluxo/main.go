package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/server"
	"fluxo/internal/services/bootstrap"
)

var version = "dev"

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

	// When --reset-token is used in dev mode but the prod database
	// exists at /var/lib/fluxo/fluxo.db, use it instead. This prevents
	// accidentally resetting the wrong database when run from CLI
	// without FLUXO_ENV=prod.
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

	// Clean up any deployments left in 'running' state on startup
	if _, err := database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Deployment was interrupted by a server restart.' WHERE status = 'running'"); err != nil {
		log.Printf("Warning: failed to clean up running deployments: %v", err)
	}

	if err := config.InitEncryption(cfg.DataDir); err != nil {
		log.Fatalf("Encryption initialization failed: %v", err)
	}

	// Encrypt existing secrets if any
	database.EncryptExistingSecrets()

	if *resetToken {
		bootstrap.ResetAdminToken()
		return
	}

	bootstrap.InitAdminToken()
	bootstrap.InitFluxoUser()

	// pprof debugging server bound to loopback only — not exposed externally.
	go func() {
		log.Println("Starting pprof server on 127.0.0.1:6060")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof failed: %v\n", err)
		}
	}()

	srv := server.NewServer()

	// Start SQLite daily backup in background (prod only).
	if cfg.Env == "prod" {
		go database.BackupLoop(cfg.DBPath, cfg.DataDir)
	}

	port := ":" + cfg.Port

	if os.Getenv("FLUXO_USE_HTTP") == "1" {
		log.Printf("Listening on http://0.0.0.0%s (FLUXO_USE_HTTP=1)\n", port)
		if err := http.ListenAndServe(port, srv); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
		return
	}

	tlsConfig, err := config.LoadOrGenerateTLS(cfg.DataDir)
	if err != nil {
		log.Fatalf("TLS initialization failed: %v", err)
	}

	httpsServer := &http.Server{
		Addr:      port,
		Handler:   srv,
		TLSConfig: tlsConfig,
	}

	log.Printf("Listening on https://0.0.0.0%s\n", port)
	if err := httpsServer.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
