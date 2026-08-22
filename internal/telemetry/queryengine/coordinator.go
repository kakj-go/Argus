package queryengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/prometheus/promql"
	promqlstats "github.com/prometheus/prometheus/util/stats"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	clickhouseproto "github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/kakj-go/Argus/internal/audit"
	postgresdb "github.com/kakj-go/Argus/internal/storage/postgres/db"
	kqlengine "github.com/kakj-go/Argus/internal/telemetry/queryengine/kql"
	promqlengine "github.com/kakj-go/Argus/internal/telemetry/queryengine/promql"
	skywalking "github.com/kakj-go/Argus/internal/telemetry/queryengine/skywalking"
)

type Language string

const (
	LanguagePromQL Language = "promql"
	LanguageKQL    Language = "kql"
	LanguageTrace  Language = "skywalking_graphql"
)

type Scope struct {
	EnterpriseID         uuid.UUID
	ResourceIDs          []uuid.UUID
	AuthorizationVersion int64
	SensitiveFields      bool
}

type Budget struct {
	MaxScanBytes   int64
	MaxRows        int
	MaxSamples     int
	MaxSeries      int
	MaxResultBytes int64
	Timeout        time.Duration
}

type Request struct {
	Language   Language
	Expression string
	Pipeline   string
	Operation  string
	Variables  map[string]any
	Instant    bool
	Start      time.Time
	End        time.Time
	Step       time.Duration
	Scope      Scope
	Budget     Budget
}

type QueryMeta struct {
	PlanHash      string
	Engine        string
	EngineVersion string
	ScannedBytes  int64
	ScannedRows   int64
	ReturnedRows  int64
	LoadedSamples int64
	ElapsedMillis int64
	Partial       bool
	Warnings      []string
}

type Result struct {
	Language   Language
	ResultType string
	Data       any
	Meta       QueryMeta
}

type Engine interface {
	Execute(context.Context, Request) (Result, error)
}

type AuditEvent struct {
	Language             Language
	EnterpriseID         uuid.UUID
	ResourceIDs          []uuid.UUID
	AuthorizationVersion int64
	PlanHash             string
	ExpressionHash       string
	StartedAt            time.Time
	Elapsed              time.Duration
	Success              bool
	Error                string
	Meta                 QueryMeta
}

type AuditSink interface {
	Record(context.Context, AuditEvent) error
}

type SlogAuditSink struct{ Logger *slog.Logger }

func (sink SlogAuditSink) Record(_ context.Context, event AuditEvent) error {
	if sink.Logger == nil {
		return nil
	}
	sink.Logger.Info("telemetry query audit", "language", event.Language, "enterprise_id", event.EnterpriseID,
		"resource_count", len(event.ResourceIDs), "authorization_version", event.AuthorizationVersion,
		"plan_hash", event.PlanHash, "expression_hash", event.ExpressionHash, "elapsed_ms", event.Elapsed.Milliseconds(),
		"success", event.Success, "error", event.Error, "returned_rows", event.Meta.ReturnedRows,
		"scanned_bytes", event.Meta.ScannedBytes, "scanned_rows", event.Meta.ScannedRows, "loaded_samples", event.Meta.LoadedSamples)
	return nil
}

// PersistentAuditSink appends query audit events to the same tamper-evident
// PostgreSQL audit chain used by the rest of Argus. Query text is never stored;
// only its SHA-256 digest and bounded execution metadata are persisted.
type PersistentAuditSink struct {
	Store interface {
		InTx(context.Context, func(*postgresdb.Queries) error) error
	}
	Logger *slog.Logger
}

func (sink PersistentAuditSink) Record(ctx context.Context, event AuditEvent) error {
	if sink.Store == nil {
		return errors.New("query audit postgres store unavailable")
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = sink.record(ctx, event)
		if err == nil {
			return nil
		}
		if !retryableAuditTransaction(err) || attempt == 4 {
			break
		}
		delay := 10 * time.Millisecond * time.Duration(1<<attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	if sink.Logger != nil {
		sink.Logger.Error("telemetry query audit append failed", "enterprise_id", event.EnterpriseID, "error", err)
	}
	return err
}

func (sink PersistentAuditSink) record(ctx context.Context, event AuditEvent) error {
	enterpriseID := uuid.NullUUID{UUID: event.EnterpriseID, Valid: event.EnterpriseID != uuid.Nil}
	result := "success"
	if !event.Success {
		result = "failure"
	}
	return sink.Store.InTx(ctx, func(queries *postgresdb.Queries) error {
		if err := audit.InitializeChain(ctx, queries, "enterprise", enterpriseID); err != nil {
			return err
		}
		_, err := audit.Append(ctx, queries, audit.Entry{
			Domain: "enterprise", EnterpriseID: enterpriseID, ActorType: "system",
			ActorID: "argus-telemetry-query", Action: "telemetry.query.execute",
			ResourceType: "telemetry_query", Result: result,
			Details: map[string]any{
				"language":              string(event.Language),
				"expression_hash":       event.ExpressionHash,
				"plan_hash":             event.PlanHash,
				"authorization_version": event.AuthorizationVersion,
				"resource_count":        len(event.ResourceIDs),
				"elapsed_ms":            event.Elapsed.Milliseconds(),
				"scanned_bytes":         event.Meta.ScannedBytes,
				"scanned_rows":          event.Meta.ScannedRows,
				"loaded_samples":        event.Meta.LoadedSamples,
				"returned_rows":         event.Meta.ReturnedRows,
				"success":               event.Success,
				"error":                 persistentAuditError(event),
			},
		})
		return err
	})
}

func retryableAuditTransaction(err error) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return false
	}
	return pgError.Code == "40001" || pgError.Code == "40P01"
}

func persistentAuditError(event AuditEvent) string {
	if event.Success {
		return ""
	}
	return "query_failed"
}

type Coordinator struct {
	PromQL     Engine
	KQL        Engine
	Trace      Engine
	Policy     Policy
	Audit      AuditSink
	Logger     *slog.Logger
	once       sync.Once
	promQLGate chan struct{}
	kqlGate    chan struct{}
	traceGate  chan struct{}
}

type Policy struct {
	PromQLConcurrency int
	KQLConcurrency    int
	TraceConcurrency  int
}

var (
	ErrUnsupportedLanguage = errors.New("query language unsupported")
	ErrBudget              = errors.New("query budget exceeded")
)

func (c *Coordinator) Execute(ctx context.Context, request Request) (Result, error) {
	if c == nil {
		return Result{}, ErrUnsupportedLanguage
	}
	if request.Scope.EnterpriseID == uuid.Nil || request.Expression == "" {
		return Result{}, errors.New("invalid query request")
	}
	if request.Budget.Timeout <= 0 {
		request.Budget.Timeout = 10 * time.Second
	}
	if request.Budget.MaxRows <= 0 {
		request.Budget.MaxRows = 50_000
	}
	if request.Budget.MaxSamples <= 0 {
		request.Budget.MaxSamples = 5_000_000
	}
	if request.Budget.MaxSeries <= 0 {
		request.Budget.MaxSeries = 100_000
	}
	if request.Budget.MaxScanBytes <= 0 {
		request.Budget.MaxScanBytes = 256 << 20
	}
	if request.Budget.MaxResultBytes <= 0 {
		request.Budget.MaxResultBytes = 8 << 20
	}
	started := time.Now()
	expressionHash := hashExpression(request.Expression)
	audit := func(result Result, execErr error) {
		if c.Audit == nil {
			return
		}
		_ = c.Audit.Record(context.Background(), AuditEvent{Language: request.Language, EnterpriseID: request.Scope.EnterpriseID,
			ResourceIDs: request.Scope.ResourceIDs, AuthorizationVersion: request.Scope.AuthorizationVersion, PlanHash: result.Meta.PlanHash,
			ExpressionHash: expressionHash, StartedAt: started, Elapsed: time.Since(started), Success: execErr == nil,
			Error: errorString(execErr), Meta: result.Meta})
	}
	var engine Engine
	switch request.Language {
	case LanguagePromQL:
		engine = c.PromQL
	case LanguageKQL:
		engine = c.KQL
	case LanguageTrace:
		engine = c.Trace
	default:
		audit(Result{}, ErrUnsupportedLanguage)
		return Result{}, ErrUnsupportedLanguage
	}
	if engine == nil {
		audit(Result{}, ErrUnsupportedLanguage)
		return Result{}, ErrUnsupportedLanguage
	}
	release, err := c.acquire(ctx, request.Language)
	if err != nil {
		audit(Result{}, err)
		return Result{}, err
	}
	defer release()
	queryCtx, cancel := context.WithTimeout(ctx, request.Budget.Timeout)
	defer cancel()
	result, err := engine.Execute(queryCtx, request)
	if err != nil {
		err = normalizeExecutionError(err)
		c.logExecutionError(request, expressionHash, err)
		if errors.Is(err, context.DeadlineExceeded) {
			audit(Result{}, ErrBudget)
			return Result{}, ErrBudget
		}
		audit(Result{}, err)
		return Result{}, err
	}
	if result.Meta.ElapsedMillis > request.Budget.Timeout.Milliseconds() {
		audit(Result{}, ErrBudget)
		return Result{}, ErrBudget
	}
	if result.Meta.PlanHash == "" {
		result.Meta.PlanHash = planHash(request)
	}
	if result.Meta.ReturnedRows == 0 {
		result.Meta.ReturnedRows = resultRows(result.Data)
	}
	if result.Meta.Warnings == nil {
		result.Meta.Warnings = []string{}
	}
	result.Data = projectData(request.Language, result.Data, request.Scope.SensitiveFields)
	encoded, marshalErr := json.Marshal(result.Data)
	if marshalErr != nil {
		audit(Result{}, marshalErr)
		return Result{}, marshalErr
	}
	if int64(len(encoded)) > request.Budget.MaxResultBytes {
		audit(Result{}, ErrBudget)
		return Result{}, ErrBudget
	}
	audit(result, nil)
	return result, nil
}

func normalizeExecutionError(err error) error {
	if err == nil || errors.Is(err, ErrBudget) {
		return err
	}
	var exception *clickhouseproto.Exception
	if !errors.As(err, &exception) {
		return err
	}
	// Stable ClickHouse server error codes for enforced query-resource limits.
	// Keep the original exception in the chain for server-side diagnostics.
	switch exception.Code {
	case 158, // TOO_MANY_ROWS
		159, // TIMEOUT_EXCEEDED
		160, // TOO_SLOW
		191, // TOO_MANY_SIMULTANEOUS_QUERIES
		241, // MEMORY_LIMIT_EXCEEDED
		290, // LIMIT_EXCEEDED
		307, // TOO_MANY_BYTES
		394, // QUERY_WAS_CANCELLED
		396: // TOO_MANY_ROWS_OR_BYTES
		return errors.Join(ErrBudget, err)
	default:
		return err
	}
}

func (c *Coordinator) logExecutionError(request Request, expressionHash string, err error) {
	if c.Logger == nil || err == nil {
		return
	}
	c.Logger.Error("telemetry query engine failed",
		"language", request.Language,
		"enterprise_id", request.Scope.EnterpriseID,
		"resource_count", len(request.Scope.ResourceIDs),
		"authorization_version", request.Scope.AuthorizationVersion,
		"expression_hash", expressionHash,
		"error", err,
	)
}

func hashExpression(expression string) string {
	digest := sha256.Sum256([]byte(expression))
	return hex.EncodeToString(digest[:])
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func projectData(language Language, data any, sensitive bool) any {
	if sensitive || data == nil {
		return data
	}
	return projectValue(language, data)
}

func projectValue(language Language, value any) any {
	switch item := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(item))
		for _, row := range item {
			projected := make(map[string]any, len(row))
			for key, value := range row {
				if isSensitiveField(language, key) {
					projected[key] = "[REDACTED]"
					continue
				}
				projected[key] = projectValue(language, value)
			}
			out = append(out, projected)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, value := range item {
			if isSensitiveField(language, key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = projectValue(language, value)
		}
		return out
	case []any:
		out := make([]any, len(item))
		for index, value := range item {
			out[index] = projectValue(language, value)
		}
		return out
	default:
		return value
	}
}

func isSensitiveField(language Language, key string) bool {
	switch language {
	case LanguageKQL:
		return key == "body"
	case LanguageTrace:
		lower := strings.ToLower(key)
		return lower == "attributes" || lower == "resourceattributes" || lower == "events" || lower == "links"
	default:
		return false
	}
}

func (c *Coordinator) acquire(ctx context.Context, language Language) (func(), error) {
	c.once.Do(func() {
		if c.Policy.PromQLConcurrency <= 0 {
			c.Policy.PromQLConcurrency = 4
		}
		if c.Policy.KQLConcurrency <= 0 {
			c.Policy.KQLConcurrency = 8
		}
		if c.Policy.TraceConcurrency <= 0 {
			c.Policy.TraceConcurrency = 4
		}
		c.promQLGate = make(chan struct{}, c.Policy.PromQLConcurrency)
		c.kqlGate = make(chan struct{}, c.Policy.KQLConcurrency)
		c.traceGate = make(chan struct{}, c.Policy.TraceConcurrency)
	})
	gate := c.promQLGate
	if language == LanguageKQL {
		gate = c.kqlGate
	}
	if language == LanguageTrace {
		gate = c.traceGate
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ErrBudget
	}
}

func planHash(request Request) string {
	payload, _ := json.Marshal(struct {
		Language                        Language `json:"language"`
		Expression, Pipeline, Operation string
		Instant                         bool
		EnterpriseID                    uuid.UUID
		ResourceIDs                     []uuid.UUID
		Start, End                      time.Time
		Step                            time.Duration
		Budget                          Budget
	}{request.Language, request.Expression, request.Pipeline, request.Operation, request.Instant, request.Scope.EnterpriseID, request.Scope.ResourceIDs, request.Start, request.End, request.Step, request.Budget})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func resultRows(value any) int64 {
	if value == nil {
		return 0
	}
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.Map {
		return int64(v.Len())
	}
	return 1
}

type PromQLEngine struct {
	Engine *promqlengine.Engine
}

type KQLEngine struct {
	Conn   driver.Conn
	Router interface {
		Table(string, uuid.UUID) (string, error)
	}
}

func (e KQLEngine) Execute(ctx context.Context, request Request) (Result, error) {
	router, ok := e.Router.(interface {
		Table(string, uuid.UUID) (string, error)
	})
	if !ok {
		return Result{}, errors.New("kql tenant router unavailable")
	}
	result, err := kqlengine.Execute(ctx, e.Conn, routerAdapter{router}, kqlengine.Request{
		Expression: request.Expression, Pipeline: request.Pipeline, Start: request.Start, End: request.End,
		Scope:  kqlengine.Scope{EnterpriseID: request.Scope.EnterpriseID, ResourceIDs: request.Scope.ResourceIDs},
		Budget: kqlengine.Budget{MaxRows: request.Budget.MaxRows, MaxScanBytes: request.Budget.MaxScanBytes, Timeout: request.Budget.Timeout},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Language: LanguageKQL, ResultType: "log_entries", Data: result.Data, Meta: QueryMeta{Engine: "argus-kql", EngineVersion: "v1", ScannedBytes: result.ScannedBytes, ScannedRows: result.ScannedRows, ReturnedRows: int64(len(result.Data)), ElapsedMillis: result.Elapsed.Milliseconds(), Warnings: result.Warnings}}, nil
}

type TraceEngine struct {
	Engine skywalking.Engine
}

func (e TraceEngine) Execute(ctx context.Context, request Request) (Result, error) {
	result, err := e.Engine.Execute(ctx, skywalking.Request{Document: request.Expression, OperationName: request.Operation, Variables: request.Variables, Start: request.Start, End: request.End,
		Scope: skywalking.Scope{EnterpriseID: request.Scope.EnterpriseID, ResourceIDs: request.Scope.ResourceIDs}, Budget: skywalking.Budget{MaxRows: request.Budget.MaxRows, MaxScanBytes: request.Budget.MaxScanBytes, Timeout: request.Budget.Timeout, MaxResultBytes: request.Budget.MaxResultBytes, MaxRelationExpansions: request.Budget.MaxRows}})
	if err != nil {
		return Result{}, err
	}
	return Result{Language: LanguageTrace, ResultType: "traces", Data: result.Data, Meta: QueryMeta{Engine: "skywalking-graphql", EngineVersion: "v1", ScannedBytes: result.ScannedBytes, ScannedRows: result.ScannedRows, ElapsedMillis: result.Elapsed.Milliseconds(), Warnings: result.Errors}}, nil
}

type routerAdapter struct {
	router interface {
		Table(string, uuid.UUID) (string, error)
	}
}

func (r routerAdapter) Table(name string, id uuid.UUID) (string, error) {
	return r.router.Table(name, id)
}

func (e PromQLEngine) Execute(ctx context.Context, request Request) (Result, error) {
	result, err := e.Engine.Execute(ctx, promqlengine.Request{Expression: request.Expression, Instant: request.Instant, Start: request.Start, End: request.End, Step: request.Step, Scope: promqlengine.Scope{EnterpriseID: request.Scope.EnterpriseID, ResourceIDs: request.Scope.ResourceIDs}, MaxSamples: request.Budget.MaxSamples, MaxSeries: request.Budget.MaxSeries, MaxScanBytes: request.Budget.MaxScanBytes, Timeout: request.Budget.Timeout})
	if err != nil {
		return Result{}, err
	}
	resultType := "vector"
	switch result.Value.(type) {
	case promql.Scalar:
		resultType = "scalar"
	case promql.String:
		resultType = "string"
	case promql.Matrix:
		resultType = "matrix"
	case promql.Vector:
		resultType = "vector"
	}
	meta := QueryMeta{Engine: "prometheus-promql", EngineVersion: "v0.314.0", ScannedBytes: result.ScannedBytes, ScannedRows: result.ScannedRows, ElapsedMillis: result.Elapsed.Milliseconds(), Warnings: result.Warnings}
	if stats, ok := result.Stats.(*promqlstats.Statistics); ok && stats != nil && stats.Samples != nil {
		meta.LoadedSamples = stats.Samples.SamplesRead
	}
	return Result{Language: LanguagePromQL, ResultType: resultType, Data: result.Value, Meta: meta}, nil
}
