package maintenancetask

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// APT logging has a separate, explicit trust role. Ubuntu's /var/log may be
// root:syslog 0775; this is not an exception for cache/deletion targets or for
// arbitrary group-writable ancestors. The closed logging role is trusted to
// control log destinations, never application or package execution inputs.
func qualifyAPTLogging(p *profile) error {
	if err := trustedDatabaseDirectory("/var/log/apt"); err == nil {
		return recordAPTDirectory(p, "/var/log/apt")
	}
	if err := trustedDatabaseDirectory("/var"); err != nil {
		return err
	}
	data := map[string][]byte{}
	for _, path := range []string{"/etc/passwd", "/etc/group", "/etc/nsswitch.conf"} {
		raw, err := readRegular(path, 8<<20)
		if err != nil {
			return err
		}
		data[path] = raw
		p.Files[path] = digest(raw)
	}
	uid, gid, err := aptLoggingRole(data["/etc/passwd"], data["/etc/group"], data["/etc/nsswitch.conf"])
	if err != nil {
		return err
	}
	for path, expected := range map[string]struct {
		gid  uint32
		mode os.FileMode
	}{
		"/var/log": {gid, 0775}, "/var/log/apt": {0, 0755},
	} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || st.Uid != 0 || st.Gid != expected.gid || info.Mode().Perm() != expected.mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("APT logging directory needs maintenance review: %s", path)
		}
		if err := recordAPTDirectory(p, path); err != nil {
			return err
		}
	}
	p.External["apt-logging-role"] = fmt.Sprintf("syslog:%d:%d", uid, gid)
	return nil
}

func aptLoggingRole(passwd, group, nss []byte) (uint32, uint32, error) {
	bad := errors.New("APT logging role or membership needs maintenance review")
	if err := aptLocalIdentityPolicy(nss); err != nil {
		return 0, 0, err
	}
	type account struct {
		name, shell string
		uid, gid    uint32
	}
	var accounts []account
	usernames := map[string]bool{}
	var logging account
	for _, line := range strings.Split(string(passwd), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 7 || usernames[fields[0]] {
			return 0, 0, bad
		}
		uid, e1 := strconv.ParseUint(fields[2], 10, 32)
		gid, e2 := strconv.ParseUint(fields[3], 10, 32)
		if e1 != nil || e2 != nil {
			return 0, 0, bad
		}
		usernames[fields[0]] = true
		a := account{fields[0], fields[6], uint32(uid), uint32(gid)}
		accounts = append(accounts, a)
		if a.name == "syslog" {
			logging = a
		}
	}
	if logging.uid == 0 || logging.uid >= 1000 || logging.gid == 0 {
		return 0, 0, bad
	}
	switch logging.shell {
	case "/usr/sbin/nologin", "/sbin/nologin", "/bin/false", "/usr/bin/false":
	default:
		return 0, 0, bad
	}
	groupNames := map[string]bool{}
	matching := 0
	for _, line := range strings.Split(string(group), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 4 || groupNames[fields[0]] {
			return 0, 0, bad
		}
		gid, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return 0, 0, bad
		}
		groupNames[fields[0]] = true
		if fields[0] == "syslog" && uint32(gid) != logging.gid {
			return 0, 0, bad
		}
		if uint32(gid) != logging.gid {
			continue
		}
		if fields[0] != "syslog" {
			return 0, 0, bad
		}
		matching++
		for _, name := range strings.Split(fields[3], ",") {
			if name != "" && name != "root" && name != "syslog" {
				return 0, 0, bad
			}
		}
	}
	if matching != 1 {
		return 0, 0, bad
	}
	for _, a := range accounts {
		if a.uid == logging.uid && a.name != "syslog" {
			return 0, 0, bad
		}
		if a.gid == logging.gid && a.name != "syslog" && !(a.name == "root" && a.uid == 0) {
			return 0, 0, bad
		}
	}
	return logging.uid, logging.gid, nil
}

func aptLocalIdentityPolicy(raw []byte) error {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "#")
		key, value, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !ok || key != "passwd" && key != "group" {
			continue
		}
		if seen[key] {
			return errors.New("duplicate identity lookup policy needs maintenance review")
		}
		seen[key] = true
		policy := strings.Join(strings.Fields(value), " ")
		if policy != "files" && policy != "files systemd" {
			return errors.New("nonlocal logging-group identity policy needs maintenance review")
		}
	}
	if !seen["passwd"] || !seen["group"] {
		return errors.New("missing logging-group identity policy")
	}
	return nil
}
