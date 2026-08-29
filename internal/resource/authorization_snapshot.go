package resource

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/google/uuid"
)

type ResourceAuthorizationSnapshot struct {
	ResourceType string    `json:"resource_type"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

func NewResourceAuthorizationSnapshot(resourceType string, resourceID uuid.UUID) ResourceAuthorizationSnapshot {
	return ResourceAuthorizationSnapshot{ResourceType: resourceType, ResourceID: resourceID}
}

func HashResourceAuthorizationSnapshot(resourceType string, resourceID uuid.UUID) ([]byte, error) {
	encoded, err := json.Marshal(NewResourceAuthorizationSnapshot(resourceType, resourceID))
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}
