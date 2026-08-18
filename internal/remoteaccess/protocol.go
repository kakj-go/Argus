package remoteaccess

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

const (
	WebSocketProtocol = "argus.remote_access/v1"
	MaxFrameBytes     = 64 << 10
	HelloTimeout      = 3 * time.Second
)

var ErrProtocol = errors.New("REMOTE_ACCESS_PROTOCOL_ERROR")

type ClientFrame struct {
	Protocol string `json:"protocol"`
	Type     string `json:"type"`
	Sequence uint64 `json:"sequence"`
	Ticket   string `json:"ticket,omitempty"`
	Nonce    string `json:"nonce,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
	Data     string `json:"data,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type ProtocolState struct {
	StartedAt    time.Time
	HelloSeen    bool
	LastSequence uint64
}

func (state *ProtocolState) Accept(raw []byte, now time.Time) (ClientFrame, error) {
	if len(raw) == 0 || len(raw) > MaxFrameBytes {
		return ClientFrame{}, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var frame ClientFrame
	if err := decoder.Decode(&frame); err != nil || frame.Protocol != WebSocketProtocol {
		return ClientFrame{}, ErrProtocol
	}
	if !state.HelloSeen {
		if state.StartedAt.IsZero() {
			state.StartedAt = now
		}
		if now.Sub(state.StartedAt) > HelloTimeout || frame.Type != "client_hello" || frame.Sequence != 1 || len(frame.Ticket) < 43 || len(frame.Nonce) < 16 || !validSize(frame.Cols, frame.Rows) {
			return ClientFrame{}, ErrProtocol
		}
		state.HelloSeen, state.LastSequence = true, 1
		return frame, nil
	}
	if frame.Sequence != state.LastSequence+1 || frame.Type == "client_hello" {
		return ClientFrame{}, ErrProtocol
	}
	switch frame.Type {
	case "input":
		if frame.Data == "" {
			return ClientFrame{}, ErrProtocol
		}
	case "resize":
		if !validSize(frame.Cols, frame.Rows) {
			return ClientFrame{}, ErrProtocol
		}
	case "ping", "close":
	default:
		return ClientFrame{}, ErrProtocol
	}
	state.LastSequence = frame.Sequence
	return frame, nil
}

func validSize(cols, rows int) bool { return cols >= 20 && cols <= 1000 && rows >= 5 && rows <= 500 }
