package deploy

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestManagedStandardLifecycleEnforcesConfiguredOrigin(t *testing.T) {
	script := GenerateManagedLifecycle("standard")

	for _, command := range []string{
		`git remote get-url origin`,
		`git remote set-url origin "$FLUXO_REPO"`,
		`git remote add origin "$FLUXO_REPO"`,
		`git fetch origin`,
		`git reset --hard HEAD`,
		`git reset --hard "origin/$FLUXO_BRANCH"`,
	} {
		if !strings.Contains(script, command) {
			t.Fatalf("managed standard lifecycle is missing %q", command)
		}
	}
	if strings.Index(script, `git remote set-url origin "$FLUXO_REPO"`) > strings.Index(script, `git fetch origin`) {
		t.Fatal("managed standard lifecycle fetches before enforcing the configured origin")
	}
	if strings.Index(script, `git reset --hard HEAD`) > strings.Index(script, `git checkout "$FLUXO_BRANCH"`) {
		t.Fatal("managed standard lifecycle selects the branch before clearing tracked local changes")
	}
	if strings.Contains(script, `git pull`) {
		t.Fatal("managed standard lifecycle merges remote changes instead of matching the configured revision")
	}
}

func TestManagedZDDLifecycleProtectsActivationAndPersistence(t *testing.T) {
	script := GenerateManagedLifecycle("zero-downtime")

	for _, fragment := range []string{
		`trap cleanup_failed_release ERR`,
		`ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"`,
		`ln -s "$SHARED_STORAGE" "$RELEASE_DIR/storage"`,
		`mv -Tf "$TEMP_CURRENT" "$CURRENT_DIR"`,
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("managed zero-downtime lifecycle is missing %q", fragment)
		}
	}
	if strings.Index(script, `bash -Eeuo pipefail "$FLUXO_APPLICATION_SCRIPT"`) > strings.Index(script, `mv -Tf "$TEMP_CURRENT" "$CURRENT_DIR"`) {
		t.Fatal("managed zero-downtime lifecycle activates before application commands complete")
	}
}

func TestApplicationCommandsDoNotOwnDeploymentLifecycle(t *testing.T) {
	for _, appType := range []string{"laravel", "php", "html", "node"} {
		commands := GenerateApplicationCommands(appType, false)
		for _, protectedCommand := range []string{"git clone", "git pull", "releases/", "ln -sfn"} {
			if strings.Contains(commands, protectedCommand) {
				t.Fatalf("%s application commands contain protected lifecycle operation %q", appType, protectedCommand)
			}
		}
	}
}

func TestLaravelApplicationCommandsOnlyMigrateWithAttachedDatabase(t *testing.T) {
	withoutDatabase := GenerateApplicationCommands("laravel", false)
	if strings.Contains(withoutDatabase, "artisan migrate") {
		t.Fatalf("Laravel commands include a migration without a database:\n%s", withoutDatabase)
	}

	withDatabase := GenerateApplicationCommands("laravel", true)
	if !strings.Contains(withDatabase, "$FLUXO_PHP artisan migrate --force") {
		t.Fatalf("Laravel commands omit the migration with a database:\n%s", withDatabase)
	}
}

func TestNodeApplicationCommandsRequirePackageManifest(t *testing.T) {
	commands := GenerateApplicationCommands("node", false)
	guard := strings.Index(commands, `if [ -f package.json ]; then`)
	install := strings.Index(commands, `bash -lc "$FLUXO_NODE_INSTALL_COMMAND"`)
	build := strings.Index(commands, `bash -lc "$FLUXO_NODE_BUILD_COMMAND"`)

	if guard < 0 || install < guard || build < guard {
		t.Fatalf("Node application commands are not protected by a package.json guard:\n%s", commands)
	}
}

func TestNodeApplicationCommandsSkipPackageToolsWithoutManifest(t *testing.T) {
	if got := executeNodeApplicationCommands(t, false); got != "" {
		t.Fatalf("package commands ran without package.json: %q", got)
	}
}

func TestNodeApplicationCommandsRunPackageToolsWithManifest(t *testing.T) {
	if got := executeNodeApplicationCommands(t, true); got != "install\nbuild\n" {
		t.Fatalf("package command output = %q", got)
	}
}

func executeNodeApplicationCommands(t *testing.T, withManifest bool) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "commands-ran")
	if withManifest {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	commands := GenerateApplicationCommands("node", false)
	cmd := exec.Command("bash", "-c", "set -Eeuo pipefail\n"+commands)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
		`FLUXO_NODE_INSTALL_COMMAND=printf 'install\n' >> "$FLUXO_TEST_MARKER"`,
		`FLUXO_NODE_BUILD_COMMAND=printf 'build\n' >> "$FLUXO_TEST_MARKER"`,
		"FLUXO_TEST_MARKER=" + marker,
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Node application commands failed: %v\n%s", err, output)
	}
	got, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestNormalizeApplicationCommandsUpgradesOnlyOldNodeDefault(t *testing.T) {
	if got := NormalizeApplicationCommands("node", legacyNodeApplicationCommands, false); got != GenerateApplicationCommands("node", false) {
		t.Fatalf("old Node default was not upgraded:\n%s", got)
	}

	custom := legacyNodeApplicationCommands + "\necho custom"
	if got := NormalizeApplicationCommands("node", custom, false); got != custom {
		t.Fatalf("custom Node commands were changed:\n%s", got)
	}

	if got := NormalizeApplicationCommands("php", legacyNodeApplicationCommands, false); got != legacyNodeApplicationCommands {
		t.Fatalf("non-Node commands were changed:\n%s", got)
	}
}

func TestNormalizeApplicationCommandsRemovesMigrationOnlyFromUntouchedLaravelDefault(t *testing.T) {
	oldDefault := GenerateApplicationCommands("laravel", true)
	want := GenerateApplicationCommands("laravel", false)
	if got := NormalizeApplicationCommands("laravel", oldDefault, false); got != want {
		t.Fatalf("untouched Laravel default was not corrected for a site without a database:\n%s", got)
	}
	if got := NormalizeApplicationCommands("laravel", oldDefault, true); got != oldDefault {
		t.Fatalf("Laravel migration was removed from a site with a database:\n%s", got)
	}

	custom := oldDefault + "\necho custom"
	if got := NormalizeApplicationCommands("laravel", custom, false); got != custom {
		t.Fatalf("custom Laravel commands were changed:\n%s", got)
	}
}

func TestMigrateApplicationCommandDefaultsPreservesCustomScripts(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE sites (
		id INTEGER PRIMARY KEY,
		app_type TEXT,
		deploy_script_mode TEXT,
		deploy_script TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE databases (
		id INTEGER PRIMARY KEY,
		site_id INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	custom := legacyNodeApplicationCommands + "\necho custom"
	for _, row := range []struct {
		id      int
		appType string
		mode    string
		script  string
	}{
		{id: 1, appType: "node", mode: ScriptModeManaged, script: legacyNodeApplicationCommands},
		{id: 2, appType: "node", mode: ScriptModeManaged, script: custom},
		{id: 3, appType: "node", mode: ScriptModeLegacy, script: legacyNodeApplicationCommands},
		{id: 4, appType: "php", mode: ScriptModeManaged, script: legacyNodeApplicationCommands},
		{id: 6, appType: "laravel", mode: ScriptModeManaged, script: GenerateApplicationCommands("laravel", true)},
		{id: 7, appType: "laravel", mode: ScriptModeManaged, script: GenerateApplicationCommands("laravel", true)},
		{id: 8, appType: "laravel", mode: ScriptModeManaged, script: GenerateApplicationCommands("laravel", true) + "\necho custom"},
	} {
		if _, err := db.Exec("INSERT INTO sites (id, app_type, deploy_script_mode, deploy_script) VALUES (?, ?, ?, ?)", row.id, row.appType, row.mode, row.script); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO sites (id, app_type, deploy_script_mode, deploy_script) VALUES (?, ?, ?, ?)", 5, "node", ScriptModeManaged, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO databases (id, site_id) VALUES (?, ?)", 1, 7); err != nil {
		t.Fatal(err)
	}

	if err := MigrateApplicationCommandDefaults(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateApplicationCommandDefaults(db); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}

	want := map[int]string{
		1: GenerateApplicationCommands("node", false),
		2: custom,
		3: legacyNodeApplicationCommands,
		4: legacyNodeApplicationCommands,
		5: "",
		6: GenerateApplicationCommands("laravel", false),
		7: GenerateApplicationCommands("laravel", true),
		8: GenerateApplicationCommands("laravel", true) + "\necho custom",
	}
	rows, err := db.Query("SELECT id, COALESCE(deploy_script, '') FROM sites ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var script string
		if err := rows.Scan(&id, &script); err != nil {
			t.Fatal(err)
		}
		if script != want[id] {
			t.Fatalf("site %d deployment commands changed unexpectedly:\n%s", id, script)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
