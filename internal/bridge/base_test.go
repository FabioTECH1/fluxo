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
	for arch, want := range map[string]string{
		"amd64": "4be2cf6e84a9c0412f313721dedf432d227a2a1f1b17197639c0c1c6f7d4a418",
		"arm64": "0e9f87f7d240afbdcbe7f5f3b2506d5e097ccf1dc092e34ed67968e1466eb02c",
		"386":   "",
		"":      "",
	} {
		if got := publishedBridgeSHAForArchitecture(arch); got != want {
			t.Fatalf("bridge artifact for %q: got %q, want %q", arch, got, want)
		}
	}
	if BridgeInstallCommand != "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.55/install.sh | sudo env FLUXO_VERSION=v0.4.55 bash" {
		t.Fatal("bridge instruction is not the pinned published installer")
	}
}

func TestBaseReceiptCannotTurnSupersededBridgeIntoAdmittedBase(t *testing.T) {
	for arch, predecessors := range map[string][]string{
		"amd64": {"345b52da04ad779ffc5f1d14bc6279592face802ada55b1198b5e08a29f846e6", "1c41d04187f1783157b99c2cb2f5b2b507ca4c1552c3216281ec8022543aad0f", "430c9567fda2ab0b8d13fd86ed670ddc785116c42eebfb3c2b3e942ba060a522", "6917de23068d56295e7fce8566e14cbc3af9a1defbbb6fc4314f1d88828a8921"},
		"arm64": {"e39f4d2fd5c2f6a1d34318af7ca50f61a13229832d1a1c99918ec616363b222b", "3699fd11add2869befff273113ab26399df1cb150506bcf6de9070078326fb9b", "a89b0acde648d98452ede5de8be5f98f7e462cbda9811cdceebb01b0239de405", "5106d520c278e07c0286e0d950f7aa6fefa5cad8563a278b47ffa7661b58540b"},
	} {
		for _, previous := range predecessors {
			if !knownBridgePredecessor(arch, previous) || !knownBridgePredecessor(arch, publishedBridgeSHAForArchitecture(arch)) {
				t.Fatal("a bridge beside a receipt would skip legacy admission", arch)
			}
			if previous == publishedBridgeSHAForArchitecture(arch) || knownBridgePredecessor(arch, strings.Repeat("c", 64)) || knownBridgePredecessor("386", previous) {
				t.Fatal("superseded/unknown image became an admitted predecessor", arch)
			}
		}
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
