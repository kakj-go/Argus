package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// ActionExtension owns telemetry PendingAction commits and delegates every
// unrelated action to the next resource extension.
type ActionExtension struct {
	Next               resource.ActionExtension
	Credentials        secret.Service
	EnrollmentEndpoint string
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
	ServerCABundlePath string
}

func (extension ActionExtension) RevalidateAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) ([]byte, error) {
	if !strings.HasPrefix(action.ActionType, "telemetry.") {
		return extension.revalidateNext(ctx, q, action, raw)
	}
	if action.ActionType == "telemetry.node_host_binding.confirm" {
		var plan bindingActionPlan
		if json.Unmarshal(raw, &plan) != nil {
			return nil, resource.ErrActionInvalidated
		}
		binding, err := q.GetKubernetesNodeHostBinding(ctx, db.GetKubernetesNodeHostBindingParams{ID: plan.BindingID, EnterpriseID: action.EnterpriseID})
		if err != nil || binding.Version != plan.ExpectedVersion || hex.EncodeToString(binding.EvidenceHash) != plan.EvidenceHash {
			return nil, resource.ErrActionInvalidated
		}
		if _, err = q.GetHost(ctx, db.GetHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID}); err != nil {
			return nil, resource.ErrActionInvalidated
		}
		return snapshotHash(map[string]any{
			"cluster_id":    binding.KubernetesClusterID,
			"host_id":       plan.HostID,
			"evidence_hash": plan.EvidenceHash,
		})
	}

	var plan collectorActionPlan
	if json.Unmarshal(raw, &plan) != nil || action.ActionType != "telemetry.collector."+plan.Operation {
		return nil, resource.ErrActionInvalidated
	}
	if err := validateCollectorPlan(ctx, q, action.EnterpriseID, plan); err != nil {
		return nil, resource.ErrActionInvalidated
	}
	return snapshotHash(map[string]any{
		"resource_id": plan.ResourceID,
		"profiles":    plan.ProfileIDs,
		"route_kind":  plan.RouteKind,
	})
}

func (extension ActionExtension) CommitAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	if !strings.HasPrefix(action.ActionType, "telemetry.") {
		if extension.Next == nil {
			return resource.ActionCommitResult{}, resource.ErrActionInvalidated
		}
		return extension.Next.CommitAction(ctx, q, action, raw)
	}
	if action.ActionType == "telemetry.node_host_binding.confirm" {
		return commitNodeBinding(ctx, q, action, raw)
	}
	var plan collectorActionPlan
	if json.Unmarshal(raw, &plan) != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	return extension.commitCollectorAction(ctx, q, action, plan)
}

func (extension ActionExtension) revalidateNext(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) ([]byte, error) {
	if extension.Next == nil {
		return nil, resource.ErrActionInvalidated
	}
	return extension.Next.RevalidateAction(ctx, q, action, raw)
}

func validateCollectorPlan(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan collectorActionPlan) error {
	distributions, err := q.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return err
	}
	distributionIndex := slices.IndexFunc(distributions, func(item db.CollectorDistributionVersion) bool {
		return item.ID == plan.DistributionVersionID && item.SupportStatus == "supported"
	})
	if distributionIndex < 0 || !distributionSupportsPlatform(distributions[distributionIndex].ArtifactManifest, plan.Platform) {
		return ErrDistributionPending
	}
	currentHashes, err := artifactHashes(distributions[distributionIndex].ArtifactManifest)
	if err != nil || !slices.Equal(currentHashes, plan.ArtifactHashes) {
		return resource.ErrActionInvalidated
	}
	profiles, err := q.ListCollectionProfiles(ctx)
	if err != nil {
		return err
	}
	for _, id := range plan.ProfileIDs {
		if !slices.ContainsFunc(profiles, func(item db.CollectionProfile) bool {
			return item.ID == id && item.SupportStatus == "supported" &&
				item.ConfigSchemaVersion == distributions[distributionIndex].ConfigSchemaVersion &&
				slices.Contains(item.SupportedPlatforms, plan.Platform)
		}) {
			return resource.ErrActionInvalidated
		}
	}
	if err := validateTarget(ctx, q, enterpriseID, plan); err != nil {
		return err
	}
	// 镜像由不可变 plan 携带(preview 时已按次解析:用户覆盖或服务端默认),
	// revalidate 只复核格式,不再与全局配置比对——全局值变化不应使进行中的计划失效。
	if plan.ResourceType == "kubernetes_cluster" && !validKubernetesImage(plan.KubernetesImage) {
		return resource.ErrActionInvalidated
	}
	if plan.Operation != "install" {
		collector, err := q.GetCollectorForResource(ctx, db.GetCollectorForResourceParams{EnterpriseID: enterpriseID, ResourceType: plan.ResourceType, ResourceID: plan.ResourceID})
		if err != nil || plan.ExpectedVersion <= 0 || collector.Version != plan.ExpectedVersion {
			return resource.ErrActionInvalidated
		}
	}
	return validateGateway(ctx, q, enterpriseID, plan)
}

func validateTarget(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan collectorActionPlan) error {
	switch plan.ResourceType {
	case "host":
		host, err := q.GetHost(ctx, db.GetHostParams{ID: plan.ResourceID, EnterpriseID: enterpriseID})
		if err != nil {
			return err
		}
		expectedPlatform := hostCollectorPlatform(host)
		if expectedPlatform != plan.Platform {
			return resource.ErrActionInvalidated
		}
	case "kubernetes_cluster":
		if plan.Platform != "linux_arm64" {
			return resource.ErrActionInvalidated
		}
		if _, err := q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: plan.ResourceID, EnterpriseID: enterpriseID}); err != nil {
			return err
		}
	default:
		return resource.ErrActionInvalidated
	}
	target, err := resolveCollectorExecutionTarget(ctx, q, enterpriseID, plan.ResourceType, plan.ResourceID)
	if err != nil || target.Transport != plan.Transport || target.ConnectorID != plan.ConnectorID || target.ConnectionEpoch != plan.ConnectionEpoch ||
		target.ResourceVersion != plan.TargetResourceVersion || target.Address != plan.TargetAddress || int32(target.Port) != plan.TargetPort ||
		target.Username != plan.TargetUsername || target.PinnedHostKey != plan.PinnedHostKey || target.CredentialID != plan.CredentialID ||
		target.CredentialVersion != plan.CredentialVersion {
		return resource.ErrActionInvalidated
	}
	return nil
}

func validateGateway(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan collectorActionPlan) error {
	if plan.RouteKind == "direct_argus" {
		if plan.GatewayCollectorID.Valid {
			return resource.ErrActionInvalidated
		}
		return nil
	}
	if plan.RouteKind != "bastion_gateway" || !plan.GatewayCollectorID.Valid {
		return resource.ErrActionInvalidated
	}
	gateway, err := q.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: plan.GatewayCollectorID.UUID, EnterpriseID: enterpriseID})
	if err != nil || gateway.ResourceType != "host" || gateway.Role != "edge_gateway" || gateway.Status == "uninstalled" {
		return resource.ErrActionInvalidated
	}
	gatewayHost, err := q.GetHost(ctx, db.GetHostParams{ID: gateway.ResourceID, EnterpriseID: enterpriseID})
	if err != nil || gatewayHost.ConnectionMode != "connector_local" || !gatewayHost.BastionScopeID.Valid {
		return resource.ErrActionInvalidated
	}
	switch plan.ResourceType {
	case "host":
		target, err := q.GetHost(ctx, db.GetHostParams{ID: plan.ResourceID, EnterpriseID: enterpriseID})
		if err != nil || !target.BastionScopeID.Valid || target.BastionScopeID.UUID != gatewayHost.BastionScopeID.UUID {
			return resource.ErrActionInvalidated
		}
	case "kubernetes_cluster":
		target, err := q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: plan.ResourceID, EnterpriseID: enterpriseID})
		if err != nil || !target.BastionScopeID.Valid || target.BastionScopeID.UUID != gatewayHost.BastionScopeID.UUID {
			return resource.ErrActionInvalidated
		}
	}
	return nil
}

func commitNodeBinding(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	var plan bindingActionPlan
	if json.Unmarshal(raw, &plan) != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	evidence, err := hex.DecodeString(plan.EvidenceHash)
	if err != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	binding, err := q.ConfirmKubernetesNodeHostBinding(ctx, db.ConfirmKubernetesNodeHostBindingParams{
		ID: plan.BindingID, EnterpriseID: action.EnterpriseID,
		HostID: uuid.NullUUID{UUID: plan.HostID, Valid: true}, Version: plan.ExpectedVersion, EvidenceHash: evidence,
	})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	return resource.ActionCommitResult{ResourceType: "kubernetes_cluster", ResourceID: binding.KubernetesClusterID,
		ResourceVersion: binding.Version, Summary: "Kubernetes node to Host binding confirmed"}, nil
}

func (extension ActionExtension) commitCollectorAction(ctx context.Context, q *db.Queries, action db.PendingAction, plan collectorActionPlan) (resource.ActionCommitResult, error) {
	if plan.Operation == "uninstall" {
		collector, err := q.MarkCollectorUninstalling(ctx, db.MarkCollectorUninstallingParams{
			EnterpriseID: action.EnterpriseID, ResourceType: plan.ResourceType, ResourceID: plan.ResourceID, Column4: plan.ExpectedVersion,
		})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		if err = q.ReleaseCollectorClaims(ctx, db.ReleaseCollectorClaimsParams{EnterpriseID: action.EnterpriseID, CollectorID: collector.ID}); err != nil {
			return resource.ActionCommitResult{}, err
		}
		if err = q.RevokeCollectorCertificates(ctx, db.RevokeCollectorCertificatesParams{CollectorID: collector.ID,
			RevokeReason: pgtype.Text{String: "collector uninstall", Valid: true}}); err != nil {
			return resource.ActionCommitResult{}, err
		}
		return extension.enqueueCollectorOperation(ctx, q, action, collector, plan, nil)
	}

	collector, err := q.UpsertCollectorForAction(ctx, db.UpsertCollectorForActionParams{
		ID: newTelemetryID(), EnterpriseID: action.EnterpriseID, ResourceType: plan.ResourceType, ResourceID: plan.ResourceID,
		DistributionVersionID: plan.DistributionVersionID, Platform: plan.Platform, Role: plan.Role,
	})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	serverCA, err := os.ReadFile(extension.ServerCABundlePath)
	if err != nil {
		return resource.ActionCommitResult{}, resource.ErrActionUnavailable
	}
	rendered, err := configbundle.Render(configbundle.RenderInput{CollectorID: collector.ID.String(), ResourceID: plan.ResourceID.String(),
		ResourceType: plan.ResourceType, Role: plan.Role, RouteKind: plan.RouteKind, GatewayEndpoint: plan.GatewayEndpoint,
		GatewayServerName: plan.GatewayServerName, ProfileKeys: plan.ProfileKeys,
		EnrollmentEndpoint: extension.EnrollmentEndpoint, IngestGRPCEndpoint: extension.IngestGRPCEndpoint,
		IngestHTTPEndpoint: extension.IngestHTTPEndpoint, ServerCAPEM: string(serverCA)})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	configHash := sha256.Sum256(rendered)
	if err = q.SupersedeCollectorConfigRevisions(ctx, collector.ID); err != nil {
		return resource.ActionCommitResult{}, err
	}
	if _, err = q.CreateCollectorConfigRevision(ctx, db.CreateCollectorConfigRevisionParams{
		ID: newTelemetryID(), CollectorID: collector.ID, Revision: collector.DesiredRevision,
		ProfileIds: plan.ProfileIDs, RenderedConfig: rendered, ConfigHash: configHash[:],
	}); err != nil {
		return resource.ActionCommitResult{}, err
	}
	if _, err = q.UpsertTelemetryRoute(ctx, db.UpsertTelemetryRouteParams{
		ID: newTelemetryID(), EnterpriseID: action.EnterpriseID, CollectorID: collector.ID,
		Kind: plan.RouteKind, GatewayCollectorID: plan.GatewayCollectorID,
	}); err != nil {
		return resource.ActionCommitResult{}, err
	}
	if err = rebuildClaims(ctx, q, action.EnterpriseID, collector, plan.ProfileIDs); err != nil {
		return resource.ActionCommitResult{}, err
	}
	return extension.enqueueCollectorOperation(ctx, q, action, collector, plan, rendered)
}

type collectorArtifact struct {
	Platform     string `json:"platform"`
	URI          string `json:"uri"`
	SHA256       string `json:"sha256"`
	Signature    string `json:"signature"`
	SigningKeyID string `json:"signing_key_id"`
	ByteSize     uint64 `json:"byte_size"`
}

func (extension ActionExtension) enqueueCollectorOperation(ctx context.Context, q *db.Queries, action db.PendingAction, collector db.CollectorInstance,
	plan collectorActionPlan, rendered []byte) (resource.ActionCommitResult, error) {
	distributions, err := q.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	index := slices.IndexFunc(distributions, func(value db.CollectorDistributionVersion) bool { return value.ID == plan.DistributionVersionID })
	if index < 0 {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	artifact, err := artifactForPlatform(distributions[index].ArtifactManifest, plan.Platform)
	if err != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	configHash := sha256.Sum256(rendered)
	payload := &connectorv1.CollectorManagementCommand{
		CollectorId: collector.ID.String(), Operation: plan.Operation, ResourceId: plan.ResourceID.String(),
		CollectorVersion: uint64(collector.Version), DesiredRevision: uint64(collector.DesiredRevision), RenderedConfig: rendered,
		ConfigSha256: hex.EncodeToString(configHash[:]), RouteKind: plan.RouteKind, ResourceType: plan.ResourceType,
		TargetAddress: plan.TargetAddress, TargetPort: uint32(plan.TargetPort), TargetUsername: plan.TargetUsername, PinnedHostKey: plan.PinnedHostKey,
		KubernetesImage: plan.KubernetesImage,
		Artifact: &connectorv1.CollectorArtifact{DistributionVersionId: plan.DistributionVersionID.String(), Platform: artifact.Platform,
			Uri: artifact.URI, Sha256: artifact.SHA256, Signature: artifact.Signature, SigningKeyId: artifact.SigningKeyID, ByteSize: artifact.ByteSize},
	}
	if plan.GatewayCollectorID.Valid {
		payload.GatewayCollectorId = plan.GatewayCollectorID.UUID.String()
	}
	if plan.CredentialID.Valid {
		payload.CredentialId = plan.CredentialID.UUID.String()
		payload.CredentialVersion = uint64(plan.CredentialVersion)
	}
	encoded, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(payload)
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	encoded, err = resource.CanonicalJSON(encoded)
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	hash := sha256.Sum256(encoded)
	if len(rendered) > 0 {
		if _, err = q.MarkCollectorConfigApplying(ctx, db.MarkCollectorConfigApplyingParams{CollectorID: collector.ID, Revision: collector.DesiredRevision}); err != nil {
			return resource.ActionCommitResult{}, err
		}
	}
	if plan.Transport == "connector" {
		if !plan.ConnectorID.Valid || plan.ConnectionEpoch < 1 {
			return resource.ActionCommitResult{}, resource.ErrActionInvalidated
		}
		leaseID := uuid.NullUUID{}
		if plan.CredentialID.Valid {
			if extension.Credentials.Store == nil {
				return resource.ActionCommitResult{}, resource.ErrActionUnavailable
			}
			lease, leaseErr := extension.Credentials.PrepareLeaseWithQueries(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, secret.LeaseRequest{
				CredentialID: plan.CredentialID.UUID, OperationRef: action.ActionRef, TargetResourceType: plan.ResourceType,
				TargetResourceID: plan.ResourceID, RecipientType: "connector", RecipientID: plan.ConnectorID.UUID.String(), Protocol: collectorProtocol(plan), TTL: 5 * time.Minute,
			})
			if leaseErr != nil {
				return resource.ActionCommitResult{}, leaseErr
			}
			leaseID = uuid.NullUUID{UUID: lease.ID, Valid: true}
		}
		commandID := "cmd_" + strings.ReplaceAll(newTelemetryID().String(), "-", "")
		command, createErr := q.CreateConnectorCommand(ctx, db.CreateConnectorCommandParams{ID: newTelemetryID(), CommandID: commandID,
			EnterpriseID: action.EnterpriseID, ConnectorID: plan.ConnectorID.UUID, ConnectionEpoch: plan.ConnectionEpoch,
			OperationRef: action.ActionRef, CredentialLeaseID: leaseID, CommandType: "collector_management",
			PayloadSchemaVersion: "argus.collector_management/v1", Payload: encoded, PayloadHash: hash[:], IdempotencyKey: action.ActionRef,
			ExpiresAt: pgtype.Timestamptz{Time: minTime(action.ExpiresAt.Time, time.Now().UTC().Add(10*time.Minute)), Valid: true}})
		if createErr != nil {
			return resource.ActionCommitResult{}, createErr
		}
		return resource.ActionCommitResult{ResourceType: plan.ResourceType, ResourceID: plan.ResourceID, ResourceVersion: collector.Version,
			Summary: "Collector management command queued", ConnectorCommandID: uuid.NullUUID{UUID: command.ID, Valid: true}}, nil
	}
	if plan.Transport != "direct" {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	operation, err := q.CreateTelemetryCollectorOperation(ctx, db.CreateTelemetryCollectorOperationParams{ID: newTelemetryID(), EnterpriseID: action.EnterpriseID,
		CollectorID: collector.ID, PendingActionID: action.ID, Operation: plan.Operation, Plan: encoded, PlanHash: hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: minTime(action.ExpiresAt.Time, time.Now().UTC().Add(10*time.Minute)), Valid: true}})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	return resource.ActionCommitResult{ResourceType: plan.ResourceType, ResourceID: plan.ResourceID, ResourceVersion: collector.Version,
		Summary: "Direct Collector operation queued", TelemetryOperationID: uuid.NullUUID{UUID: operation.ID, Valid: true}}, nil
}

func artifactForPlatform(raw json.RawMessage, platform string) (collectorArtifact, error) {
	var values []collectorArtifact
	if json.Unmarshal(raw, &values) != nil {
		return collectorArtifact{}, resource.ErrActionInvalidated
	}
	for _, value := range values {
		if value.Platform == platform && value.URI != "" && value.SHA256 != "" && value.SigningKeyID != "" && value.ByteSize > 0 {
			return value, nil
		}
	}
	return collectorArtifact{}, fmt.Errorf("collector artifact for %s is unavailable", platform)
}

func collectorProtocol(plan collectorActionPlan) string {
	if plan.ResourceType == "kubernetes_cluster" {
		return "kubernetes"
	}
	return "ssh"
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func rebuildClaims(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, collector db.CollectorInstance, profileIDs []uuid.UUID) error {
	if err := q.ReleaseCollectorClaims(ctx, db.ReleaseCollectorClaimsParams{EnterpriseID: enterpriseID, CollectorID: collector.ID}); err != nil {
		return err
	}
	profiles, err := q.ListCollectionProfiles(ctx)
	if err != nil {
		return err
	}
	selector := json.RawMessage(`{}`)
	selectorHash := sha256.Sum256(selector)
	for _, profileID := range profileIDs {
		index := slices.IndexFunc(profiles, func(item db.CollectionProfile) bool { return item.ID == profileID })
		if index < 0 {
			return resource.ErrActionInvalidated
		}
		profile := profiles[index]
		physicalRefs := []string{collector.ResourceType + ":" + collector.ResourceID.String()}
		if collector.ResourceType == "kubernetes_cluster" && profile.ProfileKey == "k8s-node-container" {
			bindings, bindingErr := q.ListKubernetesNodeHostBindings(ctx, db.ListKubernetesNodeHostBindingsParams{EnterpriseID: enterpriseID, KubernetesClusterID: collector.ResourceID})
			if bindingErr != nil {
				return bindingErr
			}
			physicalRefs = physicalRefs[:0]
			for _, binding := range bindings {
				if binding.Status == "verified" && binding.HostID.Valid {
					physicalRefs = append(physicalRefs, "host:"+binding.HostID.UUID.String())
				}
			}
			if len(physicalRefs) == 0 {
				physicalRefs = append(physicalRefs, collector.ResourceType+":"+collector.ResourceID.String())
			}
		}
		claims, claimErr := expandProfileClaims(profile.ProfileKey, profile.ClaimTypes, profile.Signals)
		if claimErr != nil {
			return claimErr
		}
		for _, physicalRef := range physicalRefs {
			for _, claim := range claims {
				primary, primaryErr := q.GetActivePrimaryCollectionClaim(ctx, db.GetActivePrimaryCollectionClaimParams{
					EnterpriseID: enterpriseID, PhysicalResourceRef: physicalRef, ClaimType: claim.ClaimType, SelectorHash: selectorHash[:],
				})
				if primaryErr == nil && primary.CollectorID != collector.ID {
					rollback, _ := json.Marshal(map[string]any{"restore_primary_claim_id": primary.ID, "collector_id": primary.CollectorID})
					_, err = q.CreateMigrationCollectionClaim(ctx, db.CreateMigrationCollectionClaimParams{
						ID: newTelemetryID(), EnterpriseID: enterpriseID, PhysicalResourceRef: physicalRef, CollectorID: collector.ID,
						ProfileID: uuid.NullUUID{UUID: profile.ID, Valid: true}, ClaimType: claim.ClaimType, Signal: claim.Signal,
						Selector: selector, SelectorHash: selectorHash[:], PrimaryClaimID: uuid.NullUUID{UUID: primary.ID, Valid: true},
						RollbackPlan: rollback, ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(15 * time.Minute), Valid: true},
					})
				} else if errors.Is(primaryErr, pgx.ErrNoRows) {
					_, err = q.CreateCollectionClaim(ctx, db.CreateCollectionClaimParams{
						ID: newTelemetryID(), EnterpriseID: enterpriseID, PhysicalResourceRef: physicalRef,
						CollectorID: collector.ID, ProfileID: uuid.NullUUID{UUID: profile.ID, Valid: true}, ClaimType: claim.ClaimType,
						Signal: claim.Signal, Selector: selector, SelectorHash: selectorHash[:],
					})
				} else if primaryErr != nil {
					err = primaryErr
				}
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type profileClaim struct {
	ClaimType string
	Signal    string
}

func expandProfileClaims(profileKey string, claimTypes, signals []string) ([]profileClaim, error) {
	if len(signals) == 0 {
		return nil, resource.ErrActionInvalidated
	}
	if len(claimTypes) == 0 {
		claimTypes = []string{profileKey}
	}
	if len(claimTypes) != 1 && len(claimTypes) != len(signals) {
		return nil, resource.ErrActionInvalidated
	}
	claims := make([]profileClaim, 0, len(signals))
	seen := make(map[string]struct{}, len(signals))
	for index, signal := range signals {
		claimType := claimTypes[0]
		if len(claimTypes) == len(signals) {
			claimType = claimTypes[index]
		} else if len(signals) > 1 {
			claimType += "." + signal
		}
		if claimType == "" || signal == "" {
			return nil, resource.ErrActionInvalidated
		}
		if _, exists := seen[claimType]; exists {
			return nil, resource.ErrActionInvalidated
		}
		seen[claimType] = struct{}{}
		claims = append(claims, profileClaim{ClaimType: claimType, Signal: signal})
	}
	return claims, nil
}

func snapshotHash(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func newTelemetryID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

var _ resource.ActionExtension = ActionExtension{}
