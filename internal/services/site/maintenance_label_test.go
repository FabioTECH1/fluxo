package site

import "testing"

func TestScheduledTaskLabelSeparatesGlobalAndOrphan(t *testing.T) {
	for _, row := range []struct {
		name, domain string
		id           int
		want         string
	}{
		{"Renew SSL Certificates", "", 0, `"Renew SSL Certificates" (server-wide)`},
		{"Report", "example.test", 3, `"Report" on example.test`},
		{"Report", "", 3, `"Report" (no site domain available)`},
	} {
		if got := ScheduledTaskLabel(row.name, row.domain, row.id); got != row.want {
			t.Fatal(got, row.want)
		}
	}
}
