package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

type Tools struct{ Service Service }

func (tools Tools) Register(registry *mcp.Registry) error {
	if registry == nil || tools.Service.Store == nil {
		return ErrUnavailable
	}
	registrations := []mcp.Metadata{
		tools.metadata("telemetry.collector.list", "telemetry.collector.read", collectorListInputSchema(), collectorListOutputSchema(), tools.collectors),
		tools.metadata("telemetry.collector.get", "telemetry.collector.read", idInputSchema("collector_id"), collectorOutputSchema(), tools.collector),
		tools.metadata("telemetry.promql.query", "telemetry.query.metrics", dslQueryInputSchema("promql", "metrics"), promQLOutputSchema(), func(ctx context.Context, call mcp.Call) (mcp.Result, error) {
			return tools.dslQuery(ctx, call, queryengine.LanguagePromQL)
		}),
		tools.metadata("telemetry.kql.query", "telemetry.query.logs", dslQueryInputSchema("kql", "logs"), kqlOutputSchema(), func(ctx context.Context, call mcp.Call) (mcp.Result, error) {
			return tools.dslQuery(ctx, call, queryengine.LanguageKQL)
		}),
		tools.metadata("telemetry.skywalking.trace", "telemetry.query.traces", dslQueryInputSchema("skywalking_graphql", "traces"), traceGraphQLOutputSchema(), func(ctx context.Context, call mcp.Call) (mcp.Result, error) {
			return tools.dslQuery(ctx, call, queryengine.LanguageTrace)
		}),
		tools.metadata("telemetry.overview", "telemetry.query.metrics", overviewInputSchema(), overviewOutputSchema(), tools.overview),
	}
	for _, metadata := range registrations {
		if err := registry.Register(metadata); err != nil {
			return err
		}
	}
	return nil
}

func (tools Tools) metadata(id, permission string, input, output map[string]any, execute func(context.Context, mcp.Call) (mcp.Result, error)) mcp.Metadata {
	return mcp.Metadata{ID: id, ToolFamily: id, Risk: "read", Visibility: mcp.Visible, ExecutionMode: mcp.ParallelSafe,
		Required: []string{permission}, InputVersion: id + "/v1", OutputVersion: id + "/v1", ProjectionSchema: "argus.tool_result_projection/v1",
		MaxResultBytes: 4 << 20, InputSchema: input, OutputSchema: output, CardSafe: true, FieldTypes: telemetryFieldTypes(id), SemanticFields: telemetrySemanticFields(id),
		Authorize: tools.authorize(permission), Validate: func(value map[string]any) error {
			if value == nil {
				return ErrQueryInvalid
			}
			return nil
		}, Execute: execute,
		CardProjector: func(_ context.Context, _ mcp.Call, result mcp.Result) (map[string]any, bool, error) {
			encoded, err := json.Marshal(result.Structured)
			if err != nil {
				return nil, false, err
			}
			var output map[string]any
			err = json.Unmarshal(encoded, &output)
			return output, result.Partial, err
		},
	}
}

func (tools Tools) authorize(permission string) func(context.Context, mcp.Call) error {
	return func(ctx context.Context, call mcp.Call) error {
		actor, permissions, err := tools.actor(ctx, call)
		_ = actor
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, "*") && !slices.Contains(permissions, permission) {
			return fmt.Errorf("missing permission %s", permission)
		}
		return nil
	}
}

func (tools Tools) actor(ctx context.Context, call mcp.Call) (Actor, []string, error) {
	enterpriseID, err := uuid.Parse(call.Enterprise)
	if err != nil {
		return Actor{}, nil, err
	}
	subjectID, err := uuid.Parse(call.Subject)
	if err != nil {
		return Actor{}, nil, err
	}
	if call.SubjectType == "service_account" {
		account, err := tools.Service.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: subjectID, EnterpriseID: enterpriseID})
		if err != nil || account.Status != "active" || !slices.Contains(account.AllowedToolIds, call.ToolID) {
			return Actor{}, nil, errors.New("service account cannot use telemetry Tool")
		}
		permissions, err := tools.Service.Store.Queries.ListEffectiveServiceAccountPermissions(ctx, db.ListEffectiveServiceAccountPermissionsParams{EnterpriseID: enterpriseID, ServiceAccountID: subjectID})
		if err != nil {
			return Actor{}, nil, err
		}
		scopes, err := tools.Service.Store.Queries.ListServiceAccountDataScopes(ctx, subjectID)
		return Actor{EnterpriseID: enterpriseID, SubjectID: subjectID, AuthorizationVersion: account.AuthorizationVersion, DataScopeIDs: scopes}, permissions, err
	}
	user, err := tools.Service.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: subjectID, EnterpriseID: enterpriseID})
	if err != nil || user.Status != "active" {
		return Actor{}, nil, errors.New("telemetry Tool user unavailable")
	}
	permissions, err := tools.Service.Store.Queries.ListEffectiveUserPermissions(ctx, db.ListEffectiveUserPermissionsParams{EnterpriseID: enterpriseID, UserID: subjectID, DepartmentID: user.DepartmentID})
	if err != nil {
		return Actor{}, nil, err
	}
	scopes, err := tools.Service.Store.Queries.ListEffectiveUserDataScopes(ctx, db.ListEffectiveUserDataScopesParams{EnterpriseID: enterpriseID, UserID: subjectID, DepartmentID: user.DepartmentID})
	return Actor{EnterpriseID: enterpriseID, SubjectID: subjectID, AuthorizationVersion: user.AuthorizationVersion, DataScopeIDs: scopes}, permissions, err
}

func (tools Tools) collectors(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	items, err := tools.Service.ListCollectors(ctx, actor, "", uuid.NullUUID{}, 100)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: map[string]any{"items": items}}, nil
}
func (tools Tools) collector(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	id, err := uuid.Parse(fmt.Sprint(call.Input["collector_id"]))
	if err != nil {
		return mcp.Result{}, ErrQueryInvalid
	}
	item, err := tools.Service.GetCollector(ctx, actor, id)
	return mcp.Result{Structured: map[string]any{"collector": item}}, err
}
func (tools Tools) dslQuery(ctx context.Context, call mcp.Call, language queryengine.Language) (mcp.Result, error) {
	actor, permissions, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, from, to, limit, err := toolQueryCommon(call.Input)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, partial, err := tools.Service.AuthorizedResources(ctx, actor, ids)
	if err != nil {
		return mcp.Result{}, err
	}
	if partial {
		return mcp.Result{}, ErrQueryScope
	}
	maxScanBytes := int64(DefaultMaxScanBytes)
	if value, ok := call.Input["max_scan_bytes"].(float64); ok && value > 0 {
		maxScanBytes = int64(value)
	}
	maxResultBytes := int64(8 << 20)
	if value, ok := call.Input["max_result_bytes"].(float64); ok && value > 0 {
		maxResultBytes = int64(value)
	}
	timeout := DefaultTimeout
	if value, ok := call.Input["timeout_ms"].(float64); ok && value > 0 {
		timeout = time.Duration(value) * time.Millisecond
	}
	maxSamples := DefaultMaxSamples
	if value, ok := call.Input["max_samples"].(float64); ok && value > 0 {
		maxSamples = int(value)
	}
	maxSeries := DefaultMaxSeries
	if value, ok := call.Input["max_series"].(float64); ok && value > 0 {
		maxSeries = int(value)
	}
	step := time.Duration(0)
	if value, ok := call.Input["step_seconds"].(float64); ok && value > 0 {
		step = time.Duration(value) * time.Second
	}
	if tools.Service.Engine == nil {
		return mcp.Result{}, ErrQueryBackend
	}
	pipeline := ""
	if value, ok := call.Input["pipeline"].(string); ok {
		pipeline = value
	}
	expression := fmt.Sprint(call.Input["query"])
	if language == queryengine.LanguageTrace {
		expression = fmt.Sprint(call.Input["document"])
	}
	sensitive := slices.Contains(permissions, "*") || slices.Contains(permissions, "telemetry.sensitive_fields.read")
	result, err := tools.Service.Engine.ExecuteEngineQuery(ctx, queryengine.Request{Language: language, Expression: expression, Pipeline: pipeline, Instant: language == queryengine.LanguagePromQL && step <= 0, Start: from, End: to, Step: step,
		Scope: queryengine.Scope{EnterpriseID: actor.EnterpriseID, ResourceIDs: ids, AuthorizationVersion: actor.AuthorizationVersion, SensitiveFields: sensitive}, Budget: queryengine.Budget{MaxRows: limit, MaxSamples: maxSamples, MaxSeries: maxSeries, MaxScanBytes: maxScanBytes, MaxResultBytes: maxResultBytes, Timeout: timeout}})
	if err != nil {
		return mcp.Result{}, err
	}
	output := protocolQueryOutput(result)
	return mcp.Result{Structured: output, Partial: result.Meta.Partial}, nil
}
func (tools Tools) overview(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, err := inputUUIDs(call.Input["resource_ids"])
	if err != nil {
		return mcp.Result{}, err
	}
	lookback := 3600
	if value, ok := call.Input["lookback_seconds"].(float64); ok {
		lookback = int(value)
	}
	item, err := tools.Service.QueryOverview(ctx, actor, ids, lookback)
	encoded, _ := json.Marshal(item)
	var output map[string]any
	_ = json.Unmarshal(encoded, &output)
	return mcp.Result{Structured: output, Partial: item.Partial}, err
}

func toolQueryCommon(input map[string]any) ([]uuid.UUID, time.Time, time.Time, int, error) {
	ids, err := inputUUIDs(input["resource_ids"])
	if err != nil {
		return nil, time.Time{}, time.Time{}, 0, err
	}
	from, err := time.Parse(time.RFC3339, fmt.Sprint(input["from"]))
	if err != nil {
		return nil, time.Time{}, time.Time{}, 0, err
	}
	to, err := time.Parse(time.RFC3339, fmt.Sprint(input["to"]))
	limit := 5000
	if value, ok := input["limit"].(float64); ok {
		limit = int(value)
	}
	return ids, from, to, limit, err
}
func inputUUIDs(value any) ([]uuid.UUID, error) {
	values, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			values = make([]any, len(strings))
			for i := range strings {
				values[i] = strings[i]
			}
		} else {
			return nil, ErrQueryInvalid
		}
	}
	result := make([]uuid.UUID, 0, len(values))
	for _, item := range values {
		id, err := uuid.Parse(fmt.Sprint(item))
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func dslQueryInputSchema(language, signal string) map[string]any {
	queryField := "query"
	if language == "skywalking_graphql" {
		queryField = "document"
	}
	properties := map[string]any{
		queryField:     map[string]any{"type": "string", "minLength": 1, "maxLength": 65536},
		"pipeline":     map[string]any{"type": "string", "maxLength": 16384},
		"resource_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "uuid"}, "minItems": 1, "maxItems": 1000},
		"from":         map[string]any{"type": "string", "format": "date-time"}, "to": map[string]any{"type": "string", "format": "date-time"},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100000}, "step_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 86400},
		"max_scan_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": HardMaxScanBytes}, "timeout_ms": map[string]any{"type": "integer", "minimum": 100, "maximum": int(HardTimeout / time.Millisecond)},
		"max_result_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": HardMaxResultBytes},
		"language":         map[string]any{"const": language}, "signal": map[string]any{"const": signal},
	}
	if language == "promql" {
		properties["max_samples"] = map[string]any{"type": "integer", "minimum": 1, "maximum": HardMaxSamples}
		properties["max_series"] = map[string]any{"type": "integer", "minimum": 1, "maximum": HardMaxSeries}
	}
	return map[string]any{"type": "object", "properties": properties, "required": []string{queryField, "resource_ids", "from", "to"}, "additionalProperties": false}
}

func promQLOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"const": "success"}, "data": map[string]any{"type": "object"}, "warnings": map[string]any{"type": "array"}, "argus_meta": map[string]any{"type": "object"}}, "required": []string{"status", "data", "warnings", "argus_meta"}}
}

func kqlOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"schema_version": map[string]any{"const": "argus.kql_result/v1"}, "result_type": map[string]any{"type": "string"}, "data": map[string]any{}, "warnings": map[string]any{"type": "array"}, "partial": map[string]any{"type": "boolean"}, "meta": map[string]any{"type": "object"}}, "required": []string{"schema_version", "result_type", "data", "warnings", "partial", "meta"}}
}

func traceGraphQLOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"data": map[string]any{"type": "object"}, "errors": map[string]any{"type": "array"}, "extensions": map[string]any{"type": "object"}}, "required": []string{"data", "extensions"}}
}

func protocolQueryOutput(result queryengine.Result) map[string]any {
	meta := map[string]any{
		"plan_hash": result.Meta.PlanHash, "engine": result.Meta.Engine, "engine_version": result.Meta.EngineVersion,
		"scanned_bytes": result.Meta.ScannedBytes, "scanned_rows": result.Meta.ScannedRows, "returned_rows": result.Meta.ReturnedRows,
		"loaded_samples": result.Meta.LoadedSamples, "elapsed_ms": result.Meta.ElapsedMillis, "partial": result.Meta.Partial,
	}
	warnings := result.Meta.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	switch result.Language {
	case queryengine.LanguagePromQL:
		return map[string]any{"status": "success", "data": map[string]any{"resultType": result.ResultType, "result": result.Data}, "warnings": warnings, "argus_meta": meta}
	case queryengine.LanguageTrace:
		return map[string]any{"data": result.Data, "extensions": map[string]any{"argus": meta}}
	default:
		return map[string]any{"schema_version": "argus.kql_result/v1", "result_type": result.ResultType, "data": result.Data, "warnings": warnings, "partial": result.Meta.Partial, "meta": meta}
	}
}
func overviewInputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"resource_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "uuid"}, "minItems": 1, "maxItems": 1000}, "lookback_seconds": map[string]any{"type": "integer", "minimum": 300, "maximum": 604800}}, "required": []string{"resource_ids"}, "additionalProperties": false}
}
func collectorListInputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}
func idInputSchema(name string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string", "format": "uuid"}}, "required": []string{name}, "additionalProperties": false}
}
func collectorListOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": collectorOutputSchema()}}, "required": []string{"items"}}
}
func collectorOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "resource_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}}, "required": []string{"id", "resource_id", "status"}}
}
func overviewOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"resource_count": map[string]any{"type": "integer"}, "healthy_collectors": map[string]any{"type": "integer"}, "degraded_collectors": map[string]any{"type": "integer"}, "metric_points": map[string]any{"type": "integer"}, "log_records": map[string]any{"type": "integer"}, "spans": map[string]any{"type": "integer"}, "window_seconds": map[string]any{"type": "integer"}, "partial": map[string]any{"type": "boolean"}}, "required": []string{"resource_count", "healthy_collectors", "degraded_collectors", "metric_points", "log_records", "spans", "window_seconds", "partial"}}
}
func telemetryFieldTypes(id string) map[string]string {
	if id == "telemetry.overview" {
		return map[string]string{"$": "object", "$.metric_points": "number", "$.log_records": "number", "$.spans": "number"}
	}
	return map[string]string{"$": "object"}
}
func telemetrySemanticFields(id string) map[string]string {
	if id == "telemetry.overview" {
		return map[string]string{"$": "telemetry_overview"}
	}
	return map[string]string{"$": "telemetry_query_result"}
}
