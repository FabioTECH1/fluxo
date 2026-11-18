package phpupdate

import (
	"context"
	"errors"
	"strings"

	"fluxo/internal/safeinput"
)

// LegacyDaemonCommandBridgeReady authenticates the bare phpX.Y form retained
// by historical generated units. It never rewrites or restarts a service.
// The bridge first checks the normal current command and calls this only when
// that comparison fails. All arguments must still match the recorded command.
func LegacyDaemonCommandBridgeReady(ctx context.Context, command string, properties map[string]string) error {
	return legacyDaemonCommandBridgeReady(command, properties, func(branch string) error {
		return resolveLegacyPHPCommandContext(ctx, branch)
	})
}

func legacyDaemonCommandBridgeReady(command string, properties map[string]string, resolve func(string) error) error {
	expected := strings.Fields(command)
	if len(expected) == 0 || !strings.HasPrefix(expected[0], "php") || !safeinput.ValidatePHPVersion(strings.TrimPrefix(expected[0], "php")) || strings.ContainsAny(command, "\\\"'\n\r") {
		return errors.New("not a supported legacy branch-specific PHP command")
	}
	if properties["User"] != "fluxo" && properties["User"] != "www-data" || properties["Type"] != "simple" || properties["Restart"] != "always" {
		return errors.New("not a supported historical PHP daemon service context")
	}
	for _, key := range []string{"DynamicUser", "PrivateUsers"} {
		if properties[key] != "" && properties[key] != "no" {
			return errors.New("legacy PHP service has custom identity selectors: " + key)
		}
	}
	for _, key := range []string{"Group", "AmbientCapabilities", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "DropInPaths", "Environment", "EnvironmentFiles", "ExecSearchPath", "PassEnvironment", "RootDirectory", "RootImage", "BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem", "MountImages", "ExtensionImages", "ExtensionDirectories"} {
		if properties[key] != "" {
			return errors.New("legacy PHP service has custom execution selectors: " + key)
		}
	}
	path, args, err := serviceCommand(properties["ExecStart"])
	if err != nil {
		return err
	}
	// Keep the declared argument text exact as well as checking the parsed
	// vector. Flattening whitespace alone can conceal changed argument values
	// (for example a non-breaking space replaced by an ordinary separator).
	if digest(args) != digest(expected) || !strings.Contains(properties["ExecStart"], " ; argv[]="+command+" ; ignore_errors=") {
		return errors.New("legacy PHP service arguments differ from the recorded command")
	}
	if path == expected[0] {
		return resolve(strings.TrimPrefix(expected[0], "php"))
	}
	if path != "/usr/bin/"+expected[0] {
		return errors.New("legacy PHP service uses a different interpreter")
	}
	return nil
}
