package maintenancetask

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These synthetic selectors are reviewed grammar fixtures. The customer's
// redacted selector/pattern was not available and is not qualified by them.
const certbotLineageHookFixture = `#!/bin/sh
set -eu
case "$RENEWED_LINEAGE" in
    /etc/letsencrypt/live/example.test)
        /usr/sbin/nginx -t && /usr/bin/systemctl reload nginx
        ;;
esac
`

func TestCertbotDirectoryHookReviewedConditionalFamilies(t *testing.T) {
	for _, row := range []struct{ selector, pattern, family string }{
		{`"$RENEWED_LINEAGE"`, "/etc/letsencrypt/live/example.test", "renewed-lineage-exact"},
		{`"${RENEWED_LINEAGE}"`, `"/etc/letsencrypt/live/example.test-0001"`, "renewed-lineage-exact"},
		{`"$RENEWED_LINEAGE"`, `'/etc/letsencrypt/live/example.test'`, "renewed-lineage-exact"},
		{`"$RENEWED_DOMAINS"`, "example.test", "renewed-domains-exact"},
		{`"${RENEWED_DOMAINS}"`, `"xn--example-9d0b.test"`, "renewed-domains-exact"},
		{`" $RENEWED_DOMAINS "`, `*" example.test "*`, "renewed-domains-member"},
		{`" ${RENEWED_DOMAINS} "`, `*" example.test "*`, "renewed-domains-member"},
	} {
		raw := "#!/bin/sh\n# reviewed local reload\n\nset -eu\ncase " + row.selector + " in " + row.pattern + ") /usr/sbin/nginx -t && /usr/bin/systemctl reload nginx ;; esac\n"
		for _, interpreter := range []string{"/bin/sh", "/usr/bin/sh"} {
			candidate := strings.Replace(raw, "#!/bin/sh", "#!"+interpreter, 1)
			hook, err := parseCertbotDirectoryHook([]byte(candidate))
			if err != nil || hook.family != row.family || hook.interpreter != interpreter {
				t.Fatalf("reviewed family rejected: %q: %+v %v", candidate, hook, err)
			}
		}
	}
}

func TestCertbotDirectoryHookRejectsShellExpansionAndDelegation(t *testing.T) {
	for _, replace := range [][2]string{
		{"#!/bin/sh", "#!/usr/bin/env sh"},
		{"#!/bin/sh", "#!/bin/bash"},
		{"#!/bin/sh", "#!/bin/sh -e"},
		{"set -eu\n", ""},
		{"set -eu", "set -eu; /root/other"},
		{"set -eu", "set -eu\nPATH=/root/bin"},
		{"set -eu", "set -eu\nexport LD_PRELOAD=/root/evil.so"},
		{"set -eu", "set -eu\nalias nginx=/root/evil"},
		{"set -eu", "set -eu\nnginx() { /root/evil; }"},
		{"set -eu", "set -eu\n. /root/other"},
		{`"$RENEWED_LINEAGE"`, "$RENEWED_LINEAGE"},
		{`"$RENEWED_LINEAGE"`, `"$OTHER_VARIABLE"`},
		{`"$RENEWED_LINEAGE"`, `"${RENEWED_LINEAGE:-$(/root/evil)}"`},
		{`"$RENEWED_LINEAGE"`, `"$(/root/evil)"`},
		{`"$RENEWED_LINEAGE"`, "\"`/root/evil`\""},
		{"/etc/letsencrypt/live/example.test)", "/etc/letsencrypt/live/*)"},
		{"/etc/letsencrypt/live/example.test)", "/etc/letsencrypt/live/../example.test)"},
		{"/etc/letsencrypt/live/example.test)", "/etc/letsencrypt/live/example.test|*)"},
		{"/etc/letsencrypt/live/example.test)", `"/etc/letsencrypt/live/$(/root/evil)")`},
		{"/usr/sbin/nginx -t", "nginx -t"},
		{"/usr/sbin/nginx -t", "/usr/sbin/nginx -t -c /root/custom.conf"},
		{"/usr/sbin/nginx -t", "env /usr/sbin/nginx -t"},
		{"/usr/sbin/nginx -t", "/usr/sbin/nginx -t >/root/out"},
		{"/usr/sbin/nginx -t", "/usr/sbin/nginx -t </root/in"},
		{"&&", ";"}, {"&&", "||"}, {"&&", "&"},
		{"/usr/bin/systemctl reload nginx", "systemctl reload nginx"},
		{"/usr/bin/systemctl reload nginx", "/usr/bin/systemctl restart nginx"},
		{"/usr/bin/systemctl reload nginx", "/usr/bin/systemctl reload nginx.service"},
		{"/usr/bin/systemctl reload nginx", "/usr/bin/systemctl reload nginx; /root/evil"},
		{"/usr/bin/systemctl reload nginx", "/usr/bin/systemctl reload $SERVICE"},
		{"/usr/bin/systemctl reload nginx", "exec /usr/bin/systemctl reload nginx"},
		{";;\nesac", ";;\n*) /root/evil ;;\nesac"},
		{"esac", "esac\n/root/evil"},
		{"esac", "esac # inline comment"},
		{"esac", "esac\x00"}, {"esac", "esac\r"},
		{"set -eu", "# comment\\\nset -eu"},
	} {
		raw := strings.Replace(certbotLineageHookFixture, replace[0], replace[1], 1)
		if raw == certbotLineageHookFixture {
			t.Fatal("invalid negative fixture", replace)
		}
		if _, err := parseCertbotDirectoryHook([]byte(raw)); err == nil {
			t.Fatalf("unsafe shell admitted: %q", raw)
		}
	}
	for _, raw := range []string{"", "#!/bin/sh", strings.Repeat("#", 8193), certbotLineageHookFixture + "\u00a0"} {
		if _, err := parseCertbotDirectoryHook([]byte(raw)); err == nil {
			t.Fatal("invalid script accepted")
		}
	}
}

func TestCertbotDirectoryHookDomainSelectorsRemainLiteralAndBounded(t *testing.T) {
	for _, row := range [][2]string{
		{`" $RENEWED_DOMAINS "`, `*" "*`}, // Must refuse without a short-pattern panic.
		{`" $RENEWED_DOMAINS "`, `*example.test*`},
		{`" $RENEWED_DOMAINS "`, `*"example.test"*`},
		{`"$RENEWED_DOMAINS"`, `*" example.test "*`},
		{`" $RENEWED_DOMAINS "`, `*" example.test other.test "*`},
		{`" $RENEWED_DOMAINS "`, `*" example.test$(id) "*`},
		{`"$RENEWED_DOMAINS"`, `example.test|other.test`},
		{`"$RENEWED_DOMAINS"`, `-example.test`},
		{`"$RENEWED_DOMAINS"`, `example..test`},
		{`"$RENEWED_DOMAINS"`, `*.example.test`},
		{`"$RENEWED_DOMAINS"`, `"${OTHER}"`},
		{`"$RENEWED_DOMAINS"`, strings.Repeat("x", 64) + ".test"},
	} {
		raw := "#!/bin/sh\nset -eu\ncase " + row[0] + " in " + row[1] + ") /usr/sbin/nginx -t && /usr/bin/systemctl reload nginx ;; esac\n"
		if _, err := parseCertbotDirectoryHook([]byte(raw)); err == nil {
			t.Fatal("unsupported selector/pattern accepted", row)
		}
	}
}

type certbotHookFileInfo struct {
	mode os.FileMode
	stat syscall.Stat_t
}

func (i certbotHookFileInfo) Name() string       { return "reload-nginx" }
func (i certbotHookFileInfo) Size() int64        { return i.stat.Size }
func (i certbotHookFileInfo) Mode() os.FileMode  { return i.mode }
func (i certbotHookFileInfo) ModTime() time.Time { return time.Time{} }
func (i certbotHookFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i certbotHookFileInfo) Sys() any           { return &i.stat }

func certbotProtectedHookInfo() certbotHookFileInfo {
	return certbotHookFileInfo{mode: 0750, stat: syscall.Stat_t{Uid: 0, Gid: 0, Nlink: 1, Size: int64(len(certbotLineageHookFixture)), Ino: 10}}
}

func TestCertbotDirectoryHookRefusesUnsafeModeOwnerAndLinks(t *testing.T) {
	if err := validateCertbotHookMode(certbotProtectedHookInfo()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*certbotHookFileInfo){
		func(i *certbotHookFileInfo) { i.stat.Uid = 1000 },
		func(i *certbotHookFileInfo) { i.stat.Gid = 1000 },
		func(i *certbotHookFileInfo) { i.stat.Nlink = 2 },
		func(i *certbotHookFileInfo) { i.stat.Nlink = 0 },
		func(i *certbotHookFileInfo) { i.mode = 0770 },
		func(i *certbotHookFileInfo) { i.mode = 0752 },
		func(i *certbotHookFileInfo) { i.mode = 0640 },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeSymlink },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeDir },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeNamedPipe },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeSetuid },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeSetgid },
		func(i *certbotHookFileInfo) { i.mode |= os.ModeSticky },
	} {
		info := certbotProtectedHookInfo()
		change(&info)
		if err := validateCertbotHookMode(info); err == nil {
			t.Fatal("unsafe hook metadata accepted", info)
		}
	}
}

func TestCertbotDirectoryHookBindsCompletePathAndBytes(t *testing.T) {
	path := certbotDeployHookDirectory + "/50-reload-nginx"
	p := newProfile(CertificateRenewal)
	read := func(got string, limit int64) ([]byte, error) {
		if got != path || limit != 8192 {
			t.Fatal("unbounded or wrong hook read", got, limit)
		}
		return []byte(certbotLineageHookFixture), nil
	}
	stat := func(string) (os.FileInfo, error) { return certbotProtectedHookInfo(), nil }
	hook, err := inspectCertbotHookFile(&p, path, read, stat)
	if err != nil || hook.family != "renewed-lineage-exact" || p.Files[path] != digest([]byte(certbotLineageHookFixture)) || p.External["certbot-directory-hook:"+path] != "renewed-lineage-exact:/bin/sh" {
		t.Fatal("hook evidence incomplete", p, err)
	}
	for _, unsafe := range []string{"/root/reload-nginx", certbotDeployHookDirectory + "/../deploy/reload", certbotDeployHookDirectory + "/$(id)", certbotDeployHookDirectory + "/reload;id", certbotDeployHookDirectory + "/reload nginx", certbotDeployHookDirectory + "/'reload'", certbotDeployHookDirectory + "/.hidden"} {
		if _, err = inspectCertbotHookFile(&p, unsafe, nil, nil); err == nil {
			t.Fatal("unsafe hook filename accepted", unsafe)
		}
	}
	readFailure := errors.New("unsafe ancestor, symlink or changed input")
	if _, err = inspectCertbotHookFile(&p, path, func(string, int64) ([]byte, error) { return nil, readFailure }, stat); !errors.Is(err, readFailure) {
		t.Fatal("protected-read failure hidden", err)
	}
	calls := 0
	_, err = inspectCertbotHookFile(&p, path, read, func(string) (os.FileInfo, error) {
		info := certbotProtectedHookInfo()
		calls++
		if calls == 2 {
			info.stat.Ino++
		}
		return info, nil
	})
	if err == nil {
		t.Fatal("hook replacement during read accepted")
	}
}

func TestCertbotHookEvidenceNeverBecomesRenewalDataOrHistoricalAuthority(t *testing.T) {
	makeProfile := func() profile {
		p := newProfile(CertificateRenewal)
		p.Files[certbotDeployHookDirectory+"/reload"] = "full-script-digest"
		p.Files["/usr/bin/dash"] = "shell-payload"
		p.Files["/usr/bin/systemctl"] = "systemctl-payload"
		p.Files["/etc/nginx/nginx.conf"] = "nginx-config"
		p.Directories[certbotDeployHookDirectory] = []string{"reload"}
		p.External["certbot-platform-profile"] = "ubuntu-22.04:certbot-1.21.0:python-3.10"
		p.External["certbot-directory-hook-contract"] = certbotDirectoryHookContract
		p.External["certbot-directory-hook:"+certbotDeployHookDirectory+"/reload"] = "renewed-lineage-exact:/bin/sh"
		p.External["certbot-hook-link:/bin"] = "usr/bin"
		p.External["package:dash"] = "verified-version"
		p.External["nginx-master"] = "boot:pid:start"
		return p
	}
	before := makeProfile()
	if !compatibleProfiles(before, makeProfile(), false, false) || legacyV1Profile(before) {
		t.Fatal("current hook profile identity changed")
	}
	for key := range before.Files {
		after := makeProfile()
		after.Files[key] = "changed"
		if compatibleProfiles(before, after, false, false) || compatibleRecoveryObservations(before, after, false) {
			t.Fatal("protected hook input became mutable data", key)
		}
	}
	for key := range before.External {
		after := makeProfile()
		delete(after.External, key)
		if compatibleProfiles(before, after, false, false) || compatibleRecoveryObservations(before, after, true) {
			t.Fatal("missing current hook observation accepted", key)
		}
	}
	after := makeProfile()
	after.Directories[certbotDeployHookDirectory] = append(after.Directories[certbotDeployHookDirectory], "unknown-hook")
	if compatibleProfiles(before, after, false, false) {
		t.Fatal("new unqualified hook accepted")
	}
	after = makeProfile()
	after.Files["/etc/letsencrypt/renewal/example.test.conf"] = "renewed-data"
	if !compatibleProfiles(before, after, false, false) {
		t.Fatal("ordinary validated renewal data exception changed")
	}
}
