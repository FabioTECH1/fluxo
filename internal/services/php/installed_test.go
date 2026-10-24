package php

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledFPMIgnoresResidualAndUnconfiguredPackages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "php-fpm8.4"), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	stat := func(path string) (os.FileInfo, error) { return os.Stat(filepath.Join(root, filepath.Base(path))) }
	output := "php8.5-fpm\tconfig-files\t8.5.8-1+ubuntu24.04.1+deb.sury.org+1\nphp8.5-cli\tconfig-files\t8.5.8-1\nphp8.4-fpm\tinstalled\t8.4.27-1+vendor\nphp8.3-fpm\tunpacked\t8.3.6-1\nphp8.2-fpm\thalf-configured\t8.2.29-1\nphp8.1-cli\tinstalled\t8.1.33-1\nphp8.0-fpm:arm64\tinstalled\t8.0.30-1\n"
	got := installedFPMVersions(output, stat)
	if len(got) != 3 || got["8.4"].FullVersion != "8.4.27" || !got["8.4"].Available || got["8.3"].Available || got["8.2"].Available || got["8.3"].FullVersion != "" {
		t.Fatalf("incorrect runtime inventory: %#v", got)
	}
	if len(installedFPMVersions("", stat)) != 0 {
		t.Fatal("empty inventory invented a runtime")
	}
}

func TestInstalledFPMRetainsBrokenPackageFactsWithoutInventingPatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "php-fpm8.3"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "php-fpm8.2"), 0755); err != nil {
		t.Fatal(err)
	}
	stat := func(path string) (os.FileInfo, error) { return os.Stat(filepath.Join(root, filepath.Base(path))) }
	got := installedFPMVersions("php8.5-fpm\tinstalled\t8.5.8-1\nphp8.3-fpm\tinstalled\t2:8.3.6-0ubuntu0.24.04.6\nphp8.2-fpm\tinstalled\tcustom\nphp8.0-fpm\tinstalled\t8.4.26-1\n", stat)
	if len(got) != 4 || got["8.3"].FullVersion != "8.3.6" || got["8.2"].FullVersion != "" || got["8.0"].FullVersion != "" {
		t.Fatalf("package facts lost: %#v", got)
	}
	for branch, item := range got {
		if item.Available {
			t.Fatalf("unusable PHP %s was available", branch)
		}
	}
}
