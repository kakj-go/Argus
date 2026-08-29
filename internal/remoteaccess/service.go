package remoteaccess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/remoteaccess/revocation"
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
	ErrInvalidTransition   = errors.New("REMOTE_ACCESS_INVALID_STATE_TRANSITION")
)

type Actor struct {
	EnterpriseID          uuid.UUID
	UserID                uuid.UUID
	DepartmentID          uuid.UUID
	HTTPSessionID         uuid.UUID
	AuthorizationVersion  int64
	AuthorizedResourceIDs []uuid.UUID
	StepUpAuthenticated   bool
	SourceIP              netip.Addr
}

type GrantInput struct {
	SubjectType       string      `json:"subject_type"`
	SubjectID         uuid.UUID   `json:"subject_id"`
	HostIDs           []uuid.UUID `json:"host_ids"`
	ManagedAccountIDs []uuid.UUID `json:"managed_account_ids"`
	Protocols         []string    `json:"protocols"`
	Actions           []string    `json:"actions"`
	ValidFrom         time.Time   `json:"valid_from"`
	ValidUntil        time.Time   `json:"valid_until"`
	Status            string      `json:"status"`
	ExpectedVersion   int64       `json:"expected_version,omitempty"`
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

type createRequestResult struct {
	View              RequestView `json:"view"`
	DeniedReasonCodes []string    `json:"denied_reason_codes,omitempty"`
}

type RecordingEventPage struct {
	Recording db.RemoteAccessRecording
	Events    []RecordingEvent
	Next      int64
	Complete  bool
}

type RequestListFilter struct {
	Scope                  string
	Status, Protocol       string
	CreatedBy, HostID      uuid.UUID
	CreatedFrom, CreatedTo *time.Time
}

type SessionListFilter struct {
	Scope                            string
	Status, Protocol, ConnectionMode string
	UserID, HostID, ManagedAccountID uuid.UUID
	CreatedFrom, CreatedTo           *time.Time
}

type RecordingListFilter struct {
	Status                    string
	SessionID, UserID, HostID uuid.UUID
	CreatedFrom, CreatedTo    *time.Time
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

func (service Service) ListGrants(ctx context.Context, enterpriseID uuid.UUID) ([]db.RemoteAccessGrant, error) {
	return service.Store.Queries.ListRemoteAccessGrants(ctx, enterpriseID)
}

func (service Service) GetGrant(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessGrant, error) {
	return service.Store.Queries.GetRemoteAccessGrant(ctx, db.GetRemoteAccessGrantParams{ID: id, EnterpriseID: enterpriseID})
}

func (service Service) CreateGrant(ctx context.Context, actor Actor, input GrantInput, idempotencyKey string) (db.RemoteAccessGrant, error) {
	if !validGrantInput(input) || input.Status != GovernanceDraft {
		return db.RemoteAccessGrant{}, ErrInvalidRequest
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.grant.create", idempotencyKey, input, 201, func(q *db.Queries) (db.RemoteAccessGrant, error) {
		value, err := q.CreateRemoteAccessGrant(ctx, db.CreateRemoteAccessGrantParams{ID: newID(), EnterpriseID: actor.EnterpriseID,
			SubjectType: input.SubjectType, SubjectID: input.SubjectID, HostIds: input.HostIDs,
			ManagedAccountIds: input.ManagedAccountIDs, Protocols: input.Protocols, Actions: input.Actions,
			ValidFrom: timestamp(input.ValidFrom), ValidUntil: timestamp(input.ValidUntil), Status: input.Status, CreatedBy: actor.UserID})
		if err != nil {
			return db.RemoteAccessGrant{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant.create", "remote_access_grant", value.ID, "created")
	})
}

func (service Service) UpdateGrant(ctx context.Context, actor Actor, id uuid.UUID, input GrantInput, idempotencyKey string) (db.RemoteAccessGrant, error) {
	if input.ExpectedVersion < 1 || !validGrantFields(input) {
		return db.RemoteAccessGrant{}, ErrInvalidRequest
	}
	request := struct {
		ID    uuid.UUID  `json:"id"`
		Input GrantInput `json:"input"`
	}{ID: id, Input: input}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.grant.update", idempotencyKey, request, 200, func(q *db.Queries) (db.RemoteAccessGrant, error) {
		current, err := q.GetRemoteAccessGrant(ctx, db.GetRemoteAccessGrantParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return db.RemoteAccessGrant{}, err
		}
		if current.Status == GovernanceArchived {
			return db.RemoteAccessGrant{}, ErrInvalidTransition
		}
		value, updateErr := q.UpdateRemoteAccessGrant(ctx, db.UpdateRemoteAccessGrantParams{ID: id, EnterpriseID: actor.EnterpriseID,
			SubjectType: input.SubjectType, SubjectID: input.SubjectID, HostIds: input.HostIDs,
			ManagedAccountIds: input.ManagedAccountIDs, Protocols: input.Protocols, Actions: input.Actions,
			ValidFrom: timestamp(input.ValidFrom), ValidUntil: timestamp(input.ValidUntil), Version: input.ExpectedVersion})
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return db.RemoteAccessGrant{}, ErrVersionConflict
		}
		if updateErr != nil {
			return db.RemoteAccessGrant{}, updateErr
		}
		if err := service.invalidateGrant(ctx, q, actor.EnterpriseID, id, "grant_changed"); err != nil {
			return db.RemoteAccessGrant{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant.update", "remote_access_grant", id, "updated")
	})
}

func (service Service) TransitionGrant(ctx context.Context, actor Actor, id uuid.UUID, from, to string, expectedVersion int64, key string) (db.RemoteAccessGrant, error) {
	if expectedVersion < 1 || !ValidGovernanceTransition(from, to) {
		return db.RemoteAccessGrant{}, ErrInvalidTransition
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.grant."+to, key,
		map[string]any{"id": id, "from": from, "to": to, "expected_version": expectedVersion}, 200, func(q *db.Queries) (db.RemoteAccessGrant, error) {
			value, err := q.TransitionRemoteAccessGrant(ctx, db.TransitionRemoteAccessGrantParams{ID: id, EnterpriseID: actor.EnterpriseID, Status: to, Status_2: from, Version: expectedVersion})
			if errors.Is(err, pgx.ErrNoRows) {
				return db.RemoteAccessGrant{}, ErrVersionConflict
			}
			if err != nil {
				return db.RemoteAccessGrant{}, err
			}
			if err := service.invalidateGrant(ctx, q, actor.EnterpriseID, id, "grant_"+to); err != nil {
				return db.RemoteAccessGrant{}, err
			}
			return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.grant."+to, "remote_access_grant", id, to)
		})
}

func (service Service) GrantReferences(ctx context.Context, enterpriseID, id uuid.UUID) (db.CountRemoteAccessGrantReferencesRow, error) {
	if _, err := service.GetGrant(ctx, enterpriseID, id); err != nil {
		return db.CountRemoteAccessGrantReferencesRow{}, err
	}
	return service.Store.Queries.CountRemoteAccessGrantReferences(ctx, id)
}

func (service Service) CreateRequest(ctx context.Context, actor Actor, input RequestInput, idempotencyKey string) (RequestView, error) {
	if actor.UserID == uuid.Nil || input.HostID == uuid.Nil || input.ManagedAccountID == uuid.Nil || !validProtocol(input.Protocol) || input.Action != "terminal" || strings.TrimSpace(input.Reason) == "" {
		return RequestView{}, ErrInvalidRequest
	}
	result, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.request.create", idempotencyKey, input, 201, func(q *db.Queries) (createRequestResult, error) {
		evaluated, err := service.evaluateAccess(ctx, q, actor, input, service.now())
		if err != nil {
			return createRequestResult{}, err
		}
		if err := decisionError(evaluated.Decision); err != nil {
			if auditErr := appendDecisionAudit(ctx, q, actor, uuid.Nil, "remote_access.request.evaluate", "denied", evaluated.Decision, evaluated.SnapshotHash); auditErr != nil {
				return createRequestResult{}, auditErr
			}
			return createRequestResult{DeniedReasonCodes: slices.Clone(evaluated.Decision.ReasonCodes)}, nil
		}
		view, err := service.createEvaluatedRequest(ctx, q, actor, input, evaluated)
		return createRequestResult{View: view}, err
	})
	if err != nil {
		return RequestView{}, err
	}
	if len(result.DeniedReasonCodes) > 0 {
		return RequestView{}, decisionError(AccessDecision{Outcome: DecisionDenied, ReasonCodes: result.DeniedReasonCodes})
	}
	return result.View, nil
}

func (service Service) ListRequests(ctx context.Context, actor Actor, filter RequestListFilter) ([]RequestView, error) {
	if filter.Scope == "" {
		filter.Scope = "mine"
	}
	if filter.Scope != "mine" && filter.Scope != "approver" && filter.Scope != "processed" {
		return nil, ErrInvalidRequest
	}
	rows, err := service.Store.Queries.ListRemoteAccessRequests(ctx, db.ListRemoteAccessRequestsParams{
		EnterpriseID: actor.EnterpriseID, ActorID: actor.UserID, DepartmentID: actor.DepartmentID, Scope: filter.Scope,
		Status: optionalText(filter.Status), CreatedBy: optionalUUID(filter.CreatedBy), HostID: optionalUUID(filter.HostID),
		Protocol: optionalText(filter.Protocol), CreatedFrom: optionalTime(filter.CreatedFrom), CreatedTo: optionalTime(filter.CreatedTo),
	})
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

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func optionalUUID(value uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: value, Valid: value != uuid.Nil}
}

func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func (service Service) GetRequest(ctx context.Context, actor Actor, id uuid.UUID, admin bool) (RequestView, error) {
	request, err := service.Store.Queries.GetRemoteAccessRequest(ctx, db.GetRemoteAccessRequestParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if err != nil {
		return RequestView{}, err
	}
	if request.RequesterID != actor.UserID && !admin {
		allowed, accessErr := service.Store.Queries.CanReadRemoteAccessRequestAsApprover(ctx, db.CanReadRemoteAccessRequestAsApproverParams{
			RequestID: id, EnterpriseID: actor.EnterpriseID, ApproverID: actor.UserID,
		})
		if accessErr != nil {
			return RequestView{}, accessErr
		}
		if !allowed {
			return RequestView{}, pgx.ErrNoRows
		}
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
		if err != nil {
			return RequestView{}, err
		}
		currentVersion, err := q.GetCurrentRemoteAccessAuthorizationVersion(ctx, db.GetCurrentRemoteAccessAuthorizationVersionParams{
			UserID: request.RequesterID, EnterpriseID: request.EnterpriseID,
		})
		if err != nil {
			return RequestView{}, ErrAuthorizationStale
		}
		if stateErr := validateApprovalRequestState(request, currentVersion, service.now()); stateErr != nil {
			return RequestView{}, stateErr
		}
		if _, err := DecodeAccessDecision(request.DecisionSnapshot, request.DecisionSnapshotHash); err != nil {
			return RequestView{}, ErrAuthorizationStale
		}
		requirements, err := q.ListRemoteAccessRequirements(ctx, requestID)
		if err != nil {
			return RequestView{}, err
		}
		var target *db.RemoteAccessRequirementSnapshot
		for i := range requirements {
			if requirements[i].ID == input.RequirementID {
				target = &requirements[i]
				break
			}
		}
		if target == nil || target.Status != "pending" || !target.DeadlineAt.Valid || !service.now().Before(target.DeadlineAt.Time) {
			return RequestView{}, ErrApprovalRequired
		}
		eligible, err := q.IsRemoteAccessApproverEligible(ctx, db.IsRemoteAccessApproverEligibleParams{RequirementID: input.RequirementID, RequestID: requestID, ApproverID: actor.UserID})
		if err != nil || !eligible {
			return RequestView{}, ErrApprovalNotEligible
		}
		if _, err := q.CreateRemoteAccessDecision(ctx, db.CreateRemoteAccessDecisionParams{ID: newID(), RequestID: requestID, RequirementID: input.RequirementID,
			Decision: input.Decision, Comment: input.Comment, DecidedBy: actor.UserID}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return RequestView{}, ErrInvalidTransition
			}
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

func validateApprovalRequestState(request db.RemoteAccessRequest, currentAuthorizationVersion int64, now time.Time) error {
	if request.Status != "awaiting_approval" || !request.ExpiresAt.Valid || !now.Before(request.ExpiresAt.Time) {
		return ErrApprovalRequired
	}
	if request.AuthorizationVersion < 1 || currentAuthorizationVersion != request.AuthorizationVersion {
		return ErrAuthorizationStale
	}
	return nil
}

func (service Service) ListLeases(ctx context.Context, actor Actor, all bool, limit int32) ([]db.RemoteAccessLease, error) {
	return service.Store.Queries.ListRemoteAccessLeases(ctx, db.ListRemoteAccessLeasesParams{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID, Column3: all, Limit: boundedLimit(limit)})
}

func (service Service) RevokeLease(ctx context.Context, actor Actor, id uuid.UUID, reason, idempotencyKey string) (db.RemoteAccessLease, error) {
	request := map[string]any{"id": id, "reason": reason}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.lease.revoke", idempotencyKey, request, 200, func(q *db.Queries) (db.RemoteAccessLease, error) {
		result, err := q.RevokeRemoteAccessLease(ctx, db.RevokeRemoteAccessLeaseParams{ID: id, EnterpriseID: actor.EnterpriseID, RevokeReason: text(reason)})
		if err != nil {
			return db.RemoteAccessLease{}, err
		}
		sessions, err := q.TerminateRemoteAccessSessionsByLease(ctx, db.TerminateRemoteAccessSessionsByLeaseParams{LeaseID: id, EnterpriseID: actor.EnterpriseID, Reason: text("lease_revoked")})
		if err != nil {
			return db.RemoteAccessLease{}, err
		}
		if err := service.publishTerminations(ctx, q, sessions, "lease_revoked"); err != nil {
			return db.RemoteAccessLease{}, err
		}
		return result, appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.lease.revoke", "remote_access_lease", id, map[string]any{
			"status": "revoked", "reason_code": reason, "request_id": result.RequestID, "lease_id": result.ID,
			"authorization_version": result.AuthorizationVersion, "snapshot_hash": hex.EncodeToString(result.DecisionSnapshotHash),
			"grant_id": result.GrantID, "host_id": result.HostID, "managed_account_id": result.ManagedAccountID, "protocol": result.Protocol,
		})
	})
}

func (service Service) CreateSession(ctx context.Context, actor Actor, leaseID uuid.UUID, idempotencyKey string) (SessionView, error) {
	result, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.session.create", idempotencyKey, map[string]any{"lease_id": leaseID}, 201, func(q *db.Queries) (SessionView, error) {
		lease, err := q.GetRemoteAccessLeaseForSession(ctx, db.GetRemoteAccessLeaseForSessionParams{ID: leaseID, EnterpriseID: actor.EnterpriseID, UserID: actor.UserID})
		if err != nil {
			return SessionView{}, ErrLeaseExpired
		}
		if lease.AuthorizationVersion != actor.AuthorizationVersion {
			return SessionView{}, ErrAuthorizationStale
		}
		request, err := q.GetRemoteAccessRequestForUpdate(ctx, db.GetRemoteAccessRequestForUpdateParams{ID: lease.RequestID, EnterpriseID: actor.EnterpriseID})
		if err != nil || request.Status != "authorized" || request.RequesterID != actor.UserID || !slices.Equal(request.DecisionSnapshotHash, lease.DecisionSnapshotHash) {
			return SessionView{}, ErrAuthorizationStale
		}
		previousDecision, err := DecodeAccessDecision(lease.DecisionSnapshot, lease.DecisionSnapshotHash)
		if err != nil {
			return SessionView{}, ErrAuthorizationStale
		}
		var profile SessionProfileSnapshot
		if json.Unmarshal(lease.SessionProfileSnapshot, &profile) != nil || profile.MaxSessionSeconds < 60 || profile.IdleTimeoutSeconds < 60 || profile.IdleTimeoutSeconds > profile.MaxSessionSeconds || !reflect.DeepEqual(profile, previousDecision.SessionProfile) {
			return SessionView{}, ErrAuthorizationStale
		}
		current, err := service.evaluateAccess(ctx, q, actor, RequestInput{HostID: lease.HostID, ManagedAccountID: lease.ManagedAccountID, Protocol: lease.Protocol, Action: lease.Action, Reason: request.Reason}, service.now())
		if err != nil || !decisionSourcesEqual(previousDecision, current.Decision) {
			return SessionView{}, ErrAuthorizationStale
		}
		host, err := q.GetHost(ctx, db.GetHostParams{ID: lease.HostID, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return SessionView{}, ErrScopeDenied
		}
		if !service.Access.CanAccess(actor.AuthorizedResourceIDs, host.ID) {
			return SessionView{}, ErrScopeDenied
		}
		capacity, err := q.CountRemoteAccessCapacity(ctx, db.CountRemoteAccessCapacityParams{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID, HostID: lease.HostID})
		if err != nil {
			return SessionView{}, err
		}
		if err := (Capacity{UserActive: int(capacity.UserActive), HostActive: int(capacity.HostActive), EnterpriseActive: int(capacity.EnterpriseActive),
			UserLimit: service.UserLimit, HostLimit: service.HostLimit, EnterpriseLimit: service.EnterpriseLimit}).Check(); err != nil {
			return SessionView{}, err
		}
		if !connectionModeSupports(lease.ConnectionMode, lease.Protocol) {
			return SessionView{}, ErrScopeDenied
		}
		session, err := q.CreateRemoteAccessSession(ctx, db.CreateRemoteAccessSessionParams{ID: newID(), EnterpriseID: actor.EnterpriseID, UserID: actor.UserID,
			HttpSessionID: actor.HTTPSessionID, LeaseID: lease.ID, HostID: lease.HostID, ManagedAccountID: lease.ManagedAccountID,
			Protocol: lease.Protocol, ConnectionMode: lease.ConnectionMode, ConnectorID: lease.ConnectorID, AuthorizationVersion: actor.AuthorizationVersion,
			IdleTimeoutSeconds: int32(profile.IdleTimeoutSeconds), MaxDurationSeconds: int32(profile.MaxSessionSeconds), ConnectBefore: timestamp(service.now().Add(ConnectionWindow)),
			DecisionSnapshot: lease.DecisionSnapshot, SessionProfileSnapshot: lease.SessionProfileSnapshot, DecisionSnapshotHash: lease.DecisionSnapshotHash,
			RecordingMode: profile.RecordingMode, CommandAuditMode: profile.CommandAuditMode, ClipboardMode: profile.ClipboardMode,
			FileUploadMode: profile.FileUploadMode, FileDownloadMode: profile.FileDownloadMode, PortForwardMode: profile.PortForwardMode,
			SessionShareMode: profile.SessionShareMode, RetentionDays: int32(profile.RetentionDays), Reason: request.Reason})
		if err != nil {
			return SessionView{}, err
		}
		recordingID := uuid.Nil
		if profile.RecordingMode == "required" {
			recordingID, err = service.createSessionRecording(ctx, q, actor.EnterpriseID, session.ID)
			if err != nil {
				return SessionView{}, ErrRecordingUnavailable
			}
		}
		result := SessionView{Session: session, RecordingID: recordingID}
		return result, appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session.create", "remote_access_session", session.ID, sessionAuditDetails(session, "authorized"))
	})
	if err == nil && result.Session.RecordingMode == "optional" {
		recordingID, optionalErr := service.ensureOptionalRecording(ctx, actor.EnterpriseID, result.Session.ID)
		if optionalErr == nil {
			result.RecordingID = recordingID
		} else {
			_ = service.recordOptionalRecordingDegradation(ctx, actor, result.Session.ID)
		}
	}
	return result, err
}

func (service Service) ensureOptionalRecording(ctx context.Context, enterpriseID, sessionID uuid.UUID) (uuid.UUID, error) {
	if existing, err := service.recordingIDForSession(ctx, service.Store.Queries, enterpriseID, sessionID); err != nil || existing != uuid.Nil {
		return existing, err
	}
	var recordingID uuid.UUID
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var createErr error
		recordingID, createErr = service.createSessionRecording(ctx, q, enterpriseID, sessionID)
		return createErr
	})
	return recordingID, err
}

func (service Service) ListSessions(ctx context.Context, actor Actor, all bool, filter SessionListFilter) ([]SessionView, error) {
	if filter.Scope == "" {
		filter.Scope = "all"
	}
	if filter.Scope != "active" && filter.Scope != "history" && filter.Scope != "all" {
		return nil, ErrInvalidRequest
	}
	rows, err := service.Store.Queries.ListRemoteAccessSessions(ctx, db.ListRemoteAccessSessionsParams{
		EnterpriseID: actor.EnterpriseID, ActorID: actor.UserID, AllSessions: all, Scope: filter.Scope,
		Status: optionalText(filter.Status), UserID: optionalUUID(filter.UserID), HostID: optionalUUID(filter.HostID),
		ManagedAccountID: optionalUUID(filter.ManagedAccountID), Protocol: optionalText(filter.Protocol),
		ConnectionMode: optionalText(filter.ConnectionMode), CreatedFrom: optionalTime(filter.CreatedFrom), CreatedTo: optionalTime(filter.CreatedTo),
	})
	if err != nil {
		return nil, err
	}
	result := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		recordingID, err := service.recordingIDForSession(ctx, service.Store.Queries, actor.EnterpriseID, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, SessionView{Session: row, RecordingID: recordingID})
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
	recordingID, err := service.recordingIDForSession(ctx, service.Store.Queries, actor.EnterpriseID, id)
	return SessionView{Session: value, RecordingID: recordingID}, err
}

func (service Service) TerminateSession(ctx context.Context, actor Actor, id uuid.UUID, reason string, all bool, idempotencyKey string) (SessionView, error) {
	request := map[string]any{"id": id, "reason": reason, "all": all}
	result, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.session.terminate", idempotencyKey, request, 200, func(q *db.Queries) (SessionView, error) {
		current, err := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return SessionView{}, err
		}
		if current.UserID != actor.UserID && !all {
			return SessionView{}, pgx.ErrNoRows
		}
		if terminalSessionStatus(current.Status) {
			recordingID, recordingErr := service.recordingIDForSession(ctx, q, actor.EnterpriseID, id)
			if recordingErr != nil {
				return SessionView{}, recordingErr
			}
			return SessionView{Session: current, RecordingID: recordingID}, nil
		}
		session, err := q.TerminateRemoteAccessSession(ctx, db.TerminateRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID, TerminationReason: text(reason)})
		if errors.Is(err, pgx.ErrNoRows) {
			latest, latestErr := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID})
			if latestErr == nil && terminalSessionStatus(latest.Status) {
				recordingID, recordingErr := service.recordingIDForSession(ctx, q, actor.EnterpriseID, id)
				if recordingErr != nil {
					return SessionView{}, recordingErr
				}
				return SessionView{Session: latest, RecordingID: recordingID}, nil
			}
		}
		if err != nil {
			return SessionView{}, err
		}
		// A session still in "authorized" never had a WebSocket attach (the
		// ticket is unconsumed), so no Gateway will confirm the termination.
		// Finalize immediately instead of waiting for the two-minute
		// stuck-terminating reconciler convergence.
		if current.Status == "authorized" {
			if finished, finishErr := q.FinishRemoteAccessSession(ctx, db.FinishRemoteAccessSessionParams{
				ID: id, SessionFence: session.SessionFence, Status: "terminated", TerminationReason: text(reason),
			}); finishErr == nil {
				session = finished
			}
		}
		recordingID, err := service.recordingIDForSession(ctx, q, actor.EnterpriseID, id)
		if err != nil {
			return SessionView{}, err
		}
		result := SessionView{Session: session, RecordingID: recordingID}
		if err := service.publishTermination(ctx, q, session, reason); err != nil {
			return SessionView{}, err
		}
		details := sessionAuditDetails(session, "terminating")
		details["reason_code"] = reason
		return result, appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session.terminate", "remote_access_session", id, details)
	})
	if err == nil {
		return result, nil
	}
	// A prior request may have moved the session to a terminal state while its
	// idempotency transaction was interrupted. Treat the durable session state
	// as authoritative so a repeated terminate remains safe and successful.
	latest, latestErr := service.Store.Queries.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if latestErr == nil && (latest.UserID == actor.UserID || all) && terminalSessionStatus(latest.Status) {
		recordingID, _ := service.recordingIDForSession(ctx, service.Store.Queries, actor.EnterpriseID, id)
		return SessionView{Session: latest, RecordingID: recordingID}, nil
	}
	return result, err
}

func terminalSessionStatus(status string) bool {
	switch status {
	case "terminating", "terminated", "failed", "connection_lost", "invalidated", "expired":
		return true
	default:
		return false
	}
}

func (service Service) IssueTicket(ctx context.Context, actor Actor, sessionID uuid.UUID, idempotencyKey string) (TicketResult, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.ticket.issue", idempotencyKey, map[string]any{"session_id": sessionID}, 201, func(q *db.Queries) (TicketResult, error) {
		session, err := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: sessionID, EnterpriseID: actor.EnterpriseID})
		// status='authorized' 是首次接入票据；status='active' 支持浏览器断开后的
		// 重接票据（会话驻留在 Gateway 内，PTY 后端不因页面关闭而终止）。
		attachable := session.Status == "authorized" && service.now().Before(session.ConnectBefore.Time) ||
			session.Status == "active"
		if err != nil || session.UserID != actor.UserID || session.HttpSessionID != actor.HTTPSessionID || !attachable {
			return TicketResult{}, ErrSessionUnavailable
		}
		version, err := q.GetCurrentRemoteAccessAuthorizationVersion(ctx, db.GetCurrentRemoteAccessAuthorizationVersionParams{UserID: actor.UserID, EnterpriseID: actor.EnterpriseID})
		if err != nil || version != session.AuthorizationVersion || version != actor.AuthorizationVersion {
			return TicketResult{}, ErrAuthorizationStale
		}
		issuer := TicketIssuer{Store: ticketStore{queries: q}, Now: service.Now}
		value, binding, err := issuer.Issue(ctx, TicketBinding{SessionID: session.ID, HTTPSessionID: actor.HTTPSessionID, EnterpriseID: actor.EnterpriseID,
			UserID: actor.UserID, HostID: session.HostID, ManagedAccountID: session.ManagedAccountID, LeaseID: session.LeaseID, Protocol: session.Protocol,
			AuthorizationVersion: session.AuthorizationVersion, SessionFence: session.SessionFence})
		if err != nil {
			return TicketResult{}, err
		}
		result := TicketResult{SessionID: session.ID, Ticket: value, ExpiresAt: binding.ExpiresAt}
		return result, appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.ticket.issue", "remote_access_session", session.ID, map[string]any{
			"status": "issued", "remote_session_id": session.ID, "lease_id": session.LeaseID, "authorization_version": session.AuthorizationVersion,
			"session_fence": session.SessionFence, "snapshot_hash": hex.EncodeToString(session.DecisionSnapshotHash),
			"host_id": session.HostID, "managed_account_id": session.ManagedAccountID, "protocol": session.Protocol,
		})
	})
}

func (service Service) GetRecording(ctx context.Context, actor Actor, id uuid.UUID, all bool) (db.RemoteAccessRecording, error) {
	var recording db.RemoteAccessRecording
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var err error
		recording, err = service.getRecording(ctx, q, actor, id, all)
		if err != nil {
			return err
		}
		return appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.recording.read", "remote_access_recording", recording.ID, recordingAuditDetails(recording, "metadata_read"))
	})
	return recording, err
}

func (service Service) ListRecordings(ctx context.Context, actor Actor, filter RecordingListFilter) ([]db.RemoteAccessRecording, error) {
	var result []db.RemoteAccessRecording
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var err error
		result, err = q.ListRemoteAccessRecordings(ctx, db.ListRemoteAccessRecordingsParams{
			EnterpriseID: actor.EnterpriseID, Status: optionalText(filter.Status), SessionID: optionalUUID(filter.SessionID),
			UserID: optionalUUID(filter.UserID), HostID: optionalUUID(filter.HostID),
			CreatedFrom: optionalTime(filter.CreatedFrom), CreatedTo: optionalTime(filter.CreatedTo),
		})
		if err != nil {
			return err
		}
		return appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.recording.read", "remote_access_recording", uuid.Nil, map[string]any{"status": "list", "result_count": len(result)})
	})
	return result, err
}

func (service Service) ListRecordingChunks(ctx context.Context, actor Actor, id uuid.UUID, after int64, limit int32, all bool) (db.RemoteAccessRecording, []db.RemoteAccessRecordingChunk, error) {
	recording, err := service.getRecording(ctx, service.Store.Queries, actor, id, all)
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
		if err := service.Store.InTx(ctx, func(q *db.Queries) error {
			return appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.recording.read", "remote_access_recording", recording.ID, recordingAuditDetails(recording, "events_read"))
		}); err != nil {
			return RecordingEventPage{}, err
		}
		return RecordingEventPage{Recording: recording, Events: []RecordingEvent{}, Next: after, Complete: recording.Status == "available" || recording.Status == "incomplete"}, nil
	}
	envelope, err := recordingEnvelope(recording)
	if err != nil {
		return RecordingEventPage{}, err
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
	if err := service.Store.InTx(ctx, func(q *db.Queries) error {
		details := recordingAuditDetails(recording, "events_read")
		details["chunk_sequence"] = chunk.Sequence
		return appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.recording.read", "remote_access_recording", recording.ID, details)
	}); err != nil {
		return RecordingEventPage{}, err
	}
	complete := chunk.Sequence >= int64(recording.ChunkCount) && recording.Status != "recording"
	return RecordingEventPage{Recording: recording, Events: events, Next: chunk.Sequence, Complete: complete}, nil
}

func (service Service) getRecording(ctx context.Context, q *db.Queries, actor Actor, id uuid.UUID, all bool) (db.RemoteAccessRecording, error) {
	recording, err := q.GetRemoteAccessRecording(ctx, db.GetRemoteAccessRecordingParams{ID: id, EnterpriseID: actor.EnterpriseID})
	if err != nil {
		return db.RemoteAccessRecording{}, err
	}
	session, err := q.GetRemoteAccessSession(ctx, db.GetRemoteAccessSessionParams{ID: recording.SessionID, EnterpriseID: actor.EnterpriseID})
	if err != nil || (session.UserID != actor.UserID && !all) {
		return db.RemoteAccessRecording{}, pgx.ErrNoRows
	}
	return recording, nil
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
	if request.Status != "authorized" || request.AuthorizationVersion < 1 || !service.now().Before(request.ExpiresAt.Time) {
		return db.RemoteAccessLease{}, ErrAuthorizationStale
	}
	for _, requirement := range requirements {
		if requirement.Status != "satisfied" {
			return db.RemoteAccessLease{}, ErrApprovalRequired
		}
	}
	currentVersion, err := q.GetCurrentRemoteAccessAuthorizationVersion(ctx, db.GetCurrentRemoteAccessAuthorizationVersionParams{UserID: request.RequesterID, EnterpriseID: request.EnterpriseID})
	if err != nil || currentVersion != request.AuthorizationVersion {
		return db.RemoteAccessLease{}, ErrAuthorizationStale
	}
	decision, err := DecodeAccessDecision(request.DecisionSnapshot, request.DecisionSnapshotHash)
	if err != nil || decision.AuthorizationVersion != request.AuthorizationVersion || decision.Outcome == DecisionDenied || decision.Outcome == DecisionAwaitingMFA {
		return db.RemoteAccessLease{}, ErrAuthorizationStale
	}
	profileJSON, err := json.Marshal(decision.SessionProfile)
	if err != nil {
		return db.RemoteAccessLease{}, err
	}
	expires := service.now().Add(LeaseTTL)
	if request.ExpiresAt.Time.Before(expires) {
		expires = request.ExpiresAt.Time
	}
	lease, err := q.CreateRemoteAccessLease(ctx, db.CreateRemoteAccessLeaseParams{ID: newID(), RequestID: request.ID, EnterpriseID: request.EnterpriseID,
		UserID: request.RequesterID, GrantID: request.GrantID, HostID: request.HostID, ManagedAccountID: request.ManagedAccountID,
		Protocol: request.Protocol, Action: request.Action, AuthorizationVersion: request.AuthorizationVersion, ExpiresAt: timestamp(expires),
		DecisionSnapshot: request.DecisionSnapshot, SessionProfileSnapshot: profileJSON, DecisionSnapshotHash: request.DecisionSnapshotHash})
	if err != nil {
		return db.RemoteAccessLease{}, err
	}
	if err := appendAuditDetails(ctx, q, request.RequesterID, request.EnterpriseID, "remote_access.lease.issue", "remote_access_lease", lease.ID, map[string]any{
		"status": "issued", "request_id": request.ID, "lease_id": lease.ID, "authorization_version": lease.AuthorizationVersion,
		"snapshot_hash": hex.EncodeToString(lease.DecisionSnapshotHash), "matched_grants": decision.MatchedGrantSnapshots,
		"matched_rules": decision.MatchedRuleSnapshots, "approval_workflows": decision.ApprovalRequirements,
		"session_profile_sources": decision.SessionProfile.SourceProfiles, "grant_id": lease.GrantID,
		"host_id": lease.HostID, "managed_account_id": lease.ManagedAccountID, "protocol": lease.Protocol,
	}); err != nil {
		return db.RemoteAccessLease{}, err
	}
	return lease, nil
}

func (service Service) invalidateGrant(ctx context.Context, q *db.Queries, enterpriseID, grantID uuid.UUID, reason string) error {
	return revocation.Source(ctx, q, enterpriseID, "grant", grantID, reason)
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

func (service Service) invalidateGovernanceSource(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, sourceType string, sourceID uuid.UUID, reason string) error {
	return revocation.Source(ctx, q, enterpriseID, sourceType, sourceID, reason)
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
		if slices.Contains(grant.HostIds, input.HostID) {
			return grant, true
		}
	}
	return db.RemoteAccessGrant{}, false
}

func validGrantInput(input GrantInput) bool {
	return validGrantFields(input) &&
		(input.Status == GovernanceDraft || input.Status == GovernanceEnabled || input.Status == GovernanceDisabled || input.Status == GovernanceArchived)
}

func validGrantFields(input GrantInput) bool {
	return (input.SubjectType == "user" || input.SubjectType == "department") && input.SubjectID != uuid.Nil &&
		len(input.HostIDs) > 0 && len(input.ManagedAccountIDs) > 0 && validProtocols(input.Protocols) &&
		len(input.Actions) == 1 && input.Actions[0] == "terminal" && input.ValidUntil.After(input.ValidFrom)
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
	return appendAuditDetails(ctx, q, actorID, enterpriseID, actionName, resourceType, resourceID, map[string]any{"status": status})
}

func appendAuditDetails(ctx context.Context, q *db.Queries, actorID, enterpriseID uuid.UUID, actionName, resourceType string, resourceID uuid.UUID, details map[string]any) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: "enterprise_user",
		ActorID: actorID.String(), Action: actionName, ResourceType: resourceType, ResourceID: resourceID.String(), Result: "success", Details: details})
	return err
}

func sessionAuditDetails(session db.RemoteAccessSession, status string) map[string]any {
	return map[string]any{
		"status": status, "remote_session_id": session.ID, "lease_id": session.LeaseID,
		"authorization_version": session.AuthorizationVersion, "snapshot_hash": hex.EncodeToString(session.DecisionSnapshotHash),
		"host_id": session.HostID, "managed_account_id": session.ManagedAccountID, "protocol": session.Protocol,
		"connection_mode": session.ConnectionMode, "session_fence": session.SessionFence,
		"recording_mode": session.RecordingMode, "command_audit_mode": session.CommandAuditMode,
		"clipboard_mode": session.ClipboardMode, "file_upload_mode": session.FileUploadMode,
		"file_download_mode": session.FileDownloadMode, "port_forward_mode": session.PortForwardMode,
		"session_share_mode": session.SessionShareMode,
	}
}

func recordingAuditDetails(recording db.RemoteAccessRecording, status string) map[string]any {
	return map[string]any{
		"status": status, "recording_id": recording.ID, "remote_session_id": recording.SessionID,
		"recording_status": recording.Status, "chunk_count": recording.ChunkCount, "event_count": recording.EventCount,
		"retention_until": recording.RetentionUntil,
	}
}

func (service Service) createSessionRecording(ctx context.Context, q *db.Queries, enterpriseID, sessionID uuid.UUID) (uuid.UUID, error) {
	recordingID := newID()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return uuid.Nil, err
	}
	envelope, err := service.Keyring.EncryptContext(ctx, dek, recordingKeyAAD(enterpriseID, recordingID, sessionID))
	clear(dek)
	if err != nil {
		return uuid.Nil, err
	}
	wrapped, err := json.Marshal(envelope)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = q.CreateRemoteAccessRecording(ctx, db.CreateRemoteAccessRecordingParams{ID: recordingID, EnterpriseID: enterpriseID,
		SessionID: sessionID, KeyProvider: envelope.Provider, KeyID: envelope.KeyID, KeyVersion: int32(envelope.KeyVersion), WrappedDek: wrapped})
	return recordingID, err
}

func (service Service) recordingIDForSession(ctx context.Context, q *db.Queries, enterpriseID, sessionID uuid.UUID) (uuid.UUID, error) {
	recording, err := q.GetRemoteAccessRecordingBySession(ctx, db.GetRemoteAccessRecordingBySessionParams{SessionID: sessionID, EnterpriseID: enterpriseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return recording.ID, err
}

func (service Service) recordOptionalRecordingDegradation(ctx context.Context, actor Actor, sessionID uuid.UUID) error {
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		payload, err := json.Marshal(map[string]any{"enterprise_id": actor.EnterpriseID, "session_id": sessionID, "recording_mode": "optional"})
		if err != nil {
			return err
		}
		if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.recording.degraded",
			AggregateType: "remote_access_session", AggregateID: sessionID.String(), Payload: payload}); err != nil {
			return err
		}
		return appendAuditDetails(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.recording.degrade", "remote_access_session", sessionID, map[string]any{
			"status": "optional_unavailable", "remote_session_id": sessionID, "recording_mode": "optional",
		})
	})
}

func appendDecisionAudit(ctx context.Context, q *db.Queries, actor Actor, requestID uuid.UUID, actionName, status string, decision AccessDecision, snapshotHash []byte) error {
	resourceType, resourceID := "remote_access_request", requestID.String()
	result := "success"
	if requestID == uuid.Nil {
		resourceType, resourceID = "remote_access_decision", hex.EncodeToString(snapshotHash)
	}
	if decision.Outcome == DecisionDenied {
		result = "denied"
	}
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: actor.EnterpriseID, Valid: true},
		ActorType: "enterprise_user", ActorID: actor.UserID.String(), Action: actionName, ResourceType: resourceType, ResourceID: resourceID,
		Result: result, Details: map[string]any{"status": status, "outcome": decision.Outcome, "reason_codes": decision.ReasonCodes,
			"snapshot_hash": hex.EncodeToString(snapshotHash), "matched_grants": decision.MatchedGrantSnapshots,
			"matched_rules": decision.MatchedRuleSnapshots, "approval_workflows": decision.ApprovalRequirements,
			"session_profile_sources": decision.SessionProfile.SourceProfiles, "authorization_version": decision.AuthorizationVersion}})
	return err
}

func publishDecisionNotification(ctx context.Context, q *db.Queries, requestID uuid.UUID, decision AccessDecision, snapshotHash []byte) error {
	if !decision.Notifications {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"request_id": requestID, "outcome": decision.Outcome, "reason_codes": decision.ReasonCodes,
		"snapshot_hash": hex.EncodeToString(snapshotHash), "matched_grants": decision.MatchedGrantSnapshots, "matched_rules": decision.MatchedRuleSnapshots})
	if err != nil {
		return err
	}
	return q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.decision.notify",
		AggregateType: "remote_access_request", AggregateID: requestID.String(), Payload: payload})
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
