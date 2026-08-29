package server

import (
	"testing"

	"fluxo/internal/database"
)

func TestBackupDestinationPrefix(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		fallback  string
		want      string
	}{
		{name: "new default", fallback: "fluxo-backups", want: "fluxo-backups"},
		{name: "legacy update preserves prefix", fallback: "legacy/custom", want: "legacy/custom"},
		{name: "normalizes requested prefix", requested: " /team/backups/ ", fallback: "legacy", want: "team/backups"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := backupDestinationPrefix(test.requested, test.fallback); got != test.want {
				t.Fatalf("prefix = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateBackupEncryptionPassword(t *testing.T) {
	for _, invalid := range []string{"short", "contains\nnewline", string(make([]byte, 257))} {
		if err := validateBackupEncryptionPassword(invalid); err == nil {
			t.Fatalf("password %q should be rejected", invalid)
		}
	}
	if err := validateBackupEncryptionPassword("correct horse battery staple"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}

func TestApplyBackupPlanEncryptionPreservesExistingStateWhenFieldIsOmitted(t *testing.T) {
	existing := database.BackupPlan{EncryptionEnabled: true, EncryptionPassword: "enc:existing"}
	var plan database.BackupPlan
	if err := applyBackupPlanEncryption(&plan, backupPlanRequest{}, &existing); err != nil {
		t.Fatal(err)
	}
	if !plan.EncryptionEnabled || plan.EncryptionPassword != existing.EncryptionPassword {
		t.Fatalf("encryption state was not preserved: %+v", plan)
	}

	disabled := false
	if err := applyBackupPlanEncryption(&plan, backupPlanRequest{EncryptionEnabled: &disabled}, &existing); err != nil {
		t.Fatal(err)
	}
	if plan.EncryptionEnabled || plan.EncryptionPassword != "" {
		t.Fatalf("encryption state was not explicitly disabled: %+v", plan)
	}
}
