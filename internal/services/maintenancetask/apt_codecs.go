package maintenancetask

import (
	"bytes"
	"debug/elf"
	"errors"
	"path/filepath"
	"strings"
)

// inspectAPTNativeCodecs binds the reviewed native FileFd dispatch to the
// installed, package-payload-verified libapt build. No loader/tool is executed.
// This uses the local protected package database OS trust boundary.
func inspectAPTNativeCodecs(p *profile) error {
	found := false
	for path, want := range p.Files {
		if !strings.HasPrefix(filepath.Base(path), "libapt-pkg.so.") {
			continue
		}
		if found {
			return errors.New("ambiguous libapt native-codec payload")
		}
		found = true
		raw, err := readRegular(path, 128<<20)
		if err != nil || digest(raw) != want {
			return errors.New("libapt codec payload changed")
		}
		object, err := elf.NewFile(bytes.NewReader(raw))
		if err != nil {
			return errors.New("libapt codec payload is not ELF")
		}
		libs, err := object.ImportedLibraries()
		object.Close()
		if err != nil {
			return errors.New("libapt codec linkage unavailable")
		}
		if err = requireAPTCodecLibraries(libs); err != nil {
			return err
		}
	}
	if !found {
		return errors.New("verified libapt native-codec payload missing")
	}
	p.External["apt-native-codecs"] = "zlib-bz2-lzma-lz4-zstd-v1"
	return nil
}

func requireAPTCodecLibraries(libs []string) error {
	for _, expected := range []string{"libz.so.1", "libbz2.so.1.0", "liblzma.so.5", "liblz4.so.1", "libzstd.so.1"} {
		found := false
		for _, name := range libs {
			if name == expected {
				found = true
			}
		}
		if !found {
			return errors.New("reviewed native APT codec linkage missing: " + expected)
		}
	}
	return nil
}
