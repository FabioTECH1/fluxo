package server

import "testing"

func TestInstalledPHPPatchVersionsUsesInstalledFPMRevisions(t *testing.T) {
	output := "php8.4-fpm\tinstalled\t8.4.26-1+vendor\nphp8.3-fpm\tinstalled\t2:8.3.6-0ubuntu0.24.04.6\nphp8.2-fpm\tconfig-files\t8.2.29-1\nphp8.1-cli\tinstalled\t8.1.33-1\nphp8.0-fpm\tinstalled\t8.4.26-1\n"
	got := installedPHPPatchVersions(output)
	if len(got) != 2 || got["8.4"] != "8.4.26" || got["8.3"] != "8.3.6" {
		t.Fatalf("wrong installed display versions: %#v", got)
	}
}
