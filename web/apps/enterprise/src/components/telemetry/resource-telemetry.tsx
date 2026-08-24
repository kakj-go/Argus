import { useQuery } from "@tanstack/react-query";
import { Play } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, formatApiError, useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  Field,
  Input,
  Select,
  Spinner,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
} from "@argus/ui";
import {
  TelemetryLogTable,
  TelemetryTimeSeries,
  TelemetryTraceTimeline,
} from "@argus/ui/telemetry";

type Signal = "metrics" | "logs" | "traces";
type EditorMode = "builder" | "dsl";

type BuilderDraft = {
  name: string;
  labelKey: string;
  labelOp: string;
  labelValue: string;
  aggregate: string;
  functionName: string;
  window: string;
  groupBy: string;
  lineFilter: string;
  parser: string;
  pattern: string;
  field: string;
  fieldOp: string;
  fieldValue: string;
  unwrap: string;
  relation: string;
  childKey: string;
  childValue: string;
};

type SubmittedQuery = {
  text: string;
  from: string;
  to: string;
  stepSeconds: number;
  limit: number;
};

type MetricPoint = { timestamp: string; value: number };
type MetricSeries = {
  metric_name?: string;
  labels?: Record<string, string>;
  points?: MetricPoint[];
};
type PrometheusSample = [number | string, number | string];
type PrometheusSeries = {
  metric?: Record<string, string>;
  value?: PrometheusSample;
  values?: PrometheusSample[];
};
type LogEntry = {
  timestamp: string;
  resource_id: string;
  severity_text: string;
  service_name?: string;
  body: string;
  trace_id?: string;
  stream_labels?: Record<string, string>;
};
type Span = {
  span_id: string;
  parent_span_id?: string;
  resource_id: string;
  service_name: string;
  operation: string;
  status: string;
  start_time: string;
  duration_ns: number;
};
type SpanSet = { trace_id: string; spans: Span[] };
type QueryResult = {
  result_type?: string;
  data?: unknown;
  warnings?: string[];
  partial?: boolean;
  meta?: {
    scanned_bytes?: number;
    elapsed_ms?: number;
    plan_hash?: string;
  };
};

const languageBySignal = {
  metrics: "promql",
  logs: "kql",
  traces: "skywalking_graphql",
} as const;

function defaultDraft(signal: Signal): BuilderDraft {
  if (signal === "logs") {
    return {
      name: "",
      labelKey: "",
      labelOp: "=",
      labelValue: "",
      aggregate: "",
      functionName: "",
      window: "5m",
      groupBy: "service_name",
      lineFilter: "",
      parser: "",
      pattern: "<method> <path> <duration>",
      field: "",
      fieldOp: "=",
      fieldValue: "",
      unwrap: "duration",
      relation: "",
      childKey: "",
      childValue: "",
    };
  }
  if (signal === "traces") {
    return {
      name: "",
      labelKey: "",
      labelOp: "=",
      labelValue: "",
      aggregate: "",
      functionName: "",
      window: "5m",
      groupBy: "",
      lineFilter: "",
      parser: "",
      pattern: "",
      field: "",
      fieldOp: "=",
      fieldValue: "",
      unwrap: "",
      relation: "",
      childKey: "service.name",
      childValue: "database",
    };
  }
  return {
    name: "system_cpu_utilization",
    labelKey: "",
    labelOp: "=",
    labelValue: "",
    aggregate: "avg",
    functionName: "",
    window: "5m",
    groupBy: "",
    lineFilter: "",
    parser: "",
    pattern: "",
    field: "",
    fieldOp: "=",
    fieldValue: "",
    unwrap: "",
    relation: "",
    childKey: "",
    childValue: "",
  };
}

function quote(value: string): string {
  return JSON.stringify(value);
}

function matcher(key: string, op: string, value: string): string {
  return key && value ? `${key}${op}${quote(value)}` : "";
}

function grouping(name: string, groupBy: string, expression: string): string {
  if (!name) return expression;
  const labels = groupBy
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean)
    .join(", ");
  return labels
    ? `${name} by (${labels}) (${expression})`
    : `${name}(${expression})`;
}

function serializeBuilder(signal: Signal, draft: BuilderDraft): string {
  if (signal === "metrics") {
    const selectorMatcher = matcher(
      draft.labelKey,
      draft.labelOp,
      draft.labelValue,
    );
    let expression = `${draft.name || "metric_name"}${selectorMatcher ? `{${selectorMatcher}}` : ""}`;
    if (draft.functionName) {
      expression = `${draft.functionName}(${expression}[${draft.window || "5m"}])`;
    }
    return grouping(draft.aggregate, draft.groupBy, expression);
  }
  if (signal === "logs") {
    const field = draft.labelKey || "service_name";
    let expression = draft.labelValue
      ? `${field}:${quote(draft.labelValue)}`
      : `${field} exists`;
    if (draft.lineFilter) expression += ` AND body:${quote(draft.lineFilter)}`;
    if (draft.parser === "json" || draft.parser === "logfmt")
      expression += ` | parse ${draft.parser}`;
    if (draft.field && draft.fieldValue) {
      const numeric = [">", ">=", "<", "<="].includes(draft.fieldOp);
      const parsedField = draft.parser
        ? `${draft.parser}.${draft.field}`
        : draft.field;
      expression += ` | where ${parsedField} ${draft.fieldOp} ${numeric ? draft.fieldValue : quote(draft.fieldValue)}`;
    }
    if (draft.functionName) {
      if (draft.unwrap)
        expression += ` | unwrap ${draft.parser ? `${draft.parser}.${draft.unwrap}` : draft.unwrap}`;
      expression += ` | stats count() by ${draft.groupBy || "service_name"}`;
    }
    return expression;
  }
  const serviceArgument = draft.labelValue
    ? `(serviceName: ${quote(draft.labelValue)}, pageSize: 200)`
    : "(pageSize: 200)";
  return `query { queryBasicTraces${serviceArgument} { total traces { traceId rootService rootOperation startTime duration spanCount errorCount status spans { spanId parentSpanId serviceName operationName status startTime duration } } } }`;
}

function timeRange(
  lookbackMinutes: number,
): Pick<SubmittedQuery, "from" | "to"> {
  const to = new Date();
  const from = new Date(to.getTime() - lookbackMinutes * 60 * 1000);
  return { from: from.toISOString(), to: to.toISOString() };
}

function seriesName(series: MetricSeries): string {
  const labels = Object.entries(series.labels ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}=${value}`)
    .join(", ");
  return `${series.metric_name || "result"}${labels ? `{${labels}}` : ""}`;
}

function asMetricSeries(data: unknown): MetricSeries[] {
  if (!Array.isArray(data)) return [];
  const result: MetricSeries[] = [];
  for (const item of data) {
    if (!item || typeof item !== "object") continue;
    const value = item as MetricSeries & PrometheusSeries;
    if (Array.isArray(value.points)) {
      result.push(value);
      continue;
    }
    const labels = value.metric ?? {};
    const rawPoints = Array.isArray(value.values)
      ? value.values
      : value.value
        ? [value.value]
        : [];
    const points = rawPoints.flatMap(([timestamp, sample]) => {
      const seconds = Number(timestamp);
      const numericValue = Number(sample);
      if (!Number.isFinite(seconds) || !Number.isFinite(numericValue))
        return [];
      return [
        {
          timestamp: new Date(seconds * 1000).toISOString(),
          value: numericValue,
        },
      ];
    });
    if (points.length === 0) continue;
    const { __name__: metricName, ...rest } = labels;
    result.push({ metric_name: metricName, labels: rest, points });
  }
  return result;
}

function asLogEntries(data: unknown): LogEntry[] {
  if (!Array.isArray(data)) return [];
  return data.filter((item): item is LogEntry =>
    Boolean(
      item &&
      typeof item === "object" &&
      typeof (item as LogEntry).body === "string",
    ),
  );
}

function asSpanSets(data: unknown): SpanSet[] {
  if (!Array.isArray(data)) return [];
  return data.filter((item): item is SpanSet =>
    Boolean(
      item &&
      typeof item === "object" &&
      Array.isArray((item as SpanSet).spans),
    ),
  );
}

function traceRows(data: unknown) {
  if (data && typeof data === "object") {
    const root = data as {
      queryBasicTraces?: { traces?: Array<Record<string, unknown>> };
      queryTraces?: { traces?: Array<Record<string, unknown>> };
    };
    const graphqlTraces =
      root.queryBasicTraces?.traces ?? root.queryTraces?.traces ?? [];
    return graphqlTraces.map((trace) => ({
      trace_id: String(trace.traceId ?? ""),
      service_name: String(trace.rootService ?? "-"),
      root_span_name: String(trace.rootOperation ?? "-"),
      started_at: String(trace.startTime ?? new Date(0).toISOString()),
      duration_ms: Number(trace.duration ?? 0),
      span_count: Number(trace.spanCount ?? 0),
      status: String(trace.status ?? "unset"),
    }));
  }
  return asSpanSets(data).map((set) => {
    const spans = set.spans;
    const root = spans.find((span) => !span.parent_span_id) ?? spans[0];
    const startedAt =
      spans.map((span) => span.start_time).sort()[0] ??
      new Date(0).toISOString();
    const duration =
      root?.duration_ns ??
      Math.max(0, ...spans.map((span) => span.duration_ns));
    return {
      trace_id: set.trace_id,
      service_name: root?.service_name ?? "-",
      root_span_name: root?.operation ?? "-",
      started_at: startedAt,
      duration_ms: duration / 1_000_000,
      span_count: spans.length,
      status: spans.some((span) => span.status === "error")
        ? "error"
        : (root?.status ?? "unset"),
    };
  });
}

function formatBytes(value = 0): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

export function ResourceTelemetry({
  resourceId,
  signal,
}: {
  resourceId: string;
  signal: Signal;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [mode, setMode] = useState<EditorMode>("builder");
  const [draft, setDraft] = useState(() => defaultDraft(signal));
  const [dsl, setDsl] = useState(() =>
    serializeBuilder(signal, defaultDraft(signal)),
  );
  const [lookbackMinutes, setLookbackMinutes] = useState(60);
  const [stepSeconds, setStepSeconds] = useState(60);
  const [limit, setLimit] = useState(200);
  const [submitted, setSubmitted] = useState<SubmittedQuery>(() => ({
    text: serializeBuilder(signal, defaultDraft(signal)),
    ...timeRange(60),
    stepSeconds: 60,
    limit: 200,
  }));

  const builtQuery = useMemo(
    () => serializeBuilder(signal, draft),
    [draft, signal],
  );
  const currentQuery = mode === "builder" ? builtQuery : dsl;

  useEffect(() => {
    const nextDraft = defaultDraft(signal);
    const text = serializeBuilder(signal, nextDraft);
    setDraft(nextDraft);
    setDsl(text);
    setMode("builder");
    setSubmitted({ text, ...timeRange(60), stepSeconds: 60, limit: 200 });
  }, [signal]);

  const query = useQuery({
    queryKey: ["telemetry-engine-v3", signal, resourceId, submitted],
    queryFn: async (): Promise<QueryResult> => {
      const common = {
        query: submitted.text,
        resource_ids: [resourceId],
        time_range: { from: submitted.from, to: submitted.to },
        budget: {
          max_scan_bytes: 268435456,
          max_rows: submitted.limit,
          max_samples: 5_000_000,
          max_series: 100_000,
          timeout_ms: 10000,
          max_result_bytes: 8 * 1024 * 1024,
        },
      };
      if (signal === "metrics") {
        const response = await api.telemetry.queryMetricsRange({
          ...common,
          step_seconds: submitted.stepSeconds,
        });
        return {
          result_type: response.data.resultType,
          data: response.data.result,
          warnings: response.warnings,
          partial: response.argus_meta.partial,
          meta: response.argus_meta,
        };
      }
      if (signal === "logs") {
        const [expression, ...pipeline] = submitted.text.split("|");
        return api.telemetry.queryLogs({
          ...common,
          query: (expression ?? "").trim(),
          pipeline: pipeline.length ? `|${pipeline.join("|")}` : undefined,
        });
      }
      const response = await api.telemetry.queryTraces(common);
      return {
        result_type: "traces",
        data: response.data,
        warnings: response.errors?.map((error) => error.message) ?? [],
        partial: response.extensions.argus.partial,
        meta: response.extensions.argus,
      };
    },
    retry: (failureCount, error) =>
      error instanceof ApiError && error.retryable && failureCount < 3,
    retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 4000),
  });

  const result = query.data as QueryResult | undefined;
  const metricSeries = asMetricSeries(result?.data);
  const logEntries = asLogEntries(result?.data);
  const traces = traceRows(result?.data);
  const updateDraft = (key: keyof BuilderDraft, value: string) =>
    setDraft((current) => ({ ...current, [key]: value }));
  const run = () =>
    setSubmitted({
      text: currentQuery,
      ...timeRange(lookbackMinutes),
      stepSeconds,
      limit,
    });
  const switchMode = (next: string) => {
    const editorMode = next as EditorMode;
    if (editorMode === "dsl") setDsl(currentQuery);
    setMode(editorMode);
  };

  return (
    <div className="argus-detail-section">
      <div className="argus-telemetry-query-head">
        <Tabs onValueChange={switchMode} value={mode}>
          <TabsList>
            <TabsTrigger value="builder">{t("telemetry.builder")}</TabsTrigger>
            <TabsTrigger value="dsl">{t("telemetry.dslEditor")}</TabsTrigger>
          </TabsList>
          <TabsContent value="builder">
            <BuilderFields
              draft={draft}
              signal={signal}
              t={t}
              update={updateDraft}
            />
            <Field requirement="none" label={t("telemetry.generatedDsl")}>
              <Textarea
                readOnly
                rows={3}
                spellCheck={false}
                value={builtQuery}
              />
            </Field>
          </TabsContent>
          <TabsContent value="dsl">
            <Field
              requirement="optional"
              label={`${languageBySignal[signal].toUpperCase()} DSL`}
            >
              <Textarea
                onChange={(event) => setDsl(event.target.value)}
                rows={5}
                spellCheck={false}
                value={dsl}
              />
            </Field>
          </TabsContent>
        </Tabs>
      </div>

      <div className="argus-telemetry-execution-grid">
        <Field requirement="none" label={t("telemetry.language")}>
          <Input disabled value={languageBySignal[signal].toUpperCase()} />
        </Field>
        <Field requirement="optional" label={t("telemetry.timeRange")}>
          <Select
            ariaLabel={t("telemetry.timeRange")}
            onValueChange={(value) => setLookbackMinutes(Number(value))}
            options={[
              { value: "15", label: t("telemetry.last15Minutes") },
              { value: "60", label: t("telemetry.lastHourShort") },
              { value: "360", label: t("telemetry.last6Hours") },
              { value: "1440", label: t("telemetry.last24Hours") },
            ]}
            value={String(lookbackMinutes)}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.stepSeconds")}>
          <Input
            min={1}
            onChange={(event) =>
              setStepSeconds(Math.max(1, Number(event.target.value)))
            }
            type="number"
            value={stepSeconds}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.limit")}>
          <Input
            min={1}
            onChange={(event) =>
              setLimit(Math.max(1, Number(event.target.value)))
            }
            type="number"
            value={limit}
          />
        </Field>
        <div className="argus-telemetry-run-action">
          <Button
            disabled={!currentQuery.trim()}
            loading={query.isFetching}
            onClick={run}
            variant="primary"
          >
            <Play aria-hidden size={15} />
            {t("telemetry.runQuery")}
          </Button>
        </div>
      </div>

      {query.isError ? (
        <Alert
          description={formatApiError(
            query.error,
            t("telemetry.queryFailedDescription"),
            (requestId) => t("common.requestReference", { requestId }),
          )}
          title={t("telemetry.queryFailed")}
          tone="danger"
        />
      ) : null}
      {result?.partial ? (
        <Alert
          description={t("telemetry.partialDescription")}
          title={t("telemetry.partial")}
          tone="warning"
        />
      ) : null}
      {(result?.warnings?.length ?? 0) > 0 ? (
        <Alert
          description={result?.warnings?.join("; ") ?? ""}
          title={t("telemetry.warnings")}
          tone="warning"
        />
      ) : null}

      <Card>
        <CardHeader
          description={t("telemetry.lastHour")}
          title={t(`telemetry.${signal}`)}
        />
        <CardContent>
          {result ? (
            <div className="argus-telemetry-query-meta">
              <span>
                {t("telemetry.resultType")}: <b>{result.result_type ?? "-"}</b>
              </span>
              <span>
                {t("telemetry.scanned")}:{" "}
                <b>{formatBytes(result.meta?.scanned_bytes)}</b>
              </span>
              <span>
                {t("telemetry.elapsed")}:{" "}
                <b>{result.meta?.elapsed_ms ?? 0} ms</b>
              </span>
              <span>
                {t("telemetry.planHash")}:{" "}
                <code>{result.meta?.plan_hash?.slice(0, 12) ?? "-"}</code>
              </span>
            </div>
          ) : null}
          {query.isLoading ? <Spinner label={t("common.loading")} /> : null}
          {!query.isLoading && signal === "metrics" ? (
            metricSeries.length > 0 ? (
              <TelemetryTimeSeries
                ariaLabel={t("telemetry.metricsSummary")}
                series={metricSeries.map((series) => ({
                  name: seriesName(series),
                  points: series.points ?? [],
                }))}
              />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
          {!query.isLoading && signal === "logs" ? (
            logEntries.length > 0 ? (
              <TelemetryLogTable
                rows={logEntries.map((row) => ({
                  ...row,
                  severity: row.severity_text,
                  service_name:
                    row.service_name ?? row.stream_labels?.service_name,
                }))}
              />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
          {!query.isLoading && signal === "traces" ? (
            traces.length > 0 ? (
              <TelemetryTraceTimeline rows={traces} />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}

function BuilderFields({
  draft,
  signal,
  t,
  update,
}: {
  draft: BuilderDraft;
  signal: Signal;
  t: (key: string) => string;
  update: (key: keyof BuilderDraft, value: string) => void;
}) {
  const matcherOptions = ["=", "!=", "=~", "!~"].map((value) => ({
    value,
    label: value,
  }));
  if (signal === "metrics") {
    return (
      <div className="argus-telemetry-builder-grid">
        <Field requirement="optional" label={t("telemetry.metricName")}>
          <Input
            onChange={(event) => update("name", event.target.value)}
            value={draft.name}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.labelKey")}>
          <Input
            onChange={(event) => update("labelKey", event.target.value)}
            value={draft.labelKey}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.operator")}>
          <Select
            ariaLabel={t("telemetry.operator")}
            onValueChange={(value) => update("labelOp", value)}
            options={matcherOptions}
            value={draft.labelOp}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.labelValue")}>
          <Input
            onChange={(event) => update("labelValue", event.target.value)}
            value={draft.labelValue}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.rangeFunction")}>
          <Select
            ariaLabel={t("telemetry.rangeFunction")}
            onValueChange={(value) => update("functionName", value)}
            options={[
              { value: "", label: t("telemetry.none") },
              { value: "rate", label: "rate" },
              { value: "increase", label: "increase" },
            ]}
            value={draft.functionName}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.window")}>
          <Input
            disabled={!draft.functionName}
            onChange={(event) => update("window", event.target.value)}
            value={draft.window}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.aggregation")}>
          <Select
            ariaLabel={t("telemetry.aggregation")}
            onValueChange={(value) => update("aggregate", value)}
            options={[
              { value: "", label: t("telemetry.none") },
              ...["sum", "avg", "min", "max", "count"].map((value) => ({
                value,
                label: value,
              })),
            ]}
            value={draft.aggregate}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.groupBy")}>
          <Input
            disabled={!draft.aggregate}
            onChange={(event) => update("groupBy", event.target.value)}
            value={draft.groupBy}
          />
        </Field>
      </div>
    );
  }
  if (signal === "logs") {
    return (
      <div className="argus-telemetry-builder-grid">
        <Field requirement="optional" label={t("telemetry.streamLabel")}>
          <Input
            onChange={(event) => update("labelKey", event.target.value)}
            value={draft.labelKey}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.operator")}>
          <Select
            ariaLabel={t("telemetry.operator")}
            onValueChange={(value) => update("labelOp", value)}
            options={matcherOptions}
            value={draft.labelOp}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.labelValue")}>
          <Input
            onChange={(event) => update("labelValue", event.target.value)}
            value={draft.labelValue}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.lineContains")}>
          <Input
            onChange={(event) => update("lineFilter", event.target.value)}
            value={draft.lineFilter}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.parser")}>
          <Select
            ariaLabel={t("telemetry.parser")}
            onValueChange={(value) => update("parser", value)}
            options={[
              { value: "", label: t("telemetry.none") },
              { value: "json", label: "json" },
              { value: "logfmt", label: "logfmt" },
              { value: "pattern", label: "pattern" },
            ]}
            value={draft.parser}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.pattern")}>
          <Input
            disabled={draft.parser !== "pattern"}
            onChange={(event) => update("pattern", event.target.value)}
            value={draft.pattern}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.field")}>
          <Input
            onChange={(event) => update("field", event.target.value)}
            value={draft.field}
          />
        </Field>
        <Field
          controlMode="group"
          requirement="optional"
          label={t("telemetry.fieldFilter")}
        >
          <div className="argus-telemetry-inline-filter">
            <Select
              ariaLabel={t("telemetry.fieldFilter")}
              onValueChange={(value) => update("fieldOp", value)}
              options={["=", "!=", "=~", "!~", ">", ">=", "<", "<="].map(
                (value) => ({ value, label: value }),
              )}
              value={draft.fieldOp}
            />
            <Input
              onChange={(event) => update("fieldValue", event.target.value)}
              value={draft.fieldValue}
            />
          </div>
        </Field>
        <Field requirement="optional" label={t("telemetry.rangeFunction")}>
          <Select
            ariaLabel={t("telemetry.rangeFunction")}
            onValueChange={(value) => update("functionName", value)}
            options={[
              { value: "", label: t("telemetry.none") },
              ...[
                "count_over_time",
                "rate",
                "avg_over_time",
                "sum_over_time",
                "min_over_time",
                "max_over_time",
              ].map((value) => ({ value, label: value })),
            ]}
            value={draft.functionName}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.unwrapField")}>
          <Input
            disabled={
              !draft.functionName ||
              ["count_over_time", "rate"].includes(draft.functionName)
            }
            onChange={(event) => update("unwrap", event.target.value)}
            value={draft.unwrap}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.window")}>
          <Input
            disabled={!draft.functionName}
            onChange={(event) => update("window", event.target.value)}
            value={draft.window}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.aggregation")}>
          <Select
            ariaLabel={t("telemetry.aggregation")}
            disabled={!draft.functionName}
            onValueChange={(value) => update("aggregate", value)}
            options={[
              { value: "", label: t("telemetry.none") },
              ...["sum", "avg", "min", "max", "count"].map((value) => ({
                value,
                label: value,
              })),
            ]}
            value={draft.aggregate}
          />
        </Field>
        <Field requirement="optional" label={t("telemetry.groupBy")}>
          <Input
            disabled={!draft.aggregate}
            onChange={(event) => update("groupBy", event.target.value)}
            value={draft.groupBy}
          />
        </Field>
      </div>
    );
  }
  return (
    <div className="argus-telemetry-builder-grid">
      <Field requirement="optional" label={t("telemetry.attribute")}>
        <Input
          onChange={(event) => update("labelKey", event.target.value)}
          value={draft.labelKey}
        />
      </Field>
      <Field requirement="optional" label={t("telemetry.operator")}>
        <Select
          ariaLabel={t("telemetry.operator")}
          onValueChange={(value) => update("labelOp", value)}
          options={matcherOptions}
          value={draft.labelOp}
        />
      </Field>
      <Field requirement="optional" label={t("telemetry.attributeValue")}>
        <Input
          onChange={(event) => update("labelValue", event.target.value)}
          value={draft.labelValue}
        />
      </Field>
      <Field requirement="optional" label={t("telemetry.relation")}>
        <Select
          ariaLabel={t("telemetry.relation")}
          onValueChange={(value) => update("relation", value)}
          options={[
            { value: "", label: t("telemetry.none") },
            { value: "child", label: t("telemetry.directChild") },
          ]}
          value={draft.relation}
        />
      </Field>
      <Field requirement="optional" label={t("telemetry.childAttribute")}>
        <Input
          disabled={draft.relation !== "child"}
          onChange={(event) => update("childKey", event.target.value)}
          value={draft.childKey}
        />
      </Field>
      <Field requirement="optional" label={t("telemetry.attributeValue")}>
        <Input
          disabled={draft.relation !== "child"}
          onChange={(event) => update("childValue", event.target.value)}
          value={draft.childValue}
        />
      </Field>
    </div>
  );
}
