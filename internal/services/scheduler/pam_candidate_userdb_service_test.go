package scheduler

import (
	"strings"
	"testing"
)

func pamUserdbAbsentUnit(service bool) string {
	raw := "LoadState=not-found\nActiveState=inactive\nFragmentPath=\nDropInPaths=\nNeedDaemonReload=no\nTransient=no\nPassEnvironment=\nUnsetEnvironment=\nRootDirectory=\nRootImage=\nEnvironment=\n"
	if service {
		raw += "MainPID=0\n"
	}
	return raw
}

func TestPAMUserdbPreservesStockServiceAbsence(t *testing.T) {
	for _, unit := range []string{"systemd-userdbd.service", "systemd-userdbd.socket"} {
		raw := pamUserdbAbsentUnit(strings.HasSuffix(unit, ".service"))
		m, err := pamUserdbUnitMetadata(raw, unit)
		if err != nil || m["LoadState"] != "not-found" {
			t.Fatal(unit, err)
		}
		// systemctl legitimately omits type-specific properties for an absent
		// socket; omission must not be interpreted as a loaded default service.
		for _, change := range []struct{ old, new string }{
			{"LoadState=not-found", "LoadState=loaded"},
			{"ActiveState=inactive", "ActiveState=activating"},
			{"FragmentPath=", "FragmentPath=/etc/systemd/system/custom.service"},
			{"DropInPaths=", "DropInPaths=/etc/systemd/system/custom.service.d/override.conf"},
			{"NeedDaemonReload=no", "NeedDaemonReload=yes"},
			{"Transient=no", "Transient=yes"},
			{"Environment=", "Environment=SYSTEMD_USERWORK_PATH=/custom/worker"},
			{"RootDirectory=", "RootDirectory=/alternate/root"},
			{"PassEnvironment=", "PassEnvironment=SYSTEMD_USERWORK_PATH"},
		} {
			if _, err := pamUserdbUnitMetadata(strings.Replace(raw, change.old, change.new, 1), unit); err == nil {
				t.Fatal("accepted configuration drift", change.old)
			}
		}
		for _, authority := range []string{"ExecStart=/custom/worker", "ExecStartPre=/custom/hook", "EnvironmentFiles=/custom/env", "BindPaths=/custom", "MainPID=234"} {
			candidate := raw + authority + "\n"
			if _, err := pamUserdbUnitMetadata(candidate, unit); err == nil {
				t.Fatal("accepted retained authority", authority)
			}
		}
		if _, err := pamUserdbUnitMetadata(raw+"LoadState=not-found\n", unit); err == nil {
			t.Fatal("duplicate metadata accepted")
		}
		if _, err := pamUserdbUnitMetadata(strings.Replace(raw, "NeedDaemonReload=no\n", "", 1), unit); err == nil {
			t.Fatal("missing readback accepted")
		}
	}
}

func TestPAMUserdbManagerSelectorsNeverExposeValues(t *testing.T) {
	if err := pamUserdbForbiddenEnvironment("LANG=C.UTF-8\nPATH=/usr/bin:/bin\nUNRELATED_SECRET=do-not-log\n"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "SYSTEMD_NSS_DYNAMIC_BYPASS", "SYSTEMD_USERDB_ONLY", "SYSTEMD_USERWORK_PATH", "SYSTEMD_BYPASS_USERDB", "SYSTEMD_ONLY_USERDB", "NSS_WRAPPER_PASSWD", "GLIBC_TUNABLES", "GCONV_PATH", "LOCPATH", "NLSPATH"} {
		err := pamUserdbForbiddenEnvironment("PATH=/usr/bin\x00" + key + "=private-selector-value\x00")
		if err == nil || strings.Contains(err.Error(), "private-selector-value") {
			t.Fatal("selector accepted or its value disclosed", key, err)
		}
	}
	if pamUserdbForbiddenEnvironment("malformed-entry") == nil {
		t.Fatal("malformed manager readback accepted")
	}
}

func TestPAMUserdbEndpointRequiresExactListeningSocket(t *testing.T) {
	path := "/run/systemd/userdb/io.systemd.DynamicUser"
	line := "0000000000000000: 00000002 00000000 00010000 0001 01 1234 " + path + "\n"
	if inodes, err := pamUserdbListeningInodes(line, path); err != nil || len(inodes) != 1 || inodes[0] != "1234" {
		t.Fatal(inodes, err)
	}
	for _, raw := range []string{
		line + line,
		strings.Replace(line, "00010000", "00000000", 1),
		strings.Replace(line, "0001 01", "0002 01", 1),
		strings.Replace(line, "1234", "0", 1),
		strings.Replace(line, path, path+"-custom", 1),
	} {
		if _, err := pamUserdbListeningInodes(raw, path); err == nil {
			t.Fatal("ambiguous or non-listening identity endpoint accepted")
		}
	}
}

func TestPAMUserdbNativeMultipleListenersRequireEveryPID1FD(t *testing.T) {
	path := "/run/systemd/userdb/io.systemd.DynamicUser"
	first := "ffff8c0f41135f80: 00000002 00000000 00010000 0001 01 698 " + path + "\n"
	second := "ffff8c0f4b6f1200: 00000002 00000000 00010000 0001 01 7033 " + path + "\n"
	inodes, err := pamUserdbListeningInodes(first+second, path)
	if err != nil || len(inodes) != 2 || inodes[0] != "698" || inodes[1] != "7033" {
		t.Fatal(inodes, err)
	}
	reversed, err := pamUserdbListeningInodes(second+first, path)
	if err != nil || strings.Join(reversed, ",") != strings.Join(inodes, ",") {
		t.Fatal("kernel table ordering changed listener meaning", err)
	}
	if pamUserdbAllListenersOwned(inodes, map[string]bool{"socket:[698]": true, "socket:[7033]": true}) != nil {
		t.Fatal("complete authenticated PID1 listener set refused")
	}
	for _, partial := range []map[string]bool{{"socket:[698]": true}, {"socket:[7033]": true}, {"socket:[1234]": true}} {
		if pamUserdbAllListenersOwned(inodes, partial) == nil {
			t.Fatal("selected one listener while ignoring an unowned same-path listener")
		}
	}
	malformed := strings.Replace(second, "0001 01", "0002 01", 1)
	if _, err := pamUserdbListeningInodes(first+malformed, path); err == nil {
		t.Fatal("valid listener hid invalid same-path socket")
	}
}
