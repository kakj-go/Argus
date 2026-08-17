package connector

import (
	"sync"

	"github.com/google/uuid"
)

// DispatchHub routes best-effort Redis wakeups to the Gateway process that
// owns the Connector stream. PostgreSQL remains the authoritative queue.
type DispatchHub struct {
	mu      sync.RWMutex
	waiters map[uuid.UUID]map[chan struct{}]struct{}
}

func NewDispatchHub() *DispatchHub {
	return &DispatchHub{waiters: make(map[uuid.UUID]map[chan struct{}]struct{})}
}

func (hub *DispatchHub) Register(connectorID uuid.UUID) (<-chan struct{}, func()) {
	if hub == nil {
		return nil, func() {}
	}
	wakeup := make(chan struct{}, 1)
	hub.mu.Lock()
	if hub.waiters[connectorID] == nil {
		hub.waiters[connectorID] = make(map[chan struct{}]struct{})
	}
	hub.waiters[connectorID][wakeup] = struct{}{}
	hub.mu.Unlock()
	return wakeup, func() {
		hub.mu.Lock()
		delete(hub.waiters[connectorID], wakeup)
		if len(hub.waiters[connectorID]) == 0 {
			delete(hub.waiters, connectorID)
		}
		hub.mu.Unlock()
	}
}

func (hub *DispatchHub) Notify(connectorID uuid.UUID) {
	if hub == nil {
		return
	}
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for wakeup := range hub.waiters[connectorID] {
		select {
		case wakeup <- struct{}{}:
		default:
		}
	}
}
