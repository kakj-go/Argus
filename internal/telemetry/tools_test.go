package telemetry

import (
	"testing"

	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

func TestDSLQueryInputSchemaPromQLBudgets(t *testing.T) {
	promQL := schemaProperties(t, dslQueryInputSchema("promql", "metrics"))
	for _, field := range []string{"max_samples", "max_series"} {
		if _, ok := promQL[field]; !ok {
			t.Fatalf("PromQL input schema is missing %q", field)
		}
	}
	for _, language := range []struct {
		name   string
		signal string
	}{
		{name: "kql", signal: "logs"},
		{name: "skywalking_graphql", signal: "traces"},
	} {
		properties := schemaProperties(t, dslQueryInputSchema(language.name, language.signal))
		for _, field := range []string{"max_samples", "max_series"} {
			if _, ok := properties[field]; ok {
				t.Fatalf("%s input schema unexpectedly exposes PromQL budget %q", language.name, field)
			}
		}
	}
}

func TestProtocolQueryOutputUsesNativeProtocolShapes(t *testing.T) {
	meta := queryengine.QueryMeta{
		PlanHash: "plan", Engine: "engine", EngineVersion: "v1", ScannedBytes: 1,
		ScannedRows: 2, ReturnedRows: 3, LoadedSamples: 4, ElapsedMillis: 5,
	}

	promQL := protocolQueryOutput(queryengine.Result{Language: queryengine.LanguagePromQL, ResultType: "vector", Data: []any{"sample"}, Meta: meta})
	if promQL["status"] != "success" {
		t.Fatalf("unexpected PromQL status: %#v", promQL)
	}
	promData, ok := promQL["data"].(map[string]any)
	if !ok || promData["resultType"] != "vector" || promData["result"] == nil || promQL["argus_meta"] == nil {
		t.Fatalf("unexpected PromQL output: %#v", promQL)
	}

	kql := protocolQueryOutput(queryengine.Result{Language: queryengine.LanguageKQL, ResultType: "log_entries", Data: []any{"entry"}, Meta: meta})
	if kql["schema_version"] != "argus.kql_result/v1" || kql["result_type"] != "log_entries" || kql["meta"] == nil {
		t.Fatalf("unexpected KQL output: %#v", kql)
	}

	trace := protocolQueryOutput(queryengine.Result{Language: queryengine.LanguageTrace, ResultType: "traces", Data: map[string]any{"queryTrace": map[string]any{"traceId": "trace-1"}}, Meta: meta})
	if trace["data"] == nil {
		t.Fatalf("unexpected GraphQL output: %#v", trace)
	}
	extensions, ok := trace["extensions"].(map[string]any)
	if !ok || extensions["argus"] == nil {
		t.Fatalf("GraphQL output is missing Argus extensions: %#v", trace)
	}
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties have unexpected type: %#v", schema["properties"])
	}
	return properties
}
