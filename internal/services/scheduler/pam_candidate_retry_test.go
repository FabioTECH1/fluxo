package scheduler

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPAMSnapshotRetryDiscardsOnlyProtectedDirectoryChurn(t *testing.T) {
	churn := &pamDirectorySnapshotChanged{"/run/systemd/transient", "during inspection"}
	for failures := 0; failures <= 3; failures++ {
		calls := 0
		report, err := retryPAMDirectorySnapshot(func() (pamCandidateReport, error) {
			calls++
			if calls <= failures {
				return pamCandidateReport{Candidate: "discarded"}, fmt.Errorf("wrapped: %w", churn)
			}
			return pamCandidateReport{Candidate: "fresh-full-inspection"}, nil
		})
		if failures < 3 && (err != nil || calls != failures+1 || report.Candidate != "fresh-full-inspection") {
			t.Fatal(failures, calls, report, err)
		}
		if failures == 3 && (err == nil || calls != 3) {
			t.Fatal("unbounded retry or unstable success", calls, err)
		}
	}
	calls := 0
	refused := errors.New("changed executable, permission or selector")
	_, err := retryPAMDirectorySnapshot(func() (pamCandidateReport, error) { calls++; return pamCandidateReport{}, refused })
	if calls != 1 || !errors.Is(err, refused) {
		t.Fatal("ordinary refusal retried", calls, err)
	}
}

func TestPAMDirectoryRetryRequiresSameProtectedInodeAndAuthority(t *testing.T) {
	a := pamCandidateObservation{Dev: 1, Ino: 2, Mode: unix.S_IFDIR | 0755, UID: 0, GID: 0, Nlink: 2}
	b := a
	b.Size++
	b.Nlink++
	b.Ctime.Sec++
	b.Mtime.Sec++
	if !pamSameProtectedDirectory(a, b) {
		t.Fatal("directory membership churn not classified")
	}
	for name, mutate := range map[string]func(*pamCandidateObservation){
		"file":     func(o *pamCandidateObservation) { o.Mode = unix.S_IFREG | 0644 },
		"symlink":  func(o *pamCandidateObservation) { o.Mode = unix.S_IFLNK | 0777 },
		"device":   func(o *pamCandidateObservation) { o.Dev++ },
		"inode":    func(o *pamCandidateObservation) { o.Ino++ },
		"mode":     func(o *pamCandidateObservation) { o.Mode ^= 0100 },
		"writable": func(o *pamCandidateObservation) { o.Mode |= 0020 },
		"owner":    func(o *pamCandidateObservation) { o.UID++ },
		"group":    func(o *pamCandidateObservation) { o.GID++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := b
			mutate(&changed)
			if pamSameProtectedDirectory(a, changed) || pamSameProtectedDirectory(changed, changed) && (name == "file" || name == "symlink" || name == "writable" || name == "owner") {
				t.Fatal("unsafe input classified as transient directory churn")
			}
		})
	}
	p := pamCandidateInspection{observed: map[string]pamCandidateObservation{"/fixture": a}}
	var churn *pamDirectorySnapshotChanged
	if !errors.As(p.remember("/fixture", b), &churn) || p.observed["/fixture"] != a {
		t.Fatal("changed observation reused")
	}
}
