package backup

import "testing"

func TestShouldSkipRebuildableDependencyDirectories(t *testing.T) {
	for _, path := range []string{".git", "node_modules", "backend/.venv", "releases/42/api/.venv/lib"} {
		if !shouldSkipBackupPath(path, true, false) {
			t.Fatalf("expected %q to be excluded from site backups", path)
		}
	}
	for _, path := range []string{"venv", "backend/app.py", ".env"} {
		if shouldSkipBackupPath(path, true, false) {
			t.Fatalf("expected %q to remain in site backups", path)
		}
	}
}
