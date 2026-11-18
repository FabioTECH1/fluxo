package scheduler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func absenceInspection() *pamCandidateInspection {
	return &pamCandidateInspection{observed: map[string]pamCandidateObservation{}, report: pamCandidateReport{Files: map[string]string{}, Identity: map[string]string{}}}
}

func TestNobleAbsenceGateRetainsProtectedDirectoryChurn(t *testing.T) {
	root, err := pamCandidateObserve("/etc")
	if err != nil {
		t.Fatal(err)
	}
	root.Mtime.Sec--
	p := absenceInspection()
	p.observed["/etc"] = root
	err = p.nobleSELinuxInputs(os.Getpid())
	var churn *pamDirectorySnapshotChanged
	if !errors.As(err, &churn) || len(p.report.Files) != 0 {
		t.Fatalf("absence gate hid traversal failure or recorded false absence: %v", err)
	}
}

func TestAbsentInputRetriesFreshInspectionAndKeepsAuthorityRefusals(t *testing.T) {
	path := "/etc/fluxo-absence-test-" + uuid.NewString()
	root, err := pamCandidateObserve("/etc")
	if err != nil {
		t.Fatal(err)
	}
	root.Mtime.Sec--
	calls := 0
	report, err := retryPAMDirectorySnapshot(func() (pamCandidateReport, error) {
		calls++
		p := absenceInspection()
		p.report.Candidate = fmt.Sprint(calls)
		if calls == 1 {
			p.observed["/etc"] = root
		}
		err := p.requireAbsent(path, "present test leaf")
		if err != nil && len(p.report.Files) != 0 {
			t.Fatal("failed traversal recorded absence")
		}
		return p.report, err
	})
	if err != nil || calls != 2 || report.Candidate != "2" || report.Files[path] != "absent" {
		t.Fatalf("did not retry a fresh full inspection: calls=%d report=%+v error=%v", calls, report, err)
	}
	for name, path := range map[string]string{"present": "/etc", "unprotected-parent": filepath.Join(t.TempDir(), "absent")} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			_, err := retryPAMDirectorySnapshot(func() (pamCandidateReport, error) {
				calls++
				p := absenceInspection()
				err := p.requireAbsent(path, "present test leaf")
				if len(p.report.Files) != 0 {
					t.Fatal("unqualified input recorded as absent")
				}
				return p.report, err
			})
			if err == nil || calls != 1 {
				t.Fatalf("authority refusal retried or admitted: calls=%d error=%v", calls, err)
			}
		})
	}
}
