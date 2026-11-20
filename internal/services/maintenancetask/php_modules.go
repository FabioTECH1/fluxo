package maintenancetask

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// These are the non-ZTS distribution PHP module APIs, not arbitrary directories
// discovered by a glob (which can select another installed PHP branch).
func phpModuleAPI(version string) (string, error) {
	api := map[string]string{"8.1": "20210902", "8.2": "20220829", "8.3": "20230831", "8.4": "20240924", "8.5": "20250925"}[version]
	if api == "" {
		return "", errors.New("PHP CLI module API needs compatibility review")
	}
	return api, nil
}

type phpModuleInspection struct {
	read     func(string, int64) ([]byte, error)
	resolve  func(string) (string, error)
	packages func(context.Context, *profile, ...string) error
	query    func(context.Context, *profile, ...string) (string, error)
}

func queryPHPModulePackages(ctx context.Context, p *profile, args ...string) (string, error) {
	bootstrap, err := inspectDpkgBootstrap(readRegular)
	if err != nil {
		return "", err
	}
	bootstrap.record(p)
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	err = bootstrap.verifyAndRun(readRegular, func() error {
		return syscmd.RunMaintenance(probe, "", &out, nil, "/usr/bin/dpkg-query", append([]string{"--admindir=/var/lib/dpkg"}, args...)...)
	})
	return out.String(), err
}

func qualifyPHPModules(ctx context.Context, p *profile, version string, modules []string) error {
	inputs := phpModuleInspection{readRegular, filepath.EvalSymlinks, packageInputs, queryPHPModulePackages}
	if err := inputs.qualify(ctx, p, version, modules); err != nil {
		return err
	}
	for _, module := range modules {
		if module == "imagick" {
			return qualifyImagickStartup(ctx, p, version)
		}
	}
	return nil
}

func (in phpModuleInspection) qualify(ctx context.Context, p *profile, version string, modules []string) error {
	if len(modules) == 0 {
		return nil
	}
	api, err := phpModuleAPI(version)
	if err != nil {
		return err
	}
	paths := map[string]string{}
	for _, name := range modules {
		if !standardPHPExtensions[name] && name != "imagick" {
			return fmt.Errorf("Unsupported PHP CLI extension: %s", name)
		}
		paths[name] = "/usr/lib/php/" + api + "/" + name + ".so"
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	php, err := in.read(p.External["php-interpreter"], 128<<20)
	if err != nil {
		return err
	}
	if p.Files[p.External["php-interpreter"]] != digest(php) {
		return errors.New("PHP CLI interpreter payload changed")
	}
	boundOwners := map[string]bool{}
	for _, name := range names {
		path := paths[name]
		out, err := in.query(ctx, p, "-S", path)
		if err != nil {
			return fmt.Errorf("PHP %s CLI extension %s package is unavailable: %w", version, name, err)
		}
		owner, err := phpModuleOwner(out, version, path)
		if err != nil {
			return err
		}
		if !boundOwners[owner] {
			if err = in.packages(ctx, p, owner); err != nil {
				return err
			}
			boundOwners[owner] = true
		}
		// A dpkg file-list entry alone is insufficient: require the selected module
		// itself in the verified package payload, at its actual runtime path.
		real, err := in.resolve(path)
		if err != nil {
			return err
		}
		if real != path {
			return errors.New("PHP CLI extension implementation is aliased")
		}
		raw, err := in.read(real, 128<<20)
		if err != nil {
			return err
		}
		if p.Files[real] != digest(raw) {
			return errors.New("PHP CLI extension has no matching verified package payload")
		}
		if err = inspectPHPModuleELF(php, raw); err != nil {
			return fmt.Errorf("PHP CLI extension %s: %w", name, err)
		}
		p.External["php-module:"+name+".so"] = real
	}
	return nil
}

func phpModuleOwner(output, version, path string) (string, error) {
	owner := ""
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		candidate, listed, ok := strings.Cut(line, ": /")
		if !ok || "/"+listed != path || !strings.HasPrefix(candidate, "php"+version+"-") || strings.ContainsAny(candidate, ", /\t\r\n") || owner != "" {
			return "", errors.New("PHP CLI extension has ambiguous or unreviewed package ownership")
		}
		// Architecture qualifiers occur after the package name, never in the
		// runtime path. inputClosure independently requires the native architecture.
		parts := strings.Split(candidate, ":")
		if len(parts) > 2 || len(parts) == 2 && parts[1] != runtime.GOARCH {
			return "", errors.New("PHP CLI extension owner is not the native architecture")
		}
		owner = parts[0]
	}
	if owner == "" {
		return "", errors.New("PHP CLI extension has no matching installed implementation")
	}
	return owner, nil
}

func inspectPHPModuleELF(php, module []byte) error {
	interpreter, err := elf.NewFile(bytes.NewReader(php))
	if err != nil {
		return errors.New("PHP interpreter is not ELF")
	}
	defer interpreter.Close()
	object, err := elf.NewFile(bytes.NewReader(module))
	if err != nil {
		return errors.New("module is not ELF")
	}
	defer object.Close()
	if object.Type != elf.ET_DYN || object.Class != interpreter.Class || object.Data != interpreter.Data || object.Machine != interpreter.Machine {
		return errors.New("module ELF ABI does not match the PHP interpreter")
	}
	for _, tag := range []elf.DynTag{elf.DT_RPATH, elf.DT_RUNPATH} {
		paths, err := object.DynString(tag)
		if err != nil || len(paths) != 0 {
			return errors.New("custom PHP module ELF library search path needs review")
		}
	}
	return nil
}
