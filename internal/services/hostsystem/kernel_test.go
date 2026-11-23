package hostsystem

import (
	"context"
	"os"
	"testing"
)

func TestRunningKernelUsesBootedReleaseAndFailsIndependently(t *testing.T) {
	h := fixture(t)
	if h.RunningKernel() != nil {
		t.Fatal("invented missing kernel")
	}
	put(t, h, "/proc/sys/kernel/osrelease", "6.8.0-88-generic\n")
	s, e := h.Status(context.Background())
	if e != nil || s.RunningKernel == nil || *s.RunningKernel != "6.8.0-88-generic" || s.BootID != oldBoot {
		t.Fatal(s, e)
	}
	// Package policy observations never provide this value.
	for _, bad := range []string{"", "6.8\x00fake", "6.8\nother", "../../kernel"} {
		put(t, h, "/proc/sys/kernel/osrelease", bad)
		s, e = h.Status(context.Background())
		if e != nil || s.RunningKernel != nil || s.BootID != oldBoot {
			t.Fatal("kernel failure broke System/boot observation", s, e)
		}
	}
	os.Remove(h.path("/proc/sys/kernel/osrelease"))
	if e = os.Symlink(h.path("/proc/sys/kernel/random/boot_id"), h.path("/proc/sys/kernel/osrelease")); e != nil {
		t.Fatal(e)
	}
	if h.RunningKernel() != nil {
		t.Fatal("followed substituted kernel symlink")
	}
}
