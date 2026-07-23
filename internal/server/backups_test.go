package server

import "testing"

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
