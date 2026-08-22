package kql

import "testing"

func TestParseAndCompileKQL(t *testing.T) {
	expr, err := Parse(`service_name:"api" AND severity_number >= 3`)
	if err != nil {
		t.Fatal(err)
	}
	compiler := Compiler{}
	sql, err := compiler.Compile(expr)
	if err != nil {
		t.Fatal(err)
	}
	if sql == "" || len(compiler.Args) != 2 {
		t.Fatalf("unexpected compiled query: %q %#v", sql, compiler.Args)
	}
	if compiler.Args[0] != "api" || compiler.Args[1] != int64(3) {
		t.Fatalf("unexpected arguments: %#v", compiler.Args)
	}
}

func TestKQLRejectsUnknownField(t *testing.T) {
	expr, err := Parse("password:secret")
	if err != nil {
		t.Fatal(err)
	}
	compiler := Compiler{}
	if _, err := compiler.Compile(expr); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestKQLCompilesFieldExistence(t *testing.T) {
	tests := []struct {
		expression string
		wantSQL    string
		wantArg    any
	}{
		{expression: "service_name exists", wantSQL: "notEmpty(service_name)"},
		{expression: "stream_labels.namespace exists", wantSQL: "mapContains(stream_labels, ?)", wantArg: "namespace"},
	}
	for _, test := range tests {
		expr, err := Parse(test.expression)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.expression, err)
		}
		compiler := Compiler{}
		sql, err := compiler.Compile(expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", test.expression, err)
		}
		if sql != test.wantSQL {
			t.Fatalf("Compile(%q) = %q, want %q", test.expression, sql, test.wantSQL)
		}
		if test.wantArg == nil && len(compiler.Args) != 0 {
			t.Fatalf("Compile(%q) args = %#v, want none", test.expression, compiler.Args)
		}
		if test.wantArg != nil && (len(compiler.Args) != 1 || compiler.Args[0] != test.wantArg) {
			t.Fatalf("Compile(%q) args = %#v, want %#v", test.expression, compiler.Args, test.wantArg)
		}
	}
}

func TestPipelineIsBoundedAndParameterized(t *testing.T) {
	args := make([]any, 0)
	where, order, limit, err := compilePipeline("where service_name = api | sort timestamp asc | limit 10", "body != x", "timestamp DESC", 100, &args)
	if err != nil {
		t.Fatal(err)
	}
	if where == "" || order != "timestamp ASC" || limit != 10 || len(args) != 1 || args[0] != "api" {
		t.Fatalf("unexpected pipeline compilation: %q %q %d %#v", where, order, limit, args)
	}
}

func TestPipelineParsersUnwrapAndStats(t *testing.T) {
	args := make([]any, 0)
	where, _, limit, parser, unwrap, stats, err := compilePipelineOptions("parse json | where json.duration >= 10 | unwrap json.duration | stats count() by service_name | limit 5", "body != ''", "timestamp DESC", 100, &args)
	if err != nil {
		t.Fatal(err)
	}
	if parser != "json" || unwrap != "json.duration" || stats != "service_name" || limit != 5 || where == "" {
		t.Fatalf("unexpected pipeline: %q %q %q %d", parser, unwrap, stats, limit)
	}
	if len(args) != 2 || args[0] != "duration" || args[1] != int64(10) {
		t.Fatalf("unexpected args: %#v", args)
	}
	parsed := parseStructuredBody(`{"duration":12,"status":"ok"}`, "json")
	if parsed["duration"] != "12" || parsed["status"] != "ok" {
		t.Fatalf("unexpected parsed body: %#v", parsed)
	}
}

func TestPatternParser(t *testing.T) {
	args := make([]any, 0)
	_, _, _, parser, unwrap, _, err := compilePipelineOptions(`parse pattern "request <method> <path> <status>" | unwrap pattern.status`, "body != ''", "timestamp DESC", 100, &args)
	if err != nil {
		t.Fatal(err)
	}
	if parser == "" || unwrap != "pattern.status" {
		t.Fatalf("unexpected pattern pipeline: parser=%q unwrap=%q", parser, unwrap)
	}
	values := parsePipelineBody("request GET /health 200", parser)
	if values["method"] != "GET" || values["path"] != "/health" || values["status"] != "200" {
		t.Fatalf("unexpected pattern values: %#v", values)
	}
}
