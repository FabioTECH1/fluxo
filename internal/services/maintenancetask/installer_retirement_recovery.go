package maintenancetask

import "context"

// VerifyInstallerRecoveryInvocationsLocked observes the actual journal and
// unit hierarchies under the existing task gate, even between core and floor.
func VerifyInstallerRecoveryInvocationsLocked(ctx context.Context) error {
	j, err := read()
	if err != nil {
		return err
	}
	return registrationRequalificationDomainsEmpty(ctx, j)
}
