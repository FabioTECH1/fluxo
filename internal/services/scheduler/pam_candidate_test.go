package scheduler

import (
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPAMCandidateStockFakerootLoaderDeclaration(t *testing.T) {
	a, err := nativePAMArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	name := "fakeroot-" + a.triplet + ".conf"
	directory := "/usr/lib/" + a.triplet + "/libfakeroot"
	for _, raw := range []string{directory + "\n", "# packaged declaration\n" + directory + "\n"} {
		paths, err := pamCandidateLoaderDirectories(name, []byte(raw))
		if err != nil || len(paths) != 1 || paths[0] != directory {
			t.Fatal("stock declaration refused", err)
		}
	}
	for _, input := range []struct{ name, raw string }{
		{name, directory + "/../override\n"}, {name, directory + "\n/usr/local/lib\n"},
		{name, directory + "\n" + directory}, {name, "/usr/lib\n"}, {name, ""},
		{"custom.conf", directory}, {"fakeroot-foreign-linux-gnu.conf", directory},
		{name, "/home/fluxo/libfakeroot"}, {name, "include /tmp/*.conf"},
	} {
		if _, err := pamCandidateLoaderDirectories(input.name, []byte(input.raw)); err == nil {
			t.Fatal("unreviewed declaration accepted", input.name, input.raw)
		}
	}
	if pamCandidateLibraryDirectory(directory) {
		t.Fatal("fakeroot path leaked into generic loader/RPATH allowance")
	}
}

func TestPAMCandidateNoCapabilityGrantPolicy(t *testing.T) {
	for _, raw := range []string{"", "# stock comments\n\n", "  # retained comments\n"} {
		if !pamCandidateCommentsOnly([]byte(raw)) {
			t.Fatal("inert policy refused")
		}
	}
	for _, raw := range []string{"cap_net_admin fluxo", "all *", "none *\ncap_setuid fluxo"} {
		if pamCandidateCommentsOnly([]byte(raw)) {
			t.Fatal("capability grant interpreted as inert policy")
		}
	}

	for _, raw := range []string{"none  *", "# stock policy\nnone\t*\n"} {
		if !pamCandidateCapabilityPolicy([]byte(raw)) {
			t.Fatal("clearing capability policy refused")
		}
	}
	for _, raw := range []string{"all *", "none root", "none *\nnone *", "none *\ncap_setuid fluxo"} {
		if pamCandidateCapabilityPolicy([]byte(raw)) {
			t.Fatal("unreviewed capability rule accepted")
		}
	}

	if err := validatePAMCandidateFallback("/etc/pam.d/common-session", []byte("session [default=1] pam_permit.so\nsession requisite pam_deny.so\nsession required pam_permit.so\nsession optional pam_umask.so\nsession required pam_unix.so\nsession optional pam_systemd.so\n")); err != nil {
		t.Fatal(err)
	}
	if err := validatePAMCandidateFallback("/etc/pam.d/common-password", []byte("password [success=1 default=ignore] pam_unix.so obscure yescrypt\npassword requisite pam_deny.so\npassword required pam_permit.so\n")); err != nil {
		t.Fatal(err)
	}
	// The active common-auth stack is hash-pinned separately. The fallback parser
	// must never admit another module/argument route to keepcaps or exec hooks.
	for _, raw := range []string{"auth optional pam_cap.so keepcaps", "session optional pam_exec.so /root/helper", "session optional /tmp/module.so", "@include malicious", "session [success=99 default=ignore] pam_unix.so", "auth optional pam_systemd.so"} {
		if validatePAMCandidateFallback("/etc/pam.d/common-session", []byte(raw)) == nil {
			t.Fatal("unreviewed fallback hook accepted", raw)
		}
	}
}

func TestPAMCapabilityPolicyMatchesVendorRecordBound(t *testing.T) {
	for _, length := range []int{4094, 4095, 4096} {
		comment := "#" + strings.Repeat(" ", length-1)
		for _, suffix := range []string{"", "\n", "\nnone *\n"} {
			if got := pamCandidateCapabilityPolicy([]byte(comment + suffix)); got != (length <= 4094) {
				t.Fatalf("physical record length %d accepted=%v", length, got)
			}
		}
	}
	for _, raw := range []string{
		"#" + strings.Repeat(" ", 4094) + "cap_setuid *\n",
		"# ignored\x00cap_setuid *\n",
		"none\u00a0*\n",
		"none\v*\n",
		"none\f*\n",
		"none *\r\n",
		"none *#comment\n",
	} {
		if pamCandidateCapabilityPolicy([]byte(raw)) {
			t.Fatal("vendor record/whitespace mismatch accepted")
		}
	}
	if !pamCandidateCapabilityPolicy([]byte("# stock\nnone\t* # ignored after the matched wildcard\n")) {
		t.Fatal("bounded vendor-delimited policy refused")
	}
}

func TestPAMLoaderInertGrammarDoesNotEraseFilenames(t *testing.T) {
	for _, raw := range []string{"\u00a0", "\u2003", "\x00", "# ignored\n\u00a0", "\r", "\v", "\f", "/tmp/library.so"} {
		if pamCandidateCommentsOnly([]byte(raw)) {
			t.Fatal("possible loader filename/control input treated as inert")
		}
	}
	for _, raw := range []string{"", " \t\n", "# stock\n\t # comment\n"} {
		if !pamCandidateCommentsOnly([]byte(raw)) {
			t.Fatal("ASCII inert configuration refused")
		}
	}
}

func TestPAMCandidateNSSRejectsAlternateCodeAndControlFlow(t *testing.T) {
	stock := "passwd: files systemd\ngroup: files systemd\nshadow: files\ngshadow: files\nhosts: files dns\n"

	resolute := strings.ReplaceAll(strings.ReplaceAll(stock, "shadow: files\n", "shadow: files systemd\n"), "gshadow: files\n", "gshadow: files systemd\n")
	if err := validatePAMCandidateNSSForRelease([]byte(resolute), "26.04"); err != nil {
		t.Fatal(err)
	}
	if validatePAMCandidateNSS([]byte(resolute)) == nil {
		t.Fatal("26 shadow policy widened22")
	}
	if validatePAMCandidateNSSForRelease([]byte(stock), "26.04") == nil {
		t.Fatal("22 shadow policy silently selected26")
	}
	if err := validatePAMCandidateNSS([]byte(stock)); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{strings.Replace(stock, "files systemd", "sss files systemd", 1), strings.Replace(stock, "passwd: files systemd", "passwd: files [SUCCESS=continue] systemd", 1), stock + "initgroups: files custom\n", stock + "passwd: files systemd\n"} {
		if validatePAMCandidateNSS([]byte(mutation)) == nil {
			t.Fatal("unreviewed identity provider accepted")
		}
	}
}

func TestPAMCandidateIdentityDataDoesNotBecomeExecutionOptions(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\nfluxo:x:1000:1000::/home/fluxo:/bin/bash\n"
	group := "root:x:0:\nfluxo:x:1000:\nwww-data:x:33:fluxo\nsudo:x:27:fluxo\n"
	identities, err := validatePAMCandidateAccounts([]byte(passwd), []byte(group))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(identities["root"], "x:") {
		t.Fatal("credential field leaked into public identity")
	}
	for _, mutation := range []string{strings.Replace(passwd, "fluxo:x:1000:1000:", "fluxo:x:1000:1000:UMASK=000,pri=-20,ulimit=unlimited", 1), passwd + "other:x:1000:1000::/elsewhere:/bin/bash\n", strings.Replace(passwd, "fluxo:x:1000:1000:", "fluxo:x:0:0:", 1), strings.Replace(passwd, "root:x:", "root:credential-material:", 1)} {
		if _, err = validatePAMCandidateAccounts([]byte(mutation), []byte(group)); err == nil {
			t.Fatal("ambiguous or executable identity options accepted")
		}
	}
}

func TestPAMCandidateManifestRejectsAliasedOrDuplicateAuthority(t *testing.T) {
	valid := strings.Repeat("a", 32) + "  usr/lib/x86_64-linux-gnu/libpam.so.0.85.1\n"
	if _, err := parsePAMCandidateMD5([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{valid + valid, strings.Repeat("a", 32) + "  ../etc/shadow\n", strings.Repeat("a", 32) + "  /usr/lib/module.so\n", strings.Repeat("a", 32) + "  usr/lib/../module.so\n"} {
		if _, err := parsePAMCandidateMD5([]byte(bad)); err == nil {
			t.Fatal("unsafe manifest authority accepted")
		}
	}
}

func TestPAMCandidateRepeatedObservationCannotHideReplacement(t *testing.T) {
	p := &pamCandidateInspection{observed: map[string]pamCandidateObservation{}}
	first := pamCandidateObservation{Dev: 3, Ino: 12, Size: 24}
	if err := p.remember("/protected/input", first); err != nil {
		t.Fatal(err)
	}
	if err := p.remember("/protected/input", first); err != nil {
		t.Fatal("stable repeated input refused", err)
	}
	changed := first
	changed.Ino++
	if p.remember("/protected/input", changed) == nil {
		t.Fatal("later observation erased the original protected identity")
	}
}

func TestPAMCandidateLoaderCacheBoundsAndSonameResolution(t *testing.T) {
	a, err := nativePAMArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	key, value := "libpam.so.0", "/lib/"+a.triplet+"/libpam.so.0"
	raw := make([]byte, 72)
	copy(raw, []byte("glibc-ld.so.cache1.1"))
	binary.LittleEndian.PutUint32(raw[20:24], 1)
	binary.LittleEndian.PutUint32(raw[48:52], a.cacheABI|3)
	binary.LittleEndian.PutUint32(raw[52:56], 72)
	binary.LittleEndian.PutUint32(raw[56:60], uint32(73+len(key)))
	raw = append(raw, []byte(key+"\x00"+value+"\x00")...)
	cache, err := parsePAMCandidateCache(raw)
	if err != nil || len(cache[key]) != 1 || cache[key][0] != value {
		t.Fatal(cache, err)
	}
	for _, offset := range []uint32{uint32(len(raw)), 0xffffffff} {
		bad := append([]byte{}, raw...)
		binary.LittleEndian.PutUint32(bad[52:56], offset)
		if _, err = parsePAMCandidateCache(bad); err == nil {
			t.Fatal("unbounded cache string offset accepted")
		}
	}
	bad := append([]byte{}, raw...)
	binary.LittleEndian.PutUint32(bad[20:24], 65537)
	if _, err = parsePAMCandidateCache(bad); err == nil {
		t.Fatal("unbounded cache entry count accepted")
	}
}

func TestPAMCandidateMappedLibraryRequiresSameDeviceAndInode(t *testing.T) {
	observation := pamCandidateObservation{Dev: unix.Mkdev(8, 19), Ino: 345}
	if !pamCandidateMappedIdentity(observation, "08:13", "345") {
		t.Fatal("same mapped image refused")
	}
	for _, pair := range [][2]string{{"09:13", "345"}, {"08:14", "345"}, {"08:13", "346"}, {"garbage", "345"}} {
		if pamCandidateMappedIdentity(observation, pair[0], pair[1]) {
			t.Fatal("different mapped image accepted")
		}
	}
}
