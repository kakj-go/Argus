package telemetrybinding

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	MaxNodeEvidence      = 1000
	MaxNodeEvidenceBytes = 512 << 10
)

var ErrInvalidNodeEvidence = errors.New("invalid Kubernetes node evidence")

type NodeEvidence struct {
	NodeUID     string   `json:"node_uid"`
	NodeName    string   `json:"node_name"`
	ProviderID  string   `json:"provider_id"`
	MachineID   string   `json:"machine_id"`
	SystemUUID  string   `json:"system_uuid"`
	InternalIPs []string `json:"internal_ips"`
}

type nodeIdentityEvidence struct {
	NodeUID    string `json:"node_uid"`
	NodeName   string `json:"node_name"`
	ProviderID string `json:"provider_id"`
	MachineID  string `json:"machine_id"`
	SystemUUID string `json:"system_uuid"`
}

func Collect(ctx context.Context, client kubernetes.Interface) ([]NodeEvidence, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: MaxNodeEvidence})
	if err != nil {
		return nil, err
	}
	values := make([]NodeEvidence, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		addresses := make([]string, 0, len(node.Status.Addresses))
		for _, address := range node.Status.Addresses {
			if string(address.Type) == "InternalIP" {
				addresses = append(addresses, address.Address)
			}
		}
		slices.Sort(addresses)
		addresses = slices.Compact(addresses)
		values = append(values, NodeEvidence{NodeUID: string(node.UID), NodeName: node.Name,
			ProviderID: node.Spec.ProviderID, MachineID: node.Status.NodeInfo.MachineID,
			SystemUUID: node.Status.NodeInfo.SystemUUID, InternalIPs: addresses})
	}
	slices.SortFunc(values, func(left, right NodeEvidence) int { return strings.Compare(left.NodeUID, right.NodeUID) })
	if err := Validate(values); err != nil {
		return nil, err
	}
	return values, nil
}

func Validate(values []NodeEvidence) error {
	if len(values) == 0 || len(values) > MaxNodeEvidence {
		return ErrInvalidNodeEvidence
	}
	encoded, err := json.Marshal(values)
	if err != nil || len(encoded) > MaxNodeEvidenceBytes {
		return ErrInvalidNodeEvidence
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value.NodeUID == "" || len(value.NodeUID) > 256 || value.NodeName == "" || len(value.NodeName) > 253 ||
			len(value.ProviderID) > 1024 || len(value.MachineID) > 256 || len(value.SystemUUID) > 256 || len(value.InternalIPs) > 32 {
			return ErrInvalidNodeEvidence
		}
		if _, exists := seen[value.NodeUID]; exists {
			return ErrInvalidNodeEvidence
		}
		seen[value.NodeUID] = struct{}{}
		for _, raw := range value.InternalIPs {
			if address, err := netip.ParseAddr(raw); err != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
				return ErrInvalidNodeEvidence
			}
		}
	}
	return nil
}

func Upsert(ctx context.Context, q *db.Queries, enterpriseID, clusterID uuid.UUID, values []NodeEvidence) error {
	if err := Validate(values); err != nil {
		return err
	}
	hosts, err := q.ListHosts(ctx, enterpriseID)
	if err != nil {
		return err
	}
	for _, value := range values {
		value = normalize(value)
		candidates := make([]uuid.UUID, 0, 1)
		for _, host := range hosts {
			if host.Status == "active" && slices.Contains(value.InternalIPs, host.Address) {
				candidates = append(candidates, host.ID)
			}
		}
		evidence, err := json.Marshal(value)
		if err != nil {
			return err
		}
		hash, err := identityHash(value)
		if err != nil {
			return err
		}
		hostID, confidence := uuid.NullUUID{}, int32(40)
		if len(candidates) == 1 {
			hostID, confidence = uuid.NullUUID{UUID: candidates[0], Valid: true}, 70
		}
		if _, err = q.UpsertKubernetesNodeHostBindingProposal(ctx, db.UpsertKubernetesNodeHostBindingProposalParams{
			ID: uuid.Must(uuid.NewV7()), EnterpriseID: enterpriseID, KubernetesClusterID: clusterID,
			NodeUid: value.NodeUID, NodeName: value.NodeName, HostID: hostID, MatchedBy: "ip",
			Evidence: evidence, EvidenceHash: hash[:], Confidence: confidence,
		}); err != nil {
			return err
		}
	}
	return nil
}

func normalize(value NodeEvidence) NodeEvidence {
	value.InternalIPs = append([]string(nil), value.InternalIPs...)
	slices.Sort(value.InternalIPs)
	value.InternalIPs = slices.Compact(value.InternalIPs)
	return value
}

func identityHash(value NodeEvidence) ([sha256.Size]byte, error) {
	identity, err := json.Marshal(nodeIdentityEvidence{NodeUID: value.NodeUID, NodeName: value.NodeName,
		ProviderID: value.ProviderID, MachineID: value.MachineID, SystemUUID: value.SystemUUID})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(identity), nil
}
