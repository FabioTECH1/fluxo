package maintenancetask

import (
	"context"
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// A real, parseable ELF structure, with controlled dynamic linkage and PHP API
// evidence. Tests never load a PHP module or invoke an ELF interpreter.
func phpTestELF(api string, libraries []string, runpath string) []byte {
	str := []byte{0}
	add := func(s string) uint32 {
		n := uint32(len(str))
		str = append(str, []byte(s)...)
		str = append(str, 0)
		return n
	}
	symbol := add("get_module")
	var dynamic []byte
	dyn := func(tag elf.DynTag, value uint32) {
		entry := make([]byte, 16)
		binary.LittleEndian.PutUint64(entry, uint64(tag))
		binary.LittleEndian.PutUint64(entry[8:], uint64(value))
		dynamic = append(dynamic, entry...)
	}
	for _, lib := range libraries {
		dyn(elf.DT_NEEDED, add(lib))
	}
	if runpath != "" {
		dyn(elf.DT_RUNPATH, add(runpath))
	}
	dyn(elf.DT_NULL, 0)
	sym := make([]byte, 48)
	binary.LittleEndian.PutUint32(sym[24:], symbol)
	sym[28] = 0x12
	binary.LittleEndian.PutUint16(sym[30:], 1)
	shstr := []byte("\x00.dynstr\x00.dynamic\x00.dynsym\x00.shstrtab\x00")
	raw := make([]byte, 64+5*64)
	copy(raw, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(raw[16:], uint16(elf.ET_DYN))
	binary.LittleEndian.PutUint16(raw[18:], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(raw[20:], 1)
	binary.LittleEndian.PutUint64(raw[40:], 64)
	binary.LittleEndian.PutUint16(raw[52:], 64)
	binary.LittleEndian.PutUint16(raw[58:], 64)
	binary.LittleEndian.PutUint16(raw[60:], 5)
	binary.LittleEndian.PutUint16(raw[62:], 4)
	section := func(i int, name uint32, typ elf.SectionType, data []byte, link uint32, entry uint64) {
		h := raw[64+i*64 : 64+(i+1)*64]
		binary.LittleEndian.PutUint32(h, name)
		binary.LittleEndian.PutUint32(h[4:], uint32(typ))
		binary.LittleEndian.PutUint64(h[24:], uint64(len(raw)))
		binary.LittleEndian.PutUint64(h[32:], uint64(len(data)))
		binary.LittleEndian.PutUint32(h[40:], link)
		binary.LittleEndian.PutUint64(h[56:], entry)
		raw = append(raw, data...)
	}
	section(1, 1, elf.SHT_STRTAB, str, 0, 0)
	section(2, 9, elf.SHT_DYNAMIC, dynamic, 1, 16)
	section(3, 18, elf.SHT_DYNSYM, sym, 1, 24)
	section(4, 26, elf.SHT_STRTAB, shstr, 0, 0)
	return append(raw, []byte("API"+api+",NTS\x00")...)
}

func TestMaintenancePHPINIDoesNotChangeHistoricalAcceptance(t *testing.T) {
	for _, value := range []string{"imagick", "imagick.so"} {
		ini := []byte("extension=" + value + "\n")
		if err := validateMaintenancePHPINI(ini); err != nil {
			t.Fatal(err)
		}
		if validatePHPINI(ini) == nil {
			t.Fatal("historical V1 validator accepted Imagick")
		}
	}
	for _, value := range []string{"extension=/root/imagick.so", "extension=custom.so", "extension=../imagick.so", "zend_extension=imagick.so", "extension_dir=/root/modules", "auto_prepend_file=/root/a.php"} {
		if validateMaintenancePHPINI([]byte(value)) == nil {
			t.Fatal("unreviewed startup accepted", value)
		}
	}
	if err := validateMaintenancePHPINI([]byte("extension=curl.so\nzend_extension=opcache.so\n")); err != nil {
		t.Fatal(err)
	}
}

func TestPHPModuleOwnerSelectsOnlyExactBranchAndAPI(t *testing.T) {
	path := "/usr/lib/php/20240924/imagick.so"
	for _, owner := range []string{"php8.4-imagick", "php8.4-imagick:" + runtime.GOARCH} {
		got, err := phpModuleOwner(owner+": "+path+"\n", "8.4", path)
		if err != nil || got != "php8.4-imagick" {
			t.Fatal(got, err)
		}
	}
	for _, output := range []string{"", "php8.5-imagick: " + path, "local-imagick: " + path, "php8.4-imagick:foreign: " + path, "php8.4-imagick:amd64:extra: " + path, "php8.4-imagick, local: " + path, "php8.4-imagick: /usr/lib/php/20250925/imagick.so", "php8.4-imagick: " + path + "\nphp8.4-common: " + path} {
		if _, err := phpModuleOwner(output, "8.4", path); err == nil {
			t.Fatal("unsafe module owner accepted", output)
		}
	}
}

func TestPHPModuleQualificationRequiresSelectedPayload(t *testing.T) {
	for _, scenario := range []string{"packaged", "missing", "tampered", "unowned", "aliased", "wrong-machine", "custom-rpath", "not-ELF", "wrong-api-directory", "unverified-package"} {
		t.Run(scenario, func(t *testing.T) {
			modulePath := "/usr/lib/php/20240924/curl.so"
			php := phpTestELF("20240924", nil, "")
			module := phpTestELF("20240924", []string{"libc.so.6"}, "")
			p := newProfile(ComposerUpdate)
			p.External["php-interpreter"] = "/usr/bin/php8.4"
			p.Files["/usr/bin/php8.4"] = digest(php)
			verified := digest(module)
			if scenario == "tampered" {
				module = append(module, 'x')
			}
			if scenario == "wrong-machine" {
				binary.LittleEndian.PutUint16(module[18:], uint16(elf.EM_AARCH64))
				verified = digest(module)
			}
			if scenario == "custom-rpath" {
				module = phpTestELF("20240924", nil, "/opt/local/lib")
				verified = digest(module)
			}
			if scenario == "not-ELF" {
				module = []byte("custom code")
				verified = digest(module)
			}
			in := phpModuleInspection{
				read: func(path string, _ int64) ([]byte, error) {
					if path == "/usr/bin/php8.4" {
						return php, nil
					}
					if path != modulePath {
						t.Fatalf("unexpected read %s", path)
					}
					if scenario == "missing" {
						return nil, os.ErrNotExist
					}
					if scenario == "unowned" {
						return nil, errors.New("not root-owned")
					}
					return module, nil
				},
				resolve: func(path string) (string, error) {
					if scenario == "aliased" {
						return "/opt/imagick.so", nil
					}
					return path, nil
				},
				packages: func(_ context.Context, p *profile, names ...string) error {
					if scenario != "unverified-package" {
						p.Files[modulePath] = verified
					}
					return nil
				},
				query: func(_ context.Context, _ *profile, args ...string) (string, error) {
					if strings.Join(args, " ") != "-S "+modulePath {
						t.Fatalf("not an exact API module query: %v", args)
					}
					path := modulePath
					if scenario == "wrong-api-directory" {
						path = "/usr/lib/php/20250925/curl.so"
					}
					return "php8.4-curl: " + path + "\n", nil
				},
			}
			err := in.qualify(context.Background(), &p, "8.4", []string{"curl"})
			if scenario == "packaged" {
				if err != nil || p.External["php-module:curl.so"] != modulePath {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid module accepted")
			}
		})
	}
}

func TestPHPModuleQualificationRejectsCustomBeforeQuery(t *testing.T) {
	in := phpModuleInspection{query: func(context.Context, *profile, ...string) (string, error) {
		t.Fatal("queried a custom module")
		return "", nil
	}}
	p := newProfile(ComposerUpdate)
	if in.qualify(context.Background(), &p, "8.4", []string{"arbitrary"}) == nil {
		t.Fatal("custom module accepted")
	}
	if in.qualify(context.Background(), &p, "9.9", []string{"curl"}) == nil {
		t.Fatal("unreviewed PHP API accepted")
	}
}
