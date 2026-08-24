package argusdev

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *App) verifyM7BastionGateway(ctx context.Context, env *E2EEnvironment) (returnErr error) {
	rootHostID := env.State.Values["m3_bastion_root_host_id"]
	bastionClusterID := env.State.Values["m3_bastion_cluster_id"]
	if rootHostID == "" || bastionClusterID == "" {
		return fmt.Errorf("M7 bastion gateway requires the M3 Bastion root Host and Kubernetes resource")
	}
	clusterID := env.State.Values["m3_cluster_id"]
	hostID := env.State.Values["m7_host_id"]
	gatewayCollectorID, err := a.postgresQuery(ctx, env, "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='"+clusterID+"' ORDER BY created_at DESC LIMIT 1;")
	if err != nil {
		return err
	}
	leafCollectorID, err := a.postgresQuery(ctx, env, "SELECT id FROM collector_instances WHERE resource_type='host' AND resource_id='"+hostID+"' ORDER BY created_at DESC LIMIT 1;")
	if err != nil {
		return err
	}
	gatewayCollectorID = strings.TrimSpace(gatewayCollectorID)
	leafCollectorID = strings.TrimSpace(leafCollectorID)
	if gatewayCollectorID == "" || leafCollectorID == "" {
		return fmt.Errorf("M7 bastion gateway Collector identities are unavailable")
	}
	identity := map[string][]byte{}
	for _, file := range []string{"client.pem", "client-key.pem", "ca.pem"} {
		value, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-direct-executor", "argus-e2e-systemd-host", "cat", "/var/lib/argus-otelcol/identity/"+file)
		if err != nil {
			return err
		}
		identity[file] = []byte(value + "\n")
	}
	secretName := "argus-m7-bastion-leaf-identity"
	_ = env.Kube.Client.CoreV1().Secrets(m7CollectorNamespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if _, err := env.Kube.Client.CoreV1().Secrets(m7CollectorNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: m7CollectorNamespace, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}}, Data: identity,
	}, metav1.CreateOptions{}); err != nil {
		return err
	}
	rebind := "UPDATE collector_instances SET resource_type='host',resource_id='" + rootHostID + "',role='edge_gateway',updated_at=now() WHERE id='" + gatewayCollectorID + "';" +
		"UPDATE collector_instances SET resource_type='kubernetes_cluster',resource_id='" + bastionClusterID + "',role='leaf',updated_at=now() WHERE id='" + leafCollectorID + "';" +
		"UPDATE telemetry_routes SET kind='bastion_gateway',gateway_collector_id='" + gatewayCollectorID + "',status='active',updated_at=now() WHERE collector_id='" + leafCollectorID + "';"
	if _, err := a.postgresQuery(ctx, env, rebind); err != nil {
		return err
	}
	defer func() {
		restore := "UPDATE collector_instances SET resource_type='kubernetes_cluster',resource_id='" + clusterID + "',role='daemonset',updated_at=now() WHERE id='" + gatewayCollectorID + "';" +
			"UPDATE collector_instances SET resource_type='host',resource_id='" + hostID + "',role='direct',updated_at=now() WHERE id='" + leafCollectorID + "';" +
			"UPDATE telemetry_routes SET kind='direct_argus',gateway_collector_id=NULL,status='active',updated_at=now() WHERE collector_id='" + leafCollectorID + "';"
		if _, restoreErr := a.postgresQuery(context.Background(), env, restore); returnErr == nil && restoreErr != nil {
			returnErr = restoreErr
		}
	}()
	if err := a.runM7GeneratorWithIdentity(ctx, env, "bastion-gateway", bastionClusterID, gatewayCollectorID, secretName); err != nil {
		return err
	}
	return a.verifyM7Signals(ctx, env, bastionClusterID, "bastion-gateway")
}
