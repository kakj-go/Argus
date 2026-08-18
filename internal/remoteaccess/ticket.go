package remoteaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTicketExpired  = errors.New("REMOTE_ACCESS_TICKET_EXPIRED")
	ErrTicketConsumed = errors.New("REMOTE_ACCESS_TICKET_CONSUMED")
	ErrTicketBinding  = errors.New("REMOTE_ACCESS_SCOPE_DENIED")
)

type TicketBinding struct {
	TicketID             uuid.UUID
	SessionID            uuid.UUID
	HTTPSessionID        uuid.UUID
	EnterpriseID         uuid.UUID
	UserID               uuid.UUID
	HostID               uuid.UUID
	ManagedAccountID     uuid.UUID
	LeaseID              uuid.UUID
	Protocol             string
	AuthorizationVersion int64
	SessionFence         int64
	ExpiresAt            time.Time
}

type TicketStore interface {
	CreateTicket(context.Context, TicketBinding, [32]byte) error
	ConsumeTicket(context.Context, [32]byte, TicketBinding, time.Time) (TicketBinding, error)
}

type TicketIssuer struct {
	Store TicketStore
	Now   func() time.Time
}

func (issuer TicketIssuer) Issue(ctx context.Context, binding TicketBinding) (string, TicketBinding, error) {
	if issuer.Store == nil {
		return "", TicketBinding{}, ErrTicketBinding
	}
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", TicketBinding{}, err
	}
	now := issuer.now()
	binding.TicketID = uuid.New()
	binding.ExpiresAt = now.Add(TicketTTL)
	hash := sha256.Sum256(value)
	if err := issuer.Store.CreateTicket(ctx, binding, hash); err != nil {
		return "", TicketBinding{}, err
	}
	return base64.RawURLEncoding.EncodeToString(value), binding, nil
}

func (issuer TicketIssuer) Consume(ctx context.Context, value string, expected TicketBinding) (TicketBinding, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) != 32 {
		return TicketBinding{}, ErrTicketBinding
	}
	hash := sha256.Sum256(raw)
	return issuer.Store.ConsumeTicket(ctx, hash, expected, issuer.now())
}

func (issuer TicketIssuer) now() time.Time {
	if issuer.Now != nil {
		return issuer.Now().UTC()
	}
	return time.Now().UTC()
}
