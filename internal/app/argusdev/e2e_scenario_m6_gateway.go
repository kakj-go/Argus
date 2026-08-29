package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *App) verifyM6CrossGatewayDrain(ctx context.Context, env *E2EEnvironment) error {
	hostID := env.State.Values["m3_bastion_root_host_id"]
	connectorID := env.State.Values["m3_bastion_connector_id"]
	if hostID == "" || connectorID == "" {
		return fmt.Errorf("M6 Bastion Host or Connector is unavailable")
	}
	client, _ := scenarioHTTP(env)
	host, err := client.JSON(ctx, "m6-bastion-host-current", "enterprise", http.MethodGet, "/enterprise/hosts/"+hostID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	version, _ := numberField(host, "resource_version")
	preview, err := client.JSON(ctx, "m6-bastion-host-reauthorize", "enterprise", http.MethodPost, "/enterprise/hosts/"+hostID+"/actions/preview-update", http.StatusCreated,
		map[string]any{"labels": map[string]string{"team": "m3", "route": "bastion"}, "expected_version": version}, enterpriseHeaders(env, "m6-bastion-reauthorize"))
	if err != nil {
		return err
	}
	actionRef, _ := stringField(preview, "action_ref")
	if _, err := a.confirmPendingAction(ctx, env, "m6-bastion-reauthorize-confirm", actionRef); err != nil {
		return err
	}
	hostID, accountID, err := a.createM6BastionTarget(ctx, env)
	if err != nil {
		return err
	}
	if err := env.Kube.ScaleDeployment(ctx, env.SystemNS, "argus-connector-gateway", 2); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-connector-gateway", 5*time.Minute); err != nil {
		return err
	}
	owner, peer, peerIP, err := a.m6GatewayPair(ctx, env, connectorID)
	if err != nil {
		return err
	}
	validFrom := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	validUntil := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	grant, err := client.JSON(ctx, "m6-bastion-grant", "enterprise", http.MethodPost, "/enterprise/remote-access-grants", http.StatusCreated,
		map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "host_ids": []string{hostID}, "managed_account_ids": []string{accountID}, "protocols": []string{"ssh"}, "actions": []string{"terminal"}, "valid_from": validFrom, "valid_until": validUntil, "status": "draft"}, enterpriseHeaders(env, "m6-bastion-grant"))
	if err != nil {
		return err
	}
	grantID, _ := stringField(grant, "id")
	if _, err := client.JSON(ctx, "m6-enable-bastion-grant", "enterprise", http.MethodPost,
		"/enterprise/remote-access-grants/"+grantID+"/enable?expected_version=1", http.StatusOK, nil,
		enterpriseHeaders(env, "m6-enable-bastion-grant")); err != nil {
		return err
	}
	if err := a.stepUpEnterprise(ctx, env); err != nil {
		return err
	}
	request, err := client.JSON(ctx, "m6-bastion-request", "enterprise", http.MethodPost, "/enterprise/remote-access-requests", http.StatusCreated,
		map[string]any{"host_id": hostID, "managed_account_id": accountID, "protocol": "ssh", "action": "terminal", "reason": "M6 cross-Gateway Drain"}, enterpriseHeaders(env, "m6-bastion-request"))
	if err != nil {
		return err
	}
	requestID, _ := stringField(request, "id")
	leaseID, err := a.findM6Lease(ctx, env, requestID)
	if err != nil {
		return err
	}
	session, err := client.JSON(ctx, "m6-bastion-session", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions", http.StatusCreated,
		map[string]any{"lease_id": leaseID, "terminal_cols": 100, "terminal_rows": 30}, enterpriseHeaders(env, "m6-bastion-session"))
	if err != nil {
		return err
	}
	sessionID, _ := stringField(session, "id")
	ticketResult, err := client.JSON(ctx, "m6-bastion-ticket", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions/"+sessionID+"/tickets", http.StatusCreated, nil, enterpriseHeaders(env, "m6-bastion-ticket"))
	if err != nil {
		return err
	}
	ticket, err := validateM6Ticket(ticketResult)
	if err != nil {
		return err
	}
	password, err := dataCredentialValue(ctx, env, "redis-password")
	if err != nil {
		return err
	}
	if _, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-redis", "redis", "redis-cli", "-a", password, "FLUSHALL"); err != nil {
		return err
	}
	clientPod := kubernetesNameForDev("argus-m6-remote-client-" + env.Options.RunID)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: clientPod, Namespace: env.SystemNS, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{
			Name: "remoteclient", Image: env.State.FixtureImages["ssh"], ImagePullPolicy: corev1.PullNever,
			Command: []string{"/usr/local/bin/argus-e2e-remoteclient"}, Args: []string{
				"--url", "ws://" + peerIP + ":9445/v1/sessions/" + sessionID, "--origin", env.EnterpriseOrigin(),
				"--command", "stream", "--expect-status", "terminated", "--expect-reason", "gateway_drain", "--timeout", "90s",
			}, Stdin: true, StdinOnce: true,
			SecurityContext: &corev1.SecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: int64PointerValue(65532), AllowPrivilegeEscalation: boolPointer(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		}}},
	}
	if _, err := env.Kube.Client.CoreV1().Pods(env.SystemNS).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return err
	}
	defer env.Kube.Client.CoreV1().Pods(env.SystemNS).Delete(context.Background(), clientPod, metav1.DeleteOptions{})
	if err := env.Kube.WaitPodReady(ctx, env.SystemNS, clientPod, time.Minute); err != nil {
		return err
	}
	log, err := openArtifact(filepath.Join(env.Options.Artifacts, "m6-cross-gateway-drain.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	attachDone := make(chan error, 1)
	go func() {
		attachDone <- env.Kube.AttachPod(ctx, env.SystemNS, clientPod, "remoteclient", strings.NewReader(ticket+"\n"), log, log)
	}()
	if err := a.waitPostgresValue(ctx, env, "SELECT gateway_instance FROM remote_access_routes WHERE session_id='"+sessionID+"';", peer, 30*time.Second); err != nil {
		return fmt.Errorf("M6 route owner=%s peer=%s: %w", owner, peer, err)
	}
	if err := env.Kube.Client.CoreV1().Pods(env.SystemNS).Delete(ctx, peer, metav1.DeleteOptions{}); err != nil {
		return err
	}
	select {
	case err := <-attachDone:
		if err != nil {
			return fmt.Errorf("M6 cross-Gateway client: %w", err)
		}
	case <-time.After(2 * time.Minute):
		return fmt.Errorf("M6 cross-Gateway client did not observe Drain")
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT status || '|' || coalesce(termination_reason,'') FROM remote_access_sessions WHERE id='"+sessionID+"';", "terminated|gateway_drain", 30*time.Second); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-connector-gateway", 5*time.Minute); err != nil {
		return err
	}
	if _, err = client.JSON(ctx, "m6-disable-bastion-grant", "enterprise", http.MethodPost,
		"/enterprise/remote-access-grants/"+grantID+"/disable?expected_version=2", http.StatusOK, nil,
		enterpriseHeaders(env, "m6-disable-bastion")); err != nil {
		return err
	}
	return a.refreshEnterpriseLogin(ctx, env)
}

func (a *App) createM6BastionTarget(ctx context.Context, env *E2EEnvironment) (string, string, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return "", "", err
	}
	sshAddress, err := waitForServiceIP(ctx, env, env.SystemNS, "argus-e2e-ssh-target", 3*time.Minute)
	if err != nil {
		return "", "", fmt.Errorf("M6 bastion SSH fixture load balancer: %w", err)
	}
	target := map[string]any{
		"address": sshAddress, "port": 22, "platform": "linux",
		"connection_mode": "via_bastion", "bastion_scope_id": env.State.Values["m3_bastion_scope_id"],
		"credential_id": env.State.Values["m3_credential_id"], "username": "argus",
	}
	test, err := client.JSON(ctx, "m6-bastion-host-test", "enterprise", http.MethodPost, "/enterprise/hosts/connection-tests", http.StatusAccepted,
		target, enterpriseHeaders(env, "m6-bastion-host-test"))
	if err != nil {
		return "", "", err
	}
	testID, err := stringField(test, "id")
	if err != nil {
		return "", "", err
	}
	if err := a.waitConnectionTest(ctx, env, testID); err != nil {
		return "", "", fmt.Errorf("M6 Bastion SSH connection test: %w", err)
	}
	previewInput := map[string]any{}
	for key, value := range target {
		previewInput[key] = value
	}
	previewInput["name"] = "m6-bastion-ssh-host"
	previewInput["environment"] = "production"
	previewInput["labels"] = map[string]string{"team": "m3", "route": "bastion", "terminal": "ssh"}
	previewInput["connection_test_id"] = testID
	preview, err := client.JSON(ctx, "m6-bastion-host-preview", "enterprise", http.MethodPost, "/enterprise/hosts/actions/preview-create", http.StatusCreated,
		previewInput, enterpriseHeaders(env, "m6-bastion-host-preview"))
	if err != nil {
		return "", "", err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return "", "", err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "m6-bastion-host-confirm", actionRef)
	if err != nil {
		return "", "", err
	}
	hostID, err := stringField(confirmed, "resource_ref", "resource_id")
	if err != nil {
		return "", "", err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return "", "", err
	}
	accounts, err := client.JSON(ctx, "m6-bastion-accounts", "enterprise", http.MethodGet, "/enterprise/managed-accounts", http.StatusOK, nil,
		map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", "", err
	}
	account, err := findItem(objectItems(accounts), func(item map[string]any) bool { return item["host_id"] == hostID })
	if err != nil {
		return "", "", fmt.Errorf("M6 managed account for Bastion SSH Host %s: %w", hostID, err)
	}
	accountID, err := stringField(account, "id")
	if err != nil {
		return "", "", err
	}
	return hostID, accountID, nil
}

func (a *App) m6GatewayPair(ctx context.Context, env *E2EEnvironment, connectorID string) (string, string, string, error) {
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		ownerValue, err := a.postgresQuery(ctx, env, "SELECT gateway_instance_id FROM connector_sessions WHERE connector_id='"+connectorID+"' ORDER BY connected_at DESC LIMIT 1;")
		if err != nil {
			return "", "", "", err
		}
		owner := strings.TrimSpace(ownerValue)
		pods, err := env.Kube.Client.CoreV1().Pods(env.SystemNS).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=argus-connector-gateway"})
		if err != nil {
			return "", "", "", err
		}
		sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
		for _, pod := range pods.Items {
			if owner != "" && pod.Name != owner && pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
				return owner, pod.Name, pod.Status.PodIP, nil
			}
		}
		time.Sleep(time.Second)
	}
	return "", "", "", fmt.Errorf("M6 could not select a non-owner Gateway")
}
