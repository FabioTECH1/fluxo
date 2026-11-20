package maintenancetask

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// DT_NEEDED names must resolve into the verified installed dependency closure,
// not a local library with the same SONAME. Never use ldd: that can execute the
// very interpreter/module being inspected. ldconfig.real -p only reads cache.
func qualifyImagickLoader(ctx context.Context, p *profile, module []byte) error {
	if err := packageInputs(ctx, p, "libc-bin"); err != nil {
		return err
	}
	const program = "/usr/sbin/ldconfig.real"
	raw, err := readRegular(program, 32<<20)
	if err != nil || p.Files[program] != digest(raw) {
		return errors.New("Verified ImageMagick loader inspection program missing")
	}
	if _, err = os.Lstat("/etc/ld.so.preload"); !os.IsNotExist(err) {
		return errors.New("Custom ELF preload needs maintenance review")
	}
	p.Files["/etc/ld.so.preload"] = "absent"
	if err = p.file("/etc/ld.so.cache", false); err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err = syscmd.RunMaintenance(probe, "", &out, nil, program, "-p"); err != nil {
		return err
	}
	cache, err := parseImagickLoaderCache(out.String())
	if err != nil {
		return err
	}
	return bindImagickLibraries(p, module, cache, readRegular, filepath.EvalSymlinks)
}

func parseImagickLoaderCache(output string) (map[string][]string, error) {
	cache := map[string][]string{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "=>") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[len(fields)-2] != "=>" {
			return nil, errors.New("Unsupported ELF loader cache output")
		}
		path := fields[len(fields)-1]
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(fields[0], "/\x00") {
			return nil, errors.New("Unsafe ELF loader cache entry")
		}
		cache[fields[0]] = append(cache[fields[0]], path)
	}
	if len(cache) == 0 {
		return nil, errors.New("ELF loader cache unavailable")
	}
	return cache, nil
}

func bindImagickLibraries(p *profile, module []byte, cache map[string][]string, read func(string, int64) ([]byte, error), resolve func(string) (string, error)) error {
	root, err := elf.NewFile(bytes.NewReader(module))
	if err != nil {
		return err
	}
	defer root.Close()
	seen := map[string]bool{}
	var inspect func([]byte) error
	inspect = func(code []byte) error {
		if err := inspectPHPModuleELF(module, code); err != nil {
			return err
		}
		object, err := elf.NewFile(bytes.NewReader(code))
		if err != nil {
			return err
		}
		imports, err := object.ImportedLibraries()
		object.Close()
		if err != nil {
			return err
		}
		for _, name := range imports {
			if strings.Contains(name, "/") {
				return errors.New("Custom Imagick dependency path needs review")
			}
			found := false
			for _, path := range cache[name] {
				real, err := resolve(path)
				if err != nil {
					return err
				}
				raw, err := read(real, 128<<20)
				if err != nil {
					return err
				}
				candidate, err := elf.NewFile(bytes.NewReader(raw))
				if err != nil {
					return err
				}
				native := candidate.Class == root.Class && candidate.Data == root.Data && candidate.Machine == root.Machine
				candidate.Close()
				if !native {
					continue
				} // another installed architecture is not a candidate
				found = true
				if p.Files[real] != digest(raw) {
					return fmt.Errorf("Imagick dependency is outside verified package payload: %s", real)
				}
				p.External["imagick-loader:"+name+":"+path] = real
				if seen[real] {
					continue
				}
				seen[real] = true
				if len(seen) > 256 {
					return errors.New("Imagick dependency closure exceeds reviewed bounds")
				}
				if err = inspect(raw); err != nil {
					return err
				}
			}
			if !found {
				return errors.New("Verified Imagick loader dependency missing: " + name)
			}
		}
		return nil
	}
	return inspect(module)
}
