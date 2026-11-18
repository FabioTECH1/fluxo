package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// A full identity includes generation inputs (for example INVOCATION_ID and
// PID1's live DynamicUser sockets). PolicyHash deliberately separates reviewed
// static code/configuration/account resolution from those live observations.
func qualifyLoaderProvider(ctx context.Context, saved *LoaderProvider) (LoaderProvider, error) {
	result, err := inspectLoaderProvider(ctx, saved)
	if err != nil {
		return result, err
	}
	if saved != nil && (saved.Identity.Backend != result.Identity.Backend || saved.Profile != result.Profile || saved.PolicyHash != result.PolicyHash) {
		return LoaderProvider{}, errors.New("loader provider policy changed; rerun the normal installer to verify supported changes")
	}
	return result, nil
}

// Observation retains the saved backend selector. Only the explicit reviewed
// publication path may use it without the ordinary policy-equality guard.
func inspectLoaderProvider(ctx context.Context, saved *LoaderProvider) (LoaderProvider, error) {
	var host CronBackendIdentity
	var err error
	if saved == nil {
		host, err = QualifyCronBackend(ctx)
	} else {
		host, err = RequalifyCronBackend(ctx, saved.Identity)
	}
	if err != nil {
		return LoaderProvider{}, err
	}
	profile := host.Backend
	if profile == "" {
		profile = nobleLoaderPAMProfileID
	} else if profile == "cronie-noble-1" {
		profile = cronieLoaderPAMProfileID
	} else if !newDistributionBackend(profile) {
		return LoaderProvider{}, errors.New("backend requires a separately qualified current loader profile")
	}
	report, err := inspectPAMCandidate(profile, host.PID)
	if err != nil {
		return LoaderProvider{}, err
	}
	identity := map[string]string{}
	for key, value := range report.Identity {
		if key != "userdb-dynamic-listener" && key != "cron-mount-namespace" {
			identity[key] = value
		}
	}
	c, err := distributionCandidate(profile)
	if err != nil {
		return LoaderProvider{}, err
	}
	environment, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", host.PID))
	if err != nil {
		return LoaderProvider{}, err
	}
	stableEnvironment, err := loaderStableEnvironment(environment, host.PID)
	if err != nil {
		return LoaderProvider{}, err
	}
	policy, err := json.Marshal(struct {
		Profile      string              `json:"profile"`
		Binary       string              `json:"binary"`
		Revision     string              `json:"revision"`
		Files        map[string]string   `json:"files"`
		Packages     map[string]string   `json:"packages"`
		Dependencies map[string][]string `json:"dependencies"`
		Identity     map[string]string   `json:"identity"`
		Environment  string              `json:"stable_daemon_environment"`
	}{profile, c.binary, report.Revision, report.Files, report.Packages, report.Dependencies, identity, stableEnvironment})
	if err != nil {
		return LoaderProvider{}, err
	}
	now, err := RequalifyCronBackend(ctx, host)
	if err != nil || now != host {
		return LoaderProvider{}, errors.New("loader provider changed around static closure qualification")
	}
	runtimeReport, err := json.Marshal(struct {
		Files        map[string]string   `json:"files"`
		Packages     map[string]string   `json:"packages"`
		Dependencies map[string][]string `json:"dependencies"`
		Identity     map[string]string   `json:"identity"`
		Environment  string              `json:"daemon_environment"`
	}{report.Files, report.Packages, report.Dependencies, report.Identity, hashBytes(environment)})
	if err != nil {
		return LoaderProvider{}, err
	}
	result := LoaderProvider{Identity: host, Profile: profile + "/loader-policy-v1", PolicyHash: hashBytes(policy), RuntimeHash: hashBytes(runtimeReport)}
	return result, nil
}

func sameLoaderProvider(a, b LoaderProvider) bool { return reflect.DeepEqual(a, b) }

func loaderStableEnvironment(raw []byte, pid int) (string, error) {
	environment := map[string]string{}
	seen := map[string]bool{}
	for _, entry := range strings.Split(string(raw), "\x00") {
		if entry == "" {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return "", errors.New("invalid provider environment")
		}
		if seen[key] {
			return "", errors.New("ambiguous provider environment")
		}
		seen[key] = true
		// These are systemd-generated runtime identities, not configurable
		// selectors. Validate their shape and preserve every other key/value,
		// including unknown variables, in the stable policy digest.
		switch key {
		case "INVOCATION_ID":
			if len(value) != 32 || !loaderDigestValid(value+value) {
				return "", errors.New("invalid provider invocation environment")
			}
			continue
		case "SYSTEMD_EXEC_PID":
			if value != strconv.Itoa(pid) {
				return "", errors.New("provider execution environment PID changed")
			}
			continue
		case "JOURNAL_STREAM":
			parts := strings.Split(value, ":")
			if len(parts) != 2 {
				return "", errors.New("invalid journal stream generation")
			}
			for _, part := range parts {
				if n, e := strconv.ParseUint(part, 10, 64); e != nil || strconv.FormatUint(n, 10) != part {
					return "", errors.New("invalid journal stream generation")
				}
			}
			continue
		}
		environment[key] = value
	}
	data, err := json.Marshal(environment)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}
