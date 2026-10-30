package nodeupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/packages"
	"github.com/google/uuid"
)

func migrationMajor(source nodetoolchain.LegacySource, target nodetoolchain.Release) error {
	a, aOK := versionMajor(source.Version)
	b, bOK := versionMajor(target.Version)
	if !aOK || !bOK || a != b {
		return errors.New("Migration must keep the installed Node.js major version")
	}
	if err := nodetoolchain.ValidateMigrationVersion(source.Version, target.Version); err != nil {
		return err
	}
	return nil
}

func (m *Manager) discoverMigration(ctx context.Context) (Snapshot, error) {
	source, err := m.InspectLegacy(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	target, err := m.Find(ctx, source.Version)
	if err != nil {
		return Snapshot{}, err
	}
	if target.Version == "" {
		target, err = m.Authenticate(ctx, source.Version, "")
		if err != nil {
			return Snapshot{}, err
		}
	}
	if err = migrationMajor(source, target); err != nil {
		return Snapshot{}, err
	}
	target, err = m.Stage(ctx, target, nil)
	if err != nil {
		return Snapshot{}, err
	}
	// Staging may take minutes. Never cache a source that changed in the meantime.
	fresh, err := m.InspectLegacy(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if digest(source) != digest(fresh) {
		return Snapshot{}, ErrChanged
	}
	return Snapshot{State: "migration_available", LegacySource: &source, Candidate: &target, DiscoveryID: uuid.NewString(), CheckedAt: time.Now().Unix(), AttemptedAt: time.Now().Unix()}, nil
}

func migrationNativeLocks(ctx context.Context) (func(), error) {
	if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" {
		return nil, ErrSupervisor
	}
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unlock, err := packages.Acquire(lockCtx)
	if err != nil {
		return nil, err
	}
	native, err := packages.NativeDPKG(lockCtx)
	if err != nil {
		unlock()
		return nil, err
	}
	return func() { native(); unlock() }, nil
}

func (m *Manager) reviewMigration(ctx context.Context, s Snapshot, id string) (Plan, error) {
	if s.DiscoveryID != id || s.LegacySource == nil || s.Candidate == nil || time.Now().Unix()-s.CheckedAt >= FreshSeconds {
		return Plan{}, ErrChanged
	}
	unlock, err := migrationNativeLocks(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer unlock()
	source, err := m.InspectLegacy(ctx)
	if err != nil {
		return Plan{}, err
	}
	if digest(source) != digest(s.LegacySource) {
		return Plan{}, ErrChanged
	}
	if err = migrationMajor(source, *s.Candidate); err != nil {
		return Plan{}, err
	}
	impact, err := inspectConsumersAt(ctx, []nodeExecutable{legacyExecutable(source)}, "", nil, observeUnit)
	if err != nil {
		return Plan{}, err
	}
	target, err := m.Stage(ctx, *s.Candidate, nil)
	if err != nil {
		return Plan{}, err
	}
	if !sameSelection(target, *s.Candidate) {
		return Plan{}, ErrChanged
	}
	if err = nodetoolchain.ValidateLegacySource(ctx, source); err != nil {
		return Plan{}, err
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "migrate", DiscoveryID: s.DiscoveryID, CreatedAt: time.Now().Unix(), Migration: &source, After: target, Impact: impact}
	return p, savePlan(p)
}

func (m *Manager) refreshMigrationTarget(ctx context.Context, original Plan) (nodetoolchain.Release, error) {
	if original.Migration == nil {
		return nodetoolchain.Release{}, ErrChanged
	}
	if err := migrationMajor(*original.Migration, original.After); err != nil {
		return nodetoolchain.Release{}, err
	}
	fresh, err := m.Authenticate(ctx, original.After.Version, original.After.NPM)
	if err != nil {
		return fresh, err
	}
	if !sameRetainedArchive(original.After, fresh) {
		return fresh, ErrChanged
	}
	fresh, err = m.Stage(ctx, fresh, nil)
	if err != nil {
		return fresh, err
	}
	if !sameRetainedPayload(original.After, fresh) {
		return fresh, errors.New("The official migration target differs from its reviewed archive")
	}
	if err = m.ValidateRelease(fresh); err != nil {
		return fresh, err
	}
	return fresh, nil
}

func inspectMigrationRecovery(ctx context.Context, g Generation, target nodetoolchain.Release) (Impact, error) {
	if g.Plan.Migration == nil {
		return Impact{}, ErrChanged
	}
	if err := nodetoolchain.ValidateLegacySource(ctx, *g.Plan.Migration); err != nil {
		return Impact{}, err
	}
	if err := nodetoolchain.ValidateMigrationLinks(*g.Plan.Migration, target); err != nil {
		return Impact{}, err
	}
	original := g.executionImpact()
	impact, err := inspectConsumersAt(ctx, g.beforeDrainExecutables(), g.ID, &original, observeUnit)
	if g.PHPState != nil {
		impact.PHPResumes = g.PHPState.Resumes
	}
	return impact, err
}

func (m *Manager) reviewMigrationRecovery(ctx context.Context, g Generation, kind string) (Plan, error) {
	unlock, err := migrationNativeLocks(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer unlock()
	target, err := m.refreshMigrationTarget(ctx, g.Plan)
	if err != nil {
		return Plan{}, err
	}
	impact, err := inspectMigrationRecovery(ctx, g, target)
	if err != nil {
		return Plan{}, err
	}
	if !sameOriginalConsumers(impact, g.Plan.Impact) {
		return Plan{}, recoveryReviewMismatch(impact)
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: kind, GenerationID: g.ID, Head: g.Head, CreatedAt: time.Now().Unix(), Migration: g.Plan.Migration, After: target, Impact: impact}
	return p, savePlan(p)
}

func (m *Manager) applyMigration(ctx context.Context, o Operation, p Plan) error {
	if p.Migration == nil || (o.Kind != "migrate" && o.Kind != "repair" && o.Kind != "rollback") {
		return ErrChanged
	}
	unlock, err := migrationNativeLocks(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	var count int
	if err = database.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM deployments WHERE status='running')+(SELECT COUNT(*) FROM commands WHERE status='running')`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("Wait for accepted deployments and site commands before migrating Node.js")
	}
	var g Generation
	if o.Kind == "migrate" {
		s, err := readSnapshot()
		if err != nil {
			return err
		}
		if s.State != "migration_available" || s.DiscoveryID != p.DiscoveryID || s.LegacySource == nil || s.Candidate == nil || digest(s.LegacySource) != digest(p.Migration) || !sameSelection(*s.Candidate, p.After) || time.Now().Unix()-s.CheckedAt >= FreshSeconds {
			return ErrChanged
		}
		source, err := m.InspectLegacy(ctx)
		if err != nil {
			return err
		}
		if digest(source) != digest(p.Migration) {
			return ErrChanged
		}
		impact, err := inspectConsumersAt(ctx, []nodeExecutable{legacyExecutable(source)}, "", nil, observeUnit)
		if err != nil {
			return err
		}
		if !sameApplyConsumers(impact, p.Impact) {
			return ErrChanged
		}
		if err = updateOperation(o.ID, "running", "staging", "Verifying the official same-major migration target; the APT runtime remains unchanged"); err != nil {
			return err
		}
		target, err := m.refreshMigrationTarget(ctx, p)
		if err != nil {
			return err
		}
		if digest(target) != digest(p.After) {
			return ErrChanged
		}
		if err = nodetoolchain.ValidateLegacySource(ctx, source); err != nil {
			return err
		}
		fresh, err := inspectConsumersAt(ctx, []nodeExecutable{legacyExecutable(source)}, "", nil, observeUnit)
		if err != nil {
			return err
		}
		coordination, err := applyCoordination(p.Impact, impact, fresh)
		if err != nil {
			return err
		}
		boot, err := bootID()
		if err != nil {
			return err
		}
		g = Generation{ID: o.ID, Head: o.ID, State: "prepared", Plan: p, Boot: boot, Goal: "after", MigrationTarget: &target, Coordination: &coordination}
		if err = saveGeneration(g); err != nil {
			return err
		}
	} else {
		g, err = getGeneration(p.GenerationID)
		if err != nil {
			return err
		}
		if g.Head != o.ID || g.Plan.Migration == nil || digest(g.Plan.Migration) != digest(p.Migration) {
			return ErrChanged
		}
		target, err := m.refreshMigrationTarget(ctx, g.Plan)
		if err != nil {
			return err
		}
		if digest(target) != digest(p.After) {
			return ErrChanged
		}
		impact, err := inspectMigrationRecovery(ctx, g, target)
		if err != nil {
			return err
		}
		if !sameConsumers(impact, p.Impact) || !samePHPIntent(impact.PHP, p.Impact.PHP) || !sameOriginalConsumers(impact, g.Plan.Impact) {
			return ErrChanged
		}
		g.MigrationTarget = &target
		g.Coordination = &impact
		g.Goal = "after"
		if o.Kind == "rollback" {
			g.Goal = "before"
		}
		g.State = "prepared"
		if err = saveGeneration(g); err != nil {
			return err
		}
	}
	return m.executeGeneration(ctx, o, &g)
}

func (m *Manager) transitionTarget(ctx context.Context, g *Generation) (nodetoolchain.Release, nodeExecutable, error) {
	if g.Plan.Migration != nil {
		if g.MigrationTarget == nil || !sameRetainedPayload(g.Plan.After, *g.MigrationTarget) {
			return nodetoolchain.Release{}, nodeExecutable{}, errors.New("Fresh official migration proof is required before stopping applications")
		}
		target := *g.MigrationTarget
		if err := m.ValidateRelease(target); err != nil {
			return target, nodeExecutable{}, err
		}
		if err := nodetoolchain.ValidateLegacySource(ctx, *g.Plan.Migration); err != nil {
			return target, nodeExecutable{}, err
		}
		if err := nodetoolchain.ValidateMigrationLinks(*g.Plan.Migration, target); err != nil {
			return target, nodeExecutable{}, err
		}
		if g.Goal == "before" {
			return target, legacyExecutable(*g.Plan.Migration), nil
		}
		return target, officialExecutable(filepath.Join(nodetoolchain.NodeRoot(), "v"+target.Version)), nil
	}
	for _, r := range []nodetoolchain.Release{g.Retained.Before, g.Retained.After} {
		if err := m.ValidateRelease(r); err != nil {
			return r, nodeExecutable{}, err
		}
	}
	target := g.Retained.After
	if g.Goal == "before" {
		target = g.Retained.Before
	}
	return target, officialExecutable(filepath.Join(nodetoolchain.NodeRoot(), "v"+target.Version)), nil
}

func (m *Manager) selectAndValidateRuntime(ctx context.Context, g *Generation, target nodetoolchain.Release) error {
	if g.Plan.Migration != nil {
		before := g.Goal == "before"
		if err := nodetoolchain.ValidateLegacySource(ctx, *g.Plan.Migration); err != nil {
			return err
		}
		err := nodetoolchain.SelectMigration(*g.Plan.Migration, target, before, func(path string) error { g.MigrationStep = path; return saveGeneration(*g) })
		if err != nil {
			return err
		}
		return nodetoolchain.ValidateMigrationSelection(ctx, *g.Plan.Migration, target, before)
	}
	if err := nodetoolchain.ActivateRelease(target); err != nil {
		return err
	}
	active, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if active.Release.Version != target.Version || active.Release.NPM != target.NPM || active.Release.TreeSHA256 != target.TreeSHA256 {
		return errors.New("Activated Node.js or bundled npm differs from the approved release")
	}
	if err = nodetoolchain.ValidateManagers(ctx, g.Plan.Before.Managers); err != nil {
		return fmt.Errorf("Package manager compatibility: %w", err)
	}
	return nil
}
