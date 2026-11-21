package maintenancetask

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fluxo/internal/services/executiondomain"
)

const manualLineageFixture = `version = 2.9.0
archive_dir = /etc/letsencrypt/archive/wildcard.example.test
cert = /etc/letsencrypt/live/wildcard.example.test/cert.pem
privkey = /etc/letsencrypt/live/wildcard.example.test/privkey.pem
chain = /etc/letsencrypt/live/wildcard.example.test/chain.pem
fullchain = /etc/letsencrypt/live/wildcard.example.test/fullchain.pem
[renewalparams]
account = exampleaccount
authenticator = manual
pref_challs = dns-01,
server = https://acme-v02.api.letsencrypt.org/directory
key_type = ecdsa
`

func TestManualCertbotCurrentOnlyNoExecutableHooks(t *testing.T) {
	for _, extra := range []string{"", "manual_public_ip_logging_ok = True\n", "manual_auth_hook = ''\nmanual_cleanup_hook = \"\"\n", "deploy_hook = systemctl reload nginx\n"} {
		raw := []byte(manualLineageFixture + extra)
		if manual, err := validateCurrentCertbotConfig(raw, true); err != nil || !manual {
			t.Fatal("hook-free manual lineage was not recognized", extra, manual, err)
		}
		if err := validateCertbotConfig(raw, true); err == nil {
			t.Fatal("historical V1 observation acquired manual execution support")
		}
	}
	for _, extra := range []string{
		"manual_auth_hook = /root/auth.sh", "manual-cleanup-hook = /root/cleanup.sh",
		"manual_auth_hook = None", "manual_auth_hook = node /root/auth.js",
		"pre_hook = /root/before.sh", "post_hook = /root/after.sh", "installer = nginx",
		"deploy_hook = systemctl reload nginx && node job.js", "manual_public_ip_logging_ok = /root/code",
		"authenticator = dns-cloudflare", "config_dir = /tmp/config", "work_dir = /tmp/checkpoints",
		"manual_auth_hook =\nmanual-auth-hook = /root/injected.sh", "account = %(payload)s",
	} {
		if _, err := validateCurrentCertbotConfig([]byte(manualLineageFixture+extra+"\n"), true); err == nil {
			t.Fatal("manual compatibility accepted extra execution or ambiguous input", extra)
		}
	}
	for _, raw := range []string{"authenticator = manual\n", "[renewalparams]\nauthenticator = manual\n"} {
		if _, err := validateCurrentCertbotConfig([]byte(raw), false); err == nil {
			t.Fatal("global defaults selected a new manual plugin")
		}
	}
	if manual, err := validateCurrentCertbotConfig([]byte("[renewalparams]\nauthenticator = webroot\nwebroot_path = /var/www/html,\n"), true); err != nil || manual {
		t.Fatal("existing webroot profile changed", manual, err)
	}
}

func TestManualCertbotNeverAuthorizesCheckpointRecovery(t *testing.T) {
	work := "/var/lib/letsencrypt"
	for _, scenario := range []string{"absent", "temp_checkpoint", "IN_PROGRESS", "untrusted-parent", "unreadable-state", "unsafe-backups"} {
		t.Run(scenario, func(t *testing.T) {
			p := newProfile(CertificateRenewal)
			var inspected []string
			err := inspectManualCertbotReverter(&p, work, func(path string) error {
				if path != work {
					t.Fatal(path)
				}
				if scenario == "untrusted-parent" {
					return os.ErrPermission
				}
				return nil
			}, func(path string) (os.FileInfo, error) {
				inspected = append(inspected, path)
				if filepath.Base(path) == scenario {
					return nil, nil
				}
				if scenario == "unreadable-state" {
					return nil, os.ErrPermission
				}
				return nil, os.ErrNotExist
			}, func(path string, optional bool) ([]os.DirEntry, error) {
				if path != filepath.Join(work, "backups") || !optional {
					t.Fatal("unexpected state access", path)
				}
				if scenario == "unsafe-backups" {
					return nil, os.ErrPermission
				}
				p.Directories[path] = nil
				return nil, nil
			})
			if scenario == "absent" {
				if err != nil || len(inspected) != 2 || len(p.Files) != 2 || len(p.Directories) != 1 {
					t.Fatal(p, inspected, err)
				}
				for _, path := range inspected {
					if p.Files[path] != "absent" {
						t.Fatal("absence not bound", path)
					}
				}
			} else if err == nil {
				t.Fatal("unfinished or untrusted reverter state admitted", scenario)
			}
		})
	}
}

func TestManualCertbotRequiresActualNewHelper(t *testing.T) {
	p := newProfile(CertificateRenewal)
	p.External[certbotManualProfileKey] = certbotManualNoHooksContract
	old := HelperCapabilities()
	old.RoleContracts[optionalProfileRole] = []string{imagickStartupProfile, certbotDirectoryHookContract, certbotDirectoryHookExtendedContract}
	if executiondomain.RequireCapabilities(old, profileHelperRequirements(p)) == nil {
		t.Fatal("old v2-hook helper authorized manual semantics")
	}
	if err := executiondomain.RequireCapabilities(HelperCapabilities(), profileHelperRequirements(p)); err != nil {
		t.Fatal(err)
	}
	if err := requireProfileHelper(context.Background(), "/unavailable-helper", p); err == nil {
		t.Fatal("panel substituted for actual retained helper")
	}
	j := journal{Entries: map[int]registration{3: {Contract: Contract{ID: 3, Kind: CertificateRenewal}, Enabled: true}}}
	for _, needManual := range []bool{false, true} {
		ids, err := inspectOptionalProfileHelpers(context.Background(), j, func(context.Context, registration) (executiondomain.ReaderCapabilities, error) { return old, nil }, func(context.Context, Contract) (profile, error) {
			if needManual {
				return p, nil
			}
			return newProfile(CertificateRenewal), nil
		})
		if err != nil || needManual && !reflect.DeepEqual(ids, []int{3}) || !needManual && len(ids) != 0 {
			t.Fatal(ids, err)
		}
	}
	if len(executiondomain.Capabilities().RoleContracts[optionalProfileRole]) != 0 {
		t.Fatal("optional feature changed global reader requirements")
	}
}
