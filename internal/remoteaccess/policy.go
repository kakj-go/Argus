// Package remoteaccess owns human remote access authorization and session invariants.
package remoteaccess

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGrantRequired    = errors.New("REMOTE_ACCESS_GRANT_REQUIRED")
	ErrScopeDenied      = errors.New("REMOTE_ACCESS_SCOPE_DENIED")
	ErrApprovalRequired = errors.New("REMOTE_ACCESS_APPROVAL_REQUIRED")
	ErrMFARequired      = errors.New("REMOTE_ACCESS_MFA_REQUIRED")
	ErrLeaseExpired     = errors.New("REMOTE_ACCESS_LEASE_EXPIRED")
	ErrCapacityExceeded = errors.New("REMOTE_ACCESS_CAPACITY_EXCEEDED")
)

const (
	LeaseTTL           = 15 * time.Minute
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
	HostSelector      map[string][]string
	ManagedAccountIDs []uuid.UUID
	Protocols         []string
	Actions           []string
	ValidFrom         time.Time
	ValidUntil        time.Time
	Enabled           bool
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
	if !grant.Enabled || grant.EnterpriseID != intent.EnterpriseID || now.Before(grant.ValidFrom) || !now.Before(grant.ValidUntil) {
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
	return slices.Contains(grant.HostIDs, intent.HostID) || matchesSelector(grant.HostSelector, intent.HostLabels)
}

func matchesSelector(selector map[string][]string, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, allowed := range selector {
		value, ok := labels[key]
		if !ok || !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}

type Policy struct {
	ID                 uuid.UUID
	Version            int64
	Enabled            bool
	Priority           int
	Protocols          []string
	HostSelector       map[string][]string
	ApproverRoleIDs    []uuid.UUID
	MinimumApprovals   int
	SeparationOfDuties bool
	RequireMFA         bool
	MaxSessionDuration time.Duration
	IdleTimeout        time.Duration
}

type Requirement struct {
	PolicyID           uuid.UUID
	PolicyVersion      int64
	ApproverRoleIDs    []uuid.UUID
	MinimumApprovals   int
	SeparationOfDuties bool
	RequireMFA         bool
}

type PolicyResult struct {
	Requirements       []Requirement
	MaxSessionDuration time.Duration
	IdleTimeout        time.Duration
	SnapshotHash       [32]byte
}

func MatchPolicies(policies []Policy, intent Intent) (PolicyResult, error) {
	result := PolicyResult{MaxSessionDuration: DefaultMaxDuration, IdleTimeout: DefaultIdleTimeout}
	for _, policy := range policies {
		if !policy.Enabled || !slices.Contains(policy.Protocols, intent.Protocol) {
			continue
		}
		if len(policy.HostSelector) != 0 && !matchesSelector(policy.HostSelector, intent.HostLabels) {
			continue
		}
		if policy.RequireMFA {
			return PolicyResult{}, ErrMFARequired
		}
		minimum := max(policy.MinimumApprovals, 1)
		result.Requirements = append(result.Requirements, Requirement{
			PolicyID: policy.ID, PolicyVersion: policy.Version, ApproverRoleIDs: slices.Clone(policy.ApproverRoleIDs),
			MinimumApprovals: minimum, SeparationOfDuties: policy.SeparationOfDuties,
		})
		if policy.MaxSessionDuration > 0 && policy.MaxSessionDuration < result.MaxSessionDuration {
			result.MaxSessionDuration = policy.MaxSessionDuration
		}
		if policy.IdleTimeout > 0 && policy.IdleTimeout < result.IdleTimeout {
			result.IdleTimeout = policy.IdleTimeout
		}
	}
	slices.SortFunc(result.Requirements, func(a, b Requirement) int { return strings.Compare(a.PolicyID.String(), b.PolicyID.String()) })
	canonical, _ := json.Marshal(result)
	result.SnapshotHash = sha256.Sum256(canonical)
	return result, nil
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
