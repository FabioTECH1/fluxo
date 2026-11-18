package phpupdate

import (
	"errors"
	"strings"
	"testing"
)

func TestBridgeLegacyDaemonPreservesArgumentsAndVerifiesInterpreter(t *testing.T) {
	command := "php8.4 artisan queue:work database --timeout=600"
	unit := func(path, argv string) map[string]string {
		return map[string]string{"User": "fluxo", "Type": "simple", "Restart": "always", "ExecStart": "{ path=" + path + " ; argv[]=" + argv + " ; ignore_errors=no ; start_time=[n/a] }"}
	}
	for _, tc := range []struct {
		name, path, argv string
		shadow, refuse   bool
	}{
		{"legacy", "php8.4", command, false, false},
		{"resolved legacy", "/usr/bin/php8.4", command, false, false},
		{"shadow", "php8.4", command, true, true},
		{"other executable", "/opt/php8.4", command, false, true},
		{"other branch", "php8.3", command, false, true},
		{"other arguments", "php8.4", command + " --force", false, true},
		{"other action", "php8.4", "php8.4 artisan migrate", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := legacyDaemonCommandBridgeReady(command, unit(tc.path, tc.argv), func(branch string) error {
				calls++
				if branch != "8.4" {
					t.Fatal("wrong branch resolved", branch)
				}
				if tc.shadow {
					return errors.New("shadowed interpreter")
				}
				return nil
			})
			if (err != nil) != tc.refuse {
				t.Fatalf("error=%v refuse=%v", err, tc.refuse)
			}
			if (!tc.refuse || tc.shadow) && (calls == 1) != (tc.path == "php8.4") {
				t.Fatal("incorrect executable resolution", calls)
			}
		})
	}
	for _, selector := range []string{"User", "Type", "Restart", "DynamicUser", "PrivateUsers", "Group", "AmbientCapabilities", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "DropInPaths", "Environment", "EnvironmentFiles", "ExecSearchPath", "PassEnvironment", "RootDirectory", "RootImage", "BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages", "ExtensionDirectories"} {
		t.Run(selector, func(t *testing.T) {
			p := unit("php8.4", command)
			p[selector] = "custom"
			if err := legacyDaemonCommandBridgeReady(command, p, func(string) error { t.Fatal("custom selectors reached resolver"); return nil }); err == nil {
				t.Fatal("custom execution selector accepted")
			}
		})
	}
	for _, other := range []string{"php artisan queue:work", "/opt/php8.4 artisan queue:work", "php8.4 artisan 'queue:work'", "php8.4 artisan queue:work\n", "php8.4x artisan queue:work"} {
		if err := legacyDaemonCommandBridgeReady(other, unit("php8.4", other), func(string) error { t.Fatal("unsupported command reached resolver"); return nil }); err == nil {
			t.Fatal("unsupported command accepted", other)
		}
	}
	for _, raw := range []string{"", unit("php8.4", command)["ExecStart"] + unit("php8.4", command)["ExecStart"], strings.Replace(unit("php8.4", command)["ExecStart"], " ; ignore_errors=", " ; invalid=", 1)} {
		p := unit("php8.4", command)
		p["ExecStart"] = raw
		if err := legacyDaemonCommandBridgeReady(command, p, func(string) error { t.Fatal("ambiguous command reached resolver"); return nil }); err == nil {
			t.Fatal("malformed command accepted")
		}
	}
}
