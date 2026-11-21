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
				if scenario == "already-capable" {
					t.Fatal("capable helper triggered unnecessary host-profile qualification")
				}
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

func TestOptionalProfileReviewPropagatesCanceledQualification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := journal{Entries: map[int]registration{1: {Contract: Contract{ID: 1, Kind: ComposerUpdate}, Enabled: true}}}
	_, err := inspectOptionalProfileHelpers(ctx, j, func(context.Context, registration) (executiondomain.ReaderCapabilities, error) {
		return executiondomain.Capabilities(), nil
	}, func(context.Context, Contract) (profile, error) { cancel(); return profile{}, context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled overall review treated as unsupported configuration", err)
	}
}

func TestOptionalProfileReviewRefreshesV1HelperOnlyForExtendedHook(t *testing.T) {
	j := journal{Entries: map[int]registration{
		3: {Contract: Contract{ID: 3, Kind: CertificateRenewal}, Enabled: true},
		9: {Contract: Contract{ID: 9, Kind: ComposerUpdate}, Enabled: true},
	}}
	for _, contract := range []string{certbotDirectoryHookContract, certbotDirectoryHookExtendedContract} {
		ids, err := inspectOptionalProfileHelpers(context.Background(), j, func(context.Context, registration) (executiondomain.ReaderCapabilities, error) {
			old := HelperCapabilities()
			old.RoleContracts[optionalProfileRole] = []string{imagickStartupProfile, certbotDirectoryHookContract}
			return old, nil
		}, func(_ context.Context, c Contract) (profile, error) {
			if c.Kind == ComposerUpdate {
				t.Fatal("unrelated capable Composer helper requalified")
			}
			p := newProfile(c.Kind)
			p.External["certbot-directory-hook-contract"] = contract
			return p, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if contract == certbotDirectoryHookExtendedContract {
			if !reflect.DeepEqual(ids, []int{3}) {
				t.Fatal("v1 helper not selected for archived refresh", ids)
			}
		} else if len(ids) != 0 {
			t.Fatal("v1 hook unnecessarily refreshed", ids)
		}
	}
}
