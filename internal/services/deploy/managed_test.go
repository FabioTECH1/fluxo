package deploy

import (
	"strings"
	"testing"
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
		commands := GenerateApplicationCommands(appType)
		for _, protectedCommand := range []string{"git clone", "git pull", "releases/", "ln -sfn"} {
			if strings.Contains(commands, protectedCommand) {
				t.Fatalf("%s application commands contain protected lifecycle operation %q", appType, protectedCommand)
			}
		}
	}
}
