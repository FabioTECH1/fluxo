package maintenancetask

import "testing"

func TestApplicationContractIsIndependentOfNameAndExecutable(t *testing.T) {
	for _, cmd := range []string{"/usr/local/bin/node /home/fluxo/example/job.js", "php8.4 artisan custom:command", "python3 report.py && printf done", "/home/fluxo/example/run-report.sh"} {
		c, ok := QualifyApplication(1, 2, "/home/fluxo/example/current", "*/5 * * * *", cmd, "fluxo")
		if !ok || !validContract(c) || c.Command != cmd || c.Directory != "/home/fluxo/example/current" {
			t.Fatal("ordinary manual job rejected", cmd, c)
		}
		if _, ok = QualifyApplication(1, 2, c.Directory, c.Expression, cmd, "root"); ok {
			t.Fatal("arbitrary root code adopted")
		}
	}
	if _, ok := QualifySite(1, 999, "", "", "", "", "* * * * *", "echo hello", "fluxo"); ok {
		t.Fatal("orphan became a global task")
	}
	if _, ok := QualifyApplication(1, 2, "/home/fluxo/example/../../root", "* * * * *", "echo hello", "fluxo"); ok {
		t.Fatal("unbound directory accepted")
	}
}

func TestCronCommandPreservesPercentInput(t *testing.T) {
	for _, tc := range []struct{ raw, command, input string }{
		{"date +\\%F", "date +%F", ""},
		{"cat%first%second", "cat", "first\nsecond\n"},
		{"cat%a\\%b%", "cat", "a%b\n"},
		{"printf done", "printf done", ""},
		{`cat\\%data`, `cat\\`, "data\n"},
		{`cat\\\%literal`, `cat\\%literal`, ""},
		{`cat%a\\%b`, "cat", "a\\%b\n"},
		{`cat%\`, "cat", `\`},
		{`cat%`, "cat", ""},
	} {
		cmd, input := cronCommand(tc.raw)
		if cmd != tc.command || input != tc.input {
			t.Fatalf("%q => %q / %q", tc.raw, cmd, input)
		}
	}
}

func TestProfilesPermitOnlyDeclaredDataEffects(t *testing.T) {
	a, b := newProfile(ComposerUpdate), newProfile(ComposerUpdate)
	a.Files["/usr/local/bin/composer"] = "old"
	b.Files["/usr/local/bin/composer"] = "new"
	if compatibleProfiles(a, b, false, false) {
		t.Fatal("unconfirmed replacement accepted")
	}
	if !compatibleProfiles(a, b, true, false) {
		t.Fatal("successful authenticated self-update rejected")
	}
	b.Files["/usr/bin/php8.4"] = "foreign"
	if compatibleProfiles(a, b, true, false) {
		t.Fatal("interpreter drift hidden by Composer data allowance")
	}
	a, b = newProfile(CertificateRenewal), newProfile(CertificateRenewal)
	a.External["nginx-master"] = "oldboot:123:100"
	b.External["nginx-master"] = "newboot:456:200"
	if compatibleProfiles(a, b, false, false) {
		t.Fatal("same-boot nginx replacement accepted")
	}
	if !compatibleProfiles(a, b, false, true) {
		t.Fatal("separately verified new-boot nginx identity rejected")
	}
}
