package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"

	"fluxo/config"
	"fluxo/database"
	"fluxo/server"
	"fluxo/services/bootstrap"
)

// main is the entrypoint for the Fluxo daemon. Startup sequence:
// 1. Load config from environment variables
// 2. Initialize SQLite database (schema + migrations)
// 3. Bootstrap admin credentials (day-zero auth)
// 4. Bootstrap the fluxo system user
// 5. Start pprof debug server on localhost:6060 (background)
// 6. Start the HTTP server (foreground, blocks)
func main() {
	log.Println("Starting Fluxo daemon...")

	cfg := config.LoadConfig()

	err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	log.Println("Database initialized successfully.")

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

	port := ":" + cfg.Port
	log.Printf("Listening on %s\n", port)
	if err := http.ListenAndServe(port, srv); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
