package remoteaccess

import (
	"sync"

	"github.com/google/uuid"
)

type Termination struct {
	Fence  int64
	Reason string
}

type TerminationHub struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]map[chan Termination]int64
}

func NewTerminationHub() *TerminationHub {
	return &TerminationHub{sessions: map[uuid.UUID]map[chan Termination]int64{}}
}

func (hub *TerminationHub) Register(sessionID uuid.UUID, fence int64) (<-chan Termination, func()) {
	if hub == nil || sessionID == uuid.Nil || fence < 1 {
		return nil, func() {}
	}
	channel := make(chan Termination, 1)
	hub.mu.Lock()
	if hub.sessions[sessionID] == nil {
		hub.sessions[sessionID] = map[chan Termination]int64{}
	}
	hub.sessions[sessionID][channel] = fence
	hub.mu.Unlock()
	return channel, func() {
		hub.mu.Lock()
		if registered := hub.sessions[sessionID]; registered != nil {
			delete(registered, channel)
			if len(registered) == 0 {
				delete(hub.sessions, sessionID)
			}
		}
		hub.mu.Unlock()
	}
}

func (hub *TerminationHub) Notify(sessionID uuid.UUID, termination Termination) {
	if hub == nil || sessionID == uuid.Nil || termination.Fence < 1 {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for channel, activeFence := range hub.sessions[sessionID] {
		if termination.Fence <= activeFence {
			continue
		}
		select {
		case channel <- termination:
		default:
		}
	}
}
