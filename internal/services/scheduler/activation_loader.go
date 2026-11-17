package scheduler

import (
	"errors"
	"os"
)

// A legacy core cannot own new loader storage. Its presence is damaged or a
// partially fenced generation transition, never authority to initialize anew.
func validateLegacyLoaderAbsence() error {
	for _, path := range []string{loaderStorePath(), loaderAnchorPath(), loaderInitialPath(), loaderArchiveDirectory()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return errors.New("legacy Scheduler has loader-generation evidence; recover its exact original transition")
		}
	}
	return nil
}
