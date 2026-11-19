package maintenancetask

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoricalWithdrawalRequiresExactTerminalReceipt(t *testing.T) {
	const unit = "fluxo-system-task-03874837-a476-4897-9135-fb523ec7e542.service"
	const added = "fluxo-system-task-8f1775dc-48bb-4e96-885f-fbde39f9b996.service"
	root := t.TempDir()
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(root, name+".json")) }
	r := registration{Enabled: true, Received: true, Published: true, Contract: Contract{ID: 9, Kind: CertificateRenewal}, Boot: "boot"}
	prior := invocation{Unit: unit, Registration: r, Boot: r.Boot, Status: "completed", Profile: newProfile(CertificateRenewal)}
	old := journal{1, map[int]registration{9: r}, map[string]invocation{unit: prior}}
	live := journal{1, map[int]registration{9: r}, map[string]invocation{}}
	check := func(ok bool) {
		t.Helper()
		if err := validateHistoricalCompaction(old, live, read); (err == nil) != ok {
			t.Fatalf("want accepted=%t, got %v", ok, err)
		}
	}
	check(false)
	raw, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, unit+".json")
	if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	check(true)
	changed := r
	changed.Contract.Command = "different"
	live.Entries[9] = changed
	check(false)
	live.Entries[9] = r
	v := prior
	v.Unit = added
	v.Status = "blocked"
	v.Error = "wait for maintenance lock: context deadline exceeded"
	live.Invocations[added] = v
	check(true)
	for _, status := range []string{"submitted", "started", "failed", "completed", "interrupted"} {
		other := v
		other.Status = status
		live.Invocations[added] = other
		check(false)
	}
	for _, change := range []func(*invocation){func(i *invocation) { i.ApplicationStarted = true }, func(i *invocation) { i.Error = "foreign" }, func(i *invocation) { i.Boot = "foreign" }, func(i *invocation) { i.Registration.Helper = "foreign" }, func(i *invocation) {
		i.Profile = newProfile(CertificateRenewal)
		i.Profile.External["unexpected"] = "profile"
	}} {
		other := v
		change(&other)
		live.Invocations[added] = other
		check(false)
	}
	delete(live.Invocations, added)
	live.Invocations[unit] = prior
	check(false)
}
