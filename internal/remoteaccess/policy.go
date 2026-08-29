// Package remoteaccess owns human remote access authorization and session invariants.
package remoteaccess

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGrantRequired    = errors.New("REMOTE_ACCESS_GRANT_REQUIRED")
	ErrScopeDenied      = errors.New("REMOTE_ACCESS_SCOPE_DENIED")
	ErrApprovalRequired = errors.New("REMOTE_ACCESS_APPROVAL_REQUIRED")
	ErrLeaseExpired     = errors.New("REMOTE_ACCESS_LEASE_EXPIRED")
	ErrCapacityExceeded = errors.New("REMOTE_ACCESS_CAPACITY_EXCEEDED")
)

const (
	LeaseTTL           = 15 * time.Minute
	RequestTTL         = 7 * 24 * time.Hour
	TicketTTL          = time.Minute
	ConnectionWindow   = 5 * time.Minute
	DefaultIdleTimeout = 15 * time.Minute
	DefaultMaxDuration = time.Hour
)

type Grant struct {
	ID                uuid.UUID
	EnterpriseID      uuid.UUID
	SubjectType       string
	SubjectID         uuid.UUID
	HostIDs           []uuid.UUID
	ManagedAccountIDs []uuid.UUID
	Protocols         []string
	Actions           []string
	ValidFrom         time.Time
	ValidUntil        time.Time
	Status            string
	Version           int64
}

type Intent struct {
	EnterpriseID      uuid.UUID
	UserID            uuid.UUID
	DepartmentID      uuid.UUID
	HostID            uuid.UUID
	HostLabels        map[string]string
	ManagedAccountID  uuid.UUID
	Protocol          string
	Action            string
	AuthorizationTime time.Time
}

func (grant Grant) Authorizes(intent Intent) bool {
	now := intent.AuthorizationTime
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if grant.Status != GovernanceEnabled || grant.EnterpriseID != intent.EnterpriseID || now.Before(grant.ValidFrom) || !now.Before(grant.ValidUntil) {
		return false
	}
	if grant.SubjectType == "user" && grant.SubjectID != intent.UserID {
		return false
	}
	if grant.SubjectType == "department" && grant.SubjectID != intent.DepartmentID {
		return false
	}
	if grant.SubjectType != "user" && grant.SubjectType != "department" {
		return false
	}
	if !slices.Contains(grant.ManagedAccountIDs, intent.ManagedAccountID) || !slices.Contains(grant.Protocols, intent.Protocol) || !slices.Contains(grant.Actions, intent.Action) {
		return false
	}
	if slices.Contains(grant.HostIDs, intent.HostID) {
		return true
	}
	return false
}

type Capacity struct {
	UserActive       int
	HostActive       int
	EnterpriseActive int
	UserLimit        int
	HostLimit        int
	EnterpriseLimit  int
}

func (capacity Capacity) Check() error {
	userLimit, hostLimit, enterpriseLimit := capacity.UserLimit, capacity.HostLimit, capacity.EnterpriseLimit
	if userLimit == 0 {
		userLimit = 3
	}
	if hostLimit == 0 {
		hostLimit = 5
	}
	if enterpriseLimit == 0 {
		enterpriseLimit = 50
	}
	if capacity.UserActive >= userLimit || capacity.HostActive >= hostLimit || capacity.EnterpriseActive >= enterpriseLimit {
		return ErrCapacityExceeded
	}
	return nil
}
