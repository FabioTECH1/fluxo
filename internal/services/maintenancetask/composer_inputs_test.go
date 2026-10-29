package maintenancetask

import (
	"os"
	"testing"
)

func TestComposerTrustKeysArePinnedToApprovedImplementation(t *testing.T) {
	for _, name := range []string{"keys.dev.pub", "keys.tags.pub"} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = validateComposerKey(name, raw); err != nil {
			t.Fatal(err)
		}
		raw[len(raw)/2] ^= 1
		if err = validateComposerKey(name, raw); err == nil {
			t.Fatal("modified self-update key accepted")
		}
	}
	if err := validateComposerKey("other", []byte("custom")); err == nil {
		t.Fatal("unknown trust key accepted")
	}
}
