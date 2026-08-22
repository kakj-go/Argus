package skywalking

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	graphgophers "github.com/graph-gophers/graphql-go"
	graphqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/kakj-go/Argus/internal/telemetry/queryengine/chstats"
)

type Scope struct {
	EnterpriseID uuid.UUID
	ResourceIDs  []uuid.UUID
}

type Budget struct {
	MaxRows               int
	MaxScanBytes          int64
	Timeout               time.Duration
	MaxDepth              int
	MaxFields             int
	MaxResultBytes        int64
	MaxRelationExpansions int
	progress              *chstats.Tracker
}

type Request struct {
	Document, OperationName string
	Variables               map[string]any
	Start, End              time.Time
	Scope                   Scope
	Budget                  Budget
}

type Result struct {
	Data         map[string]any
	Errors       []string
	Elapsed      time.Duration
	ScannedRows  int64
	ScannedBytes int64
}

type TableRouter interface {
	Table(string, uuid.UUID) (string, error)
}

type Engine struct {
	Conn   driver.Conn
	Router TableRouter
}

//go:embed schema/trace.graphql
var schemaFS embed.FS

var traceSchema = graphgophers.MustParseSchema(
	mustSchemaSource(),
	&rootResolver{},
	graphgophers.MaxDepth(64),
	graphgophers.MaxParallelism(1),
	graphgophers.DisableIntrospection(),
)

func mustSchemaSource() string {
	data, err := schemaFS.ReadFile("schema/trace.graphql")
	if err != nil {
		panic(err)
	}
	return string(data)
}

func (e Engine) Execute(ctx context.Context, request Request) (Result, error) {
	if e.Conn == nil || e.Router == nil || request.Scope.EnterpriseID == uuid.Nil {
		return Result{}, fmt.Errorf("trace graphql storage unavailable")
	}
	if request.Budget.MaxRows <= 0 {
		request.Budget.MaxRows = 50_000
	}
	if request.Budget.MaxDepth <= 0 {
		request.Budget.MaxDepth = 8
	}
	if request.Budget.MaxFields <= 0 {
		request.Budget.MaxFields = 100
	}
	if request.Budget.MaxResultBytes <= 0 {
		request.Budget.MaxResultBytes = 8 << 20
	}
	if request.Budget.MaxRelationExpansions <= 0 {
		request.Budget.MaxRelationExpansions = request.Budget.MaxRows
	}
	request.Budget.progress = &chstats.Tracker{}
	document, err := graphqlparser.Parse(graphqlparser.ParseParams{Source: request.Document})
	if err != nil {
		return Result{}, fmt.Errorf("graphql parse failed: %w", err)
	}
	depth, fields, err := validateDocument(document)
	if err != nil {
		return Result{}, err
	}
	if depth > request.Budget.MaxDepth {
		return Result{}, fmt.Errorf("graphql query depth exceeded")
	}
	if fields > request.Budget.MaxFields {
		return Result{}, fmt.Errorf("graphql field budget exceeded")
	}
	tables, err := e.tables(request.Scope.EnterpriseID)
	if err != nil {
		return Result{}, err
	}
	state := &executionState{engine: e, request: request, tables: tables}
	started := time.Now()
	response := traceSchema.Exec(withExecutionState(ctx, state), request.Document, request.OperationName, request.Variables)
	if len(response.Errors) > 0 {
		return Result{Errors: schemaErrors(response.Errors), Elapsed: time.Since(started), ScannedRows: request.Budget.progress.Rows(), ScannedBytes: request.Budget.progress.Bytes()}, fmt.Errorf("graphql query failed")
	}
	var data map[string]any
	if err := json.Unmarshal(response.Data, &data); err != nil {
		return Result{}, fmt.Errorf("graphql result invalid: %w", err)
	}
	if int64(len(response.Data)) > request.Budget.MaxResultBytes {
		return Result{}, fmt.Errorf("graphql result byte budget exceeded")
	}
	return Result{Data: data, Elapsed: time.Since(started), ScannedRows: request.Budget.progress.Rows(), ScannedBytes: request.Budget.progress.Bytes()}, nil
}

func (e Engine) tables(enterpriseID uuid.UUID) (traceTables, error) {
	summary, err := e.Router.Table("trace_summary", enterpriseID)
	if err != nil {
		return traceTables{}, err
	}
	spans, err := e.Router.Table("traces", enterpriseID)
	if err != nil {
		return traceTables{}, err
	}
	edges, err := e.Router.Table("trace_span_edges", enterpriseID)
	if err != nil {
		return traceTables{}, err
	}
	return traceTables{summary: summary, spans: spans, edges: edges}, nil
}

func queryContext(ctx context.Context, budget Budget) context.Context {
	settings := clickhouse.Settings{"max_result_rows": budget.MaxRows, "max_execution_time": max(1, int(budget.Timeout.Seconds()))}
	if budget.MaxScanBytes > 0 {
		settings["max_bytes_to_read"] = budget.MaxScanBytes
	}
	if budget.progress == nil {
		return clickhouse.Context(ctx, clickhouse.WithSettings(settings))
	}
	return budget.progress.Context(ctx, settings)
}
