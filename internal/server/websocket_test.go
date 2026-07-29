package server

import "testing"

func TestHubKeepsCommandAndDeploymentReplayBuffersSeparate(t *testing.T) {
	hub := &Hub{
		clients:         make(map[*WSClient]bool),
		logs:            make(map[int64][]string),
		logBytes:        make(map[int64]int),
		commandLogs:     make(map[int64][]string),
		commandLogBytes: make(map[int64]int),
	}

	hub.BroadcastLog(1, 10, "deploy\n")
	hub.BroadcastCommandLog(1, 10, "command\n")

	if got := len(hub.logs[10]); got != 1 {
		t.Fatalf("deployment replay entries = %d, want 1", got)
	}
	if got := hub.logs[10][0]; got != "deploy\n" {
		t.Fatalf("deployment replay = %q, want deploy log", got)
	}
	if got := len(hub.commandLogs[10]); got != 1 {
		t.Fatalf("command replay entries = %d, want 1", got)
	}
	if got := hub.commandLogs[10][0]; got != "command\n" {
		t.Fatalf("command replay = %q, want command log", got)
	}

	hub.ClearCommandLog(1, 10)
	if _, ok := hub.commandLogs[10]; ok {
		t.Fatal("command replay buffer was not cleared")
	}
	if _, ok := hub.logs[10]; !ok {
		t.Fatal("clearing command replay must not clear deployment replay")
	}
}

func TestHubIgnoresInvalidCommandBroadcastIDs(t *testing.T) {
	hub := &Hub{
		clients:         make(map[*WSClient]bool),
		logs:            make(map[int64][]string),
		logBytes:        make(map[int64]int),
		commandLogs:     make(map[int64][]string),
		commandLogBytes: make(map[int64]int),
	}

	hub.BroadcastCommandLog(1, 0, "ignored")

	if len(hub.commandLogs) != 0 {
		t.Fatalf("invalid command broadcast created replay buffers: %#v", hub.commandLogs)
	}
}
