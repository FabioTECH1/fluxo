package phpupdate

// Bind recorded shutdown modes to the approved consumer inventory before any
// signal, transaction or activation. An absent/corrupted "natural" field must
// never turn an unfamiliar Artisan command into a SIGTERM consumer on Repair.
func validateConsumerProtocols(p Plan, g recoveryGeneration) error {
	if p.Version != g.Version || p.Impact.MaintenancePolicy != 0 && p.Impact.MaintenancePolicy != ManagedProcessPolicy {
		return ErrPlanChanged
	}
	policies := map[string]*ManagedStopPolicy{}
	expected := map[string]bool{"php" + p.Version + "-fpm.service": false}
	for _, c := range p.Impact.Workers {
		if isManagedProcess(&c) && p.Impact.MaintenancePolicy != ManagedProcessPolicy {
			return ErrPlanChanged
		}
		for i := 1; i <= c.Instances; i++ {
			expected[unitName(c.ID, i)] = isNaturalConsumer(&c)
			policy, err := managedStopPolicy(&c)
			if err != nil {
				return err
			}
			policies[unitName(c.ID, i)] = policy
		}
	}
	for _, snapshot := range []runtimeSnapshot{g.Original, g.Attempt} {
		if len(snapshot.Units) != len(expected) {
			return ErrPlanChanged
		}
		seen := map[string]bool{}
		for _, u := range snapshot.Units {
			natural, ok := expected[u.Name]
			if !ok || seen[u.Name] || u.Natural != natural || u.Natural && u.Octane != nil || digest(u.ManagedStop) != digest(policies[u.Name]) {
				return ErrPlanChanged
			}
			seen[u.Name] = true
		}
	}
	return nil
}
