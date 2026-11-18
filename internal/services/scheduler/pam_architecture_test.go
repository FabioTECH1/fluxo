package scheduler

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPAMArchitectureKeepsNativeCodeBoundaries(t *testing.T) {
	for _, name := range []string{"amd64", "arm64"} {
		t.Run(name, func(t *testing.T) {
			a, err := pamArchitectureFor(name)
			if err != nil {
				t.Fatal(err)
			}
			foreignName := "arm64"
			if name == "arm64" {
				foreignName = "amd64"
			}
			foreign, _ := pamArchitectureFor(foreignName)
			if !a.libraryDirectory("/usr/lib/"+a.triplet) || a.libraryDirectory("/usr/lib/"+foreign.triplet) || a.libraryDirectory("/usr/lib/"+a.triplet+"/../override") {
				t.Fatal("native library directory boundary changed")
			}
			for _, flags := range []uint32{a.cacheABI | 3, foreign.cacheABI | 3, 0x0b03, 0x0003} {
				key, path := "libfixture.so.0", "/lib/"+a.triplet+"/libfixture.so.0"
				raw := make([]byte, 72)
				copy(raw, "glibc-ld.so.cache1.1")
				binary.LittleEndian.PutUint32(raw[20:24], 1)
				binary.LittleEndian.PutUint32(raw[48:52], flags)
				binary.LittleEndian.PutUint32(raw[52:56], 72)
				binary.LittleEndian.PutUint32(raw[56:60], uint32(73+len(key)))
				raw = append(raw, []byte(key+"\x00"+path+"\x00")...)
				got, err := parsePAMCacheFor(a, raw)
				if err != nil || (len(got[key]) == 1) != (flags == a.cacheABI|3) {
					t.Fatal("foreign cache ABI selected", flags, got, err)
				}
			}
			for _, machine := range []elf.Machine{a.machine, foreign.machine, elf.EM_ARM} {
				f := &elf.File{FileHeader: elf.FileHeader{Class: elf.ELFCLASS64, Data: elf.ELFDATA2LSB, Machine: machine}}
				if (a.validateELF(f) == nil) != (machine == a.machine) {
					t.Fatal("foreign ELF machine accepted", machine)
				}
				f.Class = elf.ELFCLASS32
				if a.validateELF(f) == nil {
					t.Fatal("32-bit ELF accepted")
				}
				f.Class = elf.ELFCLASS64
				f.Data = elf.ELFDATA2MSB
				if a.validateELF(f) == nil {
					t.Fatal("big-endian ELF accepted")
				}
			}
			status := func(arch, version string) string {
				return fmt.Sprintf("Package: fixture\nStatus: install ok installed\nArchitecture: %s\nVersion: %s\n\n", arch, version)
			}
			got, err := pamInstalledFor(a, []byte(status(name, "1")+status(foreignName, "foreign")))
			if err != nil || got["fixture"] != "1" {
				t.Fatal("foreign package metadata selected", got, err)
			}
			if _, err = pamInstalledFor(a, []byte(status(name, "1")+status("all", "2"))); err == nil {
				t.Fatal("ambiguous package accepted")
			}
		})
	}
	for _, name := range []string{"386", "arm", "riscv64", "", "ARM64"} {
		if _, err := pamArchitectureFor(name); err == nil {
			t.Fatal("unreviewed architecture accepted", name)
		}
	}
}

func TestARMProfilesRetainAMD64ConfigurationAndHistoricalMeaning(t *testing.T) {
	for _, id := range []string{nobleLoaderPAMProfileID, cronieLoaderPAMProfileID, jammyCronCandidateID, resoluteCronCandidateID} {
		amd, err := distributionCandidateForArchitecture(id, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		arm, err := distributionCandidateForArchitecture(id, "arm64")
		if err != nil {
			t.Fatal(err)
		}
		if !loaderDigestValid(arm.binary) || arm.binary == amd.binary {
			t.Fatal("missing separate ARM executable pin", id)
		}
		arm.binary = amd.binary
		if !reflect.DeepEqual(amd, arm) {
			t.Fatal("architecture changed an existing configuration or backend meaning", id)
		}
	}
	if _, err := distributionCandidateForArchitecture(jammyCronCandidateID, "arm"); err == nil {
		t.Fatal("32-bit profile accepted")
	}
	// ARM loader profiles must not acquire new historical reader semantics.
	for _, id := range BackendReaderContracts() {
		if strings.Contains(id, "arm64") {
			t.Fatal("architecture leaked into immutable reader roles")
		}
	}
}
