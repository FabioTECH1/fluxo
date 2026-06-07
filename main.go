package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	_ "net/http/pprof"

	"fluxo/config"
	"fluxo/database"
	"fluxo/server"
)

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func initAdminToken() {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}

	if count == 0 {
		token := generateToken()
		hash := sha256.Sum256([]byte(token))
		hashStr := hex.EncodeToString(hash[:])

		_, err = database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin", hashStr)
		if err != nil {
			log.Fatalf("Failed to create admin user: %v", err)
		}

		log.Println("=========================================================")
		log.Println("DAY ZERO AUTHENTICATION")
		log.Println("An initial admin user has been created.")
		log.Printf("Username: admin\n")
		log.Printf("Token:    %s\n", token)
		log.Println("Please save this token. It will only be shown once.")
		log.Println("=========================================================")
	}
}

func main() {
	log.Println("Starting Fluxo daemon...")

	cfg := config.LoadConfig()

	err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	log.Println("Database initialized successfully.")

	initAdminToken()

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
