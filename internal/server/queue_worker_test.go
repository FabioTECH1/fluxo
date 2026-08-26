package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateQueueWorkerConfig(t *testing.T) {
	config := defaultQueueWorkerConfig()
	config.Connection = " redis-primary "
	config.Queues = " high, default ,low "
	config.Processes = 4

	validated, err := validateQueueWorkerConfig(config)
	if err != nil {
		t.Fatalf("validateQueueWorkerConfig() error = %v", err)
	}
	if validated.Connection != "redis-primary" {
		t.Fatalf("connection = %q, want redis-primary", validated.Connection)
	}
	if validated.Queues != "high,default,low" {
		t.Fatalf("queues = %q, want high,default,low", validated.Queues)
	}
	if validated.Processes != 4 {
		t.Fatalf("processes = %d, want 4", validated.Processes)
	}
}

func TestValidateQueueWorkerConfigRejectsUnsafeSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*queueWorkerConfig)
	}{
		{name: "synchronous connection", mutate: func(config *queueWorkerConfig) { config.Connection = "SYNC" }},
		{name: "unsafe connection", mutate: func(config *queueWorkerConfig) { config.Connection = "redis;reboot" }},
		{name: "unsafe queue", mutate: func(config *queueWorkerConfig) { config.Queues = "default;reboot" }},
		{name: "too many processes", mutate: func(config *queueWorkerConfig) { config.Processes = 17 }},
		{name: "invalid timeout", mutate: func(config *queueWorkerConfig) { config.TimeoutSeconds = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := defaultQueueWorkerConfig()
			test.mutate(&config)
			if _, err := validateQueueWorkerConfig(config); err == nil {
				t.Fatal("validateQueueWorkerConfig() accepted invalid settings")
			}
		})
	}
}

func TestQueueWorkerCommand(t *testing.T) {
	config := defaultQueueWorkerConfig()
	config.Connection = "redis"
	config.Queues = "high,default"
	config.Processes = 3
	config.Force = true

	command := queueWorkerCommand("8.4", config)
	for _, expected := range []string{
		"php8.4 artisan queue:work redis",
		"--queue=high,default",
		"--sleep=3",
		"--tries=3",
		"--timeout=60",
		"--memory=128",
		"--max-time=3600",
		"--force",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command %q does not contain %q", command, expected)
		}
	}
}

func TestReadDotEnvValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# queue settings\nexport QUEUE_CONNECTION=\"redis\"\nQUEUE_DRIVER='database'\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if got := readDotEnvValue(path, "QUEUE_CONNECTION"); got != "redis" {
		t.Fatalf("QUEUE_CONNECTION = %q, want redis", got)
	}
	if got := readDotEnvValue(path, "QUEUE_DRIVER"); got != "database" {
		t.Fatalf("QUEUE_DRIVER = %q, want database", got)
	}
}
