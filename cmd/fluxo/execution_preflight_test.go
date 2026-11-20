package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
)

func TestInstallerGitHubLinksKeepsWriterPreflightAndExactArguments(t *testing.T) {
	if !needsExecutionPreflight([]string{"installer-github-links"}, "prod") {
		t.Fatal("GitHub metadata mutation bypassed reader admission")
	}
	if needsExecutionPreflight([]string{"--installer-github-links-support"}, "prod") {
		t.Fatal("pure capability query requires mutable initialization")
	}
	for _, arg := range []string{"--installer-github-links-support=true", "-installer-github-links-support", "--installer-github-links-support=false"} {
		if !needsExecutionPreflight([]string{arg}, "prod") {
			t.Fatal("unsupported capability spelling bypassed admission", arg)
		}
	}
	for _, args := range [][]string{{"installer-github-links", "unexpected"}, {"--installer-github-links-support", "unexpected"}} {
		if validateFixedCommandArguments(args) == nil {
			t.Fatal("malformed internal command accepted", args)
		}
	}
}

func TestExecutionPreflightCoversMutationEntrypoints(t *testing.T) {
	for _, command := range []string{"tinker-execute", "system-task-reconcile", "system-task-application", "system-task-execute", "system-task-dispatch", "system-task-receipt", "node-update-worker", "scheduler-installer-verify", "scheduler-verify-panel", "scheduler-installer-policy", "scheduler-execute", "scheduler-installer-release", "scheduler-installer-lease", "scheduler-dispatch", "scheduler-migration", "php-repository-setup", "php-update-worker", "php-update-transaction", "node-toolchain", "python-toolchain", "--reset-token"} {
		if !needsExecutionPreflight([]string{command}, "") {
			t.Fatal("installed mutation can bypass reader inspection", command)
		}
	}
	if !needsExecutionPreflight(nil, "prod") || !needsExecutionPreflight(nil, "dev") || !needsExecutionPreflight([]string{"--version=false"}, "prod") {
		t.Fatal("startup/metadata classification changed meaning")
	}
	for _, args := range [][]string{{"--version"}, {"--execution-capabilities"}, {"--scheduler-support"}, {"--system-task-support"}, {"--scheduler-required-support"}, {"--system-task-required-support"}, {"--nginx-http-defaults-recovery-ready"}, {"baseline-bridge", "inspect"}} {
		if needsExecutionPreflight(args, "prod") {
			t.Fatal("pure metadata became unavailable during recovery", args)
		}
	}
}

func TestExecutionCapabilitiesDoNotInitializeProductionState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", dir)
	previous := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = previous }()
	if !executionMetadata([]string{"--execution-capabilities"}) {
		t.Fatal("capability branch did not return")
	}
	w.Close()
	raw, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	var c executiondomain.ReaderCapabilities
	if err = json.Unmarshal(raw, &c); err != nil || c.Format != 1 || !c.Activation || c.SchedulerSupport != executiondomain.SchedulerCompiledReaderSupport || c.SystemTaskSupport != executiondomain.SystemTaskReaderSupport {
		t.Fatal("unsupported capability claim", string(raw), err)
	}
	if _, err = os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("pure capability query initialized a production directory", err)
	}
}

func TestExecutionPreflightResolvesInstalledDirectoryWithoutEnvironmentExemption(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "installed")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "execution-readers.json"), []byte(`{"phase":"prepared"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"", "dev", "prod"} {
		t.Run("environment="+environment, func(t *testing.T) {
			t.Setenv("FLUXO_ENV", environment)
			t.Setenv("FLUXO_DATA_DIR", data)
			dirs, err := executionDataDirectories()
			if err != nil || len(dirs) != 2 || dirs[1] != data {
				t.Fatal("selected reader floor escaped inspection", dirs, err)
			}
			for _, args := range [][]string{nil, {"--version=false"}, {"--version", "--reset-token"}} {
				if !needsExecutionPreflight(args, environment) {
					t.Fatal("mutable startup escaped inspection", args)
				}
			}
		})
	}
	t.Chdir(root)
	t.Setenv("FLUXO_ENV", "dev")
	t.Setenv("FLUXO_DATA_DIR", "installed")
	dirs, err := executionDataDirectories()
	if err != nil || len(dirs) != 2 || dirs[1] != data {
		t.Fatal("relative installed path escaped inspection", dirs, err)
	}
	t.Setenv("FLUXO_DATA_DIR", "fresh-workspace")
	dirs, err = executionDataDirectories()
	if err != nil || len(dirs) != 1 {
		t.Fatal("fresh workspace incorrectly required an existing root floor", dirs, err)
	}
	if _, err = os.Stat(filepath.Join(root, "fresh-workspace")); !os.IsNotExist(err) {
		t.Fatal("inspection initialized workspace", err)
	}
}

func TestNginxRecoveryMetadataIsPureAndPreservesFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", dir)
	var out bytes.Buffer
	calls := 0
	ready := func() error { calls++; return nil }
	handled, err := nginxRecoveryMetadata([]string{"--nginx-http-defaults-recovery-ready"}, ready, &out)
	if !handled || err != nil || calls != 1 || out.String() != "1\n" {
		t.Fatal(handled, err, calls, out.String())
	}
	pending := errors.New("pending HTTP defaults recovery")
	out.Reset()
	handled, err = nginxRecoveryMetadata([]string{"--nginx-http-defaults-recovery-ready"}, func() error { return pending }, &out)
	if !handled || !errors.Is(err, pending) || out.Len() != 0 {
		t.Fatal("pending recovery was reported ready", handled, err, out.String())
	}
	if _, err = os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("metadata initialized production configuration", err)
	}
}

func TestMalformedFixedCommandsRefuseBeforeReaderOrDatabaseInitialization(t *testing.T) {
	commands := []struct {
		name      string
		arguments int
	}{
		{"installer-github-links", 0}, {"--installer-github-links-support", 0},
		{"installer-runtime-state", 0}, {"system-task-diagnostics", 0}, {"system-task-reconcile", 0}, {"system-task-apt-refuse", 0},
		{"system-task-application", 1}, {"system-task-execute", 1}, {"system-task-dispatch", 2}, {"system-task-receipt", 2},
		{"scheduler-installer-verify", 0}, {"scheduler-diagnostics", 0}, {"scheduler-verify-panel", 0}, {"scheduler-installer-policy", 1},
		{"scheduler-installer-verify-upgrade", 1},
		{"scheduler-execute", 1}, {"scheduler-installer-release", 1}, {"scheduler-installer-lease", 1}, {"scheduler-migration", 2}, {"php-repository-setup", 0},
	}
	for _, command := range commands {
		for count := 0; count <= command.arguments+1; count++ {
			if count == command.arguments {
				continue
			}
			t.Run(command.name+"/"+string(rune('0'+count)), func(t *testing.T) {
				directory := t.TempDir()
				// Even a lost/corrupt private reader floor must not turn invalid syntax
				// into reader admission or daemon startup. A regressed guard remains safe:
				// the deliberately invalid floor blocks initialization in this subprocess.
				floor := []byte("invalid reader evidence\n")
				if err := os.WriteFile(filepath.Join(directory, "execution-readers.json"), floor, 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{command.name}
				for n := 0; n < count; n++ {
					args = append(args, "invalid")
				}
				raw, _ := json.Marshal(args)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMalformedFixedCommandChild$")
				child.Env = append(os.Environ(), "FLUXO_CLI_BAD_ARGV="+string(raw), "FLUXO_ENV=prod", "FLUXO_DATA_DIR="+directory)
				var stdout, stderr bytes.Buffer
				child.Stdout = &stdout
				child.Stderr = &stderr
				err := child.Run()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 2 {
					t.Fatal("malformed command did not exit with usage status before reader admission")
				}
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 1 || entries[0].Name() != "execution-readers.json" {
					t.Fatal("malformed command initialized installation state", err)
				}
				after, err := os.ReadFile(filepath.Join(directory, "execution-readers.json"))
				if err != nil || !bytes.Equal(after, floor) {
					t.Fatal("malformed command changed reader evidence", err)
				}
			})
		}
	}
}

func TestMalformedFixedCommandChild(t *testing.T) {
	request := os.Getenv("FLUXO_CLI_BAD_ARGV")
	if request == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(request), &args); err != nil {
		os.Exit(90)
	}
	os.Args = append([]string{"fluxo"}, args...)
	main()
	os.Exit(91)
}
