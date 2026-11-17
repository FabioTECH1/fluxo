package scheduler

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (p *pamCandidateInspection) loaderConfiguration() error {
	raw, err := p.optional("/etc/ld.so.preload", 1<<20)
	if err != nil {
		return err
	}
	if !pamCandidateCommentsOnly(raw) {
		return errors.New("candidate refuses dynamic-loader preload authority")
	}
	// System-wide loader cache resolution is data, parsed without executing ldd,
	// ldconfig, the target executable or its dynamic linker.
	raw, err = p.read("/etc/ld.so.cache", 16<<20)
	if err != nil {
		return err
	}
	p.cache, err = parsePAMCandidateCache(raw)
	if err != nil {
		return err
	}
	p.search = []string{"/lib/x86_64-linux-gnu", "/usr/lib/x86_64-linux-gnu", "/lib", "/usr/lib"}
	raw, err = p.read("/etc/ld.so.conf", 1<<20)
	if err != nil {
		return err
	}
	if strings.Join(pamCandidateActiveLines(raw), "\n") != "include /etc/ld.so.conf.d/*.conf" {
		return errors.New("unreviewed loader configuration include policy")
	}
	directory := "/etc/ld.so.conf.d"
	if _, err = p.canonical(directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		if !entry.Type().IsRegular() {
			return errors.New("loader include must be a protected regular file")
		}
		names = append(names, entry.Name())
		raw, err = p.read(filepath.Join(directory, entry.Name()), 1<<20)
		if err != nil {
			return err
		}
		directories, e := pamCandidateLoaderDirectories(entry.Name(), raw)
		if e != nil {
			return e
		}
		p.search = append(p.search, directories...)
	}
	sort.Strings(names)
	p.report.Files[directory+"/*conf"] = strings.Join(names, "\n")
	// glibc hardware capability subdirectories can override ordinary SONAMEs.
	// This first disabled profile deliberately refuses any populated override.
	seen := map[string]bool{}
	for _, dir := range p.search {
		canonical, e := p.canonical(dir)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		hw := filepath.Join(dir, "glibc-hwcaps")
		if _, e = p.canonical(hw); os.IsNotExist(e) {
			p.report.Files[hw] = "absent"
			continue
		} else if e != nil {
			return e
		}
		children, e := os.ReadDir(hw)
		if e != nil {
			return e
		}
		if len(children) != 0 {
			return fmt.Errorf("unreviewed glibc hardware-capability override directory: %s", hw)
		}
		p.report.Files[hw+"/*"] = "empty"
	}
	return nil
}

func pamCandidateLoaderDirectories(name string, raw []byte) ([]string, error) {
	lines := pamCandidateActiveLines(raw)
	// The standard Python build toolchain installs libfakeroot. Admit only its
	// exact amd64 declaration; each path still passes canonical ownership checks,
	// HWCAP refusal and ambiguity-free vendor validation for every required SONAME.
	// This adds no fakeroot code to the authenticated payload allowlist.
	if name == "fakeroot-x86_64-linux-gnu.conf" {
		if len(lines) == 1 && lines[0] == "/usr/lib/x86_64-linux-gnu/libfakeroot" {
			return lines, nil
		}
		return nil, errors.New("unreviewed fakeroot loader declaration")
	}
	for _, line := range lines {
		if !pamCandidateLibraryDirectory(line) {
			return nil, fmt.Errorf("unreviewed loader search directory in %s", name)
		}
	}
	return lines, nil
}

func pamCandidateActiveLines(raw []byte) []string {
	var result []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
func pamCandidateLibraryDirectory(path string) bool {
	switch path {
	case "/usr/local/lib", "/usr/local/lib/x86_64-linux-gnu", "/lib/x86_64-linux-gnu", "/usr/lib/x86_64-linux-gnu", "/lib", "/usr/lib":
		return true
	}
	return false
}

// New-format glibc cache header and entries are fixed-width little-endian on
// the reviewed amd64 hosts. Unknown/old/extended interpretation fails closed.
// Cache choices are not trusted code: every reachable path is separately bound
// to a protected vendor payload and an ambiguity-free actual search resolution.
func parsePAMCandidateCache(raw []byte) (map[string][]string, error) {
	if len(raw) < 48 || string(raw[:20]) != "glibc-ld.so.cache1.1" {
		return nil, errors.New("unreviewed glibc loader cache format")
	}
	n := binary.LittleEndian.Uint32(raw[20:24])
	if n > 65536 || uint64(48)+uint64(n)*24 > uint64(len(raw)) {
		return nil, errors.New("invalid glibc cache entry bounds")
	}
	result := map[string][]string{}
	cstring := func(offset uint32) (string, error) {
		if uint64(offset) >= uint64(len(raw)) {
			return "", errors.New("invalid loader cache string offset")
		}
		tail := raw[offset:]
		i := bytes.IndexByte(tail, 0)
		if i < 0 || i > 4096 {
			return "", errors.New("invalid loader cache string")
		}
		return string(tail[:i]), nil
	}
	for i := uint32(0); i < n; i++ {
		entry := raw[48+i*24 : 48+(i+1)*24]
		flags := binary.LittleEndian.Uint32(entry[:4])
		if flags&0xff00 != 0x0300 {
			continue
		} // only x86-64 ABI entries
		name, err := cstring(binary.LittleEndian.Uint32(entry[4:8]))
		if err != nil {
			return nil, err
		}
		path, err := cstring(binary.LittleEndian.Uint32(entry[8:12]))
		if err != nil {
			return nil, err
		}
		if name == "" || filepath.Base(name) != name || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("invalid loader cache library resolution")
		}
		result[name] = append(result[name], path)
	}
	return result, nil
}

func (p *pamCandidateInspection) elfClosure(path string, visiting map[string]bool) error {
	canonical, err := p.canonical(path)
	if err != nil {
		return err
	}
	if visiting[canonical] {
		return nil
	}
	visiting[canonical] = true
	raw, err := p.readCanonical(canonical, 128<<20)
	if err != nil {
		return err
	}
	if err = p.authenticate(canonical, raw); err != nil {
		return err
	}
	f, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("reachable vendor code is not a supported ELF: %s", canonical)
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || f.Data != elf.ELFDATA2LSB {
		return errors.New("unreviewed reachable vendor ELF architecture")
	}
	// Vendor PAM26 builds have no libeconf Build-Depends and empty vendordir.
	// Verify actual payload DT_NEEDED/symbols instead of assuming those branches
	// are absent merely from the source defaults.
	needed, err := f.ImportedLibraries()
	if err != nil {
		return err
	}
	for _, name := range needed {
		if strings.Contains(strings.ToLower(name), "econf") {
			return fmt.Errorf("libeconf-enabled PAM branch remains unqualified: %s", canonical)
		}
	}
	symbols, _ := f.ImportedSymbols()
	for _, symbol := range symbols {
		if strings.HasPrefix(symbol.Name, "econf_") {
			return fmt.Errorf("libeconf-enabled PAM code remains unqualified: %s", canonical)
		}
	}
	p.report.Dependencies[canonical] = append([]string{}, needed...)
	var local []string
	for _, tag := range []elf.DynTag{elf.DT_RPATH, elf.DT_RUNPATH} {
		values, e := f.DynString(tag)
		if e != nil {
			return e
		}
		for _, value := range values {
			for _, dir := range strings.Split(value, ":") {
				expanded := strings.ReplaceAll(strings.ReplaceAll(dir, "${ORIGIN}", filepath.Dir(canonical)), "$ORIGIN", filepath.Dir(canonical))
				if !filepath.IsAbs(expanded) || strings.Contains(expanded, "$") || !(strings.HasPrefix(filepath.Clean(expanded), "/usr/lib/x86_64-linux-gnu/") || pamCandidateLibraryDirectory(expanded)) {
					return fmt.Errorf("unreviewed vendor ELF search path: %s", canonical)
				}
				local = append(local, filepath.Clean(expanded))
			}
		}
	}

	// Include ancestor RPATH candidates conservatively for transitive lookups.
	// A final pass rechecks every SONAME after all local paths are discovered;
	// this also overapproximates RUNPATH instead of missing an inherited override.
	for _, directory := range local {
		p.search = append(p.search, directory)
		if err = p.checkHWCaps(directory); err != nil {
			return err
		}
	}
	for _, program := range f.Progs {
		if program.Type == elf.PT_INTERP {
			interp, e := ioCandidateCString(program)
			if e != nil {
				return e
			}
			if interp != "/lib64/ld-linux-x86-64.so.2" {
				return errors.New("unreviewed vendor ELF interpreter")
			}
			if e = p.elfClosure(interp, visiting); e != nil {
				return e
			}
		}
	}
	for _, name := range needed {
		if strings.ContainsAny(name, "/\\\x00") {
			return errors.New("unreviewed ELF dependency name")
		}
		resolved, e := p.resolveLibrary(name, local)
		if e != nil {
			return e
		}
		if e = p.elfClosure(resolved, visiting); e != nil {
			return e
		}
	}
	return nil
}
func ioCandidateCString(program *elf.Prog) (string, error) {
	if program.Filesz == 0 || program.Filesz > 4096 {
		return "", errors.New("invalid ELF interpreter size")
	}
	raw := make([]byte, int(program.Filesz))
	if _, err := program.ReadAt(raw, 0); err != nil {
		return "", err
	}
	if raw[len(raw)-1] != 0 || bytes.IndexByte(raw, 0) != len(raw)-1 {
		return "", errors.New("invalid ELF interpreter path")
	}
	return string(raw[:len(raw)-1]), nil
}

func (p *pamCandidateInspection) resolveLibrary(name string, local []string) (string, error) {
	paths := append([]string{}, p.cache[name]...)
	for _, dir := range append(append([]string{}, local...), p.search...) {
		paths = append(paths, filepath.Join(dir, name))
	}
	chosen := ""
	for _, path := range paths {
		canonical, err := p.canonical(path)
		if os.IsNotExist(err) {
			p.report.Files[path] = "absent"
			continue
		}
		if err != nil {
			return "", err
		}
		if chosen != "" && chosen != canonical {
			return "", fmt.Errorf("ambiguous candidate library resolution: %s", name)
		}
		if _, ok := p.payload[canonical]; !ok {
			return "", fmt.Errorf("library search resolves outside authenticated vendor payload: %s", name)
		}
		chosen = canonical
	}
	if chosen == "" {
		return "", fmt.Errorf("unresolved reachable ELF dependency: %s", name)
	}

	if p.resolutions == nil {
		p.resolutions = map[string]string{}
	}
	if old, exists := p.resolutions[name]; exists && old != chosen {
		return "", fmt.Errorf("candidate library resolution changed: %s", name)
	}
	p.resolutions[name] = chosen
	return chosen, nil
}

func (p *pamCandidateInspection) checkHWCaps(directory string) error {
	path := filepath.Join(directory, "glibc-hwcaps")
	if _, err := p.canonical(path); os.IsNotExist(err) {
		p.report.Files[path] = "absent"
		return nil
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("unreviewed vendor RPATH hardware-capability override: %s", path)
	}
	p.report.Files[path+"/*"] = "empty"
	return nil
}
