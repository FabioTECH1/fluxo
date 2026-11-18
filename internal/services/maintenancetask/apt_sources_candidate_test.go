package maintenancetask

import "testing"

func TestAPTSourcePartUsesFinalExtension(t *testing.T) {
	classic := []byte("deb https://example.invalid/ubuntu noble main\n")
	deb822 := []byte("Types: deb\nURIs: https://example.invalid/ubuntu\nSuites: noble\nComponents: main\n")
	for _, tc := range []struct {
		name   string
		raw    []byte
		count  int
		reject bool
	}{
		{"ubuntu.list", classic, 1, false}, {"ubuntu.sources", deb822, 1, false},
		{"ubuntu.sources.curtin.orig", deb822, 0, false},
		{"ubuntu.list.save", []byte("deb unreviewed://backup/ noble main\n"), 0, false},
		{"provider.sources.disabled", []byte("unparsed backup data\x00"), 0, false},
		{"provider.list.dpkg-old", classic, 0, false}, {"README", classic, 0, false},
		{"ubuntu.sources~", deb822, 0, false}, {"ubuntu.SOURCES", deb822, 0, false},
		{"backup.orig.list", classic, 1, false}, {"backup.disabled.sources", deb822, 1, false},
		{"backup.orig.list", []byte("deb unreviewed://active/ noble main\n"), 0, true},
		{"backup.disabled.sources", []byte("Types: deb\nURIs: unreviewed://active/\nSuites: noble\n"), 0, true},
		{"broken.list", []byte("not a source"), 0, true}, {"broken.sources", []byte("not a source"), 0, true},
		{".hidden.sources", []byte("not a source"), 0, true}, // conservative for active-looking names
		{"bad-\xff.orig", classic, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := validateAPTSourcePart(tc.name, tc.raw)
			if (err != nil) != tc.reject || err == nil && n != tc.count {
				t.Fatalf("count=%d err=%v", n, err)
			}
		})
	}
}
