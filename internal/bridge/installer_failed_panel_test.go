package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFailedPanelRecoveryRetainsExactInstallerBindings(t *testing.T) {
	v := installerExecutionIntent{Phase: "pending", Operation: "2177ef94-161b-4a2e-b37d-7dab78129c9d", Plan: strings.Repeat("d", 64), Image: strings.Repeat("a", 64), Version: "0.4.48", InstallationID: strings.Repeat("b", 32), Service: strings.Repeat("c", 64)}
	if !installerFailedRecoveryBound(&v, v.Image, v.Version, v.InstallationID) {
		t.Fatal("exact pending operation refused")
	}
	for _, name := range []string{"no-intent", "unpublished", "complete", "committed", "image", "version", "installation", "plan"} {
		t.Run(name, func(t *testing.T) {
			x := v
			var candidate *installerExecutionIntent = &x
			switch name {
			case "no-intent":
				candidate = nil
			case "unpublished":
				x.Operation = ""
			case "complete", "committed":
				x.Phase = name
			case "image":
				x.Image = strings.Repeat("e", 64)
			case "version":
				x.Version = "0.4.49"
			case "installation":
				x.InstallationID = strings.Repeat("e", 32)
			case "plan":
				x.Plan = ""
			}
			if installerFailedRecoveryBound(candidate, v.Image, v.Version, v.InstallationID) {
				t.Fatal("unbound failed state admitted")
			}
		})
	}
}

func TestFailedPanelRecoveryChecksArchiveAndDomainBeforeReset(t *testing.T) {
	p := map[string]string{"LoadState": "loaded", "ActiveState": "failed", "MainPID": "0", "ControlPID": "0", "ControlGroup": "/system.slice/fluxo.service"}
	intent := installerExecutionIntent{Phase: "pending", Operation: "exact-operation", Plan: "exact-plan", Service: "exact-service"}
	var calls []string
	check := func(name string) func() error { return func() error { calls = append(calls, name); return nil } }
	if err := installerFailedPanelTransition(p, intent, intent.Service, check("archive+binding"), check("empty"), check("reset"), check("stopped+binding")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"archive+binding", "empty", "archive+binding", "empty", "reset", "stopped+binding"}) {
		t.Fatal("wrong failed-state recovery order", calls)
	}
	for _, name := range []string{"service", "unpublished", "complete", "running-main", "running-control", "foreign-domain", "not-loaded"} {
		t.Run(name, func(t *testing.T) {
			q := map[string]string{}
			for k, v := range p {
				q[k] = v
			}
			v := intent
			switch name {
			case "service":
				v.Service = "other"
			case "unpublished":
				v.Operation = ""
			case "complete":
				v.Phase = "complete"
			case "running-main":
				q["MainPID"] = "12"
			case "running-control":
				q["ControlPID"] = "13"
			case "foreign-domain":
				q["ControlGroup"] = "/other.slice/fluxo.service"
			case "not-loaded":
				q["LoadState"] = "not-found"
			}
			calls = nil
			if installerFailedPanelTransition(q, v, intent.Service, check("archive"), check("empty"), check("reset"), check("stopped")) == nil || len(calls) != 0 {
				t.Fatal("unrelated or live failed panel was modified", calls)
			}
		})
	}
	for _, failAt := range []int{0, 1, 2, 3, 4, 5} {
		t.Run("failed-proof-"+string(rune('0'+failAt)), func(t *testing.T) {
			count := 0
			reset := false
			failure := errors.New("refuse changed evidence")
			step := func() error {
				n := count
				count++
				if n == failAt {
					return failure
				}
				return nil
			}
			err := installerFailedPanelTransition(p, intent, intent.Service, step, step, func() error { reset = true; return step() }, step)
			if !errors.Is(err, failure) || count != failAt+1 || (failAt < 4 && reset) {
				t.Fatal("crossed a failed recovery boundary", count, reset, err)
			}
		})
	}
	calls = nil
	p["ActiveState"] = "inactive"
	if err := installerFailedPanelTransition(p, intent, intent.Service, check("archive"), check("empty"), check("reset"), check("stopped")); err != nil || len(calls) != 0 {
		t.Fatal("ordinary inactive replay changed", calls, err)
	}
}

func TestFailedPanelRecoveryRequiresAnEmptyHierarchy(t *testing.T) {
	base := t.TempDir()
	group := filepath.Join(base, "fluxo.service")
	if err := installerPanelDomainEmpty(group); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(group, 0755); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"populated 1\nfrozen 0\n", "frozen 0\n", "", "populated unknown\n", "populated 0\npopulated 1\n"} {
		if err := os.WriteFile(filepath.Join(group, "cgroup.events"), []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if installerPanelDomainEmpty(group) == nil {
			t.Fatal("live or uncertain domain admitted", raw)
		}
	}
	if err := os.WriteFile(filepath.Join(group, "cgroup.events"), []byte("populated 0\nfrozen 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := installerPanelDomainEmpty(group); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(group, link); err != nil {
		t.Fatal(err)
	}
	if installerPanelDomainEmpty(link) == nil {
		t.Fatal("domain symlink admitted")
	}
}

func TestFailedPanelMissingHierarchyNeverReachesReset(t *testing.T) {
	root := t.TempDir()
	properties := map[string]string{"LoadState": "loaded", "ActiveState": "failed", "MainPID": "0", "ControlPID": "0"}
	intent := installerExecutionIntent{Phase: "pending", Operation: "exact-operation", Plan: "exact-plan", Service: "exact-service"}
	for _, path := range []string{filepath.Join(root, "missing", "system.slice", "fluxo.service"), filepath.Join(root, "system.slice", "fluxo.service")} {
		reset := false
		err := installerFailedPanelTransition(properties, intent, intent.Service, func() error { return nil }, func() error {
			return installerFailedPanelDomainEmpty(path)
		}, func() error { reset = true; return nil }, func() error { return nil })
		if err == nil || reset {
			t.Fatal("missing parent hierarchy was treated as an empty unit", err, reset)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "system.slice"), 0755); err != nil {
		t.Fatal(err)
	}
	if installerFailedPanelDomainEmpty(filepath.Join(root, "system.slice", "fluxo.service")) == nil {
		t.Fatal("ordinary filesystem was treated as a cgroup2 hierarchy")
	}
}
