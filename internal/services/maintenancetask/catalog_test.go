package maintenancetask

import "testing"

func TestCatalogRequiresExactGlobalRootContract(t *testing.T) {
	for _, command := range []string{PackageCleanupCommand, CertificateRenewalCommand, ComposerUpdateCommand} {
		for _, expr := range []string{"0 0 * * 0", "0 */12 * * *"} {
			if c, ok := Qualify(1, 0, expr, command, "root"); !ok || c.Command != command || c.Expression != expr {
				t.Fatal(c, ok)
			}
		}
		for _, tc := range []struct {
			id, site        int
			expr, cmd, user string
		}{{1, 1, "* * * * *", command, "root"}, {1, 999, "* * * * *", command, "root"}, {1, 0, "* * * * *", command, "fluxo"}, {0, 0, "* * * * *", command, "root"}, {1, 0, "invalid", command, "root"}, {1, 0, "* * * * *", command + " && node job.js", "root"}, {1, 0, "* * * * *", "/bin/sh -c " + command, "root"}} {
			if _, ok := Qualify(tc.id, tc.site, tc.expr, tc.cmd, tc.user); ok {
				t.Fatal("unsupported task accepted", tc)
			}
		}
	}
}
