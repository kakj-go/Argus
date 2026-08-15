import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import {
  Card,
  CardContent,
  CardHeader,
  MetricChart,
  StatCard,
} from "@argus/ui";
import { platformUsageSeries } from "../../lib/format";

/** 平台用量 Tab：确定性派生的 14 天趋势 + 汇总 StatCard。 */
export function UsageTab() {
  const { t } = useTranslation();
  const api = useApi();

  const sessions = useQuery({
    queryKey: ["platform", "sessions"],
    queryFn: () => api.platform.sessions.list(),
  });

  const usage = useMemo(() => platformUsageSeries(14), []);
  const totals = useMemo(
    () => ({
      sessions: usage.reduce((sum, point) => sum + point.sessions, 0),
      minutes: usage.reduce((sum, point) => sum + point.sessionMinutes, 0),
      cpu: usage.reduce((sum, point) => sum + point.cpuMinutes, 0),
    }),
    [usage],
  );
  const activeNow = (sessions.data ?? []).filter((item) =>
    ["requested", "starting", "running", "idle"].includes(item.status),
  ).length;

  return (
    <div className="platform-stack">
      <div className="stat-row">
        <StatCard
          label={t("sandbox.usage.totalSessions")}
          tone="accent"
          value={totals.sessions}
        />
        <StatCard
          label={t("sandbox.usage.totalMinutes")}
          value={totals.minutes}
        />
        <StatCard label={t("sandbox.usage.totalCpu")} value={totals.cpu} />
        <StatCard
          label={t("sandbox.usage.activeNow")}
          tone="info"
          value={activeNow}
        />
      </div>

      <Card>
        <CardHeader title={t("sandbox.usage.chart.title")} />
        <CardContent>
          <MetricChart
            labels={usage.map((point) => point.label)}
            series={[
              {
                name: t("sandbox.usage.chart.sessions"),
                points: usage.map((point) => point.sessions),
              },
              {
                name: t("sandbox.usage.chart.minutes"),
                points: usage.map((point) => point.sessionMinutes),
              },
              {
                name: t("sandbox.usage.chart.cpu"),
                points: usage.map((point) => point.cpuMinutes),
              },
            ]}
            showLegend
            type="bar"
          />
        </CardContent>
      </Card>
    </div>
  );
}
