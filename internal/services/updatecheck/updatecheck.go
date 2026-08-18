// Package updatecheck retrieves and validates Fluxo's latest published release.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	DefaultEndpoint  = "https://fluxo.fottify.com/api/v1/releases/latest"
	maxResponseBytes = 16 * 1024
	successCacheTTL  = 6 * time.Hour
	failureCacheTTL  = 15 * time.Minute
)

type Status struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	CheckAvailable  bool   `json:"check_available"`
	ReleaseURL      string `json:"release_url,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	CheckedAt       string `json:"checked_at,omitempty"`
}

type releaseManifest struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	PublishedAt string `json:"published_at"`
	ReleaseURL  string `json:"release_url"`
}

type Checker struct {
	currentVersion string
	endpoint       string
	client         *http.Client
	now            func() time.Time
	successTTL     time.Duration
	failureTTL     time.Duration

	mu        sync.Mutex
	cached    Status
	nextCheck time.Time
}

func New(currentVersion string) *Checker {
	return &Checker{
		currentVersion: strings.TrimSpace(currentVersion),
		endpoint:       DefaultEndpoint,
		client:         &http.Client{Timeout: 3 * time.Second},
		now:            time.Now,
		successTTL:     successCacheTTL,
		failureTTL:     failureCacheTTL,
	}
}

func normalizeVersion(version string) (string, bool) {
	value := strings.TrimSpace(version)
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return "", false
	}
	return value, true
}

func (c *Checker) Status(ctx context.Context) Status {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, validCurrent := normalizeVersion(c.currentVersion)
	if !validCurrent {
		return Status{CurrentVersion: c.currentVersion}
	}

	now := c.now().UTC()
	if !c.nextCheck.IsZero() && now.Before(c.nextCheck) {
		return c.cached
	}

	status := Status{CurrentVersion: strings.TrimPrefix(current, "v")}
	manifest, err := c.fetch(ctx)
	if err != nil {
		c.cached = status
		c.nextCheck = now.Add(c.failureTTL)
		return status
	}

	latest, _ := normalizeVersion(manifest.Version)
	status.LatestVersion = strings.TrimPrefix(latest, "v")
	status.UpdateAvailable = semver.Compare(latest, current) > 0
	status.CheckAvailable = true
	status.ReleaseURL = manifest.ReleaseURL
	status.PublishedAt = manifest.PublishedAt
	status.CheckedAt = now.Format(time.RFC3339)
	c.cached = status
	c.nextCheck = now.Add(c.successTTL)
	return status
}

func (c *Checker) fetch(ctx context.Context) (releaseManifest, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return releaseManifest{}, fmt.Errorf("prepare release request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Fluxo-Update-Check")

	response, err := c.client.Do(request)
	if err != nil {
		return releaseManifest{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return releaseManifest{}, fmt.Errorf("latest release endpoint returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return releaseManifest{}, fmt.Errorf("read latest release: %w", err)
	}
	if len(body) > maxResponseBytes {
		return releaseManifest{}, fmt.Errorf("latest release response exceeded the size limit")
	}

	var manifest releaseManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("decode latest release: %w", err)
	}
	latest, validLatest := normalizeVersion(manifest.Version)
	tag, validTag := normalizeVersion(manifest.Tag)
	if !validLatest || !validTag || latest != tag {
		return releaseManifest{}, fmt.Errorf("latest release endpoint returned an invalid version")
	}
	if err := validateReleaseURL(manifest.ReleaseURL, manifest.Tag); err != nil {
		return releaseManifest{}, err
	}
	if _, err := time.Parse(time.RFC3339, manifest.PublishedAt); err != nil {
		return releaseManifest{}, fmt.Errorf("latest release endpoint returned an invalid publication time")
	}
	return manifest, nil
}

func validateReleaseURL(rawURL, tag string) error {
	releaseURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("latest release endpoint returned an invalid release URL")
	}
	expectedPath := "/FabioTECH1/fluxo/releases/tag/" + tag
	if releaseURL.Scheme != "https" || releaseURL.Host != "github.com" || releaseURL.Path != expectedPath || releaseURL.RawQuery != "" || releaseURL.Fragment != "" {
		return fmt.Errorf("latest release endpoint returned an unexpected release URL")
	}
	return nil
}
