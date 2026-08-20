package remoteaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrInvalidRequest      = errors.New("invalid remote access request")
	ErrVersionConflict     = errors.New("remote access version conflict")
	ErrApprovalNotEligible = errors.New("REMOTE_ACCESS_APPROVAL_NOT_ELIGIBLE")
	ErrAuthorizationStale  = errors.New("AUTHORIZATION_VERSION_STALE")
	ErrSessionUnavailable  = errors.New("REMOTE_ACCESS_CONNECTION_LOST")
)

type Actor struct {
	EnterpriseID         uuid.UUID
	UserID               uuid.UUID
	DepartmentID         uuid.UUID
	HTTPSessionID        uuid.UUID
	AuthorizationVersion int64
	DataScopeIDs         []uuid.UUID
	StepUpAuthenticated  bool
}

type GrantInput struct {
	SubjectType       string          `json:"subject_type"`
	SubjectID         uuid.UUID       `json:"subject_id"`
	HostIDs           []uuid.UUID     `json:"host_ids"`
	HostSelector      json.RawMessage `json:"host_selector,omitempty"`
	ManagedAccountIDs []uuid.UUID     `json:"managed_account_ids"`
	Protocols         []string        `json:"protocols"`
	Actions           []string        `json:"actions"`
	ValidFrom         time.Time       `json:"valid_from"`
	ValidUntil        time.Time       `json:"valid_until"`
	Enabled           bool            `json:"enabled"`
	ExpectedVersion   int64           `json:"expected_version,omitempty"`
}

type PolicyInput struct {
	Name               string          `json:"name"`
	Enabled            bool            `json:"enabled"`
	Priority           int32           `json:"priority"`
	Protocols          []string        `json:"protocols"`
	HostSelector       json.RawMessage `json:"host_selector,omitempty"`
	ApproverRoleIDs    []uuid.UUID     `json:"approver_role_ids"`
	MinimumApprovals   int32           `json:"minimum_approvals"`
	SeparationOfDuties bool            `json:"separation_of_duties"`
	RequireMFA         bool            `json:"require_mfa"`
	MaxSessionSeconds  int32           `json:"max_session_seconds"`
	IdleTimeoutSeconds int32           `json:"idle_timeout_seconds"`
	ExpectedVersion    int64           `json:"expected_version,omitempty"`
}

type RequestInput struct {
	HostID           uuid.UUID `json:"host_id"`
	ManagedAccountID uuid.UUID `json:"managed_account_id"`
	Protocol         string    `json:"protocol"`
	Action           string    `json:"action"`
	Reason           string    `json:"reason"`
}

type DecisionInput struct {
	RequirementID uuid.UUID `json:"requirement_id"`
	Decision      string    `json:"decision"`
	Comment       string    `json:"comment,omitempty"`
}

type RequestView struct {
	Request      db.RemoteAccessRequest
	Requirements []RequirementView
	Decisions    []db.RemoteAccessDecision
}

type RequirementView struct {
	Requirement   db.RemoteAccessRequirementSnapshot
	ApprovedCount int32
}

type TicketResult struct {
	SessionID uuid.UUID
	Ticket    string
	ExpiresAt time.Time
}

type SessionView struct {
	Session     db.RemoteAccessSession
	RecordingID uuid.UUID
}

type RecordingEventPage struct {
	Recording db.RemoteAccessRecording
	Events    []RecordingEvent
	Next      int64
	Complete  bool
}

type Service struct {
	Store           *postgres.Store
	Idempotency     postgres.Idempotency
	Access          resource.AccessService
	Keyring         secret.Keyring
	ObjectStore     ObjectStore
	UserLimit       int
	HostLimit       int
	EnterpriseLimit int
	Now             func() time.Time
}

func (service Service) ListGrants(ctx context.Context, enterpriseID uuid.UUID, limit int32) ([]db.RemoteAccessGrant, error) {
	return service.Store.Queries.ListRemoteAccessGrants(ctx, db.ListRemoteAccessGrantsParams{EnterpriseID: enterpriseID, Limit: boundedLimit(limit)})
}

func (service Service) GetGrant(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessGrant, error) {
	return service.Store.Queries.GetRemoteAccessGrant(ctx, db.GetRemoteAccessGrantParams{ID: id, EnterpriseID: enterpriseID})
}

func (service Service) CreateGrant(ctx context.Context, actor Actor, input GrantInput, idempotencyKey string) (db.RemoteAccessGrant, error) {
	normalized, hash, err := normalizeOptionalSelector(input.HostSelector)
	if err != nil || !validGrantInput(input, normalized) {
		return db.RemoteAccessGrant{}, ErrInvalidRequest
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.grant.create", idempotencyKey, input, 201, func(q *db.Queries) (db.RemoteAccessGrant, error) {
		value, err := q.CreateRemoteAccessGrant(ctx, db.CreateRemoteAccessGrantParams{ID: newID(), EnterpriseID: actor.EnterpriseID,
			SubjectType: input.SubjectType, SubjectID: input.SubjectID, HostIds: input.HostIDs, HostSelector: normalized, HostSelectorHash: hash,
			ManagedAccountIds: input.ManagedAccountIDs, Protocols: input.Protocols, Actions: input.Actions,
			ValidFrom: timestamp(input.ValidFrom), ValidUntil: timestamp(input.ValidUntil), Enabled: input.Enabled, CreatedBy: actor.UserID})
		if err != nil {
			return db.RemoteAccessGrant{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant.create", "remote_access_grant", value.ID, "created")
	})
}

func (service Service) UpdateGrant(ctx context.Context, actor Actor, id uuid.UUID, input GrantInput) (db.RemoteAccessGrant, error) {
	normalized, hash, err := normalizeOptionalSelector(input.HostSelector)
	if err != nil || input.ExpectedVersion < 1 || !validGrantInput(input, normalized) {
		return db.RemoteAccessGrant{}, ErrInvalidRequest
	}
	var value db.RemoteAccessGrant
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		var updateErr error
		value, updateErr = q.UpdateRemoteAccessGrant(ctx, db.UpdateRemoteAccessGrantParams{ID: id, EnterpriseID: actor.EnterpriseID,
			SubjectType: input.SubjectType, SubjectID: input.SubjectID, HostIds: input.HostIDs, HostSelector: normalized, HostSelectorHash: hash,
			ManagedAccountIds: input.ManagedAccountIDs, Protocols: input.Protocols, Actions: input.Actions,
			ValidFrom: timestamp(input.ValidFrom), ValidUntil: timestamp(input.ValidUntil), Enabled: input.Enabled, Version: input.ExpectedVersion})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if updateErr != nil {
			return updateErr
		}
		if err := service.invalidateGrant(ctx, q, actor.EnterpriseID, id, "grant_changed"); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant.update", "remote_access_grant", id, "updated")
	})
	return value, err
}

func (service Service) DisableGrant(ctx context.Context, actor Actor, id uuid.UUID) error {
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.DisableRemoteAccessGrant(ctx, db.DisableRemoteAccessGrantParams{ID: id, EnterpriseID: actor.EnterpriseID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrVersionConflict
			}
			return err
		}
		if err := service.invalidateGrant(ctx, q, actor.EnterpriseID, id, "grant_disabled"); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant.disable", "remote_access_grant", id, "disabled")
	})
}

func (service Service) ListPolicies(ctx context.Context, enterpriseID uuid.UUID) ([]db.RemoteAccessPolicy, error) {
	return service.Store.Queries.ListRemoteAccessPolicies(ctx, enterpriseID)
}

func (service Service) GetPolicy(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessPolicy, error) {
	return service.Store.Queries.GetRemoteAccessPolicy(ctx, db.GetRemoteAccessPolicyParams{ID: id, EnterpriseID: enterpriseID})
}

func (service Service) CreatePolicy(ctx context.Context, actor Actor, input PolicyInput, idempotencyKey string) (db.RemoteAccessPolicy, error) {
	normalized, hash, err := normalizeOptionalSelector(input.HostSelector)
	if err != nil || !validPolicyInput(input) {
		return db.RemoteAccessPolicy{}, ErrInvalidRequest
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.policy.create", idempotencyKey, input, 201, func(q *db.Queries) (db.RemoteAccessPolicy, error) {
		value, err := q.CreateRemoteAccessPolicy(ctx, db.CreateRemoteAccessPolicyParams{ID: newID(), EnterpriseID: actor.EnterpriseID, Name: input.Name,
			Enabled: input.Enabled, Priority: input.Priority, Protocols: input.Protocols, HostSelector: normalized, HostSelectorHash: hash,
			ApproverRoleIds: input.ApproverRoleIDs, MinimumApprovals: input.MinimumApprovals, SeparationOfDuties: input.SeparationOfDuties,
			RequireMfa: input.RequireMFA, MaxSessionSeconds: input.MaxSessionSeconds, IdleTimeoutSeconds: input.IdleTimeoutSeconds, CreatedBy: actor.UserID})
		if err != nil {
			return db.RemoteAccessPolicy{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.policy.create", "remote_access_policy", value.ID, "created")
	})
}

func (service Service) UpdatePolicy(ctx context.Context, actor Actor, id uuid.UUID, input PolicyInput) (db.RemoteAccessPolicy, error) {
	normalized, hash, err := normalizeOptionalSelector(input.HostSelector)
	if err != nil || input.ExpectedVersion < 1 || !validPolicyInput(input) {
		return db.RemoteAccessPolicy{}, ErrInvalidRequest
	}
	var value db.RemoteAccessPolicy
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		var updateErr error
		value, updateErr = q.UpdateRemoteAccessPolicy(ctx, db.UpdateRemoteAccessPolicyParams{ID: id, EnterpriseID: actor.EnterpriseID, Name: input.Name,
			Enabled: input.Enabled, Priority: input.Priority, Protocols: input.Protocols, HostSelector: normalized, HostSelectorHash: hash,
			ApproverRoleIds: input.ApproverRoleIDs, MinimumApprovals: input.MinimumApprovals, SeparationOfDuties: input.SeparationOfDuties,
			RequireMfa: input.RequireMFA, MaxSessionSeconds: input.MaxSessionSeconds, IdleTimeoutSeconds: input.IdleTimeoutSeconds, Version: input.ExpectedVersion})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if updateErr != nil {
			return updateErr
		}
		if err := service.invalidateEnterprise(ctx, q, actor.EnterpriseID, "policy_changed"); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.policy.update", "remote_access_policy", id, "updated")
	})
	return value, err
}

func (service Service) DisablePolicy(ctx context.Context, actor Actor, id uuid.UUID) error {
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.DisableRemoteAccessPolicy(ctx, db.DisableRemoteAccessPolicyParams{ID: id, EnterpriseID: actor.EnterpriseID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrVersionConflict
			}
			return err
		}
		if err := service.invalidateEnterprise(ctx, q, actor.EnterpriseID, "policy_disabled"); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.policy.disable", "remote_access_policy", id, "disabled")
	})
}

func (service Service) CreateRequest(ctx context.Context, actor Actor, input RequestInput, idempotencyKey string) (RequestView, error) {
	if !actor.StepUpAuthenticated {
		return RequestView{}, ErrMFARequired
	}
	if actor.UserID == uuid.Nil || input.HostID == uuid.Nil || input.ManagedAccountID == uuid.Nil || !validProtocol(input.Protocol) || input.Action != "terminal" || strings.TrimSpace(input.Reason) == "" {
		return RequestView{}, ErrInvalidRequest
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.request.create", idempotencyKey, input, 201, func(q *db.Queries) (RequestView, error) {
		host, err := q.GetHost(ctx, db.GetHostParams{ID: input.HostID, EnterpriseID: actor.EnterpriseID})
		if err != nil || host.Status != "active" {
			return RequestView{}, ErrScopeDenied
		}
		labels, err := resource.DecodeLabels(host.Labels)
		if err != nil {
			return RequestView{}, err
		}
		allowed, _, err := service.Access.CanAccess(ctx, actor.EnterpriseID, actor.DataScopeIDs, "host", host.ID.String(), labels)
		if err != nil || !allowed {
			return RequestView{}, ErrScopeDenied
		}
		account, err := q.GetManagedAccount(ctx, db.GetManagedAccountParams{ID: input.ManagedAccountID, EnterpriseID: actor.EnterpriseID})
		if err != nil || account.Status != "active" || account.HostID != host.ID || !slices.Contains(account.AllowedProtocols, accountProtocol(input.Protocol)) {
			return RequestView{}, ErrScopeDenied
		}
		grants, err := q.ListCandidateRemoteAccessGrants(ctx, db.ListCandidateRemoteAccessGrantsParams{EnterpriseID: actor.EnterpriseID, ActorID: actor.UserID})
		if err != nil {
			return RequestView{}, err
		}
		grant, ok := selectGrant(grants, input, labels, actor.EnterpriseID)
		if !ok {
			return RequestView{}, ErrGrantRequired
		}
		policies, err := q.ListRemoteAccessPolicies(ctx, actor.EnterpriseID)
		if err != nil {
			return RequestView{}, err
		}
		matched, err := matchPolicyRecords(policies, actor.EnterpriseID, input.HostID, labels, input.Protocol, actor.StepUpAuthenticated)
		if err != nil {
			return RequestView{}, err
		}
		now := service.now()
		expires := now.Add(LeaseTTL)
		if grant.ValidUntil.Valid && grant.ValidUntil.Time.Before(expires) {
			expires = grant.ValidUntil.Time
		}
		status := "authorized"
		if len(matched) > 0 {
			status = "awaiting_approval"
		}
		request, err := q.CreateRemoteAccessRequest(ctx, db.CreateRemoteAccessRequestParams{ID: newID(), EnterpriseID: actor.EnterpriseID,
			RequesterID: actor.UserID, GrantID: grant.ID, HostID: input.HostID, ManagedAccountID: input.ManagedAccountID,
			Protocol: input.Protocol, Action: input.Action, Reason: strings.TrimSpace(input.Reason), Status: status,
			AuthorizationVersion: actor.AuthorizationVersion, ExpiresAt: timestamp(expires)})
		if err != nil {
			return RequestView{}, err
		}
		view := RequestView{Request: request}
		for _, policy := range matched {
			requirement, err := q.CreateRemoteAccessRequirement(ctx, db.CreateRemoteAccessRequirementParams{ID: newID(), RequestID: request.ID,
				PolicyID: policy.ID, PolicyVersion: policy.Version, ApproverRoleIds: policy.ApproverRoleIds, MinimumApprovals: policy.MinimumApprovals,
				SeparationOfDuties: policy.SeparationOfDuties, RequireMfa: policy.RequireMfa, MaxSessionSeconds: policy.MaxSessionSeconds,
				IdleTimeoutSeconds: policy.IdleTimeoutSeconds})
			if err != nil {
				return RequestView{}, err
			}
			view.Requirements = append(view.Requirements, RequirementView{Requirement: requirement})
		}
		if len(matched) == 0 {
			if _, err := service.issueLease(ctx, q, request, nil); err != nil {
				return RequestView{}, err
			}
		}
		if err := appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.request.create", "remote_access_request", request.ID, status); err != nil {
			return RequestView{}, err
		}
		return view, nil
	})
}

func (service Service) ListRequests(ctx context.Context, actor Actor, all bool, limit int32) ([]RequestView, error) {
	rows, err := service.Store.Queries.ListRemoteAccessRequests(ctx, db.ListRemoteAccessRequestsParams{EnterpriseID: actor.EnterpriseID, RequesterID: actor.UserID, Column3: all, Limit: boundedLimit(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]RequestView, 0, len(rows))
	for _, row := range rows {
		view, err := service.loadRequest(ctx, service.Store.Queries, row)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (service Service) GetRequest(ctx context.Context, actor Actor, id uuid.UUID, all bool) (RequestView, error) {
	request, err := service.Store.Queries.GetRemoteAccessRequest(ctx, db.GetRemoteAccessRequestParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if err != nil {
		return RequestView{}, err
	}
	if request.RequesterID != actor.UserID && !all {
		return RequestView{}, pgx.ErrNoRows
	}
	return service.loadRequest(ctx, service.Store.Queries, request)
}

func (service Service) DecideRequest(ctx context.Context, actor Actor, requestID uuid.UUID, input DecisionInput, idempotencyKey string) (RequestView, error) {
	if input.RequirementID == uuid.Nil || (input.Decision != "approve" && input.Decision != "reject") {
		return RequestView{}, ErrInvalidRequest
	}
	requestKey := struct {
		RequestID uuid.UUID     `json:"request_id"`
		Input     DecisionInput `json:"input"`
	}{requestID, input}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.request.decide", idempotencyKey, requestKey, 200, func(q *db.Queries) (RequestView, error) {
		request, err := q.GetRemoteAccessRequestForUpdate(ctx, db.GetRemoteAccessRequestForUpdateParams{ID: requestID, EnterpriseID: actor.EnterpriseID})
		if err != nil || request.Status != "awaiting_approval" || !service.now().Before(request.ExpiresAt.Time) {
			return RequestView{}, ErrApprovalRequired
		}
		eligible, err := q.IsRemoteAccessApproverEligible(ctx, db.IsRemoteAccessApproverEligibleParams{RequirementID: input.RequirementID, RequestID: requestID, ApproverID: actor.UserID})
		if err != nil || !eligible {
			return RequestView{}, ErrApprovalNotEligible
		}
		if _, err := q.CreateRemoteAccessDecision(ctx, db.CreateRemoteAccessDecisionParams{ID: newID(), RequestID: requestID, RequirementID: input.RequirementID,
			Decision: input.Decision, Comment: input.Comment, DecidedBy: actor.UserID}); err != nil {
			return RequestView{}, err
		}
		if input.Decision == "reject" {
			if _, err := q.UpdateRemoteAccessRequirementStatus(ctx, db.UpdateRemoteAccessRequirementStatusParams{ID: input.RequirementID, Status: "rejected"}); err != nil {
				return RequestView{}, err
			}
			request, err = q.UpdateRemoteAccessRequestStatus(ctx, db.UpdateRemoteAccessRequestStatusParams{ID: requestID, EnterpriseID: actor.EnterpriseID, Status: "rejected", Status_2: "awaiting_approval"})
			if err != nil {
				return RequestView{}, err
			}
		} else {
			count, err := q.CountRemoteAccessApprovals(ctx, input.RequirementID)
			if err != nil {
				return RequestView{}, err
			}
			requirements, err := q.ListRemoteAccessRequirements(ctx, requestID)
			if err != nil {
				return RequestView{}, err
			}
			for i := range requirements {
				if requirements[i].ID == input.RequirementID && count >= requirements[i].MinimumApprovals {
					updated, updateErr := q.UpdateRemoteAccessRequirementStatus(ctx, db.UpdateRemoteAccessRequirementStatusParams{ID: input.RequirementID, Status: "satisfied"})
					if updateErr != nil {
						return RequestView{}, updateErr
					}
					requirements[i] = updated
				}
			}
			allSatisfied := len(requirements) > 0
			for _, requirement := range requirements {
				allSatisfied = allSatisfied && requirement.Status == "satisfied"
			}
			if allSatisfied {
				request, err = q.UpdateRemoteAccessRequestStatus(ctx, db.UpdateRemoteAccessRequestStatusParams{ID: requestID, EnterpriseID: actor.EnterpriseID, Status: "authorized", Status_2: "awaiting_approval"})
				if err != nil {
					return RequestView{}, err
				}
				if _, err := service.issueLease(ctx, q, request, requirements); err != nil {
					return RequestView{}, err
				}
			}
		}
		if err := appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.request.decide", "remote_access_request", requestID, input.Decision); err != nil {
			return RequestView{}, err
		}
		return service.loadRequest(ctx, q, request)
	})
}

func (service Service) ListLeases(ctx context.Context, actor Actor, all bool, limit int32) ([]db.RemoteAccessLease, error) {
	return service.Store.Queries.ListRemoteAccessLeases(ctx, db.ListRemoteAccessLeasesParams{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID, Column3: all, Limit: boundedLimit(limit)})
}

func (service Service) RevokeLease(ctx context.Context, actor Actor, id uuid.UUID, reason string) (db.RemoteAccessLease, error) {
	var result db.RemoteAccessLease
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var err error
		result, err = q.RevokeRemoteAccessLease(ctx, db.RevokeRemoteAccessLeaseParams{ID: id, EnterpriseID: actor.EnterpriseID, RevokeReason: text(reason)})
		if err != nil {
			return err
		}
		sessions, err := q.TerminateRemoteAccessSessionsByLease(ctx, db.TerminateRemoteAccessSessionsByLeaseParams{LeaseID: id, EnterpriseID: actor.EnterpriseID, Reason: text("lease_revoked")})
		if err != nil {
			return err
		}
		if err := service.publishTerminations(ctx, q, sessions, "lease_revoked"); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.lease.revoke", "remote_access_lease", id, "revoked")
	})
	return result, err
}

func (service Service) CreateSession(ctx context.Context, actor Actor, leaseID uuid.UUID) (SessionView, error) {
	var result SessionView
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		lease, err := q.GetRemoteAccessLeaseForSession(ctx, db.GetRemoteAccessLeaseForSessionParams{ID: leaseID, EnterpriseID: actor.EnterpriseID, UserID: actor.UserID})
		if err != nil {
			return ErrLeaseExpired
		}
		if lease.AuthorizationVersion != actor.AuthorizationVersion {
			return ErrAuthorizationStale
		}
		host, err := q.GetHost(ctx, db.GetHostParams{ID: lease.HostID, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return ErrScopeDenied
		}
		labels, err := resource.DecodeLabels(host.Labels)
		if err != nil {
			return err
		}
		allowed, _, err := service.Access.CanAccess(ctx, actor.EnterpriseID, actor.DataScopeIDs, "host", host.ID.String(), labels)
		if err != nil || !allowed {
			return ErrScopeDenied
		}
		capacity, err := q.CountRemoteAccessCapacity(ctx, db.CountRemoteAccessCapacityParams{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID, HostID: lease.HostID})
		if err != nil {
			return err
		}
		if err := (Capacity{UserActive: int(capacity.UserActive), HostActive: int(capacity.HostActive), EnterpriseActive: int(capacity.EnterpriseActive),
			UserLimit: service.UserLimit, HostLimit: service.HostLimit, EnterpriseLimit: service.EnterpriseLimit}).Check(); err != nil {
			return err
		}
		maxDuration, idleTimeout, err := service.sessionLimits(ctx, q, lease.RequestID)
		if err != nil {
			return err
		}
		if !connectionModeSupports(lease.ConnectionMode, lease.Protocol) {
			return ErrScopeDenied
		}
		session, err := q.CreateRemoteAccessSession(ctx, db.CreateRemoteAccessSessionParams{ID: newID(), EnterpriseID: actor.EnterpriseID, UserID: actor.UserID,
			HttpSessionID: actor.HTTPSessionID, LeaseID: lease.ID, HostID: lease.HostID, ManagedAccountID: lease.ManagedAccountID,
			Protocol: lease.Protocol, ConnectionMode: lease.ConnectionMode, ConnectorID: lease.ConnectorID, AuthorizationVersion: actor.AuthorizationVersion,
			IdleTimeoutSeconds: int32(idleTimeout / time.Second), MaxDurationSeconds: int32(maxDuration / time.Second), ConnectBefore: timestamp(service.now().Add(ConnectionWindow))})
		if err != nil {
			return err
		}
		recordingID := newID()
		dek := make([]byte, 32)
		if _, err := rand.Read(dek); err != nil {
			return err
		}
		envelope, err := service.Keyring.EncryptContext(ctx, dek, recordingKeyAAD(actor.EnterpriseID, recordingID, session.ID))
		clear(dek)
		if err != nil {
			return err
		}
		wrapped, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		if _, err := q.CreateRemoteAccessRecording(ctx, db.CreateRemoteAccessRecordingParams{ID: recordingID, EnterpriseID: actor.EnterpriseID,
			SessionID: session.ID, KeyProvider: envelope.Provider, KeyID: envelope.KeyID, KeyVersion: int32(envelope.KeyVersion), WrappedDek: wrapped}); err != nil {
			return err
		}
		result = SessionView{Session: session, RecordingID: recordingID}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session.create", "remote_access_session", session.ID, "authorized")
	})
	return result, err
}

func (service Service) ListSessions(ctx context.Context, actor Actor, all bool, limit int32) ([]SessionView, error) {
	rows, err := service.Store.Queries.ListRemoteAccessSessions(ctx, db.ListRemoteAccessSessionsParams{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID, Column3: all, Limit: boundedLimit(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		recording, err := service.Store.Queries.GetRemoteAccessRecordingBySession(ctx, db.GetRemoteAccessRecordingBySessionParams{SessionID: row.ID, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return nil, err
		}
		result = append(result, SessionView{Session: row, RecordingID: recording.ID})
	}
	return result, nil
}

func (service Service) GetSession(ctx context.Context, actor Actor, id uuid.UUID, all bool) (SessionView, error) {
	value, err := service.Store.Queries.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if err == nil && value.UserID != actor.UserID && !all {
		return SessionView{}, pgx.ErrNoRows
	}
	if err != nil {
		return SessionView{}, err
	}
	recording, err := service.Store.Queries.GetRemoteAccessRecordingBySession(ctx, db.GetRemoteAccessRecordingBySessionParams{SessionID: id, EnterpriseID: actor.EnterpriseID})
	return SessionView{Session: value, RecordingID: recording.ID}, err
}

func (service Service) TerminateSession(ctx context.Context, actor Actor, id uuid.UUID, reason string, all bool) (SessionView, error) {
	var result SessionView
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil || (current.UserID != actor.UserID && !all) {
			return pgx.ErrNoRows
		}
		session, err := q.TerminateRemoteAccessSession(ctx, db.TerminateRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID, TerminationReason: text(reason)})
		if err != nil {
			return err
		}
		recording, err := q.GetRemoteAccessRecordingBySession(ctx, db.GetRemoteAccessRecordingBySessionParams{SessionID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return err
		}
		result = SessionView{Session: session, RecordingID: recording.ID}
		if err := service.publishTermination(ctx, q, session, reason); err != nil {
			return err
		}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session.terminate", "remote_access_session", id, "terminating")
	})
	return result, err
}

func (service Service) IssueTicket(ctx context.Context, actor Actor, sessionID uuid.UUID) (TicketResult, error) {
	var result TicketResult
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		session, err := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: sessionID, EnterpriseID: actor.EnterpriseID})
		if err != nil || session.UserID != actor.UserID || session.HttpSessionID != actor.HTTPSessionID || session.Status != "authorized" || !service.now().Before(session.ConnectBefore.Time) {
			return ErrSessionUnavailable
		}
		version, err := q.GetCurrentRemoteAccessAuthorizationVersion(ctx, db.GetCurrentRemoteAccessAuthorizationVersionParams{UserID: actor.UserID, EnterpriseID: actor.EnterpriseID})
		if err != nil || version != session.AuthorizationVersion || version != actor.AuthorizationVersion {
			return ErrAuthorizationStale
		}
		issuer := TicketIssuer{Store: ticketStore{queries: q}, Now: service.Now}
		value, binding, err := issuer.Issue(ctx, TicketBinding{SessionID: session.ID, HTTPSessionID: actor.HTTPSessionID, EnterpriseID: actor.EnterpriseID,
			UserID: actor.UserID, HostID: session.HostID, ManagedAccountID: session.ManagedAccountID, LeaseID: session.LeaseID, Protocol: session.Protocol,
			AuthorizationVersion: session.AuthorizationVersion, SessionFence: session.SessionFence})
		if err != nil {
			return err
		}
		result = TicketResult{SessionID: session.ID, Ticket: value, ExpiresAt: binding.ExpiresAt}
		return appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.ticket.issue", "remote_access_session", session.ID, "issued")
	})
	return result, err
}

func (service Service) GetRecording(ctx context.Context, actor Actor, id uuid.UUID, all bool) (db.RemoteAccessRecording, error) {
	recording, err := service.Store.Queries.GetRemoteAccessRecording(ctx, db.GetRemoteAccessRecordingParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if err != nil {
		return db.RemoteAccessRecording{}, err
	}
	session, err := service.Store.Queries.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: recording.SessionID, EnterpriseID: actor.EnterpriseID})
	if err != nil || (session.UserID != actor.UserID && !all) {
		return db.RemoteAccessRecording{}, pgx.ErrNoRows
	}
	return recording, nil
}

func (service Service) ListRecordingChunks(ctx context.Context, actor Actor, id uuid.UUID, after int64, limit int32, all bool) (db.RemoteAccessRecording, []db.RemoteAccessRecordingChunk, error) {
	recording, err := service.GetRecording(ctx, actor, id, all)
	if err != nil {
		return db.RemoteAccessRecording{}, nil, err
	}
	chunks, err := service.Store.Queries.ListRemoteAccessRecordingChunks(ctx, db.ListRemoteAccessRecordingChunksParams{RecordingID: id, Sequence: after, Limit: boundedLimit(limit)})
	return recording, chunks, err
}

func (service Service) ReadRecordingEvents(ctx context.Context, actor Actor, id uuid.UUID, after int64, all bool) (RecordingEventPage, error) {
	recording, chunks, err := service.ListRecordingChunks(ctx, actor, id, after, 1, all)
	if err != nil {
		return RecordingEventPage{}, err
	}
	if len(chunks) == 0 {
		return RecordingEventPage{Recording: recording, Events: []RecordingEvent{}, Next: after, Complete: recording.Status == "available" || recording.Status == "incomplete"}, nil
	}
	var envelope secret.Envelope
	if json.Unmarshal(recording.WrappedDek, &envelope) != nil {
		return RecordingEventPage{}, ErrRecordingUnavailable
	}
	dek, err := service.Keyring.DecryptContext(ctx, envelope, recordingKeyAAD(recording.EnterpriseID, recording.ID, recording.SessionID))
	if err != nil || len(dek) != 32 {
		return RecordingEventPage{}, ErrRecordingUnavailable
	}
	defer clear(dek)
	chunk := chunks[0]
	ciphertext, err := service.ObjectStore.Get(ctx, chunk.ObjectKey)
	if err != nil || int64(len(ciphertext)) != chunk.CiphertextSize {
		return RecordingEventPage{}, ErrRecordingUnavailable
	}
	var previous, expected [32]byte
	if len(chunk.PreviousHash) != len(previous) || len(chunk.ChunkHash) != len(expected) {
		return RecordingEventPage{}, ErrRecordingUnavailable
	}
	copy(previous[:], chunk.PreviousHash)
	copy(expected[:], chunk.ChunkHash)
	events, err := DecryptChunk(recording.ID.String(), dek, chunk.Nonce, ciphertext, uint64(chunk.Sequence), previous, expected)
	if err != nil {
		return RecordingEventPage{}, err
	}
	complete := chunk.Sequence >= int64(recording.ChunkCount) && recording.Status != "recording"
	return RecordingEventPage{Recording: recording, Events: events, Next: chunk.Sequence, Complete: complete}, nil
}

func (service Service) loadRequest(ctx context.Context, q *db.Queries, request db.RemoteAccessRequest) (RequestView, error) {
	requirements, err := q.ListRemoteAccessRequirements(ctx, request.ID)
	if err != nil {
		return RequestView{}, err
	}
	decisions, err := q.ListRemoteAccessDecisions(ctx, request.ID)
	if err != nil {
		return RequestView{}, err
	}
	view := RequestView{Request: request, Decisions: decisions}
	for _, requirement := range requirements {
		count, err := q.CountRemoteAccessApprovals(ctx, requirement.ID)
		if err != nil {
			return RequestView{}, err
		}
		view.Requirements = append(view.Requirements, RequirementView{Requirement: requirement, ApprovedCount: count})
	}
	return view, nil
}

func (service Service) issueLease(ctx context.Context, q *db.Queries, request db.RemoteAccessRequest, requirements []db.RemoteAccessRequirementSnapshot) (db.RemoteAccessLease, error) {
	if request.AuthorizationVersion < 1 || !service.now().Before(request.ExpiresAt.Time) {
		return db.RemoteAccessLease{}, ErrAuthorizationStale
	}
	hash := sha256.Sum256([]byte("no_remote_access_policy"))
	if len(requirements) > 0 {
		encoded, err := json.Marshal(requirements)
		if err != nil {
			return db.RemoteAccessLease{}, err
		}
		hash = sha256.Sum256(encoded)
	}
	expires := service.now().Add(LeaseTTL)
	if request.ExpiresAt.Time.Before(expires) {
		expires = request.ExpiresAt.Time
	}
	return q.CreateRemoteAccessLease(ctx, db.CreateRemoteAccessLeaseParams{ID: newID(), RequestID: request.ID, EnterpriseID: request.EnterpriseID,
		UserID: request.RequesterID, GrantID: request.GrantID, HostID: request.HostID, ManagedAccountID: request.ManagedAccountID,
		Protocol: request.Protocol, Action: request.Action, AuthorizationVersion: request.AuthorizationVersion, PolicySnapshotHash: hash[:], ExpiresAt: timestamp(expires)})
}

func (service Service) sessionLimits(ctx context.Context, q *db.Queries, requestID uuid.UUID) (time.Duration, time.Duration, error) {
	requirements, err := q.ListRemoteAccessRequirements(ctx, requestID)
	if err != nil {
		return 0, 0, err
	}
	maxDuration, idle := DefaultMaxDuration, DefaultIdleTimeout
	for _, requirement := range requirements {
		if requirement.Status != "satisfied" || requirement.RequireMfa {
			return 0, 0, ErrApprovalRequired
		}
		candidateMax := time.Duration(requirement.MaxSessionSeconds) * time.Second
		candidateIdle := time.Duration(requirement.IdleTimeoutSeconds) * time.Second
		if candidateMax > 0 && candidateMax < maxDuration {
			maxDuration = candidateMax
		}
		if candidateIdle > 0 && candidateIdle < idle {
			idle = candidateIdle
		}
	}
	return maxDuration, idle, nil
}

func (service Service) invalidateGrant(ctx context.Context, q *db.Queries, enterpriseID, grantID uuid.UUID, reason string) error {
	if err := q.InvalidateRemoteAccessRequestsByGrant(ctx, db.InvalidateRemoteAccessRequestsByGrantParams{GrantID: grantID, EnterpriseID: enterpriseID}); err != nil {
		return err
	}
	if err := q.RevokeRemoteAccessLeasesByGrant(ctx, db.RevokeRemoteAccessLeasesByGrantParams{GrantID: grantID, EnterpriseID: enterpriseID, Reason: text(reason)}); err != nil {
		return err
	}
	sessions, err := q.TerminateRemoteAccessSessionsByGrant(ctx, db.TerminateRemoteAccessSessionsByGrantParams{GrantID: grantID, EnterpriseID: enterpriseID, Reason: text(reason)})
	if err != nil {
		return err
	}
	return service.publishTerminations(ctx, q, sessions, reason)
}

func (service Service) invalidateEnterprise(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, reason string) error {
	if _, err := q.BumpEnterpriseUsersAuthorizationVersion(ctx, enterpriseID); err != nil {
		return err
	}
	if err := q.InvalidateRemoteAccessRequestsByEnterprise(ctx, enterpriseID); err != nil {
		return err
	}
	if err := q.RevokeRemoteAccessLeasesByEnterprise(ctx, db.RevokeRemoteAccessLeasesByEnterpriseParams{EnterpriseID: enterpriseID, Reason: text(reason)}); err != nil {
		return err
	}
	sessions, err := q.TerminateRemoteAccessSessionsByEnterprise(ctx, db.TerminateRemoteAccessSessionsByEnterpriseParams{EnterpriseID: enterpriseID, Reason: text(reason)})
	if err != nil {
		return err
	}
	return service.publishTerminations(ctx, q, sessions, reason)
}

func (service Service) publishTerminations(ctx context.Context, q *db.Queries, sessions []db.RemoteAccessSession, reason string) error {
	for _, session := range sessions {
		if err := service.publishTermination(ctx, q, session, reason); err != nil {
			return err
		}
	}
	return nil
}

func (service Service) publishTermination(ctx context.Context, q *db.Queries, session db.RemoteAccessSession, reason string) error {
	payload, err := json.Marshal(map[string]any{"session_id": session.ID.String(), "session_fence": session.SessionFence, "reason": reason})
	if err != nil {
		return err
	}
	return q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.session.terminate", AggregateType: "remote_access_session", AggregateID: session.ID.String(), Payload: payload})
}

type ticketStore struct{ queries *db.Queries }

func (store ticketStore) CreateTicket(ctx context.Context, binding TicketBinding, hash [32]byte) error {
	_, err := store.queries.CreateRemoteAccessTicket(ctx, db.CreateRemoteAccessTicketParams{ID: binding.TicketID, SessionID: binding.SessionID,
		TicketHash: hash[:], HttpSessionID: binding.HTTPSessionID, EnterpriseID: binding.EnterpriseID, UserID: binding.UserID, HostID: binding.HostID,
		ManagedAccountID: binding.ManagedAccountID, Protocol: binding.Protocol, LeaseID: binding.LeaseID,
		AuthorizationVersion: binding.AuthorizationVersion, SessionFence: binding.SessionFence, ExpiresAt: timestamp(binding.ExpiresAt)})
	return err
}

func (store ticketStore) ConsumeTicket(ctx context.Context, hash [32]byte, expected TicketBinding, now time.Time) (TicketBinding, error) {
	value, err := store.queries.ConsumeRemoteAccessTicket(ctx, db.ConsumeRemoteAccessTicketParams{TicketHash: hash[:], ExpiresAt: timestamp(now),
		SessionID: expected.SessionID, HttpSessionID: expected.HTTPSessionID, EnterpriseID: expected.EnterpriseID, UserID: expected.UserID,
		HostID: expected.HostID, ManagedAccountID: expected.ManagedAccountID, Protocol: expected.Protocol, LeaseID: expected.LeaseID,
		AuthorizationVersion: expected.AuthorizationVersion, SessionFence: expected.SessionFence})
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketBinding{}, ErrTicketConsumed
	}
	if err != nil {
		return TicketBinding{}, err
	}
	return TicketBinding{TicketID: value.ID, SessionID: value.SessionID, HTTPSessionID: value.HttpSessionID, EnterpriseID: value.EnterpriseID,
		UserID: value.UserID, HostID: value.HostID, ManagedAccountID: value.ManagedAccountID, Protocol: value.Protocol, LeaseID: value.LeaseID,
		AuthorizationVersion: value.AuthorizationVersion, SessionFence: value.SessionFence, ExpiresAt: value.ExpiresAt.Time}, nil
}

func selectGrant(grants []db.RemoteAccessGrant, input RequestInput, labels map[string]string, enterpriseID uuid.UUID) (db.RemoteAccessGrant, bool) {
	for _, grant := range grants {
		if !slices.Contains(grant.ManagedAccountIds, input.ManagedAccountID) || !slices.Contains(grant.Protocols, input.Protocol) || !slices.Contains(grant.Actions, input.Action) {
			continue
		}
		if slices.Contains(grant.HostIds, input.HostID) || selectorMatches(grant.HostSelector, enterpriseID, input.HostID, labels) {
			return grant, true
		}
	}
	return db.RemoteAccessGrant{}, false
}

func matchPolicyRecords(policies []db.RemoteAccessPolicy, enterpriseID, hostID uuid.UUID, labels map[string]string, protocol string, stepUp bool) ([]db.RemoteAccessPolicy, error) {
	result := make([]db.RemoteAccessPolicy, 0, len(policies))
	for _, policy := range policies {
		if !policy.Enabled || !slices.Contains(policy.Protocols, protocol) || (len(policy.HostSelector) > 0 && string(policy.HostSelector) != "{}" && !selectorMatches(policy.HostSelector, enterpriseID, hostID, labels)) {
			continue
		}
		if policy.RequireMfa && !stepUp {
			return nil, ErrMFARequired
		}
		result = append(result, policy)
	}
	return result, nil
}

func selectorMatches(raw []byte, enterpriseID, hostID uuid.UUID, labels map[string]string) bool {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return false
	}
	return authorization.ScopeMatches(authorization.Scope{ID: "remote-access", EnterpriseID: enterpriseID.String(), ResourceTypes: []string{"host"}, LabelSelector: raw, Status: "active"},
		authorization.Resource{EnterpriseID: enterpriseID.String(), Type: "host", ID: hostID.String(), Labels: labels})
}

func normalizeOptionalSelector(raw json.RawMessage) ([]byte, []byte, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		hash := sha256.Sum256([]byte("{}"))
		return []byte("{}"), hash[:], nil
	}
	return authorization.NormalizeSelector(raw)
}

func validGrantInput(input GrantInput, selector []byte) bool {
	return (input.SubjectType == "user" || input.SubjectType == "department") && input.SubjectID != uuid.Nil &&
		(len(input.HostIDs) > 0 || string(selector) != "{}") && len(input.ManagedAccountIDs) > 0 && validProtocols(input.Protocols) &&
		len(input.Actions) == 1 && input.Actions[0] == "terminal" && input.ValidUntil.After(input.ValidFrom)
}

func validPolicyInput(input PolicyInput) bool {
	return strings.TrimSpace(input.Name) != "" && validProtocols(input.Protocols) && len(input.ApproverRoleIDs) > 0 && input.MinimumApprovals >= 1 &&
		input.MinimumApprovals <= 16 && input.MaxSessionSeconds >= 60 && input.MaxSessionSeconds <= 3600 && input.IdleTimeoutSeconds >= 60 && input.IdleTimeoutSeconds <= 900
}

func validProtocols(values []string) bool {
	if len(values) == 0 || len(values) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !validProtocol(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validProtocol(value string) bool { return value == "ssh" || value == "winrs" }

func accountProtocol(protocol string) string {
	if protocol == "winrs" {
		return "winrm"
	}
	return protocol
}

func connectionModeSupports(mode, protocol string) bool {
	if protocol == "ssh" {
		return mode == "via_bastion" || mode == "connector_local" || mode == "direct_ssh"
	}
	return protocol == "winrs" && (mode == "via_bastion" || mode == "connector_local" || mode == "direct_winrm")
}

func appendAudit(ctx context.Context, q *db.Queries, actorID, enterpriseID uuid.UUID, actionName, resourceType string, resourceID uuid.UUID, status string) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: "enterprise_user",
		ActorID: actorID.String(), Action: actionName, ResourceType: resourceType, ResourceID: resourceID.String(), Result: "success", Details: map[string]any{"status": status}})
	return err
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func newID() uuid.UUID {
	value, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("generate UUIDv7: %v", err))
	}
	return value
}

func boundedLimit(value int32) int32 {
	if value <= 0 || value > 200 {
		return 100
	}
	return value
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func recordingKeyAAD(enterpriseID, recordingID, sessionID uuid.UUID) []byte {
	return []byte("argus.remote_access_recording_dek/v1\x00" + enterpriseID.String() + "\x00" + recordingID.String() + "\x00" + sessionID.String())
}
