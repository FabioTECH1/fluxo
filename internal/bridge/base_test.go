package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/config"
	"strings"
	"testing"
)

func TestBaseReceiptRequiresExactIdentityAndContract(t *testing.T) {
	saved := BaseAdmission{Contract: BaseContract, Mode: "base", InstallationID: strings.Repeat("a", 32), KeySHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(saved)
	if _, err := decodeBaseAdmission(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BaseAdmission){
		func(r *BaseAdmission) { r.Contract = Contract },
		func(r *BaseAdmission) { r.Mode = "fresh" },
		func(r *BaseAdmission) { r.InstallationID = "" },
		func(r *BaseAdmission) { r.InstallationID = strings.Repeat("A", 32) },
		func(r *BaseAdmission) { r.KeySHA256 = "0.4.51" },
	} {
		changed := saved
		mutate(&changed)
		raw, _ := json.Marshal(changed)
		if _, err := decodeBaseAdmission(raw); err == nil {
			t.Fatal("invalid base identity admitted", changed)
		}
	}
	raw, _ = json.Marshal(saved)
	if _, err := decodeBaseAdmission(append(raw, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing receipt admitted")
	}
	if _, err := decodeBaseAdmission([]byte(strings.TrimSuffix(string(raw), "}") + `,"unknown":true}`)); err == nil {
		t.Fatal("unknown receipt contract admitted")
	}
}

func TestBaseBridgeSubjectIsPinnedToPublishedArtifact(t *testing.T) {
	// The gate's code pins real public artifact bytes, never a claimed version or
	// a completion phase. Both supported architectures remain explicit.
	if len(publishedBridgeSHA()) != 64 {
		t.Fatal("bridge artifact missing for build architecture")
	}
	if BridgeInstallCommand != "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.51/install.sh | sudo env FLUXO_VERSION=v0.4.51 bash" {
		t.Fatal("bridge instruction is not the pinned published installer")
	}
}

func TestBaseInspectionRetriesOnlyNaturalDrainAndRepeatsAdmission(t *testing.T) {
	pending := errors.New("typed natural drain stand-in")
	calls := 0
	want := BaseAdmission{Contract: BaseContract, Mode: "bridge"}
	got, err := retryBaseInspection(context.Background(), func() (BaseAdmission, error) {
		calls++
		if calls == 1 {
			return BaseAdmission{}, pending
		}
		return want, nil
	}, func(e error) bool { return errors.Is(e, pending) })
	if err != nil || got != want || calls != 2 {
		t.Fatal("full read-only admission was not repeated", got, err, calls)
	}
	permanent := errors.New("changed archive")
	calls = 0
	_, err = retryBaseInspection(context.Background(), func() (BaseAdmission, error) { calls++; return BaseAdmission{}, permanent }, func(e error) bool { return errors.Is(e, pending) })
	if !errors.Is(err, permanent) || calls != 1 {
		t.Fatal("permanent refusal retried", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = retryBaseInspection(ctx, func() (BaseAdmission, error) { return BaseAdmission{}, pending }, func(e error) bool { return errors.Is(e, pending) })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("natural drain wait ignored deadline", err)
	}
}

func TestBaseRetainsCurrentSettingsTokenCompatibility(t *testing.T) {
	db := fixture(t)
	if err := config.InitEncryption(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	token, err := config.EncryptSecret("settings-api-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO users(username,token_hash,github_pat) VALUES('admin','unchanged',?)", token); err != nil {
		t.Fatal(err)
	}
	if err = checkConversions(context.Background(), db); err == nil {
		t.Fatal("optional historical certification lost its strict conversion check")
	}
	if err = checkBaseConversions(context.Background(), db); err != nil {
		t.Fatal("base rejected a field still written by the current settings API", err)
	}
	var saved string
	if err = db.QueryRow("SELECT github_pat FROM users").Scan(&saved); err != nil || saved != token {
		t.Fatal("readiness changed the settings token", err)
	}
}
