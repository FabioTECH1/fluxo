package daemon

import (
	"strings"
	"testing"
)

func TestGeneratedPHPExecStartPinsBranchInterpreter(t *testing.T) {
	for _, command := range []string{"php8.4 artisan queue:work", "php8.4 artisan horizon", "php8.4 /usr/local/lib/fluxo/octane/v1/start.php artisan octane:start --host=127.0.0.1 --port=8000"} {
		content := renderServiceFile(1, 1, 1, "fluxo", "/home/fluxo/app", "", command, 1, 15, "SIGTERM")
		if !strings.Contains(content, "\nExecStart=/usr/bin/"+command+"\n") {
			t.Fatalf("unit does not pin PHP: %s", content)
		}
		if got := absolutePHPCommand(command); got != "/usr/bin/"+command {
			t.Fatalf("generated ExecStart=%s", got)
		}
	}
	for _, command := range []string{"php artisan queue:work", "php8.4-custom artisan horizon", "/opt/php8.4 artisan horizon", "node app.js", "env php8.4 artisan horizon"} {
		if got := absolutePHPCommand(command); got != command {
			t.Fatalf("changed custom command: %s", got)
		}
	}
}
