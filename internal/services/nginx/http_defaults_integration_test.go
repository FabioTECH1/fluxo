package nginx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Uses only a private prefix/config and nginx -t/-T. No system configuration,
// service, listener or existing site is changed.
func TestNginxHTTPDefaultsLongDNSAndCustomSettingPreservation(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx is not installed")
	}
	for _, order := range []string{"stock", "custom-before-include", "custom-after-include"} {
		t.Run(order, func(t *testing.T) {
			env := httpDefaultsFixture(t)
			dir := filepath.Dir(env.path)
			conf := filepath.Join(dir, "nginx.conf")
			site := filepath.Join(dir, "site.conf")
			if err := os.WriteFile(site, []byte("server { listen 127.0.0.1:19191; server_name short.test; return 204; }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before, after := "", ""
			if order == "custom-before-include" {
				before = "server_names_hash_bucket_size 64;"
			} else if order == "custom-after-include" {
				after = "server_names_hash_bucket_size 64;"
			}
			// Glob includes accept the deliberately absent candidate initially.
			original := fmt.Sprintf("pid %s;\nerror_log %s;\nevents {}\nhttp {\n%s\ninclude %s;\n%s\ninclude %s;\n}\n", filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "error.log"), before, filepath.Join(dir, "fluxo-http-*.conf"), after, site)
			if err := os.WriteFile(conf, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			run := func(flag string) (string, error) {
				out, err := exec.Command(binary, "-p", dir+"/", "-e", filepath.Join(dir, "error.log"), flag, "-c", conf).CombinedOutput()
				if err != nil {
					return string(out), fmt.Errorf("nginx: %w\n%s", err, out)
				}
				return string(out), nil
			}
			env.validate = func(context.Context) error { _, err := run("-t"); return err }
			env.loaded = func(_ context.Context, path string) (bool, error) {
				out, err := run("-T")
				return strings.Contains(out, "# configuration file "+path+":\n"+httpDefaultsConfig), err
			}
			if err := ensureHTTPDefaults(context.Background(), env); err != nil {
				t.Fatal(err)
			}
			if raw, _ := os.ReadFile(conf); string(raw) != original {
				t.Fatal("operator configuration changed")
			}
			if order != "stock" {
				if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
					t.Fatal("explicit operator value has a managed duplicate", err)
				}
				return
			}
			// RFC-valid maximum DNS length is 253 bytes, each label <=63.
			name := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
			if err := os.WriteFile(site, []byte("server { listen 127.0.0.1:19191; server_name "+name+"; return 204; }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := run("-t"); err != nil {
				t.Fatal("complete DNS name still fails Nginx validation", err)
			}
		})
	}
}
