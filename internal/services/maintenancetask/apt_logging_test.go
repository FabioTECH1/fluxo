package maintenancetask

import (
	"strings"
	"testing"
)

func TestAPTLoggingRoleExcludesApplicationMembership(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\nsyslog:x:102:102::/nonexistent:/usr/sbin/nologin\nfluxo:x:988:988::/home/fluxo:/bin/bash\n"
	group := "root:x:0:\nsyslog:x:102:\nfluxo:x:988:\n"
	nss := "passwd: files systemd\ngroup: files systemd\nhosts: files dns\n"
	uid, gid, err := aptLoggingRole([]byte(passwd), []byte(group), []byte(nss))
	if err != nil || uid != 102 || gid != 102 {
		t.Fatal("standard closed syslog role refused", uid, gid, err)
	}
	for name, change := range map[string]struct{ passwd, group, nss string }{
		"supplementary application": {passwd, strings.Replace(group, "syslog:x:102:", "syslog:x:102:fluxo", 1), nss},
		"primary application":       {strings.Replace(passwd, "fluxo:x:988:988:", "fluxo:x:988:102:", 1), group, nss},
		"shared UID application":    {strings.Replace(passwd, "fluxo:x:988:988:", "fluxo:x:102:988:", 1), group, nss},
		"group alias":               {passwd, group + "application:x:102:\n", nss},
		"interactive logging user":  {strings.Replace(passwd, "/usr/sbin/nologin", "/bin/bash", 1), group, nss},
		"non-system logging user":   {strings.Replace(passwd, "syslog:x:102:102:", "syslog:x:1102:102:", 1), group, nss},
		"external membership":       {passwd, group, strings.Replace(nss, "group: files systemd", "group: files sss", 1)},
		"duplicate identity policy": {passwd, group, nss + "group: files\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := aptLoggingRole([]byte(change.passwd), []byte(change.group), []byte(change.nss)); err == nil {
				t.Fatal("unqualified logging role accepted")
			}
		})
	}
}
