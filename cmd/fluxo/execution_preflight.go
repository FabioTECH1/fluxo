package main

import (
	"context"
	"encoding/json"
	"fluxo/internal/bridge"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/services/bootstrap"
	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/scheduler"
)

// executionMetadata runs before LoadConfig (which creates a production data
// directory). These commands remain available when ordinary mutation is blocked.
func executionMetadata(args []string) bool {
	if len(args) == 1 && args[0] == "--base-support" {
		fmt.Fprintln(os.Stdout, bridge.BaseContract)
		return true
	}
	if baseInstallCommand(args) {
		return true
	}
	if len(args) == 1 && (args[0] == "--installer-renewal-withdrawal-support" || args[0] == "--installer-historical-withdrawal-support") {
		fmt.Fprintln(os.Stdout, "1")
		return true
	}
	if len(args) == 1 && args[0] == "--installer-scheduler-support" {
		fmt.Fprintln(os.Stdout, "1")
		return true
	}
	if len(args) == 1 && args[0] == "--installer-execution-support" {
		fmt.Fprintln(os.Stdout, "1")
		return true
	}
	if len(args) == 1 && args[0] == "--installer-execution-status" {
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/fluxo"
		}
		result, err := bridge.InstallerExecutionStatus(dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if len(args) > 0 && args[0] == "maintenance-registration-installer" {
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "installer maintenance requires a handoff and action")
			os.Exit(2)
		}
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/fluxo"
		}
		scheduler.SetDatabasePath(filepath.Join(dataDir, "fluxo.db"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		result, err := bridge.InstallerMaintenanceRegistrationCommand(ctx, dataDir, version, args[1], args[2], args[3:])
		cancel()
		if e := json.NewEncoder(os.Stdout).Encode(result); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}

	if len(args) > 0 && args[0] == "maintenance-registration" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "maintenance-registration requires inspect, requalify or recover")
			os.Exit(2)
		}
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/fluxo"
		}
		scheduler.SetDatabasePath(filepath.Join(dataDir, "fluxo.db"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		result, err := bridge.MaintenanceRegistrationCommand(ctx, dataDir, version, args[1], args[2:])
		cancel()
		if e := json.NewEncoder(os.Stdout).Encode(result); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}

	if len(args) > 0 && args[0] == "scheduler-loader" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: fluxo scheduler-loader inspect | requalify EPOCH OLD_POLICY NEW_POLICY | recover PUBLICATION_UUID")
			os.Exit(2)
		}
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/fluxo"
		}
		scheduler.SetDatabasePath(filepath.Join(dataDir, "fluxo.db"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		result, err := bridge.LoaderPolicyCommand(ctx, dataDir, version, args[1], args[2:])
		cancel()
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if handled, err := nginxRecoveryMetadata(args, nginx.HTTPDefaultsRecoveryReady, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}

	if len(args) == 1 && args[0] == "--execution-capabilities" {
		if err := json.NewEncoder(os.Stdout).Encode(executiondomain.Capabilities()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if len(args) > 0 && args[0] == "execution-compatibility" {
		dataDir := os.Getenv("FLUXO_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/fluxo"
		}
		var result any
		var err error
		switch {
		case len(args) == 2 && (args[1] == "install" || args[1] == "install-complete"):
			scheduler.SetDatabasePath(filepath.Join(dataDir, "fluxo.db"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result, err = bridge.InstallExecutionFormats(ctx, dataDir, version, args[1] == "install-complete")
			cancel()
		case len(args) == 2 && args[1] == "inspect":
			result, err = executiondomain.InspectCompatibility(dataDir)
		case len(args) == 2 && args[1] == "activate-historical":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result, err = bridge.ActivateHistoricalRetirement(ctx, dataDir, version)
			cancel()
		case len(args) == 2 && args[1] == "upgrade":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result, err = bridge.UpgradeExecutionFormats(ctx, dataDir, version)
			cancel()
		case len(args) == 3 && args[1] == "refresh-task-helpers":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result, err = bridge.RefreshTaskHelpers(ctx, dataDir, version, args[2])
			cancel()
		case len(args) == 2 && args[1] == "activate", len(args) == 3 && args[1] == "recover":
			resume := ""
			if args[1] == "recover" {
				resume = args[2]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result, err = bridge.ActivateExecutionFormats(ctx, dataDir, version, resume)
			cancel()
		default:
			fmt.Fprintln(os.Stderr, "Usage: fluxo execution-compatibility inspect|activate|activate-historical|upgrade|recover OPERATION_UUID|refresh-task-helpers TASK_CORE_SHA256")
			os.Exit(2)
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}

	// Username recovery is read-only and must not mkdir a missing installation
	// or depend on a future execution reader being available.
	if len(args) == 1 && (args[0] == "--show-admin-username" || args[0] == "-show-admin-username" || args[0] == "--show-admin-username=true" || args[0] == "-show-admin-username=true") {
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "Account recovery commands must run as root")
			os.Exit(1)
		}
		dataDir, env := os.Getenv("FLUXO_DATA_DIR"), os.Getenv("FLUXO_ENV")
		if dataDir == "" {
			dataDir = "."
			if env == "prod" {
				dataDir = "/var/lib/fluxo"
			} else if env == "" {
				if _, err := os.Stat("/var/lib/fluxo/fluxo.db"); err == nil {
					dataDir = "/var/lib/fluxo"
				}
			}
		}
		if err := bootstrap.ShowAdminUsername(filepath.Join(dataDir, "fluxo.db"), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	return false
}

// Every ordinary startup can initialize its selected database, regardless of
// FLUXO_ENV. Environment labels must not exempt an installed data directory.
func needsExecutionPreflight(args []string, _ string) bool {
	if executiondomain.IsPureMetadata(args) {
		return false
	}
	if len(args) == 2 && args[0] == "baseline-bridge" && (args[1] == "inspect" || args[1] == "verify") {
		return false
	}
	return true
}

func executionDataDirectories() ([]string, error) {
	dirs := []string{"/var/lib/fluxo"}
	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		dataDir = "."
		if os.Getenv("FLUXO_ENV") == "prod" {
			dataDir = dirs[0]
		}
	}
	actual, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if actual != dirs[0] {
		// A development workspace without execution evidence needs no root-owned
		// floor directory. If a floor exists, inspect it strictly before opening
		// that workspace's database; this also covers relative paths and unset/dev.
		_, err = os.Lstat(filepath.Join(actual, "execution-readers.json"))
		if err == nil || !os.IsNotExist(err) {
			dirs = append(dirs, actual)
		}
	}
	return dirs, nil
}

func preflightExecutionCommand(args []string) error {
	if !needsExecutionPreflight(args, os.Getenv("FLUXO_ENV")) {
		return nil
	}
	dirs, err := executionDataDirectories()
	if err != nil {
		return err
	}
	for _, dataDir := range dirs {
		if _, err := executiondomain.InspectCompatibility(dataDir); err != nil {
			return fmt.Errorf("execution compatibility refused before initialization: %w", err)
		}
	}
	return nil
}

func nginxRecoveryMetadata(args []string, ready func() error, out io.Writer) (bool, error) {
	if len(args) != 1 || args[0] != "--nginx-http-defaults-recovery-ready" {
		return false, nil
	}
	if err := ready(); err != nil {
		return true, err
	}
	_, err := fmt.Fprintln(out, "1")
	return true, err
}

// These fixed-arity internal commands used exact-length routing in main. Reject
// malformed invocations before even reader preflight so they cannot become a
// daemon startup or initialize an unrelated installation. Structured command
// namespaces retain their own argument validation.
func validateFixedCommandArguments(args []string) error {
	if len(args) == 0 {
		return nil
	}
	counts := map[string]int{
		"--installer-renewal-withdrawal-support":    0,
		"--installer-historical-withdrawal-support": 0,
		"installer-runtime-state":                   0, "system-task-diagnostics": 0, "system-task-reconcile": 0, "system-task-apt-refuse": 0,
		"system-task-application": 1, "system-task-execute": 1, "system-task-dispatch": 2, "system-task-receipt": 2,
		"scheduler-installer-verify": 0, "scheduler-diagnostics": 0, "scheduler-verify-panel": 0, "scheduler-installer-policy": 1,
		"scheduler-installer-verify-upgrade": 1,
		"scheduler-execute":                  1, "scheduler-installer-release": 1, "scheduler-installer-lease": 1, "scheduler-migration": 2, "php-repository-setup": 0,
	}
	if count, known := counts[args[0]]; known && len(args) != count+1 {
		return fmt.Errorf("usage: fluxo %s requires %d argument(s)", args[0], count)
	}
	return nil
}
