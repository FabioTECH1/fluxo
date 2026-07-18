package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"

	"github.com/google/uuid"
)

var safeObjectSegment = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type Manager struct {
	dataDir       string
	wake          chan struct{}
	mu            sync.Mutex
	deletingSites map[int]bool
}

func NewManager(dataDir string) *Manager {
	return &Manager{dataDir: dataDir, wake: make(chan struct{}, 1), deletingSites: make(map[int]bool)}
}

func (manager *Manager) Start(ctx context.Context) {
	if err := database.MarkInterruptedBackupRunsFailed(); err != nil {
		log.Printf("Backup: failed to mark interrupted runs: %v", err)
	}
	manager.cleanupStaleWorkdirs()
	go manager.worker(ctx)
	go manager.scheduler(ctx)
	manager.signal()
}

func (manager *Manager) signal() {
	select {
	case manager.wake <- struct{}{}:
	default:
	}
}

func (manager *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-manager.wake:
			for {
				runID, err := database.NextQueuedBackupRunID()
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					log.Printf("Backup: load queued run: %v", err)
					break
				}
				manager.executeRunSafely(ctx, runID)
			}
		}
	}
}

func (manager *Manager) executeRunSafely(ctx context.Context, runID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = database.MarkBackupRunFailed(runID, "Backup worker stopped unexpectedly.")
			log.Printf("Backup: run %s recovered from panic: %v", runID, recovered)
		}
	}()
	manager.executeRun(ctx, runID)
}

func (manager *Manager) cleanupStaleWorkdirs() {
	tempRoot := filepath.Join(manager.dataDir, "backup-tmp")
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "run-") {
			_ = os.RemoveAll(filepath.Join(tempRoot, entry.Name()))
		}
	}
}

func (manager *Manager) scheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	manager.enqueueDuePlans(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			manager.enqueueDuePlans(now)
		}
	}
}

func (manager *Manager) enqueueDuePlans(now time.Time) {
	planIDs, err := database.DueBackupPlanIDs(now)
	if err != nil {
		log.Printf("Backup: load due plans: %v", err)
		return
	}
	for _, planID := range planIDs {
		if _, err := manager.enqueueScheduledPlan(planID, now); err != nil && !strings.Contains(err.Error(), "already has") {
			log.Printf("Backup: enqueue plan %d: %v", planID, err)
		}
	}
}

func (manager *Manager) enqueueScheduledPlan(planID int, now time.Time) (database.BackupRun, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	plan, err := database.GetBackupPlan(planID)
	if err != nil {
		return database.BackupRun{}, err
	}
	if !plan.Enabled || plan.Schedule == "manual" || plan.NextRunAt == nil || plan.NextRunAt.After(now) {
		return database.BackupRun{}, errors.New("backup plan is no longer due")
	}
	next := NextRunAt(plan.Schedule, plan.BackupHour, now)
	if next.IsZero() {
		return database.BackupRun{}, errors.New("backup plan has an invalid schedule")
	}
	active, err := database.BackupPlanHasActiveRun(plan.ID)
	if err != nil {
		return database.BackupRun{}, err
	}
	if active {
		if err := database.SetBackupPlanScheduleState(plan.ID, next, false); err != nil {
			return database.BackupRun{}, err
		}
		return database.BackupRun{}, errors.New("this plan already has a queued or running backup")
	}
	if _, err := database.GetBackupDestination(plan.DestinationID); err != nil {
		return database.BackupRun{}, errors.New("backup destination is unavailable")
	}
	run := newBackupRun(plan, "scheduled")
	if err := database.CreateScheduledBackupRun(run, next); err != nil {
		return database.BackupRun{}, err
	}
	manager.signal()
	return run, nil
}

func (manager *Manager) EnqueuePlan(planID int, trigger string) (database.BackupRun, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if trigger != "manual" && trigger != "scheduled" {
		return database.BackupRun{}, errors.New("invalid backup trigger")
	}
	active, err := database.BackupPlanHasActiveRun(planID)
	if err != nil {
		return database.BackupRun{}, err
	}
	if active {
		return database.BackupRun{}, errors.New("this plan already has a queued or running backup")
	}
	plan, err := database.GetBackupPlan(planID)
	if err != nil {
		return database.BackupRun{}, err
	}
	if _, err := database.GetBackupDestination(plan.DestinationID); err != nil {
		return database.BackupRun{}, errors.New("backup destination is unavailable")
	}
	run := newBackupRun(plan, trigger)
	if err := database.CreateBackupRun(run); err != nil {
		return database.BackupRun{}, err
	}
	manager.signal()
	return run, nil
}

func newBackupRun(plan database.BackupPlan, trigger string) database.BackupRun {
	return database.BackupRun{
		ID: uuid.NewString(), PlanID: plan.ID, PlanName: plan.Name,
		DestinationID: plan.DestinationID, DestinationName: plan.DestinationName,
		SiteID: plan.SiteID, SiteDomain: plan.SiteDomain, Trigger: trigger, Status: "queued",
	}
}

func (manager *Manager) CreatePlan(plan *database.BackupPlan) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.deletingSites[plan.SiteID] {
		return errors.New("site is being deleted")
	}
	if err := validatePlanReferences(*plan); err != nil {
		return err
	}
	return database.CreateBackupPlan(plan)
}

func (manager *Manager) UpdateDestination(destination database.BackupDestination) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return database.UpdateBackupDestination(destination)
}

func (manager *Manager) DeleteDestination(id int) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return database.DeleteBackupDestination(id)
}

func (manager *Manager) UpdatePlan(plan database.BackupPlan) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	active, err := database.BackupPlanHasActiveRun(plan.ID)
	if err != nil {
		return err
	}
	if active {
		return errors.New("plan has a queued or running backup")
	}
	if manager.deletingSites[plan.SiteID] {
		return errors.New("site is being deleted")
	}
	if err := validatePlanReferences(plan); err != nil {
		return err
	}
	return database.UpdateBackupPlan(plan)
}

func validatePlanReferences(plan database.BackupPlan) error {
	var siteExists bool
	if err := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sites WHERE id = ?)", plan.SiteID).Scan(&siteExists); err != nil {
		return err
	}
	if !siteExists {
		return errors.New("site not found")
	}
	if _, err := database.GetBackupDestination(plan.DestinationID); err != nil {
		return errors.New("backup destination not found")
	}
	for _, databaseID := range plan.DatabaseIDs {
		var siteID int
		if err := database.DB.QueryRow("SELECT site_id FROM databases WHERE id = ?", databaseID).Scan(&siteID); err != nil || siteID != plan.SiteID {
			return fmt.Errorf("database %d is not linked to the selected site", databaseID)
		}
	}
	return nil
}

func (manager *Manager) DeletePlan(planID int) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return database.DeleteBackupPlan(planID)
}

func (manager *Manager) PrepareSiteDeletion(siteID int) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var active int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM backup_runs WHERE site_id = ? AND status IN ('queued', 'running')", siteID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("wait for the site's active backup to finish before deleting it")
	}
	manager.deletingSites[siteID] = true
	if err := database.DeleteBackupPlansForSite(siteID); err != nil {
		delete(manager.deletingSites, siteID)
		return err
	}
	return nil
}

func (manager *Manager) FinishSiteDeletion(siteID int) {
	manager.mu.Lock()
	delete(manager.deletingSites, siteID)
	manager.mu.Unlock()
}

func (manager *Manager) DeleteDatabase(databaseID int, deleteFn func() error) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var references int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM backup_plan_databases WHERE database_id = ?", databaseID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("database is still used by a backup plan")
	}
	return deleteFn()
}

func NextRunAt(schedule string, hour int, from time.Time) time.Time {
	if hour < 0 || hour > 23 {
		hour = 2
	}
	switch schedule {
	case "every_6_hours":
		return nextAnchoredInterval(from, hour, 6*time.Hour)
	case "every_12_hours":
		return nextAnchoredInterval(from, hour, 12*time.Hour)
	case "daily":
		candidate := time.Date(from.Year(), from.Month(), from.Day(), hour, 0, 0, 0, from.Location())
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate
	case "weekly":
		daysUntilSunday := (7 - int(from.Weekday())) % 7
		candidate := time.Date(from.Year(), from.Month(), from.Day()+daysUntilSunday, hour, 0, 0, 0, from.Location())
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate
	default:
		return time.Time{}
	}
}

func nextAnchoredInterval(from time.Time, hour int, interval time.Duration) time.Time {
	base := time.Date(from.Year(), from.Month(), from.Day(), hour, 0, 0, 0, from.Location())
	if base.After(from) {
		base = base.Add(-24 * time.Hour)
	}
	steps := int(from.Sub(base)/interval) + 1
	return base.Add(time.Duration(steps) * interval)
}

func (manager *Manager) TestDestination(ctx context.Context, destination database.BackupDestination) error {
	prefix, err := normalizePrefix(destination.Prefix)
	if err != nil {
		return err
	}
	destination.Prefix = prefix
	store, err := newObjectStore(ctx, destination)
	if err != nil {
		return err
	}
	return store.test(ctx, prefix)
}

func (manager *Manager) PresignArtifact(ctx context.Context, runID string, artifactID int) (string, error) {
	run, err := database.GetBackupRun(runID)
	if err != nil {
		return "", err
	}
	if run.Status != "completed" {
		return "", errors.New("backup is not complete")
	}
	var artifact *database.BackupArtifact
	for index := range run.Artifacts {
		if run.Artifacts[index].ID == artifactID {
			artifact = &run.Artifacts[index]
			break
		}
	}
	if artifact == nil {
		return "", sql.ErrNoRows
	}
	destination, err := database.GetBackupDestination(run.DestinationID)
	if err != nil {
		return "", errors.New("backup destination is unavailable")
	}
	store, err := newObjectStore(ctx, destination)
	if err != nil {
		return "", err
	}
	return store.presignDownload(ctx, artifact.ObjectKey, artifact.ObjectVersionID, artifact.Filename, 5*time.Minute)
}

func (manager *Manager) DeleteRun(ctx context.Context, runID string) error {
	run, err := database.GetBackupRun(runID)
	if err != nil {
		return err
	}
	if run.Status == "queued" || run.Status == "running" {
		return errors.New("a queued or running backup cannot be deleted")
	}
	if run.Status == "failed" && len(run.Artifacts) == 0 && run.ManifestKey == "" {
		return database.DeleteBackupRunRecord(run.ID)
	}
	destination, err := database.GetBackupDestination(run.DestinationID)
	if err != nil {
		return errors.New("backup destination is unavailable")
	}
	store, err := newObjectStore(ctx, destination)
	if err != nil {
		return err
	}
	objects := make([]objectReference, 0, len(run.Artifacts)+1)
	for _, artifact := range run.Artifacts {
		objects = append(objects, objectReference{Key: artifact.ObjectKey, VersionID: artifact.ObjectVersionID})
	}
	if run.ManifestKey != "" {
		objects = append(objects, objectReference{Key: run.ManifestKey, VersionID: run.ManifestVersionID})
	}
	if err := store.deleteObjects(ctx, objects); err != nil {
		return fmt.Errorf("delete remote backup: %w", err)
	}
	return database.DeleteBackupRunRecord(run.ID)
}

func (manager *Manager) executeRun(parent context.Context, runID string) {
	if err := database.MarkBackupRunRunning(runID); err != nil {
		log.Printf("Backup: mark run %s running: %v", runID, err)
		return
	}
	run, err := database.GetBackupRun(runID)
	if err != nil {
		return
	}
	plan, err := database.GetBackupPlan(run.PlanID)
	if err != nil {
		manager.failRun(run, err, nil)
		return
	}
	destination, err := database.GetBackupDestination(run.DestinationID)
	if err != nil {
		manager.failRun(run, err, nil)
		return
	}
	var site database.Site
	err = database.DB.QueryRow(`SELECT id, domain, path, repository, branch, php_version, app_type,
		deployment_strategy, web_root, created_at, updated_at FROM sites WHERE id = ?`, run.SiteID).Scan(
		&site.ID, &site.Domain, &site.Path, &site.Repository, &site.Branch, &site.PHPVersion,
		&site.AppType, &site.DeploymentStrategy, &site.WebRoot, &site.CreatedAt, &site.UpdatedAt)
	if err != nil {
		manager.failRun(run, err, nil)
		return
	}

	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	store, err := newObjectStore(ctx, destination)
	if err != nil {
		manager.failRun(run, err, nil)
		return
	}
	tempRoot := filepath.Join(manager.dataDir, "backup-tmp")
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		manager.failRun(run, err, store)
		return
	}
	tempInfo, err := os.Lstat(tempRoot)
	if err != nil || !tempInfo.IsDir() || tempInfo.Mode()&os.ModeSymlink != 0 {
		manager.failRun(run, errors.New("backup work directory is not a secure directory"), store)
		return
	}
	_ = os.Chmod(tempRoot, 0700)
	workDir, err := os.MkdirTemp(tempRoot, "run-")
	if err != nil {
		manager.failRun(run, err, store)
		return
	}
	defer os.RemoveAll(workDir)

	artifacts, err := buildArtifacts(ctx, plan, site, run.ID, workDir)
	if err != nil {
		manager.failRun(run, err, store)
		return
	}
	baseKey, err := runObjectBase(destination, run, site)
	if err != nil {
		manager.failRun(run, err, store)
		return
	}
	manifestKey := baseKey + "/manifest.json"
	planned := make([]database.BackupArtifact, len(artifacts))
	for index := range artifacts {
		artifacts[index].ObjectKey = baseKey + "/" + artifacts[index].Filename
		planned[index] = artifacts[index].BackupArtifact
	}
	if err := database.PrepareBackupRunObjects(run.ID, manifestKey, planned); err != nil {
		manager.failRun(run, err, store)
		return
	}
	for index := range artifacts {
		artifacts[index].ID = planned[index].ID
	}
	var totalSize int64
	for index := range artifacts {
		versionID, uploadErr := store.uploadFile(ctx, artifacts[index].ObjectKey, artifacts[index].Path,
			contentTypeForArtifact(artifacts[index].Filename), artifacts[index].SHA256)
		if versionID != "" {
			artifacts[index].ObjectVersionID = versionID
			if err := database.SetBackupArtifactVersion(artifacts[index].ID, versionID); err != nil {
				manager.failRun(run, err, store)
				return
			}
		}
		if uploadErr != nil {
			manager.failRun(run, uploadErr, store)
			return
		}
		totalSize += artifacts[index].SizeBytes
	}
	manifest, manifestChecksum, err := encodeManifest(run, site, artifacts)
	if err != nil {
		manager.failRun(run, err, store)
		return
	}
	manifestVersionID, uploadErr := store.uploadBytes(ctx, manifestKey, manifest, "application/json", manifestChecksum)
	if manifestVersionID != "" {
		if err := database.SetBackupManifestVersion(run.ID, manifestVersionID); err != nil {
			manager.failRun(run, err, store)
			return
		}
	}
	if uploadErr != nil {
		manager.failRun(run, uploadErr, store)
		return
	}
	var nextRunAt *time.Time
	if next := NextRunAt(plan.Schedule, plan.BackupHour, time.Now()); !next.IsZero() {
		nextRunAt = &next
	}
	manager.mu.Lock()
	completeErr := database.CompleteBackupRun(run.ID, plan.ID, totalSize, nextRunAt)
	manager.mu.Unlock()
	if completeErr != nil {
		manager.failRun(run, completeErr, store)
		return
	}
	_, _ = database.DB.Exec(`INSERT INTO activity (site_id, type, summary, username, ip_address)
		VALUES (?, 'backup_completed', ?, 'system', '')`, site.ID, "Backup completed for "+site.Domain)
	log.Printf("Backup: completed run %s for %s (%d bytes)", run.ID, site.Domain, totalSize)
	manager.applyRetention(plan)
}

func (manager *Manager) failRun(run database.BackupRun, err error, store *objectStore) {
	if store != nil {
		if current, loadErr := database.GetBackupRun(run.ID); loadErr == nil {
			objects := make([]objectReference, 0, len(current.Artifacts)+1)
			for _, artifact := range current.Artifacts {
				objects = append(objects, objectReference{Key: artifact.ObjectKey, VersionID: artifact.ObjectVersionID})
			}
			if current.ManifestKey != "" {
				objects = append(objects, objectReference{Key: current.ManifestKey, VersionID: current.ManifestVersionID})
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			cleanupErr := store.deleteObjects(cleanupCtx, objects)
			cancel()
			if cleanupErr == nil {
				_ = database.ClearBackupRunObjects(run.ID)
			} else {
				log.Printf("Backup: remote cleanup for failed run %s failed: %v", run.ID, cleanupErr)
			}
		}
	}
	message := sanitizeBackupError(err, run.DestinationID)
	_ = database.MarkBackupRunFailed(run.ID, message)
	_, _ = database.DB.Exec(`INSERT INTO activity (site_id, type, summary, username, ip_address)
		VALUES (?, 'backup_failed', ?, 'system', '')`, run.SiteID, "Backup failed for "+run.SiteDomain)
	log.Printf("Backup: run %s failed: %s", run.ID, message)
}

func sanitizeBackupError(err error, destinationID int) string {
	if err == nil {
		return "backup failed"
	}
	message := err.Error()
	if destination, loadErr := database.GetBackupDestination(destinationID); loadErr == nil {
		for _, secret := range []string{destination.AccessKey, destination.SecretKey, config.Decrypt(destination.AccessKey), config.Decrypt(destination.SecretKey)} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
	}
	message = strings.ReplaceAll(message, "\x00", "")
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

func runObjectBase(destination database.BackupDestination, run database.BackupRun, site database.Site) (string, error) {
	prefix, err := normalizePrefix(destination.Prefix)
	if err != nil {
		return "", err
	}
	segment := strings.Trim(safeObjectSegment.ReplaceAllString(strings.ToLower(site.Domain), "-"), "-.")
	if segment == "" {
		segment = fmt.Sprintf("site-%d", site.ID)
	}
	created := run.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	return fmt.Sprintf("%s/servers/%s/sites/%d-%s/%04d/%02d/%s", prefix, destination.ServerID,
		site.ID, segment, created.Year(), int(created.Month()), run.ID), nil
}

type retentionPolicy struct {
	Recent  int
	Daily   int
	Weekly  int
	Monthly int
}

func policyForProfile(profile string) retentionPolicy {
	switch profile {
	case "minimal":
		return retentionPolicy{Recent: 7, Daily: 7}
	case "extended":
		return retentionPolicy{Recent: 12, Daily: 30, Weekly: 12, Monthly: 12}
	default:
		return retentionPolicy{Recent: 8, Daily: 14, Weekly: 8, Monthly: 6}
	}
}

func (manager *Manager) applyRetention(plan database.BackupPlan) {
	filtered, err := database.ListCompletedBackupRunsForPlan(plan.ID)
	if err != nil {
		return
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].CompletedAt.After(*filtered[j].CompletedAt) })
	policy := policyForProfile(plan.RetentionProfile)
	keep := make(map[string]bool)
	daily, weekly, monthly := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for index, run := range filtered {
		if index < policy.Recent {
			keep[run.ID] = true
		}
		when := run.CompletedAt.In(time.Local)
		dayKey := when.Format("2006-01-02")
		year, week := when.ISOWeek()
		weekKey := fmt.Sprintf("%04d-%02d", year, week)
		monthKey := when.Format("2006-01")
		if len(daily) < policy.Daily && !daily[dayKey] {
			daily[dayKey] = true
			keep[run.ID] = true
		}
		if len(weekly) < policy.Weekly && !weekly[weekKey] {
			weekly[weekKey] = true
			keep[run.ID] = true
		}
		if len(monthly) < policy.Monthly && !monthly[monthKey] {
			monthly[monthKey] = true
			keep[run.ID] = true
		}
	}
	for _, run := range filtered {
		if keep[run.ID] {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := manager.DeleteRun(ctx, run.ID); err != nil {
			log.Printf("Backup: prune run %s: %v", run.ID, err)
		}
		cancel()
	}
}
