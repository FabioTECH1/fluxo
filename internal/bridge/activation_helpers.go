package bridge

import (
	"errors"
	"os"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
)

// Stage the immutable bootstrap/worker image before the first schema5 fence.
// The mutable dispatcher remains only registration/receipt/dispatch authority.
func planSchedulerPinnedHelper(imageSHA string) ([]executiondomain.ActivationFile, error) {
	base := "/var/lib/fluxo-scheduler/helpers"
	var out []executiondomain.ActivationFile
	if _, err := os.Lstat(base); os.IsNotExist(err) {
		out = append(out, executiondomain.ActivationFile{Path: base, Before: "absent", Directory: true, Mode: 0700})
	} else if err != nil {
		return nil, err
	} else if err = trustedDirectory(base); err != nil {
		return nil, err
	}
	path := filepath.Join(base, imageSHA)
	before := "absent"
	if _, err := os.Lstat(path); err == nil {
		before, err = activationProtectedHash(path, 128<<20)
		if err != nil {
			return nil, err
		}
		if before != imageSHA {
			return nil, errors.New("content-addressed Scheduler image conflicts with its name")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return append(out, executiondomain.ActivationFile{Path: path, Before: before, Image: true, Mode: 0700}), nil
}
