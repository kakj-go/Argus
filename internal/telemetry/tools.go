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
)

type Tools struct{ Service Service }

func (tools Tools) Register(registry *mcp.Registry) error {
	if registry == nil || tools.Service.Store == nil {
		return ErrUnavailable
	}
	registrations := []mcp.Metadata{
		tools.metadata("telemetry.collector.list", "telemetry.collector.read", collectorListInputSchema(), collectorListOutputSchema(), tools.collectors),
		tools.metadata("telemetry.collector.get", "telemetry.collector.read", idInputSchema("collector_id"), collectorOutputSchema(), tools.collector),
		tools.metadata("telemetry.metrics.query", "telemetry.query.metrics", telemetryQueryInputSchema("metrics"), metricOutputSchema(), tools.metrics),
		tools.metadata("telemetry.logs.query", "telemetry.query.logs", telemetryQueryInputSchema("logs"), logsOutputSchema(), tools.logs),
		tools.metadata("telemetry.traces.query", "telemetry.query.traces", telemetryQueryInputSchema("traces"), tracesOutputSchema(), tools.traces),
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
func (tools Tools) metrics(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, from, to, limit, err := toolQueryCommon(call.Input)
	if err != nil {
		return mcp.Result{}, err
	}
	metric := fmt.Sprint(call.Input["metric_name"])
	aggregation := fmt.Sprint(call.Input["aggregation"])
	items, meta, err := tools.Service.QueryMetrics(ctx, actor, ids, from, to, limit, "", metric, aggregation, 60, false)
	return mcp.Result{Structured: map[string]any{"series": items, "meta": meta}, Partial: meta.Partial}, err
}
func (tools Tools) logs(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, from, to, limit, err := toolQueryCommon(call.Input)
	if err != nil {
		return mcp.Result{}, err
	}
	items, meta, err := tools.Service.QueryLogs(ctx, actor, ids, from, to, limit, "", map[string]any{"service_name": fmt.Sprint(call.Input["service_name"]), "text": fmt.Sprint(call.Input["text"])}, false)
	return mcp.Result{Structured: map[string]any{"records": items, "meta": meta}, Partial: meta.Partial}, err
}
func (tools Tools) traces(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	actor, _, err := tools.actor(ctx, call)
	if err != nil {
		return mcp.Result{}, err
	}
	ids, from, to, limit, err := toolQueryCommon(call.Input)
	if err != nil {
		return mcp.Result{}, err
	}
	items, meta, err := tools.Service.QueryTraces(ctx, actor, ids, from, to, limit, "", map[string]any{"service_name": fmt.Sprint(call.Input["service_name"]), "operation": fmt.Sprint(call.Input["operation"])}, false)
	return mcp.Result{Structured: map[string]any{"traces": items, "meta": meta}, Partial: meta.Partial}, err
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

func telemetryQueryInputSchema(kind string) map[string]any {
	properties := map[string]any{"resource_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "uuid"}, "minItems": 1, "maxItems": 1000}, "from": map[string]any{"type": "string", "format": "date-time"}, "to": map[string]any{"type": "string", "format": "date-time"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100000}}
	required := []string{"resource_ids", "from", "to"}
	if kind == "metrics" {
		properties["metric_name"] = map[string]any{"type": "string"}
		properties["aggregation"] = map[string]any{"type": "string", "enum": []string{"avg", "min", "max", "sum", "count", "p50", "p95", "p99"}}
		required = append(required, "metric_name", "aggregation")
	} else {
		properties["service_name"] = map[string]any{"type": "string"}
		if kind == "logs" {
			properties["text"] = map[string]any{"type": "string"}
		} else {
			properties["operation"] = map[string]any{"type": "string"}
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
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
func metricOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"series": map[string]any{"type": "array"}, "meta": map[string]any{"type": "object"}}, "required": []string{"series", "meta"}}
}
func logsOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"records": map[string]any{"type": "array"}, "meta": map[string]any{"type": "object"}}, "required": []string{"records", "meta"}}
}
func tracesOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"traces": map[string]any{"type": "array"}, "meta": map[string]any{"type": "object"}}, "required": []string{"traces", "meta"}}
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
