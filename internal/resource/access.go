package resource

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type AccessService struct{ Store *postgres.Store }

func (service AccessService) CanAccess(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID, resourceType, resourceID string, labels map[string]string) (bool, []string, error) {
	scopes, err := service.loadScopes(ctx, enterpriseID, scopeIDs)
	if err != nil {
		return false, nil, err
	}
	allowed, matched := authorization.AnyScopeMatches(scopes, authorization.Resource{EnterpriseID: enterpriseID.String(), Type: resourceType, ID: resourceID, Labels: labels})
	return allowed, matched, nil
}

func (service AccessService) FilterHosts(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID, hosts []db.Host) ([]db.Host, error) {
	result := make([]db.Host, 0, len(hosts))
	for _, host := range hosts {
		labels, err := DecodeLabels(host.Labels)
		if err != nil {
			return nil, err
		}
		allowed, _, err := service.CanAccess(ctx, enterpriseID, scopeIDs, "host", host.ID.String(), labels)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, host)
		}
	}
	return result, nil
}

func (service AccessService) FilterKubernetesClusters(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID, clusters []db.KubernetesCluster) ([]db.KubernetesCluster, error) {
	result := make([]db.KubernetesCluster, 0, len(clusters))
	for _, cluster := range clusters {
		labels, err := DecodeLabels(cluster.Labels)
		if err != nil {
			return nil, err
		}
		allowed, _, err := service.CanAccess(ctx, enterpriseID, scopeIDs, "kubernetes_cluster", cluster.ID.String(), labels)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, cluster)
		}
	}
	return result, nil
}

func (service AccessService) CanAccessNamespace(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID, clusterID uuid.UUID, namespace string) (bool, error) {
	allowed, _, err := service.CanAccess(ctx, enterpriseID, scopeIDs, "kubernetes_namespace", clusterID.String()+"/"+namespace, map[string]string{})
	return allowed, err
}

func (service AccessService) loadScopes(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID) ([]authorization.Scope, error) {
	result := make([]authorization.Scope, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scope, err := service.Store.Queries.GetDataScope(ctx, db.GetDataScopeParams{ID: scopeID, EnterpriseID: enterpriseID})
		if err != nil {
			return nil, err
		}
		selector := json.RawMessage(nil)
		if len(scope.LabelSelector) > 0 && string(scope.LabelSelector) != "null" {
			selector = append(selector, scope.LabelSelector...)
		}
		result = append(result, authorization.Scope{ID: scope.ID.String(), EnterpriseID: scope.EnterpriseID.String(), ResourceTypes: scope.ResourceTypes,
			ExplicitResourceIDs: scope.ExplicitResourceIds, LabelSelector: selector, Status: scope.Status})
	}
	return result, nil
}
