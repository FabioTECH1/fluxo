package maintenancetask

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestOptionalProfileInstallerReviewOnlyRecognizedCapabilities(t *testing.T) {
	j := journal{Entries: map[int]registration{
		9: {Contract: Contract{ID: 9, Kind: ComposerUpdate}, Enabled: true},
		3: {Contract: Contract{ID: 3, Kind: CertificateRenewal}, Enabled: true},
		4: {Contract: Contract{ID: 4, Kind: ComposerUpdate}, Enabled: false},
		5: {Contract: Contract{ID: 5, Kind: ApplicationJob}, Enabled: true},
	}}
	for _, scenario := range []string{"needs-refresh", "already-capable", "unchanged-profile", "unsupported-profile", "damaged-helper"} {
		t.Run(scenario, func(t *testing.T) {
			ids, err := inspectOptionalProfileHelpers(context.Background(), j, func(_ context.Context, r registration) (executiondomain.ReaderCapabilities, error) {
				if r.Contract.ID == 4 || r.Contract.ID == 5 {
					t.Fatal("unrelated or disabled registration inspected")
				}
				if scenario == "damaged-helper" {
					return executiondomain.ReaderCapabilities{}, errors.New("actual helper digest mismatch")
				}
				if scenario == "already-capable" {
					return HelperCapabilities(), nil
				}
				return executiondomain.Capabilities(), nil
			}, func(_ context.Context, c Contract) (profile, error) {
				p := newProfile(c.Kind)
				if scenario == "unsupported-profile" {
					return p, errors.New("preserve custom configuration for review")
				}
				if scenario != "unchanged-profile" {
					if c.Kind == ComposerUpdate {
						p.External["php-imagick-startup"] = imagickStartupProfile
					} else {
						p.External["certbot-directory-hook-contract"] = certbotDirectoryHookContract
					}
				}
				return p, nil
			})
			if scenario == "damaged-helper" {
				if err == nil {
					t.Fatal("helper damage treated as unsupported optional configuration")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario == "needs-refresh" {
				if !reflect.DeepEqual(ids, []int{3, 9}) {
					t.Fatal("exact stable affected IDs missing", ids)
				}
			} else if len(ids) != 0 {
				t.Fatal("unnecessary refresh requested", ids)
			}
		})
	}
	if !j.Entries[9].Enabled || j.Entries[4].Enabled || len(j.Invocations) != 0 {
		t.Fatal("read-only review changed authority")
	}
}
