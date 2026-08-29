import { useCallback, useMemo, useRef, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { z } from "zod";
import {
  formConstraint,
  formatApiError,
  useApi,
  useTerminalSessions,
  type Host,
  type RemoteAccessSession,
} from "@argus/api-client";
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
} from "@argus/ui";
import { RecordingDetailDialog } from "../remote-sessions/recording-detail";
import { SessionTable } from "../remote-sessions/session-table";
import { TerminalPanel } from "./terminal-panel";

const activeStates: RemoteAccessSession["status"][] = [
  "authorized",
  "connecting",
  "active",
  "terminating",
];
const reasonConstraint = formConstraint("AccessRequestCreate", "reason");

export function RealTerminalTab({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const { sessions: terminalSessions } = useTerminalSessions();
  const [error, setError] = useState("");
  const [recordingId, setRecordingId] = useState<string | null>(null);
  const terminalPanelRef = useRef<{
    createSession: (accountId: string, reason: string) => Promise<void>;
    attachSession: (session: RemoteAccessSession, ticket: any) => Promise<void>;
    showSession: (sessionId: string) => void;
  }>(null);

  const accounts = useQuery({
    queryKey: ["managed-accounts", host.id],
    queryFn: async () =>
      (await api.secrets.listManagedAccounts()).filter(
        (item) => item.host_id === host.id && item.status === "active",
      ),
  });
  // 会话表展示需要用户显示名；与「会话中心」使用同一 SessionTable 组件。
  const users = useQuery({
    queryKey: ["org", "users"],
    queryFn: () => api.org.listUsers(),
  });
  const userNames = useMemo(
    () => new Map((users.data ?? []).map((item) => [item.id, item.displayName])),
    [users.data],
  );
  // 与「会话中心」共用 ["remote-access", "sessions"] 前缀，SessionTable
  // 终止/进入操作后的失效才能同步刷新本页列表。
  const sessions = useQuery({
    queryKey: ["remote-access", "sessions", "host", host.id],
    queryFn: async () =>
      (await api.remoteAccess.listSessions()).items.filter(
        (item) => item.host_id === host.id,
      ),
    // 存在「终止中」会话时自动轮询，后端收敛后列表自动转历史。
    refetchInterval: (query) =>
      query.state.data?.some((item) => item.status === "terminating")
        ? 2000
        : false,
  });
  const activeSessions = useMemo(
    () =>
      (sessions.data ?? []).filter((item) =>
        activeStates.includes(item.status),
      ),
    [sessions.data],
  );
  const protocol = host.connection_mode === "direct_winrm" ? "winrs" : "ssh";
  const options = (accounts.data ?? [])
    .filter((item) =>
      item.allowed_protocols.includes(protocol === "winrs" ? "winrm" : "ssh"),
    )
    .map((item) => ({ value: item.id, label: item.username }));
  const schema = useMemo(
    () =>
      z.object({
        accountId: z.string().min(1, t("hosts.terminal.needAccount")),
        reason: z
          .string()
          .trim()
          .min(reasonConstraint.minLength ?? 1, t("hosts.terminal.needReason"))
          .max(
            reasonConstraint.maxLength ?? 2048,
            t("hosts.terminal.reasonTooLong"),
          ),
      }),
    [t],
  );
  type SessionForm = z.infer<typeof schema>;
  const form = useForm<SessionForm>({
    resolver: zodResolver(schema),
    defaultValues: { accountId: "", reason: "" },
  });
  const refresh = useCallback(
    () =>
      void queryClient.invalidateQueries({
        queryKey: ["remote-access", "sessions"],
      }),
    [queryClient],
  );

  const start = form.handleSubmit(async (values) => {
    setError("");
    try {
      await terminalPanelRef.current?.createSession(values.accountId, values.reason);
      form.reset();
      refresh();
    } catch (err) {
      setError(
        formatApiError(err, t("hosts.terminal.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
    }
  });

  const attach = async (session: RemoteAccessSession) => {
    setError("");
    // 本地已有连接的会话只重新显示 Dock 标签页；authorized/active 会话
    // 可签发票据接入或重接（刷新后重进同一终端）。
    if (terminalSessions.has(session.id)) {
      terminalPanelRef.current?.showSession(session.id);
      return;
    }
    try {
      const ticket = await api.remoteAccess.createTicket(session.id);
      await terminalPanelRef.current?.attachSession(session, ticket);
    } catch (err) {
      setError(
        formatApiError(err, t("hosts.terminal.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
    }
  };

  return (
    <div className="argus-hosts-stack">
      <Card>
        <CardHeader title={t("hosts.terminal.sessionConfirmTitle")} />
        <CardContent className="argus-session-confirm">
          <form onSubmit={start}>
            {error && (
              <Alert
                description={error}
                title={t("hosts.terminal.failedTitle")}
                tone="danger"
              />
            )}
            <div className="argus-form-row">
              <Controller
                control={form.control}
                name="accountId"
                render={({ field, fieldState }) => (
                  <Field
                    error={fieldState.error?.message}
                    requirement="required"
                    label={t("hosts.terminal.account")}
                  >
                    <Select
                      ariaLabel={t("hosts.terminal.account")}
                      onValueChange={field.onChange}
                      options={options}
                      placeholder={t("hosts.terminal.accountPlaceholder")}
                      value={field.value}
                    />
                  </Field>
                )}
              />
              <Field
                error={form.formState.errors.reason?.message}
                requirement="required"
                label={t("hosts.terminal.reason")}
              >
                <Input
                  maxLength={reasonConstraint.maxLength}
                  placeholder={t("hosts.terminal.reasonPlaceholder")}
                  {...form.register("reason")}
                />
              </Field>
            </div>
            <Button type="submit" variant="primary">
              {t("hosts.terminal.start")}
            </Button>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader
          action={
            <Button
              aria-label={t("hosts.terminal.refreshList")}
              loading={sessions.isFetching}
              onClick={() => void queryClient.invalidateQueries({ queryKey: ["remote-access", "sessions"] })}
              size="sm"
              title={t("hosts.terminal.refreshList")}
              variant="ghost"
            >
              <RefreshCw size={14} />
            </Button>
          }
          title={t("hosts.terminal.activeSessions")}
        />
        <CardContent>
          {activeSessions.length === 0 ? (
            <EmptyState
              description=""
              title={t("hosts.terminal.noActiveSessions")}
            />
          ) : (
            <SessionTable
              accountName={(id) =>
                (accounts.data ?? []).find((item) => item.id === id)?.username ?? id
              }
              allowTerminate
              hostName={(id) => (id === host.id ? host.name : id)}
              onAttach={(session) => void attach(session)}
              onRecording={setRecordingId}
              sessions={activeSessions}
              userName={(id) => userNames.get(id) ?? id}
            />
          )}
        </CardContent>
      </Card>
      <TerminalPanel ref={terminalPanelRef} host={host} />
      <RecordingDetailDialog
        onOpenChange={(open) => { if (!open) setRecordingId(null); }}
        recordingId={recordingId}
      />
    </div>
  );
}
