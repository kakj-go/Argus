package kql

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/telemetry/queryengine/chstats"
)

type Scope struct {
	EnterpriseID uuid.UUID
	ResourceIDs  []uuid.UUID
}
type Budget struct {
	MaxRows      int
	MaxScanBytes int64
	Timeout      time.Duration
}
type Request struct {
	Expression, Pipeline string
	Start, End           time.Time
	Scope                Scope
	Budget               Budget
}
type Result struct {
	Data         []map[string]any
	Warnings     []string
	Elapsed      time.Duration
	ScannedBytes int64
	ScannedRows  int64
}

type TableRouter interface {
	Table(string, uuid.UUID) (string, error)
}

type Op string

const (
	OpEqual    Op = "="
	OpNotEqual Op = "!="
	OpGT       Op = ">"
	OpGTE      Op = ">="
	OpLT       Op = "<"
	OpLTE      Op = "<="
	OpContains Op = ":"
	OpExists   Op = "exists"
)

type Expr interface{ exprNode() }
type Predicate struct {
	Field string
	Op    Op
	Value string
}

func (Predicate) exprNode() {}

type BoolExpr struct {
	Left  Expr
	Op    string
	Right Expr
}

func (BoolExpr) exprNode() {}

type NotExpr struct{ Inner Expr }

func (NotExpr) exprNode() {}

type Parser struct {
	tokens []string
	index  int
}

func Parse(input string) (Expr, error) {
	tokens, err := lex(input)
	if err != nil {
		return nil, err
	}
	parser := &Parser{tokens: tokens}
	expr, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.index != len(tokens) {
		return nil, fmt.Errorf("unexpected token %q", tokens[parser.index])
	}
	return expr, nil
}
func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek("OR") {
		p.index++
		right, e := p.parseAnd()
		if e != nil {
			return nil, e
		}
		left = BoolExpr{Left: left, Op: "OR", Right: right}
	}
	return left, nil
}
func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek("AND") {
		p.index++
		right, e := p.parseUnary()
		if e != nil {
			return nil, e
		}
		left = BoolExpr{Left: left, Op: "AND", Right: right}
	}
	return left, nil
}
func (p *Parser) parseUnary() (Expr, error) {
	if p.peek("NOT") {
		p.index++
		inner, e := p.parseUnary()
		return NotExpr{Inner: inner}, e
	}
	if p.peek("(") {
		p.index++
		inner, e := p.parseOr()
		if e != nil {
			return nil, e
		}
		if !p.peek(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.index++
		return inner, nil
	}
	return p.parsePredicate()
}
func (p *Parser) parsePredicate() (Expr, error) {
	if p.index >= len(p.tokens) {
		return nil, fmt.Errorf("predicate expected")
	}
	field := p.tokens[p.index]
	p.index++
	if p.index >= len(p.tokens) {
		return nil, fmt.Errorf("operator expected")
	}
	op := Op(p.tokens[p.index])
	p.index++
	if strings.EqualFold(string(op), string(OpExists)) {
		return Predicate{Field: field, Op: OpExists}, nil
	}
	if p.index >= len(p.tokens) {
		return nil, fmt.Errorf("value expected")
	}
	value := p.tokens[p.index]
	p.index++
	return Predicate{Field: field, Op: op, Value: value}, nil
}
func (p *Parser) peek(value string) bool {
	return p.index < len(p.tokens) && strings.EqualFold(p.tokens[p.index], value)
}

func lex(input string) ([]string, error) {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range input {
		if quoted {
			if r == '"' {
				quoted = false
				flush()
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '"' {
			flush()
			quoted = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		if strings.ContainsRune("()", r) {
			flush()
			out = append(out, string(r))
			continue
		}
		if r == ':' {
			flush()
			out = append(out, ":")
			continue
		}
		if strings.ContainsRune("=!<>", r) {
			if b.Len() > 0 && strings.ContainsRune("=!<>", r) {
				b.WriteRune(r)
				continue
			}
			flush()
			b.WriteRune(r)
			continue
		}
		b.WriteRune(r)
	}
	if quoted {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return out, nil
}

type Compiler struct {
	Table  string
	Args   []any
	Parser string
}

func (c *Compiler) Compile(expr Expr) (string, error) {
	switch node := expr.(type) {
	case Predicate:
		return c.predicate(node)
	case BoolExpr:
		l, e := c.Compile(node.Left)
		if e != nil {
			return "", e
		}
		r, e := c.Compile(node.Right)
		if e != nil {
			return "", e
		}
		return "(" + l + " " + node.Op + " " + r + ")", nil
	case NotExpr:
		inner, e := c.Compile(node.Inner)
		if e != nil {
			return "", e
		}
		return "NOT (" + inner + ")", nil
	default:
		return "", fmt.Errorf("unsupported expression")
	}
}
func (c *Compiler) predicate(p Predicate) (string, error) {
	if p.Op == OpExists {
		return c.exists(p.Field)
	}
	field, key, ok := allowedFieldForParser(p.Field, c.Parser)
	if !ok {
		return "", fmt.Errorf("field %q is not queryable", p.Field)
	}
	if key != "" {
		c.Args = append(c.Args, key)
	}
	switch p.Op {
	case OpContains:
		if strings.ContainsAny(p.Value, "*?") {
			pattern := strings.ReplaceAll(strings.ReplaceAll(p.Value, "%", "\\%"), "*", "%")
			pattern = strings.ReplaceAll(pattern, "?", "_")
			c.Args = append(c.Args, pattern)
			return "lower(" + field + ") LIKE lower(?) ESCAPE '\\'", nil
		}
		c.Args = append(c.Args, p.Value)
		return "positionCaseInsensitive(" + field + ", ?)>0", nil
	case OpEqual, OpNotEqual, OpGT, OpGTE, OpLT, OpLTE:
		c.Args = append(c.Args, typedValue(p.Value))
		return field + " " + string(p.Op) + " ?", nil
	default:
		return "", fmt.Errorf("operator %q is unsupported", p.Op)
	}
}

func (c *Compiler) exists(field string) (string, error) {
	switch field {
	case "body", "severity_text", "service_name", "trace_id", "span_id":
		column, _, _ := allowedField(field)
		return "notEmpty(" + column + ")", nil
	case "timestamp", "severity_number":
		return "1", nil
	}
	for prefix, column := range map[string]string{
		"stream_labels.":       "stream_labels",
		"structured_metadata.": "structured_metadata",
		"resource_attributes.": "resource_attributes",
	} {
		if key, ok := strings.CutPrefix(field, prefix); ok && key != "" {
			c.Args = append(c.Args, key)
			return "mapContains(" + column + ", ?)", nil
		}
	}
	return "", fmt.Errorf("field %q is not queryable", field)
}
func allowedField(field string) (string, string, bool) {
	switch field {
	case "body":
		return "body", "", true
	case "timestamp":
		return "timestamp", "", true
	case "severity_text":
		return "severity_text", "", true
	case "severity_number":
		return "severity_number", "", true
	case "service_name":
		return "service_name", "", true
	case "trace_id":
		return "trace_id", "", true
	case "span_id":
		return "span_id", "", true
	default:
		if strings.HasPrefix(field, "stream_labels.") {
			return "stream_labels[?]", strings.TrimPrefix(field, "stream_labels."), true
		}
		if strings.HasPrefix(field, "structured_metadata.") {
			return "structured_metadata[?]", strings.TrimPrefix(field, "structured_metadata."), true
		}
		if strings.HasPrefix(field, "resource_attributes.") {
			return "resource_attributes[?]", strings.TrimPrefix(field, "resource_attributes."), true
		}
		return "", "", false
	}
}

func allowedFieldForParser(field, parser string) (string, string, bool) {
	if value, key, ok := allowedField(field); ok {
		return value, key, true
	}
	if parser == "json" && strings.HasPrefix(field, "json.") {
		return "JSONExtractString(body, ?)", strings.TrimPrefix(field, "json."), true
	}
	if parser == "logfmt" && strings.HasPrefix(field, "logfmt.") {
		return "extractKeyValue(body, ?)", strings.TrimPrefix(field, "logfmt."), true
	}
	return "", "", false
}
func typedValue(value string) any {
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

func Execute(ctx context.Context, conn driver.Conn, router TableRouter, request Request) (Result, error) {
	if conn == nil {
		return Result{}, fmt.Errorf("kql storage unavailable")
	}
	if request.Scope.EnterpriseID == uuid.Nil {
		return Result{}, fmt.Errorf("enterprise id required")
	}
	expr, err := Parse(request.Expression)
	if err != nil {
		return Result{}, err
	}
	table, err := router.Table("logs", request.Scope.EnterpriseID)
	if err != nil {
		return Result{}, err
	}
	compiler := Compiler{Table: table}
	where, err := compiler.Compile(expr)
	if err != nil {
		return Result{}, err
	}
	args := []any{request.Start, request.End}
	args = append(args, compiler.Args...)
	order := "timestamp DESC"
	limit := request.Budget.MaxRows
	parserName, unwrapField, statsBy := "", "", ""
	if request.Pipeline != "" {
		where, order, limit, parserName, unwrapField, statsBy, err = compilePipelineOptions(request.Pipeline, where, order, limit, &args)
		if err != nil {
			return Result{}, err
		}
	}
	if len(request.Scope.ResourceIDs) > 0 {
		where += " AND resource_id IN (?)"
		args = append(args, request.Scope.ResourceIDs)
	}
	query := fmt.Sprintf("SELECT timestamp, resource_id, severity_text, severity_number, service_name, body, trace_id, span_id FROM `%s` WHERE timestamp >= ? AND timestamp < ? AND %s ORDER BY %s LIMIT ?", table, where, order)
	if statsBy != "" {
		field, key, ok := allowedFieldForParser(statsBy, parserName)
		if !ok {
			return Result{}, fmt.Errorf("stats field %q is not queryable", statsBy)
		}
		if key != "" {
			args = append([]any{key}, args...)
		}
		args = append(args, limit)
		query = fmt.Sprintf("SELECT %s AS group_value, count() FROM `%s` WHERE timestamp >= ? AND timestamp < ? AND %s GROUP BY group_value ORDER BY count() DESC LIMIT ?", field, table, where)
	} else {
		args = append(args, limit)
	}
	started := time.Now()
	progress := &chstats.Tracker{}
	settings := clickhouse.Settings{"max_result_rows": limit, "max_execution_time": max(1, int(request.Budget.Timeout.Seconds()))}
	if request.Budget.MaxScanBytes > 0 {
		settings["max_bytes_to_read"] = request.Budget.MaxScanBytes
	}
	rows, err := conn.Query(progress.Context(ctx, settings), query, args...)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	result := Result{Data: make([]map[string]any, 0)}
	for rows.Next() {
		if statsBy != "" {
			var fieldValue string
			var count uint64
			if err := rows.Scan(&fieldValue, &count); err != nil {
				return Result{}, err
			}
			result.Data = append(result.Data, map[string]any{"field": statsBy, "value": fieldValue, "count": count})
			continue
		}
		var timestamp time.Time
		var resourceID uuid.UUID
		var severity string
		var severityNumber uint8
		var service, body, traceID, spanID string
		if err := rows.Scan(&timestamp, &resourceID, &severity, &severityNumber, &service, &body, &traceID, &spanID); err != nil {
			return Result{}, err
		}
		result.Data = append(result.Data, map[string]any{"timestamp": timestamp, "resource_id": resourceID, "severity_text": severity, "severity_number": severityNumber, "service_name": service, "body": body, "trace_id": traceID, "span_id": spanID})
		if parserName != "" {
			parsed := parsePipelineBody(body, parserName)
			item := result.Data[len(result.Data)-1]
			item["parsed_fields"] = parsed
			if unwrapField != "" {
				key := unwrapField
				if strings.HasPrefix(key, "pattern.") {
					key = strings.TrimPrefix(key, "pattern.")
				}
				item["unwrap"] = typedValue(parsed[key])
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Elapsed = time.Since(started)
	result.ScannedBytes = progress.Bytes()
	result.ScannedRows = progress.Rows()
	return result, nil
}

func compilePipeline(pipeline, where, order string, limit int, args *[]any) (string, string, int, error) {
	where, order, limit, _, _, _, err := compilePipelineOptions(pipeline, where, order, limit, args)
	return where, order, limit, err
}

func compilePipelineOptions(pipeline, where, order string, limit int, args *[]any) (string, string, int, string, string, string, error) {
	parserName, unwrapField, statsBy := "", "", ""
	for _, raw := range strings.Split(pipeline, "|") {
		stage := strings.TrimSpace(raw)
		if stage == "" {
			continue
		}
		lower := strings.ToLower(stage)
		switch {
		case lower == "parse json":
			parserName = "json"
		case lower == "parse logfmt":
			parserName = "logfmt"
		case strings.HasPrefix(lower, "parse pattern"):
			pattern := strings.TrimSpace(stage[len("parse pattern"):])
			pattern = strings.Trim(pattern, "\"")
			if pattern == "" || len(pattern) > 2048 {
				return "", "", 0, "", "", "", fmt.Errorf("parse pattern requires a bounded pattern")
			}
			parserName = "pattern:" + pattern
		case strings.HasPrefix(lower, "unwrap "):
			unwrapField = strings.TrimSpace(stage[len("unwrap "):])
			if unwrapField == "" {
				return "", "", 0, "", "", "", fmt.Errorf("unwrap field is required")
			}
			if strings.HasPrefix(parserName, "pattern:") {
				if !strings.HasPrefix(unwrapField, "pattern.") || strings.TrimPrefix(unwrapField, "pattern.") == "" {
					return "", "", 0, "", "", "", fmt.Errorf("pattern unwrap requires pattern.<field>")
				}
			} else if _, _, ok := allowedFieldForParser(unwrapField, parserName); !ok {
				return "", "", 0, "", "", "", fmt.Errorf("unwrap field %q is not queryable", unwrapField)
			}
		case strings.HasPrefix(lower, "stats "):
			value := strings.TrimSpace(stage[len("stats "):])
			if !strings.HasPrefix(strings.ToLower(value), "count() by ") {
				return "", "", 0, "", "", "", fmt.Errorf("only stats count() by field is supported")
			}
			statsBy = strings.TrimSpace(value[len("count() by "):])
			if _, _, ok := allowedFieldForParser(statsBy, parserName); !ok {
				return "", "", 0, "", "", "", fmt.Errorf("stats field %q is not queryable", statsBy)
			}
		case strings.HasPrefix(lower, "where "):
			expr, err := Parse(strings.TrimSpace(stage[len("where "):]))
			if err != nil {
				return "", "", 0, "", "", "", err
			}
			compiler := Compiler{Parser: parserName}
			compiled, err := compiler.Compile(expr)
			if err != nil {
				return "", "", 0, "", "", "", err
			}
			where += " AND (" + compiled + ")"
			*args = append(*args, compiler.Args...)
		case strings.HasPrefix(lower, "sort "):
			parts := strings.Fields(lower)
			if len(parts) != 3 || parts[1] != "timestamp" || (parts[2] != "asc" && parts[2] != "desc") {
				return "", "", 0, "", "", "", fmt.Errorf("unsupported sort pipeline")
			}
			order = "timestamp " + strings.ToUpper(parts[2])
		case strings.HasPrefix(lower, "limit "):
			value, err := strconv.Atoi(strings.TrimSpace(stage[len("limit "):]))
			if err != nil || value < 1 || value > limit {
				return "", "", 0, "", "", "", fmt.Errorf("pipeline limit exceeds budget")
			}
			limit = value
		default:
			return "", "", 0, "", "", "", fmt.Errorf("unsupported pipeline stage %q", stage)
		}
	}
	return where, order, limit, parserName, unwrapField, statsBy, nil
}

func parseStructuredBody(body, parser string) map[string]string {
	result := map[string]string{}
	if parser == "json" {
		var values map[string]any
		if json.Unmarshal([]byte(body), &values) == nil {
			for key, value := range values {
				result[key] = fmt.Sprint(value)
			}
		}
	}
	if parser == "logfmt" {
		for _, token := range strings.Fields(body) {
			parts := strings.SplitN(token, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = strings.Trim(parts[1], "\"")
			}
		}
	}
	return result
}

func parsePipelineBody(body, parser string) map[string]string {
	if strings.HasPrefix(parser, "pattern:") {
		return parsePatternBody(body, strings.TrimPrefix(parser, "pattern:"))
	}
	return parseStructuredBody(body, parser)
}

// parsePatternBody supports the bounded KQL pattern form where captures are
// written as <name>, for example `request <method> <path> <status>`. It is
// intentionally a literal matcher and never becomes a user-provided SQL
// expression.
func parsePatternBody(body, pattern string) map[string]string {
	result := map[string]string{}
	var literals []string
	var names []string
	for {
		start := strings.IndexByte(pattern, '<')
		if start < 0 {
			literals = append(literals, pattern)
			break
		}
		end := strings.IndexByte(pattern[start+1:], '>')
		if end < 0 {
			return result
		}
		end += start + 1
		literals = append(literals, pattern[:start])
		name := strings.TrimSpace(pattern[start+1 : end])
		if name == "" || strings.ContainsAny(name, " <>\t\r\n") {
			return result
		}
		names = append(names, name)
		pattern = pattern[end+1:]
	}
	if len(names) == 0 || len(literals) != len(names)+1 {
		return result
	}
	position := 0
	for i, name := range names {
		literal := literals[i]
		if literal != "" {
			index := strings.Index(body[position:], literal)
			if index < 0 {
				return result
			}
			position += index + len(literal)
		}
		next := len(body)
		if literals[i+1] != "" {
			index := strings.Index(body[position:], literals[i+1])
			if index < 0 {
				return result
			}
			next = position + index
		}
		result[name] = body[position:next]
		position = next
	}
	return result
}
