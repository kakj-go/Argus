import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { Alert, Dialog, Spinner, StatusBadge, Tabs, TabsContent, TabsList, TabsTrigger, TerminalPlayer } from "@argus/ui";
import { useRecordingEvents } from "../../lib/recording-events";
// 组件被会话中心与主机页共用，样式随组件引入，避免依赖具体页面的 CSS 导入。
import "../../styles/remote-sessions.css";

function eventText(event: { time: number; type: string; data: unknown }): string {
  const data = typeof event.data === "string" ? event.data : JSON.stringify(event.data);
  return `${event.time.toFixed(3)} ${event.type} ${data}`;
}

/**
 * 会话录像详情弹框：按录像 ID 加载元数据与全量事件，主机详情页与
 * 远程会话页共用同一入口，保证回放体验一致。
 */
export function RecordingDetailDialog({ recordingId, onOpenChange }: { recordingId: string | null; onOpenChange(open: boolean): void }) {
  const { t } = useTranslation();
  const api = useApi();
  const meta = useQuery({
    enabled: recordingId !== null,
    queryKey: ["remote-access", "recording", recordingId],
    queryFn: () => api.remoteAccess.getRecording(recordingId!),
  });
  const recording = meta.data ?? null;
  const { events, duration, isPending, isError } = useRecordingEvents(recordingId);
  return (
    <Dialog
      description={t("remoteSessions.recordingDescription")}
      onOpenChange={onOpenChange}
      open={recordingId !== null}
      size="lg"
      title={t("remoteSessions.recordingTitle")}
    >
      {recordingId !== null && (meta.isPending ? <Spinner label={t("common.loading")} /> : meta.isError ? (
        <Alert description={t("remoteSessions.unavailable")} title={t("remoteSessions.recordingTitle")} tone="danger" />
      ) : recording && (
        <div className="argus-recording-detail">
          {recording.status === "recording" && (
            <Alert description={t("remoteSessions.recordingInProgressDescription")} title={t("remoteSessions.recordingInProgress")} tone="warning" />
          )}
          <dl className="argus-recording-meta">
            <div><dt>Session</dt><dd>{recording.session_id}</dd></div>
            <div><dt>{t("remoteSessions.columns.status")}</dt><dd><StatusBadge tone={recording.status === "available" ? "success" : recording.status === "failed" ? "danger" : "warning"}>{t(`remoteSessions.status.${recording.status}`)}</StatusBadge></dd></div>
            <div><dt>{t("remoteSessions.columns.chunks")}</dt><dd>{recording.chunk_count}</dd></div>
            <div><dt>{t("remoteSessions.columns.size")}</dt><dd>{recording.size_bytes.toLocaleString()} B</dd></div>
            <div><dt>{t("remoteSessions.columns.retention")}</dt><dd>{new Date(recording.retention_until).toLocaleString()}</dd></div>
            <div><dt>{t("remoteSessions.recordingDuration")}</dt><dd>{t("remoteSessions.recordingDurationValue", { seconds: Math.round(duration) })}</dd></div>
            <div className="argus-recording-meta__wide"><dt>{t("remoteSessions.hashChain")}</dt><dd><code>{recording.final_hash ?? "-"}</code></dd></div>
          </dl>
          {isError && <Alert description={t("remoteSessions.unavailable")} title={t("remoteSessions.recordingTitle")} tone="danger" />}
          {!isError && isPending && <Spinner label={t("common.loading")} />}
          {!isError && !isPending && events.length === 0 && (
            <TerminalPlayer emptyLabel={t("remoteSessions.emptyEvents")} events={events} />
          )}
          {!isError && !isPending && events.length > 0 && (
            <Tabs defaultValue="replay">
              <TabsList>
                <TabsTrigger value="replay">{t("remoteSessions.replayTab")}</TabsTrigger>
                <TabsTrigger value="events">{t("remoteSessions.eventsTab")}</TabsTrigger>
              </TabsList>
              <TabsContent value="replay">
                <TerminalPlayer emptyLabel={t("remoteSessions.emptyEvents")} events={events} loadingLabel={t("common.loading")} />
              </TabsContent>
              <TabsContent value="events">
                <pre className="argus-recording-events">{events.map(eventText).join("\n")}</pre>
              </TabsContent>
            </Tabs>
          )}
        </div>
      ))}
    </Dialog>
  );
}
