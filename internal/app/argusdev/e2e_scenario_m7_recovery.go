package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *App) verifyM7Recovery(ctx context.Context, env *E2EEnvironment, clusterID string) error {
	if err := env.Kube.ScaleDeployment(ctx, env.ObservNS, "argus-telemetry-writer", 0); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.ObservNS, "argus-telemetry-writer", 3*time.Minute); err != nil {
		return err
	}
	if err := a.runM7Generator(ctx, env, "backlog"); err != nil {
		return err
	}
	if err := env.Kube.ScaleDeployment(ctx, env.ObservNS, "argus-telemetry-writer", 1); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.ObservNS, "argus-telemetry-writer", 5*time.Minute); err != nil {
		return err
	}
	if err := a.waitM7Metric(ctx, env, clusterID, "argus_m7_e2e_gauge_backlog"); err != nil {
		return err
	}
	if err := a.verifyM7DLQ(ctx, env, clusterID); err != nil {
		return err
	}
	if err := env.Kube.ScaleStatefulSet(ctx, env.SystemNS, "argus-redis", 0); err != nil {
		return err
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-redis", 0, 3*time.Minute); err != nil {
		return err
	}
	if err := a.runM7Generator(ctx, env, "redis-recovery"); err != nil {
		return err
	}
	if err := env.Kube.ScaleStatefulSet(ctx, env.SystemNS, "argus-redis", 1); err != nil {
		return err
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-redis", 1, 5*time.Minute); err != nil {
		return err
	}
	if err := a.waitM7Metric(ctx, env, clusterID, "argus_m7_e2e_gauge_redis_recovery"); err != nil {
		return err
	}
	if err := a.verifyM7QueryBudgets(ctx, env, clusterID); err != nil {
		return err
	}
	if err := a.verifyM7Authorization(ctx, env); err != nil {
		return err
	}
	if err := env.Kube.DeletePods(ctx, env.ObservNS, "app.kubernetes.io/name=argus-telemetry-query"); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.ObservNS, "argus-telemetry-query", 5*time.Minute); err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		response, err := client.JSON(ctx, fmt.Sprintf("m7-query-after-restart-%d", attempt), "enterprise", http.MethodPost, "/enterprise/telemetry/query/overview", http.StatusOK,
			map[string]any{"resource_ids": []string{clusterID}, "lookback_seconds": 3600}, enterpriseHeaders(env, ""))
		if err == nil {
			return nil
		}
		if response["code"] != "TELEMETRY_DEPENDENCY_UNAVAILABLE" {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("M7 Telemetry Query did not recover after Pod restart: %w", lastErr)
}

func (a *App) verifyM7DLQ(ctx context.Context, env *E2EEnvironment, clusterID string) error {
	collectorID, err := a.postgresQuery(ctx, env, "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='"+clusterID+"' ORDER BY created_at DESC LIMIT 1;")
	if err != nil {
		return err
	}
	collectorID = strings.TrimSpace(collectorID)
	backoff := int32(1)
	injectPod := corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{
		Name: "inject", Image: env.State.FixtureImages["backend"], ImagePullPolicy: corev1.PullNever,
		Command: []string{"/usr/local/bin/argus-telemetry-e2e"}, Args: []string{
			"--kafka-brokers=argus-kafka-kafka-bootstrap." + env.ObservNS + ".svc:9093", "--kafka-username=argus-telemetry",
			"--enterprise-id=" + env.State.Values["enterprise_id"], "--resource-id=" + clusterID, "--collector-id=" + collectorID,
		}, Env: []corev1.EnvVar{{Name: "ARGUS_E2E_KAFKA_PASSWORD", ValueFrom: secretEnvSource("argus-telemetry", "password")}},
	}}}
	inject := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "argus-m7-dlq-inject", Namespace: env.ObservNS, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Spec:       batchv1.JobSpec{BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: injectPod}},
	}
	if err := env.Kube.RunJob(ctx, inject, 2*time.Minute); err != nil {
		return err
	}
	var recordID string
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		value, queryErr := a.postgresQuery(ctx, env, "SELECT id FROM telemetry_dlq_records WHERE status='pending' ORDER BY first_seen_at DESC LIMIT 1;")
		if queryErr != nil {
			return queryErr
		}
		recordID = strings.TrimSpace(value)
		if recordID != "" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if recordID == "" {
		return fmt.Errorf("M7 Writer did not isolate a permanent record in DLQ")
	}
	replayPod := corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ServiceAccountName: "argus-telemetry-writer", Containers: []corev1.Container{{
		Name: "replay", Image: env.State.FixtureImages["backend"], ImagePullPolicy: corev1.PullNever,
		Command: []string{"/usr/local/bin/argus-telemetry-dlq-replay"}, Args: []string{"--record-id=" + recordID}, Env: []corev1.EnvVar{
			{Name: "ARGUS_DATABASE_URL", ValueFrom: secretEnvSource("argus-telemetry-runtime", "writer-database-url")},
			{Name: "ARGUS_KAFKA_BROKERS", Value: "argus-kafka-kafka-bootstrap." + env.ObservNS + ".svc:9093"},
			{Name: "ARGUS_KAFKA_USERNAME", Value: "argus-telemetry"},
			{Name: "ARGUS_KAFKA_PASSWORD", ValueFrom: secretEnvSource("argus-telemetry", "password")},
		},
	}}}
	replay := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: kubernetesNameForDev("argus-m7-dlq-replay-" + recordID), Namespace: env.ObservNS, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Spec:       batchv1.JobSpec{BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: replayPod}},
	}
	if err := env.Kube.RunJob(ctx, replay, 3*time.Minute); err != nil {
		return err
	}
	status, err := a.postgresQuery(ctx, env, "SELECT status FROM telemetry_dlq_records WHERE id='"+recordID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "replayed" {
		return fmt.Errorf("M7 DLQ replay status is %q", strings.TrimSpace(status))
	}
	return nil
}

func secretEnvSource(name, key string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}}
}

func (a *App) verifyM7Authorization(ctx context.Context, env *E2EEnvironment) error {
	sensitive := "Authorization: Bearer m7-redaction-fixture"
	hostID := env.State.Values["m7_host_id"]
	if _, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-direct-executor", "argus-direct-executor",
		"/usr/local/bin/argus-telemetry-e2e", "--endpoint=127.0.0.1:4317", "--resource-id="+hostID, "--marker=redaction", "--log-body="+sensitive); err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(3 * time.Minute)
	foundSensitiveLog := false
	lastQueryErr := fmt.Errorf("query deadline expired before a valid response was observed")
	for time.Now().Before(deadline) {
		response, err := client.JSON(ctx, "m7-sensitive-raw", "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusOK, map[string]any{
			"query": `service_name = argus-m7-e2e AND body : "` + sensitive + `"`, "resource_ids": []string{hostID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(10),
		}, enterpriseHeaders(env, ""))
		if err != nil {
			return err
		}
		lastQueryErr = assertKQLResponse(response, sensitive, 0)
		if lastQueryErr == nil {
			foundSensitiveLog = true
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if !foundSensitiveLog {
		return fmt.Errorf("M7 sensitive telemetry fixture did not reach Query: %w", lastQueryErr)
	}
	mutation := "DELETE FROM role_permissions WHERE permission_id='telemetry.sensitive_fields.read' AND role_id=(SELECT id FROM roles WHERE enterprise_id='" + env.State.Values["enterprise_id"] + "' AND identity_key='enterprise_admin');" +
		"UPDATE enterprise_users SET authorization_version=authorization_version+1,updated_at=now() WHERE id='" + env.State.Values["admin_user_id"] + "' AND enterprise_id='" + env.State.Values["enterprise_id"] + "';"
	if _, err := a.postgresQuery(ctx, env, mutation); err != nil {
		return err
	}
	stale, err := client.JSON(ctx, "m7-stale-session", "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusConflict, map[string]any{
		"query": "service_name = argus-m7-e2e", "resource_ids": []string{hostID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	if stale["code"] != "AUTHORIZATION_VERSION_STALE" {
		return fmt.Errorf("M7 stale session returned %v", stale["code"])
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	redacted, err := client.JSON(ctx, "m7-sensitive-redacted", "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusOK, map[string]any{
		"query": "service_name = argus-m7-e2e", "resource_ids": []string{hostID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	items, _ := redacted["data"].([]any)
	foundRedaction := false
	for _, value := range items {
		entry, _ := value.(map[string]any)
		body, _ := entry["body"].(string)
		if body == sensitive {
			return fmt.Errorf("M7 sensitive telemetry body remained visible")
		}
		if body == "[REDACTED]" || body == "[redacted by telemetry field policy]" {
			foundRedaction = true
		}
	}
	if !foundRedaction {
		return fmt.Errorf("M7 sensitive telemetry body was not replaced by the field policy")
	}
	restore := "INSERT INTO role_permissions (role_id,permission_id) SELECT id,'telemetry.sensitive_fields.read' FROM roles WHERE enterprise_id='" + env.State.Values["enterprise_id"] + "' AND identity_key='enterprise_admin' ON CONFLICT DO NOTHING;" +
		"UPDATE enterprise_users SET authorization_version=authorization_version+1,updated_at=now() WHERE id='" + env.State.Values["admin_user_id"] + "' AND enterprise_id='" + env.State.Values["enterprise_id"] + "';"
	if _, err := a.postgresQuery(ctx, env, restore); err != nil {
		return err
	}
	return a.refreshEnterpriseLogin(ctx, env)
}

func (a *App) verifyM7QueryBudgets(ctx context.Context, env *E2EEnvironment, clusterID string) error {
	client, _ := scenarioHTTP(env)
	limited, err := client.JSON(ctx, "m7-query-limit", "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusOK, map[string]any{
		"query": "service_name = argus-m7-e2e", "resource_ids": []string{clusterID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(1),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	if err := assertKQLResponse(limited, "", 1); err != nil {
		return fmt.Errorf("M7 query row budget: %w", err)
	}
	budget := telemetryBudget(100)
	budget["max_scan_bytes"] = int64(1)
	response, err := client.JSON(ctx, "m7-query-budget", "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusRequestEntityTooLarge, map[string]any{
		"query": "service_name = argus-m7-e2e", "resource_ids": []string{clusterID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": budget,
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	if response["code"] != "QUERY_BUDGET_EXCEEDED" {
		return fmt.Errorf("M7 query scan budget returned %v", response["code"])
	}
	return nil
}
