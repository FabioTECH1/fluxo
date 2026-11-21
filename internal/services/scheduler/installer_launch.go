package scheduler

import "errors"

type installerLaunchPendingError struct{}

func (*installerLaunchPendingError) Error() string {
	return "reconcile outstanding Scheduler launch authority before continuing"
}

// InstallerLaunchPending permits only a bounded installer wait for a current,
// structurally valid, unconsumed bootstrap launch. It says nothing about whether
// an application has run. The caller must release lifecycle admission before
// waiting, then repeat native reconciliation and every admission check. No
// invocation is consumed, retired or replayed by this classification.
func InstallerLaunchPending(err error) bool {
	for err != nil {
		if observed, ok := err.(*installerLaunchPendingError); ok {
			return observed != nil
		}
		// Mixed errors retain their original refusal, including a joined error
		// whose other side indicates changed evidence or maintenance ownership.
		err = errors.Unwrap(err)
	}
	return false
}
