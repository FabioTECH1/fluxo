// Package siteexecution coordinates interactive sessions with site mutations.
package siteexecution

import (
	"fluxo/internal/services/packages"
	"sync"
)

var mu sync.Mutex
var gates = map[int]*sync.RWMutex{}

func gate(id int) *sync.RWMutex {
	mu.Lock()
	defer mu.Unlock()
	if gates[id] == nil {
		gates[id] = &sync.RWMutex{}
	}
	return gates[id]
}

func Read(id int) (func(), bool) {
	if packages.UnownedTinkerDomainsIdle() != nil {
		return nil, false
	}
	g := gate(id)
	if !g.TryRLock() {
		return nil, false
	}
	return g.RUnlock, true
}

func Write(id int) (func(), bool) {
	if packages.UnownedTinkerDomainsIdle() != nil {
		return nil, false
	}
	g := gate(id)
	if !g.TryLock() {
		return nil, false
	}
	return g.Unlock, true
}
