package main

import (
	"context"
	"encoding/json"
	"flag"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/scheduler"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/releaseinfo"
	"fluxo/internal/server"
	backupservice "fluxo/internal/services/backup"
	"fluxo/internal/services/bootstrap"
	"fluxo/internal/services/deploy"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/nodeupdate"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/pythontoolchain"
	"github.com/google/uuid"
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
	if len(os.Args) == 2 && os.Args[1] == "--node-update-support" {
		fmt.Println("1")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "node-update-worker" {
		if len(os.Args) != 3 || os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" || !filepath.IsAbs(os.Getenv("FLUXO_DATA_DIR")) {
			fmt.Fprintln(os.Stderr, "Node.js maintenance requires root, production configuration and an operation UUID")
			os.Exit(2)
		}
		if _, err := uuid.Parse(os.Args[2]); err != nil {
			os.Exit(2)
		}
		cfg := config.LoadConfig()
		scheduler.SetDatabasePath(cfg.DBPath)
		if err := database.InitDB(cfg.DBPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := nodeupdate.NewManager(cfg.DataDir).RunWorker(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "scheduler-installer-verify" {
		if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" || !filepath.IsAbs(os.Getenv("FLUXO_DATA_DIR")) {
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := phpupdate.CheckInstallerDatabase(ctx, os.Getenv("FLUXO_DATA_DIR")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--scheduler-compatibility-required" {
		required, err := scheduler.CompatibilityRequired()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if required {
			fmt.Println("1")
		} else {
			fmt.Println("0")
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--scheduler-required-support" {
		required, err := scheduler.RequiredSupport()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(required)
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "scheduler-diagnostics" || os.Args[1] == "scheduler-verify-panel") {
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "Scheduler commands require root")
			os.Exit(2)
		}
		cfg := config.LoadConfig()
		// SSH support commands target the installed server by default, without
		// initializing/migrating its database or starting the HTTP daemon.
		if os.Getenv("FLUXO_ENV") == "" && os.Getenv("FLUXO_DATA_DIR") == "" {
			cfg.DataDir, cfg.DBPath = "/var/lib/fluxo", "/var/lib/fluxo/fluxo.db"
		}
		scheduler.SetDatabasePath(cfg.DBPath)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if os.Args[1] == "scheduler-verify-panel" {
			if err := phpupdate.VerifySchedulerPanel(ctx, cfg.DataDir); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Manual Scheduler boundary verified. Review the PHP update again; cron migration and background-process checks still apply.")
			return
		}
		if err := json.NewEncoder(os.Stdout).Encode(scheduler.DiagnosticBundle(ctx)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "scheduler-installer-policy" {
		if os.Geteuid() != 0 || (os.Args[2] != "fresh" && os.Args[2] != "existing") {
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		json.NewEncoder(os.Stdout).Encode(scheduler.InstallerPolicy(ctx, os.Args[2] == "existing"))
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--scheduler-support" {
		fmt.Println("3")
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "scheduler-execute" {
		if err := scheduler.Execute(context.Background(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "scheduler-installer-release" {
		if err := scheduler.SignalUpgrade(context.Background(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "scheduler-installer-lease" {
		if os.Getenv("FLUXO_ENV") != "prod" {
			os.Exit(2)
		}
		parent, err := strconv.Atoi(os.Args[2])
		if err != nil {
			os.Exit(2)
		}
		if err := phpupdate.HoldInstallerLease(os.Getenv("FLUXO_DATA_DIR"), parent, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "scheduler-dispatch" {
		if len(os.Args) != 3 && len(os.Args) != 4 {
			os.Exit(2)
		}
		id, err := strconv.Atoi(os.Args[2])
		if err != nil || id <= 0 {
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if len(os.Args) == 4 {
			if err = scheduler.EntryReceipt(ctx, id, os.Args[3]); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		if _, err = scheduler.Dispatch(ctx, id); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "scheduler-migration" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := scheduler.Receipt(ctx, os.Args[2], os.Args[3], 0); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) == 2 && os.Args[1] == "--php-repository-support" {
		fmt.Println("1")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "php-repository-setup" {
		if os.Getenv("FLUXO_ENV") != "prod" || !filepath.IsAbs(os.Getenv("FLUXO_DATA_DIR")) {
			fmt.Fprintln(os.Stderr, "Repository setup requires explicit production configuration")
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := phpupdate.SetupRepositoryFromInstaller(ctx, os.Getenv("FLUXO_DATA_DIR")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "php-update-worker" || os.Args[1] == "php-update-transaction") {
		if len(os.Args) != 3 || os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "PHP maintenance requires root and an operation UUID")
			os.Exit(2)
		}
		if _, err := uuid.Parse(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "Invalid operation UUID")
			os.Exit(2)
		}
		if os.Getenv("FLUXO_ENV") != "prod" || !filepath.IsAbs(os.Getenv("FLUXO_DATA_DIR")) {
			fmt.Fprintln(os.Stderr, "PHP maintenance requires explicit production configuration and an absolute data directory")
			os.Exit(2)
		}
		cfg := config.LoadConfig()
		scheduler.SetDatabasePath(cfg.DBPath)
		if err := database.InitDB(cfg.DBPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		m := phpupdate.NewManager(cfg.DataDir)
		var err error
		if os.Args[1] == "php-update-worker" {
			err = m.RunWorker(os.Args[2])
		} else {
			err = m.RunTransaction(os.Args[2])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "node-toolchain" {
		if (len(os.Args) != 3 && len(os.Args) != 5) || os.Args[2] != "install" || len(os.Args) == 5 && os.Args[3] != "--installer-lease" {
			fmt.Fprintln(os.Stderr, "Usage: fluxo node-toolchain install [--installer-lease UUID]")
			os.Exit(2)
		}
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "Node.js toolchain installation must run as root")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if len(os.Args) == 5 {
			if err := scheduler.VerifyInstallerLease(ctx, os.Args[4]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			lease, err := packages.Lifecycle(ctx, false)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			defer lease()
		}
		for _, dataDir := range []string{"/var/lib/fluxo", config.LoadConfig().DataDir} {
			if _, err := os.Stat(filepath.Join(dataDir, "fluxo.db")); err == nil {
				if err = phpupdate.CheckInstallerDatabase(ctx, dataDir); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else if !os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		status, err := nodetoolchain.InstallWithProgress(ctx, func(message string) {
			fmt.Println(message)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "Node.js toolchain installation failed:", err)
			os.Exit(1)
		}
		fmt.Printf("Node.js toolchain ready: Node.js %s, npm %s, pnpm %s, Yarn %s, Corepack %s, Bun %s\n",
			status.Version, status.NPM, status.PNPM, status.Yarn, status.Corepack, status.Bun)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "python-toolchain" {
		if len(os.Args) != 3 || os.Args[2] != "install" {
			fmt.Fprintln(os.Stderr, "Usage: fluxo python-toolchain install")
			os.Exit(2)
		}
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "Python application support installation must run as root")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		status, err := pythontoolchain.Install(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Python application support installation failed:", err)
			os.Exit(1)
		}
		fmt.Printf("Python application support ready: Python %s, pip %s, uv %s\n", status.Version, status.Pip, status.UV)
		return
	}

	resetToken := flag.Bool("reset-token", false, "Reset the admin token and report the configured username")
	showAdminUsername := flag.Bool("show-admin-username", false, "Print the configured admin username")
	showVersion := flag.Bool("version", false, "Print version and exit")
	supportsNodeToolchain := flag.Bool("supports-node-toolchain", false, "Report support for managed Node.js toolchains")
	supportsPythonToolchain := flag.Bool("supports-python-toolchain", false, "Report support for managed Python application tools")
	installerToolVersions := flag.Bool("installer-tool-versions", false, "Print release-pinned installer tool versions")
	flag.Parse()

	if *supportsNodeToolchain {
		fmt.Println("supported")
		return
	}
	if *supportsPythonToolchain {
		fmt.Println("supported")
		return
	}
	if *installerToolVersions {
		composer, composerSHA256, wpCLI, err := releaseinfo.InstallerToolVersions()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("composer=%s\ncomposer-sha256=%s\nwp-cli=%s\n", composer, composerSHA256, wpCLI)
		return
	}

	if *showVersion {
		fmt.Println("fluxo version", version)
		return
	}
	if *resetToken && *showAdminUsername {
		fmt.Fprintln(os.Stderr, "Use only one account recovery command at a time")
		os.Exit(2)
	}

	if (*resetToken || *showAdminUsername) && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Account recovery commands must run as root")
		os.Exit(1)
	}

	server.Version = version

	cfg := config.LoadConfig()
	scheduler.SetDatabasePath(cfg.DBPath)

	// With no explicit data configuration, CLI recovery should target an installed server.
	useInstalledRecoveryDB := os.Getenv("FLUXO_ENV") == "" && os.Getenv("FLUXO_DATA_DIR") == ""
	if (*resetToken || *showAdminUsername) && useInstalledRecoveryDB {
		if _, err := os.Stat("/var/lib/fluxo/fluxo.db"); err == nil {
			cfg.DBPath = "/var/lib/fluxo/fluxo.db"
			cfg.DataDir = "/var/lib/fluxo"
			cfg.Env = "prod"
		}
	}
	if *showAdminUsername {
		if err := bootstrap.ShowAdminUsername(cfg.DBPath, os.Stdout); err != nil {
			log.Fatalf("Failed to retrieve admin username: %v", err)
		}
		return
	}

	err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	if *resetToken {
		bootstrap.ResetAdminToken(cfg.DataDir, cfg.Env == "prod", os.Stdout)
		return
	}
	if cfg.Env == "prod" {
		recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer recoveryCancel()
		if err := nodetoolchain.RecoverInterruptedInstall(recoveryCtx); err != nil {
			log.Fatalf("Interrupted Node.js toolchain recovery failed: %v", err)
		}
	}

	if cfg.Env == "prod" {
		if err := phpupdate.ReconcileSchedulerHolds(context.Background()); err != nil {
			log.Fatalf("Scheduler maintenance reconciliation failed: %v", err)
		}
		if err := cron.ReconcileSchedulers(context.Background()); err != nil {
			log.Printf("Warning: %v", err)
		}
	}
	log.Println("Starting Fluxo daemon...")
	if err := deploy.MigrateApplicationCommandDefaults(database.DB); err != nil {
		log.Fatalf("Deployment defaults migration failed: %v", err)
	}
	log.Println("Database initialized successfully.")
	if err := config.InitEncryption(cfg.DataDir); err != nil {
		log.Fatalf("Encryption initialization failed: %v", err)
	}

	// Encrypt existing secrets before any background worker can read them.
	database.EncryptExistingSecrets()

	// Clean up any deployments left in 'running' state on startup
	if _, err := database.DB.Exec(`UPDATE deployments
		SET status = 'failed', output = 'Deployment was interrupted by a server restart.',
			failure_reason = 'Deployment was interrupted by a server restart.', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'`); err != nil {
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
