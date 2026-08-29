package remoteaccess

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	GovernanceDraft    = "draft"
	GovernanceEnabled  = "enabled"
	GovernanceDisabled = "disabled"
	GovernanceArchived = "archived"
)

var (
	ErrInvalidGovernance = errors.New("invalid remote access governance configuration")
)

var validRuleEffects = []string{"deny", "require_mfa", "require_approval", "notify"}

type RemoteAccessRule struct {
	ID                 uuid.UUID
	EnterpriseID       uuid.UUID
	Name               string
	Description        string
	Priority           int
	Protocols          []string
	Actions            []string
	SourceCIDRs        []string
	TimeWindows        []TimeWindow
	Effects            []string
	ApprovalWorkflowID uuid.UUID
	SessionProfileID   uuid.UUID
	Status             string
	Version            int64
}

type ApprovalWorkflow struct {
	ID                     uuid.UUID
	EnterpriseID           uuid.UUID
	Name                   string
	Description            string
	ApproverRoleIDs        []uuid.UUID
	EscalationRoleIDs      []uuid.UUID
	MinimumApprovals       int
	SeparationOfDuties     bool
	ApprovalTimeoutSeconds int
	EscalationAfterSeconds int
	TimeoutEffect          string
	Status                 string
	Version                int64
}

type SessionProfile struct {
	ID                 uuid.UUID
	EnterpriseID       uuid.UUID
	Name               string
	Description        string
	MaxSessionSeconds  int
	IdleTimeoutSeconds int
	RecordingMode      string
	CommandAuditMode   string
	ClipboardMode      string
	FileUploadMode     string
	FileDownloadMode   string
	PortForwardMode    string
	SessionShareMode   string
	RetentionDays      int
	Status             string
	Version            int64
}

type TimeWindow struct {
	DayOfWeek int    `json:"day_of_week"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Timezone  string `json:"timezone"`
}

func ValidateRule(name string, protocols, actions, effects []string, workflowID, profileID uuid.UUID, status string) error {
	if strings.TrimSpace(name) == "" || !validProtocols(protocols) || len(actions) != 1 || actions[0] != "terminal" || !validGovernanceStatus(status) || len(effects) > len(validRuleEffects) || (len(effects) == 0 && profileID == uuid.Nil) {
		return ErrInvalidGovernance
	}
	seen := make(map[string]struct{}, len(effects))
	for _, effect := range effects {
		if !slices.Contains(validRuleEffects, effect) {
			return ErrInvalidGovernance
		}
		if _, exists := seen[effect]; exists {
			return ErrInvalidGovernance
		}
		seen[effect] = struct{}{}
	}
	if slices.Contains(effects, "deny") && (len(effects) != 1 || workflowID != uuid.Nil || profileID != uuid.Nil) {
		return ErrInvalidGovernance
	}
	if slices.Contains(effects, "require_approval") && workflowID == uuid.Nil {
		return ErrInvalidGovernance
	}
	return nil
}

// ValidateGovernanceCreateStatus enforces an explicit draft-first lifecycle.
func ValidateGovernanceCreateStatus(status string) error {
	if status != GovernanceDraft {
		return ErrInvalidGovernance
	}
	return nil
}

func ValidateWorkflow(name string, roles []uuid.UUID, minimum, timeout int, status string) error {
	if strings.TrimSpace(name) == "" || len(roles) == 0 || minimum < 1 || minimum > len(roles) || minimum > 16 || timeout < 60 || timeout > 604800 || !validGovernanceStatus(status) || !validRoleIDs(roles) {
		return ErrInvalidGovernance
	}
	return nil
}

// ValidateWorkflowRoles validates both approval and escalation role sets as a
// single role namespace. A role cannot be repeated across either set because
// that would make quorum and escalation semantics ambiguous.
func ValidateWorkflowRoles(name string, approverRoles, escalationRoles []uuid.UUID, minimum, timeout, escalationAfter int, status string) error {
	if ValidateWorkflow(name, approverRoles, minimum, timeout, status) != nil {
		return ErrInvalidGovernance
	}
	if escalationAfter < 30 || escalationAfter >= timeout {
		return ErrInvalidGovernance
	}
	if len(escalationRoles) > 64 || !validRoleIDs(escalationRoles) {
		return ErrInvalidGovernance
	}
	seen := make(map[uuid.UUID]struct{}, len(approverRoles)+len(escalationRoles))
	for _, roleID := range append(append([]uuid.UUID(nil), approverRoles...), escalationRoles...) {
		if _, exists := seen[roleID]; exists {
			return ErrInvalidGovernance
		}
		seen[roleID] = struct{}{}
	}
	return nil
}

func validRoleIDs(values []uuid.UUID) bool {
	if len(values) > 64 {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func ValidateSessionProfile(name string, maxSession, idle, retention int, recording, audit, status string) error {
	if strings.TrimSpace(name) == "" || maxSession < 60 || maxSession > 86400 || idle < 60 || idle > maxSession || retention < 1 || retention > 3650 || !slices.Contains([]string{"required", "optional", "disabled"}, recording) || !slices.Contains([]string{"required", "optional", "disabled"}, audit) || !validGovernanceStatus(status) {
		return ErrInvalidGovernance
	}
	return nil
}

func ValidateSourceCIDRs(values []string) error {
	if len(values) > 64 {
		return ErrInvalidGovernance
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return ErrInvalidGovernance
		}
		canonical := prefix.Masked().String()
		if _, exists := seen[canonical]; exists {
			return ErrInvalidGovernance
		}
		seen[canonical] = struct{}{}
	}
	return nil
}

// ValidateTimeWindows keeps the wire contract simple while enforcing the
// scheduling semantics at the domain boundary.
func ValidateTimeWindows(value []byte) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return ErrInvalidGovernance
	}
	var windows []struct {
		DayOfWeek int    `json:"day_of_week"`
		Start     string `json:"start"`
		End       string `json:"end"`
		Timezone  string `json:"timezone"`
	}
	if err := json.Unmarshal(trimmed, &windows); err != nil || len(windows) > 32 {
		return ErrInvalidGovernance
	}
	for _, window := range windows {
		if window.DayOfWeek < 0 || window.DayOfWeek > 6 || strings.TrimSpace(window.Timezone) == "" {
			return ErrInvalidGovernance
		}
		if _, err := time.LoadLocation(window.Timezone); err != nil {
			return ErrInvalidGovernance
		}
		start, err := time.Parse("15:04", window.Start)
		if err != nil {
			return ErrInvalidGovernance
		}
		end, err := time.Parse("15:04", window.End)
		if err != nil || !end.After(start) {
			return ErrInvalidGovernance
		}
	}
	return nil
}

func validGovernanceStatus(value string) bool {
	return value == GovernanceDraft || value == GovernanceEnabled || value == GovernanceDisabled || value == GovernanceArchived
}

func ValidGovernanceTransition(from, to string) bool {
	switch from {
	case GovernanceDraft:
		return to == GovernanceEnabled
	case GovernanceEnabled:
		return to == GovernanceDisabled || to == GovernanceArchived
	case GovernanceDisabled:
		return to == GovernanceEnabled || to == GovernanceArchived
	case GovernanceArchived:
		return to == GovernanceDraft
	default:
		return false
	}
}
