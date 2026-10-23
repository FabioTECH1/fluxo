package phpupdate

// Bind recorded shutdown modes to the approved consumer inventory before any
// signal, transaction or activation. An absent/corrupted "natural" field must
// never turn an unfamiliar Artisan command into a SIGTERM consumer on Repair.
func validateConsumerProtocols(p Plan, g recoveryGeneration) error {
	if p.Version != g.Version {
		return ErrPlanChanged
	}
	expected := map[string]bool{"php" + p.Version + "-fpm.service": false}
	for _, c := range p.Impact.Workers {
		for i := 1; i <= c.Instances; i++ {
			expected[unitName(c.ID, i)] = isNaturalConsumer(&c)
		}
	}
	for _, snapshot := range []runtimeSnapshot{g.Original, g.Attempt} {
		if len(snapshot.Units) != len(expected) {
			return ErrPlanChanged
		}
		seen := map[string]bool{}
		for _, u := range snapshot.Units {
			natural, ok := expected[u.Name]
			if !ok || seen[u.Name] || u.Natural != natural || u.Natural && u.Octane != nil {
				return ErrPlanChanged
			}
			seen[u.Name] = true
		}
	}
	return nil
}
