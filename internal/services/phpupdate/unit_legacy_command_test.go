package phpupdate

import (
	"errors"
	"fluxo/internal/services/octaneruntime"
	"testing"
)

func TestGeneratedPHPUnitExecutableContract(t *testing.T) {
	for _, kind := range []string{"laravel_queue", "laravel_horizon", "laravel_octane"} {
		action := "queue:work"
		if kind == "laravel_horizon" {
			action = "horizon"
		}
		c := &Consumer{Kind: kind, Command: "php8.4 artisan " + action, Directory: "/home/fluxo/app"}
		if kind == "laravel_octane" {
			c.Port = 8000
			c.Command = octaneruntime.Command("8.4", 8000)
		}
		for _, tc := range []struct {
			name, path, search, env string
			shadow, wantError       bool
		}{
			{name: "absolute", path: "/usr/bin/php8.4"},
			{name: "legacy verified", path: "php8.4"},
			{name: "legacy shadowed", path: "php8.4", shadow: true, wantError: true},
			{name: "custom executable", path: "/usr/local/bin/php8.4", wantError: true},
			{name: "custom search", path: "php8.4", search: "/home/fluxo/bin", wantError: true},
			{name: "custom PATH", path: "php8.4", env: "PATH=/home/fluxo/bin", wantError: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				suffix := c.Command[len("php8.4 "):]
				p := unitProperties("Id=fluxo-daemon-1.service\nExecStart={ path=" + tc.path + " ; argv[]=" + tc.path + " " + suffix + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\nUser=fluxo\nType=simple\nWorkingDirectory=/home/fluxo/app\nRestart=always\nExecSearchPath=" + tc.search + "\nEnvironment=" + tc.env)
				calls := 0
				err := validateUnitDefinitionWithResolver("8.4", c, p, func(string) error {
					calls++
					if tc.shadow {
						return errors.New("shadowed")
					}
					return nil
				})
				if (err != nil) != tc.wantError {
					t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
				}
				if !tc.wantError && ((tc.path == "php8.4" && calls != 1) || (tc.path != "php8.4" && calls != 0)) {
					t.Fatal("incorrect legacy resolution path")
				}
			})
		}
	}
}
