package maintenancetask

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func dpkgBootstrapFixture() (map[string][]byte, func(string, int64) ([]byte, error)) {
	files := map[string][]byte{"/usr/bin/dpkg": []byte("reviewed dpkg payload"), "/usr/bin/dpkg-query": []byte("reviewed query payload")}
	var manifest string
	for _, path := range dpkgBootstrapPrograms {
		hash := md5.Sum(files[path])
		manifest += hex.EncodeToString(hash[:]) + "  " + strings.TrimPrefix(path, "/") + "\n"
	}
	files[dpkgBootstrapManifest] = []byte(manifest)
	return files, func(path string, limit int64) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return append([]byte(nil), data...), nil
	}
}

func TestDpkgBootstrapRefusesUnknownCodeBeforeQuery(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte){
		"missing metadata":     func(f map[string][]byte) { delete(f, dpkgBootstrapManifest) },
		"missing code":         func(f map[string][]byte) { delete(f, "/usr/bin/dpkg-query") },
		"code digest mismatch": func(f map[string][]byte) { f["/usr/bin/dpkg-query"] = []byte("unreviewed wrapper") },
		"duplicate entry": func(f map[string][]byte) {
			f[dpkgBootstrapManifest] = append(f[dpkgBootstrapManifest], f[dpkgBootstrapManifest]...)
		},
		"dot alias": func(f map[string][]byte) {
			f[dpkgBootstrapManifest] = []byte(strings.Replace(string(f[dpkgBootstrapManifest]), "  usr/bin/dpkg-query", "  ./usr/bin/dpkg-query", 1))
		},
		"absolute alias": func(f map[string][]byte) {
			f[dpkgBootstrapManifest] = []byte(strings.Replace(string(f[dpkgBootstrapManifest]), "  usr/bin/dpkg-query", "  /usr/bin/dpkg-query", 1))
		},
		"usrmerge alias": func(f map[string][]byte) {
			f[dpkgBootstrapManifest] = append(f[dpkgBootstrapManifest], []byte(strings.Repeat("0", 32)+"  bin/dpkg-query\n")...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			files, read := dpkgBootstrapFixture()
			proof, err := inspectDpkgBootstrap(read)
			if err != nil {
				t.Fatal(err)
			}
			change(files)
			calls := 0
			if err = proof.verifyAndRun(read, func() error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatal("unverified package-query code executed", err, calls)
			}
		})
	}
}

func TestDpkgBootstrapBindsMetadataAndCodeEvidence(t *testing.T) {
	files, read := dpkgBootstrapFixture()
	proof, err := inspectDpkgBootstrap(read)
	if err != nil {
		t.Fatal(err)
	}
	p := newProfile(PackageCleanup)
	proof.record(&p)
	if len(p.Files) != 3 || p.External["dpkg-bootstrap-contract"] != dpkgBootstrapRevision {
		t.Fatal("bootstrap evidence not preserved")
	}
	calls := 0
	if err = proof.verifyAndRun(read, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("verified query refused", err, calls)
	}
	// An otherwise consistent package replacement still invalidates a captured
	// proof; do not execute using old evidence after a concurrent update.
	files["/usr/bin/dpkg-query"] = []byte("different authenticated package image")
	hash := md5.Sum(files["/usr/bin/dpkg-query"])
	lines := strings.Split(string(files[dpkgBootstrapManifest]), "\n")
	for i, line := range lines {
		if strings.HasSuffix(line, "  usr/bin/dpkg-query") {
			lines[i] = hex.EncodeToString(hash[:]) + "  usr/bin/dpkg-query"
		}
	}
	files[dpkgBootstrapManifest] = []byte(strings.Join(lines, "\n"))
	calls = 0
	if err = proof.verifyAndRun(read, func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("changed bootstrap authority accepted", err, calls)
	}
}

func TestVMPackageBootstrapBeforeDependencyQuery(t *testing.T) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on dedicated acceptance VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newProfile(PackageCleanup)
	inputs, err := inputClosure(ctx, []string{"dpkg"}, &p)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 || p.Files[dpkgBootstrapManifest] == "" || p.Files["/usr/bin/dpkg-query"] == "" || p.External["dpkg-bootstrap-contract"] != dpkgBootstrapRevision {
		t.Fatal("native bootstrap proof missing")
	}
}
