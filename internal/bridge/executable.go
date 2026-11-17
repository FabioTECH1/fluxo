package bridge

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Bind the receipt to the image actually doing qualification. A concurrent
// manual binary replacement must not attribute the old process's checks to
// the newly installed file at the same pathname.
func readCertifyingExecutable() ([]byte, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err = trustedDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	image, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, err
	}
	defer image.Close()
	before, err := image.Stat()
	if err != nil {
		return nil, err
	}
	named, err := protectedInfo(path)
	if err != nil {
		return nil, err
	}
	if !sameObservation(before, named) {
		return nil, errors.New("certifying binary was replaced; rerun the installed bridge")
	}
	data, err := io.ReadAll(io.LimitReader(image, (256<<20)+1))
	if err != nil {
		return nil, err
	}
	after, err := image.Stat()
	if err != nil {
		return nil, err
	}
	named, err = protectedInfo(path)
	if err != nil || len(data) > 256<<20 || !sameObservation(before, after) || !sameObservation(before, named) {
		return nil, errors.New("certifying binary changed during qualification")
	}
	return data, nil
}
