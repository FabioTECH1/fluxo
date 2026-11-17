package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func httpDefaultsFixture(t *testing.T) httpDefaultsEnvironment {
	t.Helper()
	return httpDefaultsEnvironment{
		path: filepath.Join(t.TempDir(), "fluxo-http-defaults.conf"),
		trusted: func(path string) error {
			info, err := os.Lstat(path)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return errors.New("linked fixture")
			}
			return err
		},
		validate: func(context.Context) error { return nil },
		loaded:   func(context.Context, string) (bool, error) { return true, nil },
	}
}

func TestHTTPDefaultsPublicationAndReloadedReadback(t *testing.T) {
	env := httpDefaultsFixture(t)
	if err := ensureHTTPDefaults(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(env.path)
	if err != nil || string(before) != httpDefaultsConfig {
		t.Fatal("managed HTTP default was not published", err)
	}
	// A subsequent startup must still establish that the included file loads.
	env.loaded = func(context.Context, string) (bool, error) { return false, nil }
	if err = ensureHTTPDefaults(context.Background(), env); err == nil {
		t.Fatal("unloaded existing default accepted")
	}
	after, _ := os.ReadFile(env.path)
	if string(after) != string(before) {
		t.Fatal("readback refusal changed the existing default")
	}
}

func TestHTTPDefaultsRestoreAbsenceOnFailure(t *testing.T) {
	for _, reason := range []string{"baseline", "candidate", "unloaded"} {
		t.Run(reason, func(t *testing.T) {
			env := httpDefaultsFixture(t)
			calls := 0
			env.validate = func(context.Context) error {
				calls++
				if reason == "baseline" && calls == 1 || reason == "candidate" && calls == 2 {
					return errors.New("unrelated configuration error")
				}
				return nil
			}
			env.loaded = func(context.Context, string) (bool, error) { return reason != "unloaded", nil }
			if err := ensureHTTPDefaults(context.Background(), env); err == nil {
				t.Fatal("failure was masked")
			}
			if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
				t.Fatal("failed candidate was not removed", err)
			}
		})
	}
}

func TestHTTPDefaultsPreserveExplicitOperatorValuesInEitherIncludeOrder(t *testing.T) {
	for _, lastDeclaration := range []string{"managed", "operator"} {
		t.Run(lastDeclaration, func(t *testing.T) {
			env := httpDefaultsFixture(t)
			operator := filepath.Join(filepath.Dir(env.path), "nginx.conf")
			original := "http {\n server_names_hash_bucket_size 64; # intentional\n}\n"
			if err := os.WriteFile(operator, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			env.validate = func(context.Context) error {
				calls++
				if calls == 2 {
					where := env.path
					if lastDeclaration == "operator" {
						where = operator
					}
					return fmt.Errorf("nginx: [emerg] \"server_names_hash_bucket_size\" directive is duplicate in %s:2\nnginx: configuration file %s test failed", where, operator)
				}
				return nil
			}
			env.loaded = func(context.Context, string) (bool, error) { t.Fatal("duplicate was retained"); return false, nil }
			if err := ensureHTTPDefaults(context.Background(), env); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatal("original configuration was not revalidated")
			}
			if raw, err := os.ReadFile(operator); err != nil || string(raw) != original {
				t.Fatal("operator setting changed", err)
			}
			if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
				t.Fatal("managed duplicate remains", err)
			}
		})
	}
}

func TestHTTPDefaultsDuplicateExceptionCannotMaskOtherErrors(t *testing.T) {
	for _, extra := range []string{
		"\nnginx: [emerg] another configuration error",
		"\nnginx: [emerg] \"other\" directive is duplicate in /etc/nginx/other.conf:1",
	} {
		env := httpDefaultsFixture(t)
		calls := 0
		env.validate = func(context.Context) error {
			calls++
			if calls == 2 {
				return fmt.Errorf("nginx: [emerg] \"server_names_hash_bucket_size\" directive is duplicate in %s:2%s", env.path, extra)
			}
			return nil
		}
		if err := ensureHTTPDefaults(context.Background(), env); err == nil {
			t.Fatal("unrelated error masked")
		}
		if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
			t.Fatal("failed candidate remains", err)
		}
	}
}

func TestHTTPDefaultsPreserveCustomManagedPath(t *testing.T) {
	env := httpDefaultsFixture(t)
	custom := "# Operator owned\nserver_names_hash_bucket_size 128;\n"
	if err := os.WriteFile(env.path, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureHTTPDefaults(context.Background(), env); err == nil || !strings.Contains(err.Error(), "preserve custom") {
		t.Fatal("custom managed-path contents accepted", err)
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != custom {
		t.Fatal("custom contents changed")
	}
}
