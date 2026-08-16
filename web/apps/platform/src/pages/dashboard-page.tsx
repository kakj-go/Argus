import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { PlatformAuditEvent } from "@argus/api-client";
import {
  Card,
  CardContent,
  CardHeader,
  DataTable,
  EmptyState,
  MetricChart,
  PageShell,
  Spinner,
  StatCard,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime, platformUsageSeries } from "../lib/format";

type AuditRow = {
  id: string;
  createdAt: string;
  actorName: string;
  action: string;
  summary: string;
  result: PlatformAuditEvent["result"];
};

function resultTone(result: PlatformAuditEvent["result"]) {
  if (result === "success") return "success" as const;
  if (result === "denied") return "warning" as const;
  return "danger" as const;
}

/** 仪表盘：平台级统计 + 用量趋势 + 最近平台审计事件。 */
export function DashboardPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const sessions = useQuery({
    queryKey: ["platform", "sessions"],
    queryFn: () => api.platform.sessions.list(),
  });
  const images = useQuery({
    queryKey: ["platform", "images"],
    queryFn: () => api.platform.images.list(),
  });
  const admins = useQuery({
    queryKey: ["platform", "admins"],
    queryFn: () => api.platform.admins.list(),
  });
  const audit = useQuery({
    queryKey: ["platform", "audit", "recent"],
    queryFn: () => api.platform.audit.list(),
  });

  const enterpriseItems = enterprises.data?.items ?? [];
  const activeEnterprises = enterpriseItems.filter(
    (item) => item.status === "active",
  ).length;
  const activeSessions = (sessions.data ?? []).filter((item) =>
    ["requested", "starting", "running", "idle"].includes(item.status),
  ).length;
  const pendingInvites = (admins.data ?? []).filter(
    (item) => item.credentialStatus === "temporary_password",
  ).length;
  const pendingImages = (images.data ?? []).filter(
    (item) =>
      item.scanStatus !== "passed" || item.signatureStatus !== "verified",
  ).length;

  const usage = useMemo(() => platformUsageSeries(14), []);
  const recentEvents: AuditRow[] = (audit.data?.items ?? [])
    .slice(0, 8)
    .map((item) => ({
      id: item.id,
      createdAt: item.createdAt,
      actorName: item.actorName,
      action: item.action,
      summary: item.summary,
      result: item.result,
    }));

  return (
    <PageShell
      description={t("dashboard.description")}
      title={t("dashboard.title")}
    >
      <div className="argus-platform-stack">
        <div className="argus-stat-row">
          <StatCard
            label={t("dashboard.stats.enterprises")}
            tone="accent"
            value={enterpriseItems.length}
          />
          <StatCard
            label={t("dashboard.stats.activeEnterprises")}
            tone="success"
            value={activeEnterprises}
          />
          <StatCard
            label={t("dashboard.stats.activeSessions")}
            tone="info"
            value={activeSessions}
          />
          <StatCard
            detail={t("dashboard.pendingDetail", {
              invites: pendingInvites,
              images: pendingImages,
            })}
            label={t("dashboard.stats.pending")}
            tone={pendingInvites + pendingImages > 0 ? "warning" : "neutral"}
            value={pendingInvites + pendingImages}
          />
        </div>

        <Card>
          <CardHeader title={t("dashboard.usage.title")} />
          <CardContent>
            <MetricChart
              labels={usage.map((point) => point.label)}
              series={[
                {
                  name: t("dashboard.usage.sessions"),
                  points: usage.map((point) => point.sessions),
                },
                {
                  name: t("dashboard.usage.minutes"),
                  points: usage.map((point) => point.sessionMinutes),
                },
              ]}
              showLegend
              type="area"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("dashboard.recent.title")} />
          <CardContent>
            {audit.isPending ? (
              <Spinner />
            ) : recentEvents.length === 0 ? (
              <EmptyState description="" title={t("dashboard.recent.empty")} />
            ) : (
              <DataTable<AuditRow>
                columns={[
                  {
                    key: "createdAt",
                    header: t("dashboard.recent.time"),
                    render: (row) =>
                      formatDateTime(row.createdAt, i18n.language),
                  },
                  { key: "actorName", header: t("dashboard.recent.actor") },
                  {
                    key: "action",
                    header: t("dashboard.recent.action"),
                    render: (row) => (
                      <code className="argus-mono">{row.action}</code>
                    ),
                  },
                  { key: "summary", header: t("dashboard.recent.summary") },
                  {
                    key: "result",
                    header: t("dashboard.recent.result"),
                    render: (row) => (
                      <StatusBadge tone={resultTone(row.result)}>
                        {t(`audit.results.${row.result}`)}
                      </StatusBadge>
                    ),
                  },
                ]}
                data={recentEvents}
                getRowKey={(row) => row.id}
              />
            )}
          </CardContent>
        </Card>
      </div>
    </PageShell>
  );
}
