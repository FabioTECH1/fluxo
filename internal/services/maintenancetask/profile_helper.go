package maintenancetask

import (
	"context"
	"fmt"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
)

const optionalProfileRole = "system-task-optional-profile"

// HelperCapabilities advertises optional executable profile support without
// changing the committed reader floor or requirements for historical recovery.
// The optional roles are required only for new submissions that use them.
func HelperCapabilities() executiondomain.ReaderCapabilities {
	c := executiondomain.Capabilities()
	c.RoleContracts[optionalProfileRole] = []string{imagickStartupProfile, certbotDirectoryHookContract}
	return c
}

func profileHelperRequirements(p profile) executiondomain.ReaderCapabilities {
	need := executiondomain.ReaderCapabilities{Format: 1, RoleContracts: map[string][]string{}}
	if p.External["php-imagick-startup"] == imagickStartupProfile {
		need.RoleContracts[optionalProfileRole] = append(need.RoleContracts[optionalProfileRole], imagickStartupProfile)
	}
	if p.External["certbot-directory-hook-contract"] == certbotDirectoryHookContract {
		need.RoleContracts[optionalProfileRole] = append(need.RoleContracts[optionalProfileRole], certbotDirectoryHookContract)
	}
	return need
}

func requireProfileHelper(ctx context.Context, helper string, p profile) error {
	need := profileHelperRequirements(p)
	if len(need.RoleContracts[optionalProfileRole]) == 0 {
		return nil
	}
	have, err := executiondomain.ReadHelperCapabilities(ctx, helper, filepath.Base(helper))
	if err != nil {
		return fmt.Errorf("Retained task helper could not be verified; preserve task state and restore its exact original image before retrying: %w", err)
	}
	if err = executiondomain.RequireCapabilities(have, need); err != nil {
		return fmt.Errorf("Rerun this Fluxo release's installer with --reinstall to update the retained task helper, then retry: %w", err)
	}
	return nil
}
