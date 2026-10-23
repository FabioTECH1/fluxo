package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNodeToolchainAdmissionDoesNotInferWorkingDirectoryDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	// A stale development DB can exist in the SSH user's home. It must never
	// become a second installed-panel database merely because of the cwd.
	if err := os.WriteFile("fluxo.db", []byte("unrelated database"), 0000); err != nil {
		t.Fatal(err)
	}
	for _, env := range []string{"", "dev", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("FLUXO_ENV", env)
			t.Setenv("FLUXO_DATA_DIR", "")
			dirs, err := nodeToolchainDataDirs()
			if err != nil || !reflect.DeepEqual(dirs, []string{"/var/lib/fluxo"}) {
				t.Fatal("SSH cwd influenced installed maintenance admission", dirs, err)
			}
		})
	}
}

func TestNodeToolchainAdmissionKeepsExplicitAbsoluteDatabase(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "panel")
	t.Setenv("FLUXO_DATA_DIR", custom)
	dirs, err := nodeToolchainDataDirs()
	if err != nil || !reflect.DeepEqual(dirs, []string{"/var/lib/fluxo", custom}) {
		t.Fatal("custom production database omitted", dirs, err)
	}
	t.Setenv("FLUXO_DATA_DIR", "/var/lib/fluxo/.")
	dirs, err = nodeToolchainDataDirs()
	if err != nil || !reflect.DeepEqual(dirs, []string{"/var/lib/fluxo"}) {
		t.Fatal("default production database not deduplicated", dirs, err)
	}
	for _, relative := range []string{".", "data", "../other-panel"} {
		t.Setenv("FLUXO_DATA_DIR", relative)
		if dirs, err := nodeToolchainDataDirs(); err == nil || dirs != nil {
			t.Fatal("relative maintenance configuration accepted", dirs, err)
		}
	}
}
