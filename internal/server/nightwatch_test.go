package server

import "testing"

func TestNightwatchPortFromCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		port    int
		ok      bool
	}{
		{
			name:    "standard command",
			command: "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:2407",
			port:    2407,
			ok:      true,
		},
		{
			name:    "arguments after listen address",
			command: "php8.3 artisan nightwatch:agent --listen-on=127.0.0.1:2410 --verbose",
			port:    2410,
			ok:      true,
		},
		{
			name:    "unrelated daemon",
			command: "php8.4 artisan horizon",
			port:    0,
			ok:      false,
		},
		{
			name:    "invalid port",
			command: "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:not-a-port",
			port:    0,
			ok:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port, ok := nightwatchPortFromCommand(test.command)
			if port != test.port || ok != test.ok {
				t.Fatalf("nightwatchPortFromCommand() = (%d, %t), want (%d, %t)", port, ok, test.port, test.ok)
			}
		})
	}
}
