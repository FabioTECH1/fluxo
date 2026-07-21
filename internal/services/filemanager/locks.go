package filemanager

import (
	"sync"
)

type referencedPathLock struct {
	mutex sync.Mutex
	refs  int
}

var mutationLocks = struct {
	sync.Mutex
	items map[string]*referencedPathLock
}{items: make(map[string]*referencedPathLock)}

// lockSiteMutations serializes writes within one site. A site-level lock also
// covers aliases such as current/file and releases/<id>/file that can refer to
// the same inode in a zero-downtime layout. Different sites remain independent.
func lockSiteMutations(root string) func() {
	mutationLocks.Lock()
	lock := mutationLocks.items[root]
	if lock == nil {
		lock = &referencedPathLock{}
		mutationLocks.items[root] = lock
	}
	lock.refs++
	mutationLocks.Unlock()

	lock.mutex.Lock()

	return func() {
		lock.mutex.Unlock()
		mutationLocks.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(mutationLocks.items, root)
		}
		mutationLocks.Unlock()
	}
}
