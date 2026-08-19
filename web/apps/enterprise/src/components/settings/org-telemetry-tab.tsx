import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { DataTable, EmptyState, Spinner, StatusBadge } from "@argus/ui";

type RetentionRow = { signal: "metrics" | "logs" | "traces"; days: number };

const RETENTION: RetentionRow[] = [
  { signal: "metrics", days: 30 },
  { signal: "logs", days: 14 },
  { signal: "traces", days: 7 },
];

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let index = -1;
  do {
    amount /= 1024;
    index += 1;
  } while (amount >= 1024 && index < units.length - 1);
  return `${amount.toFixed(amount >= 10 ? 1 : 2)} ${units[index]}`;
}

export function OrgTelemetryTab() {
  const { t } = useTranslation();
  const api = useApi();
  const usage = useQuery({ queryKey: ["telemetry", "usage"], queryFn: () => api.telemetry.usage() });
  const distributions = useQuery({ queryKey: ["telemetry", "distributions"], queryFn: () => api.telemetry.listDistributions() });
  const profiles = useQuery({ queryKey: ["telemetry", "profiles"], queryFn: () => api.telemetry.listProfiles() });
  const pending = usage.isPending || distributions.isPending || profiles.isPending;
  const failed = usage.isError || distributions.isError || profiles.isError;

  if (pending) return <Spinner />;
  if (failed) {
    return <EmptyState description={t("settings.org.telemetry.loadFailedDescription")} title={t("settings.org.telemetry.loadFailed")} />;
  }

  const usageRows = usage.data ? [
    { key: "ingested", value: formatBytes(usage.data.ingested_bytes) },
    { key: "storage", value: formatBytes(usage.data.estimated_storage_bytes) },
    { key: "metrics", value: usage.data.metric_points.toLocaleString() },
    { key: "logs", value: usage.data.log_records.toLocaleString() },
    { key: "traces", value: usage.data.spans.toLocaleString() },
  ] : [];

  return <div className="argus-settings-section">
    <div className="argus-settings-section__head"><div><h2 className="argus-settings-section__title">{t("settings.org.telemetry.title")}</h2><p className="argus-settings-section__hint">{t("settings.org.telemetry.description")}</p></div></div>
    <h3 className="argus-settings-section__title">{t("settings.org.telemetry.retention")}</h3>
    <DataTable<RetentionRow> columns={[
      { key: "signal", header: t("settings.org.telemetry.signal"), render: (row) => t(`settings.org.telemetry.signals.${row.signal}`) },
      { key: "days", header: t("settings.org.telemetry.days") },
    ]} data={RETENTION} getRowKey={(row) => row.signal} />
    <h3 className="argus-settings-section__title">{t("settings.org.telemetry.usage")}</h3>
    <DataTable columns={[
      { key: "key", header: t("settings.org.telemetry.measure"), render: (row) => t(`settings.org.telemetry.measures.${row.key}`) },
      { key: "value", header: t("settings.org.telemetry.value") },
    ]} data={usageRows} getRowKey={(row) => row.key} />
    <h3 className="argus-settings-section__title">{t("settings.org.telemetry.distributions")}</h3>
    <DataTable columns={[
      { key: "name", header: t("settings.common.name") },
      { key: "version", header: t("settings.org.telemetry.version") },
      { key: "platform", header: t("settings.org.telemetry.platform"), render: (row) => row.artifacts.map((artifact) => artifact.platform).join(", ") },
      { key: "support_status", header: t("settings.common.status"), render: (row) => <StatusBadge tone={row.support_status === "supported" ? "success" : "warning"}>{t(`settings.org.telemetry.support.${row.support_status}`)}</StatusBadge> },
    ]} data={distributions.data ?? []} getRowKey={(row) => row.id} />
    <h3 className="argus-settings-section__title">{t("settings.org.telemetry.profiles")}</h3>
    <DataTable columns={[
      { key: "name", header: t("settings.common.name") },
      { key: "signals", header: t("settings.org.telemetry.signal"), render: (row) => row.signals.join(", ") },
      { key: "supported_platforms", header: t("settings.org.telemetry.platform"), render: (row) => row.supported_platforms.join(", ") },
      { key: "support_status", header: t("settings.common.status"), render: (row) => <StatusBadge tone={row.support_status === "supported" ? "success" : "warning"}>{t(`settings.org.telemetry.support.${row.support_status}`)}</StatusBadge> },
    ]} data={profiles.data ?? []} getRowKey={(row) => row.id} />
  </div>;
}
