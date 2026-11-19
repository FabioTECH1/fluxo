package scheduler

import (
	"errors"
	"os"
	"testing"
)

func TestBaseInstallerAdmissionObservesOnlyProvablyStaleHolds(t *testing.T) {
	j := journal{Upgrade: &upgradeLease{ID: "installer", Boot: "boot", PID: 42, Start: "original"}, Holds: map[string]string{"8.4": "installer"}}
	for _, tc := range []struct {
		name, boot string
		start      func(int) (string, error)
		allowed    bool
	}{
		{"dead owner", "boot", func(int) (string, error) { return "", os.ErrNotExist }, true},
		{"reused pid", "boot", func(int) (string, error) { return "new", nil }, true},
		{"previous boot", "new-boot", func(int) (string, error) { t.Fatal("old boot pid consulted"); return "", nil }, true},
		{"live owner", "boot", func(int) (string, error) { return "original", nil }, false},
		{"uncertain owner", "boot", func(int) (string, error) { return "", os.ErrPermission }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := withoutStaleInstallerHold(j, tc.boot, tc.start)
			if (err == nil) != tc.allowed {
				t.Fatal(err)
			}
			if tc.allowed && (result.Upgrade != nil || len(result.Holds) != 0) {
				t.Fatal("stale projection retained hold")
			}
			if j.Upgrade == nil || len(j.Holds) != 1 {
				t.Fatal("read-only admission mutated source journal")
			}
		})
	}
	j.Holds["8.3"] = "real-php-operation"
	if _, err := withoutStaleInstallerHold(j, "boot", func(int) (string, error) { return "", os.ErrNotExist }); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatal("foreign maintenance hold ignored", err)
	}
}
