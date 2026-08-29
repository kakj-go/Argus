import { useTranslation } from "react-i18next";
import type { RemoteAccessRecording } from "@argus/api-client";
import { DataTable, EmptyState, RowAction, StatusBadge } from "@argus/ui";

export function Recordings({ recordings, onSelect }: { recordings: RemoteAccessRecording[]; onSelect(recording: RemoteAccessRecording): void }) {
  const { t } = useTranslation();
  if (recordings.length === 0) return <EmptyState description="" title={t("remoteSessions.empty.recordings")} />;
  return <DataTable columns={[
    { key: "status", header: t("remoteSessions.columns.status"), render: (row) => <StatusBadge tone={row.status === "available" ? "success" : row.status === "failed" ? "danger" : "warning"}>{t(`remoteSessions.status.${row.status}`)}</StatusBadge> },
    { key: "session_id", header: "Session" },
    { key: "chunk_count", header: t("remoteSessions.columns.chunks") },
    { key: "size_bytes", header: t("remoteSessions.columns.size"), render: (row) => `${row.size_bytes.toLocaleString()} B` },
    { key: "retention_until", header: t("remoteSessions.columns.retention"), render: (row) => new Date(row.retention_until).toLocaleString() },
    { key: "actions", header: t("remoteSessions.columns.actions"), render: (row) => <RowAction onClick={() => onSelect(row)}>{t("remoteSessions.viewRecording")}</RowAction> },
  ]} data={recordings} getRowKey={(row) => row.id} />;
}
