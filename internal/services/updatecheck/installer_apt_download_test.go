package updatecheck

import (
	"os/exec"
	"testing"
)

func TestInstallerAPTDownloadRecovery(t *testing.T) {
	if out, err := exec.Command("python3", "installer_apt_download_test.py").CombinedOutput(); err != nil {
		t.Fatalf("installer APT download recovery: %v\n%s", err, out)
	}
}
