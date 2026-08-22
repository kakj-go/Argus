package skywalking

import (
	"strings"
	"testing"

	graphqlparser "github.com/graphql-go/graphql/language/parser"
)

func TestGraphQLLimits(t *testing.T) {
	if got := documentDepth(`query { queryBasicTraces { traces { traceId } } }`); got != 3 {
		t.Fatalf("depth = %d, want 3", got)
	}
	if got := fieldCount(`query { queryBasicTraces { total traces { traceId } } }`); got == 0 {
		t.Fatal("expected fields to be counted")
	}
}

func TestSkyWalkingTraceSchemaOperationsStayBounded(t *testing.T) {
	for _, document := range []string{
		`query { queryBasicTraces { total traces { traceId } } }`,
		`query { queryBasicTracesByName(serviceName: "api") { traces { traceId } } }`,
		`query { queryTraces(status: "ERROR") { traces { traceId spans { spanId } } } }`,
		`query { queryTrace(traceId: "abc") { traceId spans { spanId parentSpanId } } }`,
	} {
		if documentDepth(document) > 8 || fieldCount(document) > 100 {
			t.Fatalf("operation unexpectedly exceeds limits: %s", document)
		}
	}
}

func TestValidateDocumentRejectsUnsafeOperations(t *testing.T) {
	for _, document := range []string{
		`mutation { queryTrace(traceId: "x") { traceId } }`,
		`query { __schema { queryType { name } } }`,
		`query { queryBasicTraces { ...TraceFields } } fragment TraceFields on TraceQueryResult { __typename }`,
		`query { queryBasicTraces { ...A } } fragment A on TraceQueryResult { ...B } fragment B on TraceQueryResult { ...A }`,
	} {
		parsed, err := graphqlparser.Parse(graphqlparser.ParseParams{Source: document})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := validateDocument(parsed); err == nil {
			t.Fatalf("expected unsafe document rejection: %s", document)
		}
	}
}

func TestValidateDocumentSupportsBoundedFragments(t *testing.T) {
	document, err := graphqlparser.Parse(graphqlparser.ParseParams{Source: `
		query TraceList { queryBasicTraces { ...TracePage } }
		fragment TracePage on TraceQueryResult { total traces { ... on Trace { traceId rootService } } }
	`})
	if err != nil {
		t.Fatal(err)
	}
	depth, fields, err := validateDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if depth < 3 || fields != 5 {
		t.Fatalf("unexpected fragment complexity depth=%d fields=%d", depth, fields)
	}
}

func TestTraceSchemaIsFixedSDL(t *testing.T) {
	source := mustSchemaSource()
	for _, token := range []string{"type Query", "queryBasicTraces", "queryBasicTracesByName", "queryTraces", "queryTrace", "input TraceTagInput"} {
		if !strings.Contains(source, token) {
			t.Fatalf("trace schema is missing %q", token)
		}
	}
	for _, document := range []string{
		`query { queryBasicTraces(serviceName: "api", tags: [{key: "http.method", value: "GET"}]) { total } }`,
		`query { queryBasicTracesByName(operationName: "GET /users") { traces { traceId } } }`,
		`query { queryTraces(status: "ERROR") { traces { spans { spanId } edges { childSpanId } } } }`,
		`query { queryTrace(traceId: "abc") { traceId } }`,
	} {
		if errors := traceSchema.Validate(document); len(errors) != 0 {
			t.Fatalf("fixed schema rejected %q: %v", document, errors)
		}
	}
}
