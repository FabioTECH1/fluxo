package scheduler

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
	"github.com/google/uuid"
)

func sealedInvocationFixture() invocation {
	e := Entry{ID: 7, Identity: uuid.NewString(), LoaderNonce: uuid.NewString()}
	v := invocation{ExecutionContract: executiondomain.SchedulerBootstrapContract, HelperSHA256: strings.Repeat("a", 64), Adapter: executiondomain.CgroupExitV1, Guarded: true, Entry: e, Boot: uuid.NewString()}
	v.Helper = filepath.Join(stateDir, "helpers", v.HelperSHA256)
	v.ServiceIdentity = &executiondomain.ServiceIdentity{InvocationID: strings.Repeat("b", 32), Device: 1, Inode: 2}
	v.Bootstrap = &bootstrapSeal{ID: uuid.NewString(), Phase: "pending", PID: 123, Start: "987", Boot: v.Boot, EntryIdentity: e.Identity, ImageSHA256: v.HelperSHA256, ImageDevice: 1, ImageInode: 9, Service: v.ServiceIdentity, Lease: packages.LifecycleTransfer{FD: 7, Device: 1, Inode: 4, OwnerPID: 123, MarkerOffset: 10}, Loader: LoaderBinding{EpochID: uuid.NewString(), EntryID: e.ID, Binding: e.Identity, Nonce: e.LoaderNonce, TriggerHash: hashBytes([]byte(CronContent(e))), ProviderHash: strings.Repeat("c", 64)}}
	return v
}
func TestBootstrapSealAdmissionShapes(t *testing.T) {
	good := sealedInvocationFixture()
	if err := validateBootstrapInvocation(good); err != nil {
		t.Fatal(err)
	}
	consumed := good
	consumed.Started = true
	seal := *good.Bootstrap
	seal.Phase = "consumed"
	consumed.Bootstrap = &seal
	if err := validateBootstrapInvocation(consumed); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*invocation){
		"unknown contract":        func(v *invocation) { v.ExecutionContract = "future" },
		"legacy reinterpretation": func(v *invocation) { v.ExecutionContract = "" },
		"mutable dispatcher":      func(v *invocation) { v.Helper = filepath.Join(stateDir, "dispatcher") },
		"image mismatch":          func(v *invocation) { v.Bootstrap.ImageSHA256 = strings.Repeat("d", 64) },
		"boot mismatch":           func(v *invocation) { v.Bootstrap.Boot = uuid.NewString() },
		"entry mismatch":          func(v *invocation) { v.Bootstrap.EntryIdentity = uuid.NewString() },
		"nonce mismatch":          func(v *invocation) { v.Bootstrap.Loader.Nonce = uuid.NewString() },
		"wrong trigger":           func(v *invocation) { v.Bootstrap.Loader.TriggerHash = strings.Repeat("d", 64) },
		"unknown loader":          func(v *invocation) { v.Bootstrap.Loader.EpochID = "" },
		"missing service":         func(v *invocation) { v.ServiceIdentity = nil },
		"wrong FD owner":          func(v *invocation) { v.Bootstrap.Lease.OwnerPID++ },
		"stdio FD":                func(v *invocation) { v.Bootstrap.Lease.FD = 2 },
		"missing OFD marker":      func(v *invocation) { v.Bootstrap.Lease.MarkerOffset = 0 },
		"unconsumed started":      func(v *invocation) { v.Started = true },
		"consumed unstarted":      func(v *invocation) { v.Bootstrap.Phase = "consumed" },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(good)
			var v invocation
			_ = json.Unmarshal(raw, &v)
			change(&v)
			if validateBootstrapInvocation(v) == nil {
				t.Fatal("invalid durable authority accepted")
			}
		})
	}
	// Historical started records remain readable without acquiring a new seal.
	if err := validateBootstrapInvocation(invocation{Started: true}); err != nil {
		t.Fatal(err)
	}
}
