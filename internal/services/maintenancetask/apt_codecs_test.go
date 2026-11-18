package maintenancetask

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPTNativeCodecLinkageRequired(t *testing.T) {
	libs := []string{"libz.so.1", "libbz2.so.1.0", "liblzma.so.5", "liblz4.so.1", "libzstd.so.1"}
	if err := requireAPTCodecLibraries(libs); err != nil {
		t.Fatal(err)
	}
	for i := range libs {
		missing := append([]string{}, libs[:i]...)
		missing = append(missing, libs[i+1:]...)
		if requireAPTCodecLibraries(missing) == nil {
			t.Fatal("missing native codec accepted", libs[i])
		}
	}
}

// Fixed offline-generated index data; no external compressor is executed by the test.
var aptCodecFixture = map[string]string{
	"gz":   "H4sIAAAAAAACAzWOwQrCMBBE7/mK/QErCqIEEQXpTRAE79vNVhfTbEjTUv1626KHucww8+aK9MIHW6h9N+iC1DEtahlyl9jcObWiwcK22JhToqdkpimxgN6bC0rIozhZKKc6IBHHjIEY9kErde8jD9hEz4WEHr24gynFc8Bm3IiqfvljFY4rc5PPaK/WO3PmlpLEPNNjkh4zQ8AsPcP8Ef4fzRc3BCP9wgAAAA==",
	"bz2":  "QlpoOTFBWSZTWWplYXYAABdfgAAQQAOy1WUCSQA/799wIACUhqGplPBJpp6TaNRoA2p+pA1NqI9QGgJiDRiGCOsUk/qDCF4RNEPYlA1tLgstDzTBp8LC7HmhjUATv720Nbtl2FCSR5LC4nUPdXB6Gh0Ilj+VU5oYxt9fnuUvSuTXakJY23mlRoN757ZxFrVBYpcTDSOgnEauWI0eIWJz+AjVwsla+Imdi2AVhVEcgLPCfxdyRThQkGplYXY=",
	"xz":   "/Td6WFoAAATm1rRGAgAhARYAAAB0L+Wj4ADBAJ9dACgYSGbb2jCF/hbjiF4w6shEat9B59OyZ0jTD731n9p9MaVufsQPltc53ky2p9O3+KrcBRcmYMeIY4Dz6wA0eM6MXTAbemzDb5wtJ1tpAApym3bmsiOv8JeGcf0Ny5JuhDC9FeT+6eK402p+KIRDy0uMYnCcuB357WOdqRvgCLSrpdnuu7nXichUd067D4N7xQ4t5UdJosLuUwlSpshQAAAApx6FXL6dJRgAAbsBwgEAAGFSvlOxxGf7AgAAAAAEWVo=",
	"lzma": "XQAAgAD//////////wAoGEhm29owhf4W44heMOrIRGrfQefTsmdI0w+99Z/afTGlbn7ED5bXOd5MtqfTt/iq3AUXJmDHiGOA8+sANHjOjF0wG3psw2+cLSdbaQAKcpt25rIjr/CXhnH9DcuSboQwvRXk/uniuNNqfiiEQ8tLjGJwnLgd+e1jnakb4Ai0q6XZ7ru514nIVHdOuw+De8UOLeVHSaLC7lMJUqeXBXU+/3YOIAA=",
	"zst":  "KLUv/SDC3QQA4ooiH1BN2gYEm909tOVZRWTZ5GktD+iqwCAp5CGe2pjBSZlmlz+c90Ub9964qEqdVGXjClYgEHDmE9pgix9kaQKAmNOZM3CPVGEHRxEuq8hRYuA6gsThBa/BPRc7PRNPcUZ9FXsVFwwhPHOldL4VIlWCDSAEZ5a1oe5gV0ovTY784Tx4KSg58weMSGQIBAC3JhuwblfFQiKs4Qw=",
	"lz4":  "BCJNGGBAgr4AAADwI1BhY2thZ2U6IGZsdXhvLWNvZGVjLWZpeHR1cmUKVmVyc2lvbjogNy41CkFyY2hpdGVjGgDwBDogYWxsCk1haW50YWluZXI6IEY/APMlIGFjY2VwdGFuY2UgPG5vYm9keUBleGFtcGxlLmludmFsaWQ+CkZpbGVuYW1lOiBwb29sL2wA8QguZGViClNpemU6IDEyOApEZXNjcmlwdH4A8QBwcml2YXRlIG5hdGl2ZSCkAKAgZml4dHVyZQoKAAAAAA==",
}

func (f *noQueueFixture) checkNativeCodecs(t *testing.T) {
	t.Helper()
	if !f.strace {
		t.Fatal("native codec proof requires payload-verified strace")
	}
	for ext, encoded := range aptCodecFixture {
		t.Run("native-codec/"+ext, func(t *testing.T) {
			// Remove only explicitly owned private fixture files, never host caches.
			for _, suffix := range []string{"", ".gz", ".bz2", ".xz", ".lzma", ".zst", ".lz4"} {
				if err := os.Remove(f.index + suffix); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"pkgcache.bin", "srcpkgcache.bin"} {
				if err := os.Remove(filepath.Join(f.root, "cache", name)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(f.index+"."+ext, raw, 0644); err != nil {
				t.Fatal(err)
			}
			out, err := f.run(t, "codec-"+ext, true, "/usr/bin/apt-cache", "show", "fluxo-codec-fixture")
			if err != nil || !strings.Contains(out, "Version: 7.5") {
				t.Fatalf("native index parse %s failed: %v\n%s", ext, err, out)
			}
			trace, readErr := os.ReadFile(filepath.Join(f.root, "control", "exec-codec-"+ext+".log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, line := range strings.Split(string(trace), "\n") {
				if strings.Contains(line, "execve(") && !strings.Contains(line, `execve("/usr/bin/apt-cache",`) && !strings.Contains(line, `execve("/usr/bin/dpkg",`) {
					t.Fatalf("native codec unexpectedly spawned another executable: %s", line)
				}
			}
			c := f.counts(t)
			if c["method"] != 0 || c["refuse"] != 0 || c["preinstall"] != 0 || c["script"] != 0 || c["trigger"] != 0 {
				t.Fatal("codec parse crossed action boundary", c)
			}
			f.results = append(f.results, map[string]any{"case": "native-codec/" + ext, "parsed": true, "counters": c})
			if ext == "lz4" {
				for _, name := range []string{"pkgcache.bin", "srcpkgcache.bin"} {
					if err = os.Remove(filepath.Join(f.root, "cache", name)); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				}
				out, err = f.run(t, "codec-lz4-installed-program", true, "/usr/bin/apt-cache", "-o", "APT::Compressor::lz4::Binary=lz4", "-o", "APT::Compressor::lz4::CompressArg::=-1", "-o", "APT::Compressor::lz4::UncompressArg::=-d", "show", "fluxo-codec-fixture")
				if err != nil || !strings.Contains(out, "Version: 7.5") {
					t.Fatal("stock lz4 tuple did not decode natively", err)
				}
				trace, err = os.ReadFile(filepath.Join(f.root, "control", "exec-codec-lz4-installed-program.log"))
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range strings.Split(string(trace), "\n") {
					if strings.Contains(line, "execve(") && !strings.Contains(line, `execve("/usr/bin/apt-cache",`) && !strings.Contains(line, `execve("/usr/bin/dpkg",`) {
						t.Fatalf("stock lz4 tuple executed an external program: %s", line)
					}
				}
				f.results = append(f.results, map[string]any{"case": "native-codec/lz4-installed-program", "parsed": true, "external_program_executed": false})
			}
		})
	}
}
