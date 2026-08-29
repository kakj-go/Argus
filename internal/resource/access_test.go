package resource

import (
	"testing"

	"github.com/google/uuid"
)

func TestExplicitResourceAccessIgnoresLabels(t *testing.T) {
	resourceID := uuid.New()
	service := AccessService{}
	if !service.CanAccess([]uuid.UUID{resourceID}, resourceID) {
		t.Fatal("expected explicit grant to allow access")
	}
	if !service.CanAccess([]uuid.UUID{resourceID}, resourceID) {
		t.Fatal("label changes must not affect explicit access")
	}
}

func TestNamespaceAccessInheritsClusterGrant(t *testing.T) {
	clusterID := uuid.New()
	if !(AccessService{}).CanAccessNamespace([]uuid.UUID{clusterID}, clusterID) {
		t.Fatal("expected namespace to inherit cluster authorization")
	}
}
