package nodetoolchain

import (
	"errors"
	"os"
	"path/filepath"
)

// BridgeReady does not execute npm/Corepack shims (even offline shims may
// populate caches). Retained signed payload/tree evidence stays authoritative.
func BridgeReady() error {
	s, err := loadManagedState()
	if err != nil {
		return err
	}
	if !s.OfficialNode || s.ApprovedNode == nil {
		return errors.New("complete reviewed Node runtime migration")
	}
	if err = ValidateReleaseActivation(*s.ApprovedNode); err != nil {
		return err
	}
	link := filepath.Join(managedNodeRoot, "current")
	target, err := os.Readlink(link)
	if err != nil || target != filepath.Join(managedNodeRoot, "v"+s.ApprovedNode.Version) {
		return errors.New("managed Node selection differs from its approval")
	}
	for _, name := range []string{"node", "npm", "npx"} {
		actual, e := os.Readlink(filepath.Join("/usr/local/bin", name))
		if e != nil || actual != filepath.Join(link, "bin", name) {
			return errors.New("managed Node command link changed")
		}
	}
	return nil
}
