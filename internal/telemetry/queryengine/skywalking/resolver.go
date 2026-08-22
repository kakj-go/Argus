package skywalking

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	queryerrors "github.com/graph-gophers/graphql-go/errors"
)

type executionStateKey struct{}

type traceTables struct {
	summary string
	spans   string
	edges   string
}

type executionState struct {
	engine  Engine
	request Request
	tables  traceTables
}

func withExecutionState(ctx context.Context, state *executionState) context.Context {
	return context.WithValue(ctx, executionStateKey{}, state)
}

func executionStateFromContext(ctx context.Context) (*executionState, error) {
	state, ok := ctx.Value(executionStateKey{}).(*executionState)
	if !ok || state == nil {
		return nil, fmt.Errorf("trace graphql execution state unavailable")
	}
	return state, nil
}

type rootResolver struct{}

type traceTagInput struct {
	Key   string
	Value string
}

type basicTraceArgs struct {
	ServiceName         *string
	ServiceInstanceName *string
	Tags                *[]traceTagInput
	DurationMin         *float64
	DurationMax         *float64
	Order               *string
	PageNum             *int32
	PageSize            *int32
}

type namedTraceArgs struct {
	ServiceName         *string
	ServiceInstanceName *string
	OperationName       *string
	Tags                *[]traceTagInput
	DurationMin         *float64
	DurationMax         *float64
	Order               *string
	PageNum             *int32
	PageSize            *int32
}

type statusTraceArgs struct {
	ServiceName         *string
	ServiceInstanceName *string
	Status              *string
	Tags                *[]traceTagInput
	DurationMin         *float64
	DurationMax         *float64
	Order               *string
	PageNum             *int32
	PageSize            *int32
}

type traceFilter struct {
	serviceName, serviceInstanceName, operationName, status *string
	tags                                                    *[]traceTagInput
	durationMin, durationMax                                *float64
	order                                                   *string
	pageNum, pageSize                                       *int32
}

func (*rootResolver) QueryBasicTraces(ctx context.Context, args basicTraceArgs) (*traceQueryResultResolver, error) {
	return resolveTracePage(ctx, traceFilter{serviceName: args.ServiceName, serviceInstanceName: args.ServiceInstanceName, tags: args.Tags, durationMin: args.DurationMin, durationMax: args.DurationMax, order: args.Order, pageNum: args.PageNum, pageSize: args.PageSize})
}

func (*rootResolver) QueryBasicTracesByName(ctx context.Context, args namedTraceArgs) (*traceQueryResultResolver, error) {
	return resolveTracePage(ctx, traceFilter{serviceName: args.ServiceName, serviceInstanceName: args.ServiceInstanceName, operationName: args.OperationName, tags: args.Tags, durationMin: args.DurationMin, durationMax: args.DurationMax, order: args.Order, pageNum: args.PageNum, pageSize: args.PageSize})
}

func (*rootResolver) QueryTraces(ctx context.Context, args statusTraceArgs) (*traceQueryResultResolver, error) {
	return resolveTracePage(ctx, traceFilter{serviceName: args.ServiceName, serviceInstanceName: args.ServiceInstanceName, status: args.Status, tags: args.Tags, durationMin: args.DurationMin, durationMax: args.DurationMax, order: args.Order, pageNum: args.PageNum, pageSize: args.PageSize})
}

func (*rootResolver) QueryTrace(ctx context.Context, args struct{ TraceID string }) (*traceResolver, error) {
	state, err := executionStateFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if args.TraceID == "" || len(args.TraceID) > 256 {
		return nil, fmt.Errorf("invalid trace id")
	}
	item, err := state.queryTrace(ctx, args.TraceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &traceResolver{state: state, item: item}, nil
}

func resolveTracePage(ctx context.Context, filter traceFilter) (*traceQueryResultResolver, error) {
	state, err := executionStateFromContext(ctx)
	if err != nil {
		return nil, err
	}
	total, items, err := state.queryTracePage(ctx, filter)
	if err != nil {
		return nil, err
	}
	resolvers := make([]*traceResolver, 0, len(items))
	for _, item := range items {
		resolvers = append(resolvers, &traceResolver{state: state, item: item})
	}
	return &traceQueryResultResolver{total: boundedInt32(total), traces: resolvers}, nil
}

type traceQueryResultResolver struct {
	total  int32
	traces []*traceResolver
}

func (r *traceQueryResultResolver) Total() int32             { return r.total }
func (r *traceQueryResultResolver) Traces() []*traceResolver { return r.traces }

type traceRecord struct {
	traceID, rootService, rootOperation, status string
	startTime                                   time.Time
	duration                                    uint64
	spanCount, errorCount                       uint32
}

type traceResolver struct {
	state *executionState
	item  traceRecord
}

func (r *traceResolver) TraceID() string       { return r.item.traceID }
func (r *traceResolver) RootService() string   { return r.item.rootService }
func (r *traceResolver) RootOperation() string { return r.item.rootOperation }
func (r *traceResolver) StartTime() string     { return r.item.startTime.Format(time.RFC3339Nano) }
func (r *traceResolver) Duration() float64     { return float64(r.item.duration) / 1e6 }
func (r *traceResolver) SpanCount() int32      { return boundedInt32(uint64(r.item.spanCount)) }
func (r *traceResolver) ErrorCount() int32     { return boundedInt32(uint64(r.item.errorCount)) }
func (r *traceResolver) Status() string        { return r.item.status }

func (r *traceResolver) Spans(ctx context.Context) ([]*spanResolver, error) {
	items, err := r.state.querySpans(ctx, r.item.traceID)
	if err != nil {
		return nil, err
	}
	result := make([]*spanResolver, 0, len(items))
	for _, item := range items {
		result = append(result, &spanResolver{item: item})
	}
	return result, nil
}

func (r *traceResolver) Edges(ctx context.Context) ([]*edgeResolver, error) {
	items, err := r.state.queryEdges(ctx, r.item.traceID)
	if err != nil {
		return nil, err
	}
	result := make([]*edgeResolver, 0, len(items))
	for _, item := range items {
		result = append(result, &edgeResolver{item: item})
	}
	return result, nil
}

type spanRecord struct {
	spanID, parentSpanID, serviceName, operationName, status string
	startTime                                                time.Time
	duration                                                 uint64
	attributes                                               string
	events, links                                            string
}

type spanResolver struct{ item spanRecord }

func (r *spanResolver) SpanID() string        { return r.item.spanID }
func (r *spanResolver) ParentSpanID() string  { return r.item.parentSpanID }
func (r *spanResolver) ServiceName() string   { return r.item.serviceName }
func (r *spanResolver) OperationName() string { return r.item.operationName }
func (r *spanResolver) Status() string        { return r.item.status }
func (r *spanResolver) StartTime() string     { return r.item.startTime.Format(time.RFC3339Nano) }
func (r *spanResolver) Duration() float64     { return float64(r.item.duration) / 1e6 }
func (r *spanResolver) Attributes() string    { return r.item.attributes }
func (r *spanResolver) Events() string        { return r.item.events }
func (r *spanResolver) Links() string         { return r.item.links }

type edgeRecord struct {
	parentSpanID, childSpanID, parentService, childService string
	depth                                                  uint16
}

type edgeResolver struct{ item edgeRecord }

func (r *edgeResolver) ParentSpanID() string  { return r.item.parentSpanID }
func (r *edgeResolver) ChildSpanID() string   { return r.item.childSpanID }
func (r *edgeResolver) ParentService() string { return r.item.parentService }
func (r *edgeResolver) ChildService() string  { return r.item.childService }
func (r *edgeResolver) Depth() int32          { return int32(r.item.depth) }

func (state *executionState) queryTracePage(ctx context.Context, filter traceFilter) (uint64, []traceRecord, error) {
	request := state.request
	args := []any{request.Start, request.End}
	where := "start_time >= ? AND start_time < ?"
	if len(request.Scope.ResourceIDs) > 0 {
		where += " AND resource_id IN (?)"
		args = append(args, request.Scope.ResourceIDs)
	}
	if value := stringValue(filter.serviceName); value != "" {
		where += " AND root_service = ?"
		args = append(args, value)
	}
	if value := stringValue(filter.operationName); value != "" {
		where += " AND root_operation = ?"
		args = append(args, value)
	}
	if value := stringValue(filter.status); value != "" {
		where += " AND status = ?"
		args = append(args, value)
	}
	if filter.durationMin != nil {
		if *filter.durationMin < 0 {
			return 0, nil, fmt.Errorf("durationMin must be non-negative")
		}
		where += " AND duration_ns >= ?"
		args = append(args, uint64(*filter.durationMin*1e6))
	}
	if filter.durationMax != nil {
		if *filter.durationMax < 0 {
			return 0, nil, fmt.Errorf("durationMax must be non-negative")
		}
		where += " AND duration_ns <= ?"
		args = append(args, uint64(*filter.durationMax*1e6))
	}
	if filter.durationMin != nil && filter.durationMax != nil && *filter.durationMin > *filter.durationMax {
		return 0, nil, fmt.Errorf("duration range is invalid")
	}
	if value := stringValue(filter.serviceInstanceName); value != "" {
		where += " AND trace_id IN (SELECT trace_id FROM `" + state.tables.spans + "` WHERE start_time >= ? AND start_time < ? AND (resource_attributes[?] = ? OR resource_attributes[?] = ?) LIMIT ?)"
		args = append(args, request.Start, request.End, "service.instance.name", value, "service.instance.id", value, request.Budget.MaxRelationExpansions)
	}
	if filter.tags != nil && len(*filter.tags) > 0 {
		where += " AND trace_id IN (SELECT trace_id FROM `" + state.tables.spans + "` WHERE start_time >= ? AND start_time < ?"
		args = append(args, request.Start, request.End)
		for _, tag := range *filter.tags {
			if tag.Key == "" || len(tag.Key) > 128 || len(tag.Value) > 1024 {
				return 0, nil, fmt.Errorf("invalid trace tag")
			}
			where += " AND attributes[?] = ?"
			args = append(args, tag.Key, tag.Value)
		}
		where += " LIMIT ?)"
		args = append(args, request.Budget.MaxRelationExpansions)
	}
	order := "start_time DESC"
	switch strings.ToUpper(stringValue(filter.order)) {
	case "", "START_TIME_DESC":
	case "START_TIME_ASC":
		order = "start_time ASC"
	default:
		return 0, nil, fmt.Errorf("unsupported trace order")
	}
	limit := request.Budget.MaxRows
	if filter.pageSize != nil {
		if *filter.pageSize < 1 || int(*filter.pageSize) > limit {
			return 0, nil, fmt.Errorf("graphql page size exceeds budget")
		}
		limit = int(*filter.pageSize)
	}
	page := 1
	if filter.pageNum != nil {
		if *filter.pageNum < 1 || *filter.pageNum > 1_000_000 {
			return 0, nil, fmt.Errorf("graphql page number exceeds budget")
		}
		page = int(*filter.pageNum)
	}
	offset := (page - 1) * limit
	var total uint64
	countQuery := fmt.Sprintf("SELECT count() FROM `%s` WHERE %s", state.tables.summary, where)
	if err := state.engine.Conn.QueryRow(queryContext(ctx, request.Budget), countQuery, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	queryArgs := append(append([]any(nil), args...), limit, offset)
	query := fmt.Sprintf("SELECT trace_id, root_service, root_operation, start_time, duration_ns, span_count, error_count, status FROM `%s` WHERE %s ORDER BY %s LIMIT ? OFFSET ?", state.tables.summary, where, order)
	rows, err := state.engine.Conn.Query(queryContext(ctx, request.Budget), query, queryArgs...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := make([]traceRecord, 0, limit)
	for rows.Next() {
		var item traceRecord
		if err := rows.Scan(&item.traceID, &item.rootService, &item.rootOperation, &item.startTime, &item.duration, &item.spanCount, &item.errorCount, &item.status); err != nil {
			return 0, nil, err
		}
		items = append(items, item)
	}
	return total, items, rows.Err()
}

func (state *executionState) queryTrace(ctx context.Context, traceID string) (traceRecord, error) {
	request := state.request
	where := "trace_id = ?"
	args := []any{traceID}
	if len(request.Scope.ResourceIDs) > 0 {
		where += " AND resource_id IN (?)"
		args = append(args, request.Scope.ResourceIDs)
	}
	query := fmt.Sprintf("SELECT trace_id, root_service, root_operation, start_time, duration_ns, span_count, error_count, status FROM `%s` WHERE %s LIMIT 1", state.tables.summary, where)
	var item traceRecord
	err := state.engine.Conn.QueryRow(queryContext(ctx, request.Budget), query, args...).Scan(&item.traceID, &item.rootService, &item.rootOperation, &item.startTime, &item.duration, &item.spanCount, &item.errorCount, &item.status)
	return item, err
}

func (state *executionState) querySpans(ctx context.Context, traceID string) ([]spanRecord, error) {
	request := state.request
	where := "trace_id = ?"
	args := []any{traceID}
	if len(request.Scope.ResourceIDs) > 0 {
		where += " AND resource_id IN (?)"
		args = append(args, request.Scope.ResourceIDs)
	}
	args = append(args, request.Budget.MaxRelationExpansions)
	query := fmt.Sprintf("SELECT span_id, parent_span_id, service_name, operation, status, start_time, duration_ns, attributes, events, links FROM `%s` WHERE %s ORDER BY start_time LIMIT ?", state.tables.spans, where)
	rows, err := state.engine.Conn.Query(queryContext(ctx, request.Budget), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]spanRecord, 0)
	for rows.Next() {
		var item spanRecord
		var attributes map[string]string
		if err := rows.Scan(&item.spanID, &item.parentSpanID, &item.serviceName, &item.operationName, &item.status, &item.startTime, &item.duration, &attributes, &item.events, &item.links); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(attributes)
		if err != nil {
			return nil, err
		}
		item.attributes = string(encoded)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (state *executionState) queryEdges(ctx context.Context, traceID string) ([]edgeRecord, error) {
	request := state.request
	where := "trace_id = ?"
	args := []any{traceID}
	if len(request.Scope.ResourceIDs) > 0 {
		where += " AND resource_id IN (?)"
		args = append(args, request.Scope.ResourceIDs)
	}
	args = append(args, request.Budget.MaxRelationExpansions)
	query := fmt.Sprintf("SELECT parent_span_id, child_span_id, parent_service, child_service, depth FROM `%s` WHERE %s ORDER BY depth, parent_span_id, child_span_id LIMIT ?", state.tables.edges, where)
	rows, err := state.engine.Conn.Query(queryContext(ctx, request.Budget), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]edgeRecord, 0)
	for rows.Next() {
		var item edgeRecord
		if err := rows.Scan(&item.parentSpanID, &item.childSpanID, &item.parentService, &item.childService, &item.depth); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boundedInt32(value uint64) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(value)
}

func schemaErrors(items []*queryerrors.QueryError) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Message)
	}
	return result
}
