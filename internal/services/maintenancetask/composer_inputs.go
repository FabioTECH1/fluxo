package maintenancetask

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Pinned to the keys embedded by the approved Composer 2.10.2 PHAR:
// https://github.com/composer/composer/blob/2.10.2/src/Composer/Command/SelfUpdateCommand.php
func validateComposerKey(name string, raw []byte) error {
	expected := map[string]string{
		"keys.dev.pub":  "ecc0e2e370ed3b76d21b50a96bf80095a60aa2a763c3bbdee28f3a07b6ff55b3",
		"keys.tags.pub": "a0532abb92d4a1fedf269c4e0ad08d6a2a49f958e9806ff479e24d405a59ce3e",
	}
	if expected[name] == "" || digest([]byte(strings.TrimSpace(string(raw)))) != expected[name] {
		return errors.New("Composer self-update public key needs explicit compatibility review")
	}
	return nil
}

func composerMutableData(path string) bool {
	for _, root := range []string{"/root/.composer", "/root/.config/composer", "/root/.cache/composer", "/root/.local/share/composer"} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// These are data-only roots used by the fixed self-update command, not search
// paths for project/global plugins. Reject symlinks and writable/foreign paths
// before allowing its cache/backup writes; bound the inventory size.
func qualifyComposerData(p *profile, path string) error {
	count := 0
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := p.directory(dir, true)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			count++
			if count > 4096 {
				return errors.New("Composer data inventory is too large")
			}
			child := filepath.Join(dir, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("Composer data contains a symlink")
			}
			if entry.IsDir() {
				if err = walk(child); err != nil {
					return err
				}
			} else if err = p.file(child, false); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(path)
}
