package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePythonStarterUsesPortableDjangoDatabaseDrivers(t *testing.T) {
	tests := []struct {
		name            string
		engine          string
		wantRequirement string
		wantInit        string
	}{
		{name: "mysql", engine: "mysql", wantRequirement: "PyMySQL>=1.1,<2", wantInit: "install_as_MySQLdb"},
		{name: "postgres", engine: "postgres", wantRequirement: "psycopg[binary]>=3.2,<4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writePythonStarter(dir, ProvisionRequest{PythonPreset: "django", DatabaseEngine: test.engine}); err != nil {
				t.Fatal(err)
			}
			requirements, err := os.ReadFile(filepath.Join(dir, "requirements.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(requirements), test.wantRequirement) {
				t.Fatalf("requirements.txt does not contain %q:\n%s", test.wantRequirement, requirements)
			}
			initFile, err := os.ReadFile(filepath.Join(dir, "config", "__init__.py"))
			if err != nil {
				t.Fatal(err)
			}
			if test.wantInit != "" && !strings.Contains(string(initFile), test.wantInit) {
				t.Fatalf("config/__init__.py does not contain %q:\n%s", test.wantInit, initFile)
			}
			if test.wantInit == "" && len(initFile) != 0 {
				t.Fatalf("config/__init__.py should be empty for %s:\n%s", test.engine, initFile)
			}
		})
	}
}
