package maintenancetask

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const aptHostInputsRevision = "noble-apt-config-inputs-v1"

const aptMainConfig = "/etc/apt/apt.conf"
const aptPartsConfig = "/etc/apt/apt.conf.d"

type aptInputSource struct {
	read func(string) ([]byte, error)
	list func(string) ([]string, error)
}

// The production source validates real directories and single-link protected
// regular files before returning any bytes to the closed input checker. No APT
// executable is involved in inspecting APT's own executable configuration.
func diskAPTInputSource() aptInputSource {
	return diskAPTInputSourceAt("/etc/apt")
}

// The explicit base is a test seam for guarded private fixtures. Production
// always uses /etc/apt above; it is never selected from CLI/environment input.
func diskAPTInputSourceAt(base string) aptInputSource {
	physical := func(path string) (string, error) {
		if !filepath.IsAbs(base) || filepath.Clean(base) != base {
			return "", errors.New("unsafe APT configuration input root")
		}
		switch path {
		case aptMainConfig:
			return filepath.Join(base, "apt.conf"), nil
		case aptPartsConfig:
			return filepath.Join(base, "apt.conf.d"), nil
		}
		name := strings.TrimPrefix(path, aptPartsConfig+"/")
		if name == path || filepath.Base(name) != name {
			return "", errors.New("unexpected APT configuration input path")
		}
		return filepath.Join(base, "apt.conf.d", name), nil
	}
	return aptInputSource{
		read: func(path string) ([]byte, error) {
			actual, err := physical(path)
			if err != nil {
				return nil, err
			}
			return readRegular(actual, 256<<10)
		},
		list: func(path string) ([]string, error) {
			actual, err := physical(path)
			if err != nil {
				return nil, err
			}
			if err := trustedDatabaseDirectory(actual); err != nil {
				return nil, err
			}
			entries, err := os.ReadDir(actual)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			return names, nil
		},
	}
}

type aptInputManifest struct {
	files map[string]string
	names []string
	spec  aptPlatformSpec
}

// inspectAPTHostInputs deliberately admits only exact reviewed bytes. APT's
// parser supports includes, binary-specific promotion and conditional scopes;
// an already-promoted apt-config dump cannot establish their original meaning.
// Even an entry APT would ignore is refused here, rather than deleted or
// silently excluded. Additional legitimate variants need an explicit review.
func inspectAPTHostInputs(source aptInputSource) (aptInputManifest, error) {
	return inspectAPTPlatformInputs(source, aptPlatformSpec{"24.04", "noble", "2.8.3", aptHostInputsRevision, nobleAPTConfigInputs, true})
}

func inspectAPTPlatformInputs(source aptInputSource, spec aptPlatformSpec) (aptInputManifest, error) {
	manifest := aptInputManifest{files: map[string]string{}, spec: spec}
	if spec.inputRevision == "" || len(spec.inputs) == 0 {
		return manifest, errors.New("missing APT input contract")
	}
	names, err := source.list(aptPartsConfig)
	if err != nil {
		return manifest, err
	}
	if len(names) > len(spec.inputs) {
		return manifest, errors.New("APT configuration directory has unreviewed entries")
	}
	manifest.names = append([]string(nil), names...)
	sort.Strings(manifest.names)
	for i, name := range manifest.names {
		if i > 0 && name == manifest.names[i-1] {
			return manifest, errors.New("duplicate APT configuration input")
		}
		if _, known := spec.inputs[name]; !known {
			return manifest, fmt.Errorf("APT configuration input needs maintenance review: %s", filepath.Join(aptPartsConfig, name))
		}
	}
	main, err := source.read(aptMainConfig)
	if os.IsNotExist(err) {
		manifest.files[aptMainConfig] = "absent"
	} else if err != nil {
		return manifest, err
	} else if len(main) != 0 {
		return manifest, errors.New("APT main configuration needs maintenance review: /etc/apt/apt.conf")
	} else {
		// Only exactly empty bytes have been reviewed; comments/whitespace do
		// not acquire an implicit parsing or include exception.
		manifest.files[aptMainConfig] = digest(main)
	}
	for name, input := range spec.inputs {
		path := filepath.Join(aptPartsConfig, name)
		if !slices.Contains(manifest.names, name) {
			if input.required {
				return manifest, fmt.Errorf("required APT configuration input is missing: %s", path)
			}
			manifest.files[path] = "absent"
			continue
		}
		raw, err := source.read(path)
		if err != nil {
			return manifest, err
		}
		if digest(raw) != input.sha256 {
			return manifest, fmt.Errorf("APT configuration input changed and needs maintenance review: %s", path)
		}
		manifest.files[path] = input.sha256
	}
	return manifest, nil
}

func (m aptInputManifest) record(p *profile) {
	p.Directories[aptPartsConfig] = append([]string(nil), m.names...)
	for path, hash := range m.files {
		p.Files[path] = hash
	}
	p.External["apt-host-input-contract"] = m.spec.inputRevision
}

// Recheck after package/layout qualification and immediately before APT's first
// subprocess. A normal package/configuration change fails closed. As with other
// host profiles, arbitrary privileged mutation after this check is outside the
// trust model; package/lifecycle locking must surround actual execution.
func (m aptInputManifest) verifyAndRun(source aptInputSource, run func() error) error {
	current, err := inspectAPTPlatformInputs(source, m.spec)
	if err != nil {
		return err
	}
	if !slices.Equal(m.names, current.names) || len(m.files) != len(current.files) {
		return errors.New("APT configuration inputs changed before execution")
	}
	for path, expected := range m.files {
		if current.files[path] != expected {
			return fmt.Errorf("APT configuration input changed before execution: %s", path)
		}
	}
	return run()
}
