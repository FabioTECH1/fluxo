package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestWithdrawalRejectsPartialAndIndistinguishableTargets(t *testing.T) {
	before, after := []byte("previous nonce"), []byte("renewed nonce")
	f := executiondomain.ActivationFile{Path: "cron", Cron: true, Before: digest(before), Data: after, Mode: 0644, CronMTime: 20}
	if err := validateWithdrawalTarget(f, before, 0644, 19); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		raw   []byte
		mode  os.FileMode
		mtime int64
	}{
		{"intended", after, 0644, 19}, {"foreign", nil, 0644, 19}, {"unsafe-mode", before, 0664, 19}, {"published-mtime", before, 0644, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if validateWithdrawalTarget(f, tc.raw, tc.mode, tc.mtime) == nil {
				t.Fatal("admitted ambiguous or partially applied target")
			}
		})
	}
	f.Data = before
	if validateWithdrawalTarget(f, before, 0644, 19) == nil {
		t.Fatal("indistinguishable target admitted")
	}
}

func TestWithdrawalArchiveCrashAndBoundRestart(t *testing.T) {
	for _, stage := range []bool{true, false} {
		base := t.TempDir()
		staged, published, retired := filepath.Join(base, "stage"), filepath.Join(base, "original"), filepath.Join(base, "retired")
		src := published
		if stage {
			src = staged
		}
		if err := os.Mkdir(src, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "plan.json"), []byte("exact plan"), 0600); err != nil {
			t.Fatal(err)
		}
		pointer, closed := filepath.Join(base, "intent"), filepath.Join(base, "restored-intent")
		if err := os.WriteFile(pointer, []byte("original intent"), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := withdrawalArchive(staged, published, retired); err != nil || got != src {
			t.Fatalf("before move: %q %v", got, err)
		}
		info, err := os.Stat(src)
		if err != nil {
			t.Fatal(err)
		}
		if err = withdrawalRename(src, retired); err != nil {
			t.Fatal(err)
		}
		if got, err := withdrawalArchive(staged, published, retired); err != nil || got != retired {
			t.Fatalf("after move: %q %v", got, err)
		}
		dst, err := os.Stat(retired)
		if err != nil || !os.SameFile(info, dst) {
			t.Fatal("directory identity changed")
		}
		if err = os.Mkdir(src, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err = withdrawalArchive(staged, published, retired); err == nil {
			t.Fatal("duplicate archive admitted")
		}
		if err = withdrawalRename(src, retired); err == nil {
			t.Fatal("overwrote original retired archive")
		}
		if err = os.Remove(src); err != nil {
			t.Fatal(err)
		}
		interrupted := errors.New("interrupted before closing intent")
		active, starts := false, 0
		binding := func() (bool, error) { return active, nil }
		start := func() error { active = true; starts++; return nil }
		err = finishWithdrawnRenewal(true, binding, start, func() error { return interrupted })
		if !errors.Is(err, interrupted) || !active || starts != 1 {
			t.Fatal("lost restart across interruption")
		}
		if raw, err := os.ReadFile(pointer); err != nil || string(raw) != "original intent" {
			t.Fatal("original restart pointer lost")
		}
		err = finishWithdrawnRenewal(true, binding, start, func() error { return withdrawalRename(pointer, closed) })
		if err != nil || starts != 1 {
			t.Fatalf("second start or failed completion: %v", err)
		}
		if raw, err := os.ReadFile(closed); err != nil || string(raw) != "original intent" {
			t.Fatal("wrong closed intent")
		}
		if raw, err := os.ReadFile(filepath.Join(retired, "plan.json")); err != nil || string(raw) != "exact plan" {
			t.Fatal("plan changed")
		}
	}
}

func TestWithdrawalRefusesChangedBindingAtRestart(t *testing.T) {
	calls := 0
	err := finishWithdrawnRenewal(true, func() (bool, error) {
		calls++
		if calls == 2 {
			return false, errors.New("installed image changed")
		}
		return false, nil
	}, func() error { return nil }, func() error { t.Fatal("retired intent with a changed binding"); return nil })
	if err == nil {
		t.Fatal("missing restart binding error")
	}
	if err = finishWithdrawnRenewal(false, func() (bool, error) { return false, nil }, func() error { t.Fatal("unexpected start"); return nil }, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
