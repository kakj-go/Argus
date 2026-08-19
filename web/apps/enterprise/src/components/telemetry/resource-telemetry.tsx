import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import {
  Alert,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  Spinner,
} from "@argus/ui";
import {
  TelemetryLogTable,
  TelemetryTimeSeries,
  TelemetryTraceTimeline,
} from "@argus/ui/telemetry";

type Signal = "metrics" | "logs" | "traces";

function range() {
  const to = new Date();
  const from = new Date(to.getTime() - 60 * 60 * 1000);
  return { from: from.toISOString(), to: to.toISOString() };
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
  const window = range();
  const query = useQuery({
    queryKey: ["telemetry", signal, resourceId, window.from.slice(0, 13)],
    queryFn: async () => {
      if (signal === "metrics") {
        return api.telemetry.queryMetrics({
          resource_ids: [resourceId],
          from: window.from,
          to: window.to,
          metric_name: "system.cpu.utilization",
          aggregation: "avg",
          step_seconds: 60,
          limit: 5000,
        });
      }
      if (signal === "logs") {
        return api.telemetry.queryLogs({
          resource_ids: [resourceId],
          from: window.from,
          to: window.to,
          limit: 500,
        });
      }
      return api.telemetry.queryTraces({
        resource_ids: [resourceId],
        from: window.from,
        to: window.to,
        limit: 200,
      });
    },
    retry: false,
  });

  if (query.isLoading) return <Spinner label={t("common.loading")} />;
  if (query.isError) {
    return (
      <EmptyState
        description={t("telemetry.queryFailedDescription")}
        kind="error"
        title={t("telemetry.queryFailed")}
      />
    );
  }
  const result = query.data;
  const meta = result?.meta;

  return (
    <div className="argus-detail-section">
      {meta?.partial ? (
        <Alert
          description={t("telemetry.partialDescription")}
          title={t("telemetry.partial")}
          tone="warning"
        />
      ) : null}
      <Card>
        <CardHeader
          description={t("telemetry.lastHour")}
          title={t(`telemetry.${signal}`)}
        />
        <CardContent>
          {signal === "metrics" && result && "series" in result ? (
            result.series.length > 0 ? (
              <TelemetryTimeSeries
                ariaLabel={t("telemetry.metricsSummary")}
                series={result.series.map((series) => ({
                  name: series.metric_name,
                  unit: series.unit,
                  points: series.points,
                }))}
              />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
          {signal === "logs" && result && "records" in result ? (
            result.records.length > 0 ? (
              <TelemetryLogTable rows={result.records} />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
          {signal === "traces" && result && "traces" in result ? (
            result.traces.length > 0 ? (
              <TelemetryTraceTimeline rows={result.traces} />
            ) : (
              <EmptyState description="" title={t("telemetry.empty")} />
            )
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
