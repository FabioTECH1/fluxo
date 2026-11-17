package maintenancetask

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVMPackageLinksDoNotRelaxJournalOrConfigurationReads(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded retained VM only")
	}
	dir, err := os.MkdirTemp("/root", "fluxo-packaged-links-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "payload")
	if err = os.WriteFile(path, []byte("package payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(path, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err = readRegular(path, 1024); err == nil {
		t.Fatal("hard-linked journal/configuration read accepted")
	}
	if b, e := readRegularInput(path, 1024, true); e != nil || string(b) != "package payload" {
		t.Fatal("protected package hard link refused", e)
	}
	if err = os.Chmod(path, 0620); err != nil {
		t.Fatal(err)
	}
	if _, err = readRegularInput(path, 1024, true); err == nil {
		t.Fatal("writable package hard link accepted")
	}
}
