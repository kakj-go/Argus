package connector

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"

	"google.golang.org/protobuf/types/known/anypb"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kakj-go/Argus/internal/collectormanager"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/telemetrybinding"
)

func executeCollectorManagement(ctx context.Context, payload *anypb.Any, credential []byte) (*connectorv1.CollectorManagementResult, error) {
	var command connectorv1.CollectorManagementCommand
	if payload == nil || payload.UnmarshalTo(&command) != nil {
		return nil, collectormanager.ErrInvalidCommand
	}
	var err error
	var artifactClient *http.Client
	var clientErr error
	if os.Getenv("ARGUS_OTELCOL_ARTIFACT_TLS_MODE") == "insecure" {
		artifactClient, clientErr = collectormanager.NewArtifactHTTPClientInsecure()
	} else {
		artifactClient, clientErr = collectormanager.NewArtifactHTTPClient(os.Getenv("ARGUS_OTELCOL_ARTIFACT_CA_PATH"))
	}
	if clientErr != nil {
		return nil, clientErr
	}
	manager := collectormanager.Manager{Root: os.Getenv("ARGUS_COLLECTOR_STATE_DIR"), HTTPClient: artifactClient}
	var result collectormanager.Result
	var nodes []telemetrybinding.NodeEvidence
	if command.GetResourceType() == "kubernetes_cluster" {
		var configuration *rest.Config
		if len(credential) == 0 {
			configuration, err = rest.InClusterConfig()
		} else {
			configuration, err = clientcmd.RESTConfigFromKubeConfig(credential)
		}
		if err == nil {
			var client *kubernetes.Clientset
			client, err = kubernetes.NewForConfig(configuration)
			if err == nil {
				result, err = manager.ApplyKubernetes(ctx, &command, collectormanager.KubernetesOptions{Client: client, WaitForEnrollment: true})
				if err == nil && command.GetOperation() != "uninstall" {
					nodes, err = telemetrybinding.Collect(ctx, client)
				}
			}
		}
	} else if command.GetResourceType() != "host" {
		return nil, collectormanager.ErrInvalidCommand
	} else if command.GetTargetUsername() == "" && len(credential) == 0 {
		result, err = manager.ApplyLocal(ctx, &command)
	} else {
		resolved, resolveErr := resolveCollectorAddresses(ctx, command.GetTargetAddress())
		if resolveErr != nil {
			return nil, resolveErr
		}
		result, err = manager.ApplySSH(ctx, &command, collectormanager.SSHOptions{Credential: credential,
			Dial: func(ctx context.Context) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(resolved[0].String(), fmt.Sprint(command.GetTargetPort())))
			},
			Revalidate: func(ctx context.Context) error {
				current, currentErr := resolveCollectorAddresses(ctx, command.GetTargetAddress())
				if currentErr != nil || !slices.Equal(current, resolved) {
					return collectormanager.ErrInvalidCommand
				}
				return nil
			}})
	}
	if err != nil {
		return nil, err
	}
	response := &connectorv1.CollectorManagementResult{CollectorId: result.CollectorID, EffectiveRevision: result.EffectiveRevision,
		AppliedConfigSha256: result.ConfigSHA256, Status: result.Status, DiagnosticHash: result.DiagnosticHash}
	for _, node := range nodes {
		response.KubernetesNodes = append(response.KubernetesNodes, &connectorv1.KubernetesNodeEvidence{NodeUid: node.NodeUID,
			NodeName: node.NodeName, ProviderId: node.ProviderID, MachineId: node.MachineID, SystemUuid: node.SystemUUID,
			InternalIps: append([]string(nil), node.InternalIPs...)})
	}
	return response, nil
}

func collectorManagementFailureCode(err error) string {
	return collectormanager.FailureCode(err)
}

func resolveCollectorAddresses(ctx context.Context, hostname string) ([]netip.Addr, error) {
	values, err := net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil || len(values) == 0 {
		return nil, collectormanager.ErrInvalidCommand
	}
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		result = append(result, value.Unmap())
	}
	slices.SortFunc(result, func(left, right netip.Addr) int { return left.Compare(right) })
	result = slices.Compact(result)
	return result, nil
}
