package scheduler

import (
	"context"
	"errors"
	"testing"
)

func TestCurrentNobleAllConsumersRequireFullClosureWithoutRelabelling(t *testing.T) {
	h := proofFixture().Branches["8.4"].Host
	basicCalls, inspections := 0, 0
	basic := func(context.Context) (legacyHost, error) { basicCalls++; return h, nil }
	inspect := func(profile string, pid int) (pamCandidateReport, error) {
		inspections++
		if profile != nobleLoaderPAMProfileID || pid != h.PID {
			t.Fatal("incorrect strict profile or daemon")
		}
		return pamCandidateReport{}, nil
	}
	got, err := qualifyNobleWithClosure(context.Background(), loaderEpochSchema, basic, inspect)
	if err != nil || got != h || basicCalls != 2 || inspections != 1 || got.Backend != "" || got.Configuration != "" {
		t.Fatal("strict qualification skipped or historical identity changed", got, err)
	}
	basicCalls, inspections = 0, 0
	if _, err = qualifyNobleWithClosure(context.Background(), loaderEpochSchema-1, basic, inspect); err != nil || basicCalls != 1 || inspections != 0 {
		t.Fatal("archived qualification acquired new profile semantics", err)
	}
}

func TestCurrentNobleInspectionFailureNeverFallsBackToBasicIdentity(t *testing.T) {
	h := proofFixture().Branches["8.4"].Host
	failure := errors.New("unreviewed NSS input")
	calls := 0
	basic := func(context.Context) (legacyHost, error) { calls++; return h, nil }
	got, err := qualifyNobleWithClosure(context.Background(), loaderEpochSchema, basic, func(string, int) (pamCandidateReport, error) { return pamCandidateReport{}, failure })
	if !errors.Is(err, failure) || got != (legacyHost{}) || calls != 1 {
		t.Fatal("strict closure failure fell back", got, err)
	}
	calls = 0
	basic = func(context.Context) (legacyHost, error) {
		calls++
		if calls == 2 {
			changed := h
			changed.PID++
			return changed, nil
		}
		return h, nil
	}
	if _, err = qualifyNobleWithClosure(context.Background(), loaderEpochSchema, basic, func(string, int) (pamCandidateReport, error) { return pamCandidateReport{}, nil }); err == nil {
		t.Fatal("daemon generation changed around strict inspection")
	}
}
