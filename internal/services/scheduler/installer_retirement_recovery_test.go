package scheduler

import (
	"errors"
	"strings"
	"testing"
)

// Unit observations only. Kernel reboot, systemd and cron-origin ACK evidence
// must be collected on the native fixture.
func TestInstallerHistoricalRetirementRecoveryProof(t *testing.T) {
	const newBoot = "483c861b-86fb-405d-9484-09eada7ce31c"
	for _, tc := range []string{"reboot", "original", "strict-refusal", "second-restart", "boot-alias", "config", "provider", "group", "old-receipt", "boot-read-error", "invalid-boot", "boot-race", "recheck-error", "identity-race", "qualification-error", "live-descendant", "unsettled-invocation", "invalid-archive"} {
		t.Run(tc, func(t *testing.T) {
			r := installerRetirementFixture()
			original := r
			live := r.Current
			kernel := newBoot
			switch tc {
			case "original", "strict-refusal", "second-restart", "boot-alias":
				kernel = r.Current.Boot
			case "invalid-boot":
				kernel = ""
			}
			live.Boot = kernel
			switch tc {
			case "second-restart", "boot-alias":
				live.GroupInode++
				if tc == "boot-alias" {
					r.Saved.Boot, r.Current.Boot = strings.ToUpper(kernel), strings.ToUpper(kernel)
				}
			case "config":
				live.Configuration = strings.Repeat("b", 64)
			case "provider":
				live.Backend, live.Configuration, live.Group = "", "", ""
			case "group":
				live.Group += "-other"
			case "old-receipt":
				live = r.Current
			case "invalid-archive":
				r.Current.GroupInode = r.Saved.GroupInode
			}
			var calls []string
			refusal := errors.New("unit fixture refusal")
			err := verifyInstallerRetirementRecovery(r, func() (string, error) {
				if tc == "boot-read-error" {
					return "", refusal
				}
				if tc == "boot-race" && len(calls) > 0 {
					return r.Saved.Boot, nil
				}
				calls = append(calls, "boot")
				return kernel, nil
			}, func() (legacyHost, error) {
				if tc == "qualification-error" || tc == "recheck-error" && len(calls) > 1 {
					return legacyHost{}, refusal
				}
				if tc == "identity-race" && len(calls) > 1 {
					live.PID++
				}
				calls = append(calls, "qualify")
				return live, nil
			}, func() error {
				calls = append(calls, "strict")
				if tc == "strict-refusal" || live != r.Current {
					return refusal
				}
				return nil
			}, func(host legacyHost) error {
				calls = append(calls, "drain")
				if host != live || host.Boot != newBoot {
					t.Fatal("did not inspect live provider")
				}
				if tc == "live-descendant" {
					return refusal
				}
				return nil
			}, func() error {
				calls = append(calls, "settled")
				if tc == "unsettled-invocation" {
					return refusal
				}
				return nil
			})
			switch tc {
			case "reboot":
				if err != nil || strings.Join(calls, " ") != "boot qualify settled drain qualify boot" || r != original {
					t.Fatal(calls, r, err)
				}
			case "original":
				if err != nil || strings.Join(calls, " ") != "boot strict" {
					t.Fatal(calls, err)
				}
			case "second-restart", "boot-alias":
				if err == nil || !strings.Contains(err.Error(), "operator-approved reboot") || !strings.Contains(err.Error(), "same release's normal installer") {
					t.Fatal(err)
				}
			default:
				if err == nil {
					t.Fatal("unexpected proof", calls)
				}
			}
		})
	}
}
