package site

import (
	"strings"
	"testing"
)

func TestApplicationEnvironmentsNeverFallbackToControlPlaneCredentials(t *testing.T) {
	laravelEnv := new(LaravelApp).DefaultEnv(ProvisionRequest{Domain: "app.example.com"})
	if strings.Contains(laravelEnv, "DB_USERNAME=fluxo") || strings.Contains(laravelEnv, "DB_PASSWORD=secret") {
		t.Fatalf("Laravel environment contains fallback control-plane credentials: %s", laravelEnv)
	}
	if phpEnv := new(PHPApp).DefaultEnv(ProvisionRequest{Domain: "app.example.com"}); phpEnv != "" {
		t.Fatalf("PHP site without a database should not receive database configuration: %s", phpEnv)
	}
	nodeEnv := new(NodeApp).DefaultEnv(ProvisionRequest{
		Domain:           "app.example.com",
		AppPort:          3000,
		DatabaseName:     "app_db",
		DatabaseUser:     "app_user",
		DatabasePassword: "dedicated-secret",
	})
	if !strings.Contains(nodeEnv, "DB_USERNAME=app_user") || !strings.Contains(nodeEnv, "DB_PASSWORD='dedicated-secret'") {
		t.Fatalf("Node environment did not preserve dedicated credentials: %s", nodeEnv)
	}
}

func TestDatabasePasswordsAreQuotedInGeneratedDotEnv(t *testing.T) {
	req := ProvisionRequest{
		Domain:           "app.example.com",
		DatabaseName:     "app_db",
		DatabaseUser:     "app_user",
		DatabasePassword: `space # dollar $ backslash \\ value`,
	}
	want := `DB_PASSWORD='space # dollar $ backslash \\ value'`
	if env := new(LaravelApp).DefaultEnv(req); !strings.Contains(env, want) {
		t.Fatalf("Laravel environment password was not safely quoted: %s", env)
	}
	if env := new(PHPApp).DefaultEnv(req); !strings.Contains(env, want) {
		t.Fatalf("PHP environment password was not safely quoted: %s", env)
	}
	if env := new(NodeApp).DefaultEnv(req); !strings.Contains(env, want) {
		t.Fatalf("Node environment password was not safely quoted: %s", env)
	}
}

func TestDatabasePasswordsAreQuotedWhenMergingDotEnvExamples(t *testing.T) {
	req := ProvisionRequest{
		DatabaseEngine:   "mysql",
		DatabaseName:     "app_db",
		DatabaseUser:     "app_user",
		DatabasePassword: `generated#password$with\\slashes`,
	}
	want := `DB_PASSWORD='generated#password$with\\slashes'`

	for _, envExample := range []string{
		"DB_CONNECTION=sqlite\nDB_HOST=localhost\nDB_PASSWORD=\n",
		"APP_ENV=production\n",
	} {
		env := mergeDotEnvValues(envExample, databaseDotEnvReplacements(req))
		if !strings.Contains(env, want) {
			t.Fatalf("merged environment password was not safely quoted: %s", env)
		}
		if !strings.Contains(env, "DB_HOST=127.0.0.1") {
			t.Fatalf("merged environment did not use the managed TCP database host: %s", env)
		}
	}
}

func TestMergeDotEnvGroupsDatabaseDefaultsWithoutChangingOtherValues(t *testing.T) {
	replacements := databaseDotEnvReplacements(ProvisionRequest{DatabaseName: "app_db", DatabaseUser: "app_user", DatabasePassword: "secret"})
	block := "DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_DATABASE=app_db\nDB_USERNAME=app_user\nDB_PASSWORD='secret'"
	for _, newline := range []string{"\n", "\r\n"} {
		input := "APP_NAME=Example\n# DB_HOST=localhost\nDB_CONNECTION=sqlite\nKEEP=value\nexport DB_PASSWORD=old\nDB_USERNAME=duplicate\n# DB_PORT=3306\n"
		want := "APP_NAME=Example\n" + block + "\nKEEP=value\n"
		input = strings.ReplaceAll(input, "\n", newline)
		want = strings.ReplaceAll(want, "\n", newline)
		if got := mergeDotEnvValues(input, replacements); got != want {
			t.Fatalf("merge with newline %q:\n got %q\nwant %q", newline, got, want)
		}
	}
}

func TestMergeDotEnvPreservesMultilineSecrets(t *testing.T) {
	for _, quote := range []string{"'", "\"", "`"} {
		for _, newline := range []string{"\n", "\r\n"} {
			secret := "PRIVATE_KEY=" + quote + "first\n# DB_HOST=inside-secret\nDB_CONNECTION=inside-secret\nlast" + quote
			input := secret + "\nDB_CONNECTION=sqlite\nDB_PASSWORD=" + quote + "old\npassword" + quote + "\nKEEP=yes\n"
			want := secret + "\nDB_CONNECTION=mysql\nDB_PASSWORD='new'\nKEEP=yes\n"
			input = strings.ReplaceAll(input, "\n", newline)
			want = strings.ReplaceAll(want, "\n", newline)
			got := mergeDotEnvValues(input, map[string]string{"DB_CONNECTION": "mysql", "DB_PASSWORD": "'new'"})
			if got != want {
				t.Fatalf("multiline merge (%q, %q):\n got %q\nwant %q", quote, newline, got, want)
			}
		}
	}
}

func TestLaravelAppNameFallback(t *testing.T) {
	for _, test := range []struct {
		content   string
		populated bool
	}{
		{"APP_NAME=MyApp", true}, {`APP_NAME="My App" # comment`, true},
		{"export\tAPP_NAME=CustomerApp", true},
		{`export APP_NAME='Client #1'`, true}, {"APP_NAME=first\nAPP_NAME=last", true},
		{"APP_NAME=\"Multi\nLine\"", true}, {"APP_ENV=local", false},
		{"# APP_NAME=Example", false}, {"APP_NAME=  # choose a name", false},
		{`APP_NAME="" # empty`, false}, {"APP_NAME='   '", false},
		{"APP_NAME=old\nAPP_NAME=", false},
		{"PRIVATE_KEY=\"line\nAPP_NAME=inside-secret\nend\"", false},
	} {
		for _, newline := range []string{"\n", "\r\n"} {
			content := strings.ReplaceAll(test.content, "\n", newline)
			if got := hasDotEnvValue(content, "APP_NAME"); got != test.populated {
				t.Errorf("has value for %q = %v", content, got)
			}
		}
	}
}
