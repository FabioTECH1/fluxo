package nodeupdate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"fluxo/internal/services/nodetoolchain"
)

// Exact executable identities keep APT baselines separate from official trees.
// A legacy path is never represented as a managed root such as /usr.
type nodeExecutable struct {
	Path   string
	Legacy *nodetoolchain.LegacyFile
}

func officialExecutable(root string) nodeExecutable {
	return nodeExecutable{Path: filepath.Join(root, "bin", "node")}
}
func officialExecutables(roots []string) []nodeExecutable {
	result := make([]nodeExecutable, 0, len(roots))
	for _, root := range roots {
		result = append(result, officialExecutable(root))
	}
	return result
}
func legacyExecutable(s nodetoolchain.LegacySource) nodeExecutable {
	return nodeExecutable{Path: s.Node.Path, Legacy: &s.Node}
}

// Preparation must authenticate both sides before draining. In particular,
// rollback can still have children using the failed target. Verification after
// activation deliberately uses only the selected goal's executable instead.
func (g Generation) beforeDrainExecutables() []nodeExecutable {
	before := officialExecutable(g.Plan.Before.Root)
	if g.Plan.Migration != nil {
		before = legacyExecutable(*g.Plan.Migration)
	}
	return []nodeExecutable{before, officialExecutable(filepath.Join(nodetoolchain.NodeRoot(), "v"+g.Plan.After.Version))}
}

func (e nodeExecutable) info() (os.FileInfo, error) {
	info, err := os.Stat(e.Path)
	if err != nil {
		return nil, err
	}
	if e.Legacy != nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if e.Path != "/usr/bin/node" || !ok || uint64(st.Dev) != e.Legacy.Device || st.Ino != e.Legacy.Inode || uint32(info.Mode()) != e.Legacy.Mode {
			return nil, errors.New("Retained legacy Node.js executable identity changed")
		}
	}
	return info, nil
}
