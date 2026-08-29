import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import type { RemoteAccessSession } from "@argus/api-client";
import { useApi } from "@argus/api-client";
import { ActionGroup, Button, DataTable, Dialog, Field, Input, RowAction, StatusBadge } from "@argus/ui";
// 组件被会话中心与主机页共用，样式随组件引入，避免依赖具体页面的 CSS 导入。
import "../../styles/remote-sessions.css";

type Lookup = (id: string) => string;

const activeStatuses = new Set(["authorized", "connecting", "active", "terminating"]);

export function isActiveSession(session: RemoteAccessSession): boolean {
  return activeStatuses.has(session.status);
}

function tone(status: string): "success" | "warning" | "danger" | "neutral" {
  if (status === "active") return "success";
  if (status === "connecting" || status === "terminating" || status === "authorized") return "warning";
  if (status === "failed" || status === "connection_lost" || status === "invalidated") return "danger";
  return "neutral";
}

function capabilities(session: RemoteAccessSession): string {
  const values = [
    `REC:${session.recording_mode ?? "disabled"}`,
    `CMD:${session.command_audit_mode ?? "disabled"}`,
    `CLIP:${session.clipboard_mode ?? "disabled"}`,
    `UP:${session.file_upload_mode ?? "disabled"}`,
    `DOWN:${session.file_download_mode ?? "disabled"}`,
    `PF:${session.port_forward_mode ?? "disabled"}`,
    `SHARE:${session.session_share_mode ?? "disabled"}`,
  ];
  return values.join(" · ");
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "-";
  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) return `${hours}h ${String(minutes).padStart(2, "0")}m`;
  return `${String(minutes).padStart(2, "0")}m ${String(secs).padStart(2, "0")}s`;
}

export function SessionTable({
  sessions,
  userName,
  hostName,
  accountName,
  allowTerminate,
  onRecording,
  onAttach,
}: {
  sessions: RemoteAccessSession[];
  userName: Lookup;
  hostName: Lookup;
  accountName: Lookup;
  allowTerminate: boolean;
  onRecording(id: string): void;
  onAttach?: (session: RemoteAccessSession) => void;
}) {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [terminating, setTerminating] = useState<RemoteAccessSession | null>(null);
  const [detail, setDetail] = useState<RemoteAccessSession | null>(null);
  const [reason, setReason] = useState("");
  const validatedForm = useForm({ resolver: zodResolver(z.object({ reason: z.string().trim().min(1) })), defaultValues: { reason: "" } });
  const terminate = useMutation({
    mutationFn: () => api.remoteAccess.terminateSession(terminating!.id, reason.trim()),
    onSuccess: () => {
      setTerminating(null);
      setReason("");
      void queryClient.invalidateQueries({ queryKey: ["remote-access", "sessions"] });
    },
    onError: (error) => {
      console.warn("[SessionTable] Failed to terminate session:", error);
      // 即使终止失败（例如会话已不存在），也要关闭对话框并刷新列表
      setTerminating(null);
      setReason("");
      void queryClient.invalidateQueries({ queryKey: ["remote-access", "sessions"] });
    },
  });
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";
  const date = (value?: string) => value ? new Date(value).toLocaleString(locale) : "-";
  const duration = (row: RemoteAccessSession) => {
    if (!row.connected_at) return "-";
    const end = row.terminated_at ? new Date(row.terminated_at).getTime() : Date.now();
    return formatDuration((end - new Date(row.connected_at).getTime()) / 1000);
  };

  return (
    <>
      <DataTable
        columns={[
          { key: "reason", header: t("remoteSessions.columns.reason"), render: (row) => <span className="argus-session-reason" title={row.reason || "-"}>{row.reason || "-"}</span> },
          { key: "status", header: t("remoteSessions.columns.status"), render: (row) => <StatusBadge tone={tone(row.status)}>{t(`remoteSessions.status.${row.status}`)}</StatusBadge> },
          { key: "user_id", header: t("remoteSessions.columns.user"), render: (row) => userName(row.user_id) },
          { key: "host_id", header: t("remoteSessions.columns.host"), render: (row) => hostName(row.host_id) },
          { key: "managed_account_id", header: t("remoteSessions.columns.account"), render: (row) => accountName(row.managed_account_id) },
          { key: "protocol", header: t("remoteSessions.columns.protocol") },
          { key: "connection_mode", header: t("remoteSessions.columns.mode") },
          { key: "created_at", header: t("remoteSessions.columns.created"), render: (row) => date(row.created_at) },
          {
            key: "actions",
            header: t("remoteSessions.columns.actions"),
            render: (row) => (
              <ActionGroup>
                <RowAction onClick={() => setDetail(row)}>{t("remoteSessions.detail")}</RowAction>
                {onAttach && isActiveSession(row) && row.status !== "terminating" && <RowAction onClick={() => onAttach(row)}>{t("remoteSessions.enter")}</RowAction>}
                {row.recording_id && <RowAction onClick={() => onRecording(row.recording_id!)}>{t("remoteSessions.viewRecording")}</RowAction>}
                {allowTerminate && isActiveSession(row) && <RowAction danger onClick={() => setTerminating(row)}>{t("remoteSessions.terminate")}</RowAction>}
              </ActionGroup>
            ),
          },
        ]}
        data={sessions}
        getRowKey={(row) => row.id}
      />
      <Dialog
        description={t("remoteSessions.detailDescription")}
        onOpenChange={(open) => { if (!open) setDetail(null); }}
        open={detail !== null}
        size="lg"
        title={t("remoteSessions.detailTitle")}
      >
        {detail && (
          <div className="argus-recording-detail">
            <dl className="argus-recording-meta">
              <div><dt>{t("remoteSessions.columns.reason")}</dt><dd>{detail.reason || "-"}</dd></div>
              <div><dt>{t("remoteSessions.columns.status")}</dt><dd><StatusBadge tone={tone(detail.status)}>{t(`remoteSessions.status.${detail.status}`)}</StatusBadge></dd></div>
              <div><dt>{t("remoteSessions.columns.user")}</dt><dd>{userName(detail.user_id)}</dd></div>
              <div><dt>{t("remoteSessions.columns.host")}</dt><dd>{hostName(detail.host_id)}</dd></div>
              <div><dt>{t("remoteSessions.columns.account")}</dt><dd>{accountName(detail.managed_account_id)}</dd></div>
              <div><dt>{t("remoteSessions.columns.protocol")}</dt><dd>{detail.protocol}</dd></div>
              <div><dt>{t("remoteSessions.columns.mode")}</dt><dd>{detail.connection_mode}</dd></div>
              <div><dt>{t("remoteSessions.columns.created")}</dt><dd>{date(detail.created_at)}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.connectedAt")}</dt><dd>{date(detail.connected_at)}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.endedAt")}</dt><dd>{date(detail.terminated_at)}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.duration")}</dt><dd>{duration(detail)}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.terminationReason")}</dt><dd>{detail.termination_reason ?? "-"}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.idleTimeout")}</dt><dd>{formatDuration(detail.idle_timeout_seconds)}</dd></div>
              <div><dt>{t("remoteSessions.detailFields.maxDuration")}</dt><dd>{formatDuration(detail.max_duration_seconds)}</dd></div>
              <div><dt>{t("remoteSessions.columns.gateway")}</dt><dd><code>{detail.gateway_instance ?? "-"}</code></dd></div>
              <div><dt>{t("remoteSessions.columns.connectorEpoch")}</dt><dd>{detail.connector_epoch ?? "-"}</dd></div>
              <div><dt>{t("remoteSessions.columns.fence")}</dt><dd>{detail.session_fence ?? "-"}</dd></div>
              <div><dt>{t("remoteSessions.columns.authorizationVersion")}</dt><dd>{detail.authorization_version ?? "-"}</dd></div>
              <div><dt>{t("remoteSessions.columns.capabilities")}</dt><dd><span className="argus-session-capabilities" title={t("remoteSessions.advancedDenied")}>{capabilities(detail)}</span></dd></div>
            </dl>
          </div>
        )}
      </Dialog>
      <Dialog
        description={t("remoteSessions.terminateDescription")}
        footer={
          <>
            <Button onClick={() => setTerminating(null)} variant="secondary">{t("common.cancel")}</Button>
            <Button disabled={!reason.trim()} form="terminate-session-form" loading={terminate.isPending} type="submit" variant="danger">{t("remoteSessions.terminateConfirm")}</Button>
          </>
        }
        onOpenChange={(open) => { if (!open) setTerminating(null); }}
        open={terminating !== null}
        title={t("remoteSessions.terminateTitle")}
      >
        <form id="terminate-session-form" onSubmit={validatedForm.handleSubmit(() => terminate.mutate())}>
          <Field label={t("remoteSessions.terminateReason")} requirement="required">
            <Input {...validatedForm.register("reason")} onChange={(event) => { validatedForm.setValue("reason", event.target.value, { shouldValidate: true }); setReason(event.target.value); }} placeholder={t("remoteSessions.terminateReasonPlaceholder")} value={reason} />
          </Field>
        </form>
      </Dialog>
    </>
  );
}
