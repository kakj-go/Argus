package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func telemetryTimeRange(lookback time.Duration) map[string]any {
	now := time.Now().UTC()
	return map[string]any{"from": now.Add(-lookback).Format(time.RFC3339), "to": now.Add(time.Minute).Format(time.RFC3339)}
}

func telemetryBudget(rows int64) map[string]any {
	return map[string]any{"max_scan_bytes": int64(256 << 20), "max_rows": rows, "max_samples": int64(5_000_000), "max_series": int64(100_000), "timeout_ms": int64(10_000), "max_result_bytes": int64(8 << 20)}
}

func (a *App) verifyM7Signals(ctx context.Context, env *E2EEnvironment, resourceID, marker string) error {
	metric := "argus_m7_e2e_gauge"
	logBody := "argus m7 e2e log"
	operation := "argus-m7-e2e"
	if marker != "" && marker != "base" {
		metric += "_" + strings.NewReplacer(".", "_", "-", "_").Replace(marker)
		logBody += "." + marker
		operation += "." + marker
	}
	if marker == "base" {
		metric += "_base"
		logBody += ".base"
		operation += ".base"
	}
	if err := a.waitM7Metric(ctx, env, resourceID, metric); err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	logs, err := client.JSON(ctx, "m7-logs-"+marker, "enterprise", http.MethodPost, "/enterprise/logs/query", http.StatusOK, map[string]any{
		"query": `service_name = argus-m7-e2e AND body : "` + logBody + `"`, "resource_ids": []string{resourceID},
		"time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	if err := assertKQLResponse(logs, logBody, 0); err != nil {
		return fmt.Errorf("M7 log signal: %w", err)
	}
	traces, err := client.JSON(ctx, "m7-traces-"+marker, "enterprise", http.MethodPost, "/enterprise/traces/graphql", http.StatusOK, map[string]any{
		"query":        fmt.Sprintf(`query { queryBasicTracesByName(serviceName: "argus-m7-e2e", operationName: %q, pageSize: 100) { total traces { traceId rootService rootOperation spans { spanId operationName } } } }`, operation),
		"resource_ids": []string{resourceID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	return assertTraceResponse(traces, operation)
}

func (a *App) waitM7Metric(ctx context.Context, env *E2EEnvironment, resourceID, metric string) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		result, err := client.JSON(ctx, "m7-metric-"+kubernetesNameForDev(metric), "enterprise", http.MethodPost, "/enterprise/metrics/query", http.StatusOK, map[string]any{
			"query": metric, "resource_ids": []string{resourceID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
		}, enterpriseHeaders(env, ""))
		if err != nil {
			return err
		}
		if prometheusResultContains(result, metric) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("M7 metric %s did not traverse Collector, Kafka, Writer, ClickHouse, and Query", metric)
}

func prometheusResultContains(response map[string]any, metric string) bool {
	if response["status"] != "success" || !validQueryMeta(response["argus_meta"]) {
		return false
	}
	data, _ := response["data"].(map[string]any)
	items, _ := data["result"].([]any)
	for _, item := range items {
		series, _ := item.(map[string]any)
		labels, _ := series["metric"].(map[string]any)
		if labels["__name__"] == metric {
			return true
		}
	}
	return false
}

func assertKQLResponse(response map[string]any, expectedBody string, expectedRows int) error {
	if response["schema_version"] != "argus.kql_result/v1" || response["result_type"] != "log_entries" || !validQueryMeta(response["meta"]) {
		return fmt.Errorf("invalid KQL response envelope")
	}
	items, ok := response["data"].([]any)
	if !ok {
		return fmt.Errorf("KQL data is not an array")
	}
	if expectedRows > 0 && len(items) != expectedRows {
		return fmt.Errorf("expected %d KQL rows, got %d", expectedRows, len(items))
	}
	if expectedBody == "" {
		return nil
	}
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if entry["body"] == expectedBody {
			return nil
		}
	}
	return fmt.Errorf("expected log body was not returned")
}

func assertTraceResponse(response map[string]any, operation string) error {
	extensions, _ := response["extensions"].(map[string]any)
	if !validQueryMeta(extensions["argus"]) {
		return fmt.Errorf("invalid trace query metadata")
	}
	data, _ := response["data"].(map[string]any)
	query, _ := data["queryBasicTracesByName"].(map[string]any)
	traces, _ := query["traces"].([]any)
	for _, traceValue := range traces {
		trace, _ := traceValue.(map[string]any)
		spans, _ := trace["spans"].([]any)
		for _, spanValue := range spans {
			span, _ := spanValue.(map[string]any)
			if span["operationName"] == operation {
				return nil
			}
		}
	}
	return fmt.Errorf("expected trace operation was not returned")
}

func validQueryMeta(value any) bool {
	meta, ok := value.(map[string]any)
	if !ok {
		return false
	}
	planHash, _ := meta["plan_hash"].(string)
	_, partialOK := meta["partial"].(bool)
	_, bytesOK := meta["scanned_bytes"].(float64)
	_, elapsedOK := meta["elapsed_ms"].(float64)
	return len(planHash) == 64 && partialOK && bytesOK && elapsedOK
}

func (a *App) verifyM7NodeBinding(ctx context.Context, env *E2EEnvironment, clusterID, hostID, distributionID string, profiles []string) error {
	client, _ := scenarioHTTP(env)
	bindings, err := client.JSONArray(ctx, "m7-node-bindings", "enterprise", http.MethodGet, "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id="+clusterID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return fmt.Errorf("Kubernetes Collector produced no Node/Host binding evidence")
	}
	bindingID, err := stringField(bindings[0], "id")
	if err != nil {
		return err
	}
	version, err := numberField(bindings[0], "version")
	if err != nil {
		return err
	}
	preview, err := client.JSON(ctx, "m7-node-binding-preview", "enterprise", http.MethodPost, "/enterprise/telemetry/node-host-bindings/"+bindingID+"/actions/preview-confirm", http.StatusCreated,
		map[string]any{"host_id": hostID, "expected_version": version}, enterpriseHeaders(env, "m7-node-binding"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	if _, err := a.confirmPendingAction(ctx, env, "m7-node-binding-confirm", actionRef); err != nil {
		return err
	}
	if err := a.applyM7CollectorAction(ctx, env, "kubernetes-cluster", clusterID, "configure", distributionID, profiles); err != nil {
		return err
	}
	stable, err := client.JSONArray(ctx, "m7-node-binding-stable", "enterprise", http.MethodGet, "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id="+clusterID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	for _, item := range stable {
		if item["id"] == bindingID && item["status"] == "verified" && item["matched_by"] == "manual" && item["host_id"] == hostID {
			if _, err := a.postgresQuery(ctx, env, "UPDATE kubernetes_node_host_bindings SET evidence_hash=decode(repeat('00',32),'hex') WHERE id='"+bindingID+"';"); err != nil {
				return err
			}
			if err := a.applyM7CollectorAction(ctx, env, "kubernetes-cluster", clusterID, "configure", distributionID, profiles); err != nil {
				return err
			}
			drifted, err := client.JSONArray(ctx, "m7-node-binding-drifted", "enterprise", http.MethodGet, "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id="+clusterID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
			if err != nil {
				return err
			}
			for _, changed := range drifted {
				if changed["id"] == bindingID && changed["status"] == "proposed" {
					claims, err := client.JSONArray(ctx, "m7-cluster-claims-after-drift", "enterprise", http.MethodGet, "/enterprise/telemetry/collection-claims?resource_id="+clusterID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
					if err != nil {
						return err
					}
					for _, claim := range claims {
						if claim["physical_resource_ref"] == "kubernetes_cluster:"+clusterID && claim["status"] == "active" {
							return nil
						}
					}
					return fmt.Errorf("invalidated Node binding did not restore Kubernetes cluster Claims")
				}
			}
			return fmt.Errorf("changed Kubernetes Node evidence did not invalidate the verified binding")
		}
	}
	return fmt.Errorf("unchanged Kubernetes Node evidence did not preserve the verified manual binding")
}

func (a *App) runM10QueryScenario(ctx context.Context, env *E2EEnvironment) error {
	clusterID := env.State.Values["m3_cluster_id"]
	client, _ := scenarioHTTP(env)
	instant, err := client.JSON(ctx, "m10-promql-instant", "enterprise", http.MethodPost, "/enterprise/metrics/query", http.StatusOK, map[string]any{
		"query": "argus_m7_e2e_gauge_base", "resource_ids": []string{clusterID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	data, _ := instant["data"].(map[string]any)
	if instant["status"] != "success" || data["resultType"] != "vector" || !validQueryMeta(instant["argus_meta"]) {
		return fmt.Errorf("M10 PromQL instant response does not match the wire contract")
	}
	rangeResult, err := client.JSON(ctx, "m10-promql-range", "enterprise", http.MethodPost, "/enterprise/metrics/query_range", http.StatusOK, map[string]any{
		"query": "argus_m7_e2e_native_histogram_base", "resource_ids": []string{clusterID}, "time_range": telemetryTimeRange(15 * time.Minute), "step_seconds": 60, "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	rangeData, _ := rangeResult["data"].(map[string]any)
	if rangeResult["status"] != "success" || rangeData["resultType"] != "matrix" || !validQueryMeta(rangeResult["argus_meta"]) {
		return fmt.Errorf("M10 PromQL range response does not match the wire contract")
	}
	trace, err := client.JSON(ctx, "m10-trace-edges", "enterprise", http.MethodPost, "/enterprise/traces/graphql", http.StatusOK, map[string]any{
		"query":        `query { queryBasicTraces(serviceName: "argus-m7-e2e", pageSize: 10) { total traces { traceId edges { parentSpanId childSpanId depth } } } }`,
		"resource_ids": []string{clusterID}, "time_range": telemetryTimeRange(15 * time.Minute), "budget": telemetryBudget(100),
	}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	extensions, _ := trace["extensions"].(map[string]any)
	if !validQueryMeta(extensions["argus"]) {
		return fmt.Errorf("M10 SkyWalking GraphQL response does not match the wire contract")
	}
	audit, err := client.JSON(ctx, "m10-query-audit", "enterprise", http.MethodGet, "/enterprise/audit-events", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	for _, event := range objectItems(audit) {
		if event["action"] != "telemetry.query.execute" || event["result"] != "success" {
			continue
		}
		details, _ := event["details"].(map[string]any)
		if len(asString(details["expression_hash"])) == 64 && len(asString(details["plan_hash"])) == 64 && details["query"] == nil && details["expression"] == nil && details["document"] == nil {
			return nil
		}
	}
	return fmt.Errorf("M10 query audit evidence is missing or contains raw query text")
}

func asString(value any) string {
	result, _ := value.(string)
	return result
}
