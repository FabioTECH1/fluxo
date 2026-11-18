package scheduler

import (
	"debug/elf"
	"errors"
	"runtime"
)

// Architecture is selected from the running image, never a file, environment
// variable or saved provider's untrusted label. Historical backend meanings and
// the existing amd64 policy observations remain unchanged.
type pamArchitecture struct {
	name, triplet, interpreter string
	machine                    elf.Machine
	cacheABI                   uint32
}

func pamArchitectureFor(name string) (pamArchitecture, error) {
	switch name {
	case "amd64":
		return pamArchitecture{name, "x86_64-linux-gnu", "/lib64/ld-linux-x86-64.so.2", elf.EM_X86_64, 0x0300}, nil
	case "arm64":
		return pamArchitecture{name, "aarch64-linux-gnu", "/lib/ld-linux-aarch64.so.1", elf.EM_AARCH64, 0x0a00}, nil
	}
	return pamArchitecture{}, errors.New("loader architecture has no reviewed platform definition")
}

func nativePAMArchitecture() (pamArchitecture, error) { return pamArchitectureFor(runtime.GOARCH) }

func (a pamArchitecture) libraryDirectory(path string) bool {
	for _, directory := range []string{"/usr/local/lib", "/usr/local/lib/" + a.triplet, "/lib/" + a.triplet, "/usr/lib/" + a.triplet, "/lib", "/usr/lib"} {
		if path == directory {
			return true
		}
	}
	return false
}

func (a pamArchitecture) cacheFlagsMatch(flags uint32) bool {
	if a.name == "arm64" {
		// glibc aarch64/dl-cache.h requires the exact LP64 ELF libc6 ID;
		// ELF32/ILP32, foreign ABI and other library types are not aliases.
		return flags == (a.cacheABI | 3)
	}
	return flags&0xff00 == a.cacheABI // Retain the existing amd64 interpretation.
}

func (a pamArchitecture) validateELF(f *elf.File) error {
	if f.Class != elf.ELFCLASS64 || f.Machine != a.machine || f.Data != elf.ELFDATA2LSB {
		return errors.New("unreviewed reachable vendor ELF architecture")
	}
	return nil
}
