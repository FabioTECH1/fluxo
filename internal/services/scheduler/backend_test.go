package scheduler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderStatusRequiresInstalledVersion(t *testing.T) {
	got := installedBackendPackages("cron\tnot-installed\t\tamd64\ncronie:amd64\tinstalled\t1.7.1-1\tamd64\nsystemd-cron\tconfig-files\t2.3.2-1build1\tamd64\n")
	if len(got) != 1 || got["cronie"].version != "1.7.1-1" {
		t.Fatal("residual records were treated as installed", got)
	}
}

func TestFreshProviderPolicyFindsPeriodicScripts(t *testing.T) {
	for _, dir := range []string{"cron.hourly", "cron.daily", "cron.weekly", "cron.monthly"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "etc", dir)
			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			if existingSchedulesAt(root) {
				t.Fatal("empty directory treated as configured work")
			}
			if err := os.WriteFile(filepath.Join(path, "existing-job"), []byte("#!/bin/sh\ntrue\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if !existingSchedulesAt(root) {
				t.Fatal("installing cron would activate existing periodic work")
			}
		})
	}
}
