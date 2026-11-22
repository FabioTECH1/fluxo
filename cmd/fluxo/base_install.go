package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/bridge"
	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
)

func baseInstallCommand(args []string) bool {
	if len(args) == 0 || args[0] != "base-install" {
		return false
	}
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fluxo base-install inspect|inspect-installer|bridge-command|prepare-fresh|initialize-fresh|accept")
		os.Exit(2)
	}
	if args[1] == "bridge-command" {
		fmt.Fprintln(os.Stdout, bridge.BaseBridgeInstallCommand())
		return true
	}
	dataDir := os.Getenv("FLUXO_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/fluxo"
	}
	scheduler.SetDatabasePath(filepath.Join(dataDir, "fluxo.db"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var result any
	var err error
	switch args[1] {
	case "inspect", "inspect-installer":
		result, err = bridge.BaseInspect(ctx, dataDir)
	case "prepare-fresh":
		err = bridge.BasePrepareFresh(dataDir, version)
	case "initialize-fresh":
		if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
			err = errors.New("fresh initialization requires installed root scope")
			break
		}
		if err = bridge.BaseValidateFreshImage(dataDir, version); err != nil {
			break
		}
		intent, _ := bridge.BaseFreshStatus(dataDir)
		if intent.Phase != "pending" {
			result, err = bridge.BaseInitializeFresh(ctx, dataDir, version)
			break
		}
		if err = database.InitBaseDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
			break
		}
		err = database.DB.Close()
		if err != nil {
			break
		}
		if err = config.InitEncryption(dataDir); err != nil {
			break
		}
		result, err = bridge.BaseInitializeFresh(ctx, dataDir, version)
	case "accept":
		result, err = bridge.BaseAccept(ctx, dataDir, os.Getenv("FLUXO_BASE_INSTALLER_LEASE_ID"))
	default:
		err = errors.New("unknown base installer action")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if args[1] == "inspect-installer" {
			admission, _ := result.(bridge.BaseAdmission)
			json.NewEncoder(os.Stdout).Encode(map[string]string{
				"refusal_action": bridge.BaseInstallerRefusal(ctx, dataDir, admission),
			})
		}
		os.Exit(1)
	}
	if result != nil {
		json.NewEncoder(os.Stdout).Encode(result)
	}
	return true
}
