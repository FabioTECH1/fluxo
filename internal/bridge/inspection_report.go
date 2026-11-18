package bridge

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/scheduler"
)

type readinessProbe struct {
	name     string
	requires []string
	run      func() error
}

// Collect independent failures without invoking checks whose trust prerequisites
// failed. This is advisory inspection only; Certify/Require keep their existing
// strict validation and admission sequence and never consume this report.
func collectReadiness(probes []readinessProbe) []Check {
	return collectReadinessAfter(probes, nil)
}

func collectReadinessAfter(probes []readinessProbe, prerequisites map[string]bool) []Check {
	results := map[string]bool{}
	for name, passed := range prerequisites {
		results[name] = passed
	}
	checks := make([]Check, 0, len(probes))
	for _, probe := range probes {
		c := Check{Component: probe.name}
		var blocked []string
		for _, requirement := range probe.requires {
			if !results[requirement] {
				blocked = append(blocked, requirement)
			}
		}
		if len(blocked) != 0 {
			c.Blocked = true
			c.Action = "not evaluated; requires passing " + strings.Join(blocked, ", ")
		} else if err := probe.run(); err != nil {
			c.Action = err.Error()
		} else {
			c.Passed = true
		}
		results[probe.name] = c.Passed
		checks = append(checks, c)
	}
	return checks
}

func readinessFailures(checks []Check) error {
	var failures []error
	for _, c := range checks {
		if !c.Passed {
			failures = append(failures, fmt.Errorf("%s: %s", c.Component, c.Action))
		}
	}
	return errors.Join(failures...)
}

func inspectAll(ctx context.Context, q queryer, dataDir string) Report {
	r := newReport()
	var key []byte
	var host []Check
	var schemaOK bool
	r.Checks = collectReadiness([]readinessProbe{
		{"execution_readers", nil, func() error { _, err := executiondomain.InspectCompatibility(dataDir); return err }},
		{"schema", []string{"execution_readers"}, func() error {
			err := checkSchemaMode(ctx, q, true)
			schemaOK = err == nil
			return err
		}},
		{"installation_identity", []string{"schema"}, func() error {
			var id string
			if err := q.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id); err != nil {
				return errors.New("preserve or recover the existing installation identity")
			}
			decoded, err := hex.DecodeString(id)
			if err != nil || len(decoded) != 16 || id != strings.ToLower(id) {
				return errors.New("preserve or recover the existing installation identity")
			}
			r.InstallationID = id
			return nil
		}},
		{"encryption_key", nil, func() error {
			var err error
			key, err = readKey(dataDir)
			if err == nil {
				r.keyIdentity = digest(key)
			}
			return err
		}},
		{"credentials", []string{"schema", "encryption_key"}, func() error { return checkSecrets(ctx, q, key) }},
		{"historical_conversions", []string{"schema"}, func() error { return checkConversionsMode(ctx, q, true) }},
		{"pending_operations", []string{"schema"}, func() error { return checkPendingMode(ctx, q, true) }},
		{"host", []string{"execution_readers"}, func() error {
			host = inspectHostDetails(ctx, q, dataDir, key, schemaOK)
			return readinessFailures(host)
		}},
	})
	for i := range r.Checks {
		if r.Checks[i].Component == "host" {
			r.Checks[i].Details = host
		}
	}
	r.finish()
	return r
}

func inspectHostDetails(ctx context.Context, q queryer, dataDir string, key []byte, schemaOK bool) []Check {
	var environment string
	service := func() error {
		out, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=LoadState,NeedDaemonReload,User,DynamicUser,RootDirectory,RootImage,BindPaths,BindReadOnlyPaths,TemporaryFileSystem,MountImages,ExtensionImages,ExtensionDirectories,InaccessiblePaths,ReadOnlyPaths,ReadWritePaths,ProtectHome,ProtectSystem,PrivateUsers,FragmentPath,DropInPaths,ExecStart,Environment,EnvironmentFiles,PassEnvironment,UnsetEnvironment")
		if err != nil {
			return errors.New("cannot read the installed panel service context")
		}
		p := properties(out)
		if err = serviceContext(p, dataDir, os.Getenv("FLUXO_USE_HTTP") == "1"); err != nil {
			return err
		}
		unit, err := readProtected(p["FragmentPath"], 1<<20)
		if err != nil {
			return err
		}
		if err = supportedServiceFile(string(unit)); err != nil {
			return err
		}
		environment = p["Environment"]
		return nil
	}
	protected := func() error {
		var failures []error
		for _, path := range []string{"/etc/systemd/system/fluxo.service", "/etc/sudoers.d/fluxo", "/etc/fail2ban/filter.d/fluxo-auth.conf", "/etc/fail2ban/jail.d/fluxo-auth.conf"} {
			if _, err := readProtected(path, 1<<20); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	deps := []string{"service", "protected_files"}
	dataDeps := []string{"service", "protected_files", "schema"}
	return collectReadinessAfter([]readinessProbe{
		{"service", nil, service},
		{"protected_files", []string{"service"}, protected},
		{"security", deps, func() error { return checkSecurity(ctx, environment) }},
		{"historical_credentials", []string{"service"}, func() error {
			if _, err := os.Lstat("/home/fluxo/.fluxo_credentials"); !os.IsNotExist(err) {
				return errors.New("resolve the historical credentials file before certification")
			}
			return nil
		}},
		{"ssh_syntax", []string{"security"}, func() error {
			_, err := run(ctx, "/usr/sbin/sshd", "-T")
			if err != nil {
				return errors.New("verify sshd configuration before certification")
			}
			return nil
		}},
		{"sudo_syntax", []string{"security"}, func() error {
			_, err := run(ctx, "/usr/sbin/visudo", "-c")
			if err != nil {
				return errors.New("verify visudo configuration before certification")
			}
			return nil
		}},
		{"package_state", deps, func() error {
			out, err := run(ctx, "/usr/bin/dpkg", "--audit")
			if err != nil {
				return errors.New("verify dpkg configuration before certification")
			}
			if strings.TrimSpace(out) != "" {
				return errors.New("resolve incomplete dpkg state before certification")
			}
			return nil
		}},
		{"php_repository", deps, func() error { return phpupdate.RepositoryBridgeReady(ctx, dataDir) }},
		{"scheduler", deps, func() error { return scheduler.BridgeReady(ctx, filepath.Join(dataDir, "fluxo.db")) }},
		{"guarded_jobs", deps, func() error { return maintenancetask.BridgeReady(ctx) }},
		{"sites", dataDeps, func() error { return checkSites(ctx, q) }},
		{"daemons", dataDeps, func() error { return checkDaemons(ctx, q) }},
		{"crons", dataDeps, func() error { return checkCrons(ctx, q, dataDir) }},
		{"certificates", dataDeps, func() error { return checkCertificates(ctx, q, dataDir) }},
		{"database_access", []string{"service", "protected_files", "schema", "encryption_key"}, func() error { return checkDatabaseAccess(ctx, q, key) }},
		{"node_runtime", deps, func() error {
			if _, err := os.Lstat("/var/lib/fluxo/node-toolchain.json"); err == nil {
				if err = nodetoolchain.BridgeReady(); err != nil {
					return errors.New("finish reviewed managed Node runtime qualification")
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		}},
	}, map[string]bool{"schema": schemaOK, "encryption_key": len(key) == 32})
}
