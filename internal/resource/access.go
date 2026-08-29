package resource

import (
	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type AccessService struct{}

func (AccessService) CanAccess(authorizedResourceIDs []uuid.UUID, resourceID uuid.UUID) bool {
	for _, id := range authorizedResourceIDs {
		if id == resourceID {
			return true
		}
	}
	return false
}

func (service AccessService) FilterHosts(authorizedResourceIDs []uuid.UUID, hosts []db.Host) []db.Host {
	result := make([]db.Host, 0, len(hosts))
	for _, host := range hosts {
		if service.CanAccess(authorizedResourceIDs, host.ID) {
			result = append(result, host)
		}
	}
	return result
}

func (service AccessService) FilterKubernetesClusters(authorizedResourceIDs []uuid.UUID, clusters []db.KubernetesCluster) []db.KubernetesCluster {
	result := make([]db.KubernetesCluster, 0, len(clusters))
	for _, cluster := range clusters {
		if service.CanAccess(authorizedResourceIDs, cluster.ID) {
			result = append(result, cluster)
		}
	}
	return result
}

func (service AccessService) CanAccessNamespace(authorizedResourceIDs []uuid.UUID, clusterID uuid.UUID) bool {
	// Namespace and child objects inherit the Kubernetes cluster grant.
	return service.CanAccess(authorizedResourceIDs, clusterID)
}
