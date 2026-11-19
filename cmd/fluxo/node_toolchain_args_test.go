package main

import "testing"

func TestNodeToolchainPreservationIntent(t *testing.T) {
	for _, args := range [][]string{{"install"}, {"install", "--installer-lease", "lease"}, {"install", "--preserve"}, {"install", "--preserve", "--installer-lease", "lease"}, {"install", "--installer-lease", "lease", "--preserve"}} {
		p, l, err := parseNodeToolchainArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		wantP, wantL := false, ""
		for _, arg := range args {
			if arg == "--preserve" {
				wantP = true
			}
			if arg == "lease" {
				wantL = arg
			}
		}
		if p != wantP || l != wantL {
			t.Fatalf("%v: %v/%s", args, p, l)
		}
	}
	for _, args := range [][]string{nil, {"repair"}, {"install", "--preserve", "--preserve"}, {"install", "--installer-lease"}, {"install", "--installer-lease", "a", "--installer-lease", "b"}, {"install", "unexpected"}} {
		if _, _, err := parseNodeToolchainArgs(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
