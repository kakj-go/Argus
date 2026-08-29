import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type { RemoteAccessRecording, RemoteAccessSession } from "@argus/api-client";
import { useApi, useTerminalSessions } from "@argus/api-client";
import { EmptyState, FilterBar, PageShell, Spinner, Tabs, TabsContent, TabsList, TabsTrigger } from "@argus/ui";
import { RecordingDetailDialog } from "../components/remote-sessions/recording-detail";
import { Recordings } from "../components/remote-sessions/recordings";
import { isActiveSession, SessionTable } from "../components/remote-sessions/session-table";
import "../styles/remote-sessions.css";

export function RemoteSessionsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { attachSession } = useTerminalSessions();
  const [tab, setTab] = useState("active");
  const [userId, setUserId] = useState("");
  const [hostId, setHostId] = useState("");
  const [accountId, setAccountId] = useState("");
  const [protocol, setProtocol] = useState("");
  const [connectionMode, setConnectionMode] = useState("");
  const [recordingStatus, setRecordingStatus] = useState("");
  const [recordingId, setRecordingId] = useState<string | null>(null);
  const users = useQuery({ queryKey: ["org", "users"], queryFn: () => api.org.listUsers() });
  const hosts = useQuery({ queryKey: ["hosts", "session-lookup"], queryFn: () => api.hosts.list() });
  const accounts = useQuery({ queryKey: ["managed-accounts", "session-lookup"], queryFn: () => api.secrets.listManagedAccounts() });
  const sessionFilter = { limit: 50, scope: tab === "history" ? "history" as const : "active" as const, user_id: userId || undefined, host_id: hostId || undefined, managed_account_id: accountId || undefined, protocol: protocol ? protocol as "ssh" | "winrs" : undefined, connection_mode: connectionMode ? connectionMode as "via_bastion" | "connector_local" | "direct_ssh" | "direct_winrm" : undefined };
  const sessions = useQuery({ queryKey: ["remote-access", "sessions", sessionFilter], queryFn: () => api.remoteAccess.listSessions(sessionFilter), enabled: tab !== "recordings",
    // 存在「终止中」会话时自动轮询，后端收敛后列表自动转历史。
    refetchInterval: (query) => query.state.data?.items.some((item) => item.status === "terminating") ? 2000 : false });
  const recordingFilter = { limit: 50, user_id: userId || undefined, host_id: hostId || undefined, status: recordingStatus ? recordingStatus as RemoteAccessRecording["status"] : undefined };
  const recordings = useQuery({ queryKey: ["remote-access", "recordings", recordingFilter], queryFn: () => api.remoteAccess.listRecordings(recordingFilter), enabled: tab === "recordings" });
  const lookups = useMemo(() => ({
    user: new Map((users.data ?? []).map((item) => [item.id, item.displayName])),
    host: new Map((hosts.data?.items ?? []).map((item) => [item.id, item.name])),
    account: new Map((accounts.data ?? []).map((item) => [item.id, item.username])),
  }), [accounts.data, hosts.data, users.data]);
  const all = useMemo(() => sessions.data?.items ?? [], [sessions.data?.items]);
  const activeSessions = useMemo(() => all.filter(isActiveSession), [all]);

  const handleAttach = async (session: RemoteAccessSession) => {
    try {
      // 获取会话 ticket
      const ticket = await api.remoteAccess.createTicket(session.id);

      // 获取主机名和账号名
      const hostName = lookups.host.get(session.host_id) ?? session.host_id;
      const accountName = lookups.account.get(session.managed_account_id) ?? session.managed_account_id;

      // 使用全局会话管理器附加会话
      await attachSession(session.id, session, ticket, hostName, accountName);

      // 导航到主机详情页
      navigate({ to: `/hosts/${session.host_id}` });
    } catch (error) {
      console.error("[RemoteSessions] Failed to attach session:", error);
    }
  };

  const table = (items: typeof all, emptyKey: string, terminate: boolean) => items.length === 0
    ? <EmptyState description="" title={t(emptyKey)} />
    : <SessionTable accountName={(id) => lookups.account.get(id) ?? id} allowTerminate={terminate} hostName={(id) => lookups.host.get(id) ?? id} onAttach={handleAttach} onRecording={setRecordingId} sessions={items} userName={(id) => lookups.user.get(id) ?? id} />;

  return <PageShell description={t("remoteSessions.description")} title={t("remoteSessions.title")}>
    <Tabs className="argus-remote-sessions-tabs" onValueChange={setTab} value={tab}>
      <TabsList>
        <TabsTrigger value="active">{t("remoteSessions.tabs.active")}</TabsTrigger>
        <TabsTrigger value="history">{t("remoteSessions.tabs.history")}</TabsTrigger>
        <TabsTrigger value="recordings">{t("remoteSessions.tabs.recordings")}</TabsTrigger>
      </TabsList>
      <FilterBar
        filters={[
          { key: "user", value: userId, allLabel: t("remoteSessions.filters.allUsers"), options: (users.data ?? []).map((item) => ({ value: item.id, label: item.displayName })), onChange: setUserId },
          { key: "host", value: hostId, allLabel: t("remoteSessions.filters.allHosts"), options: (hosts.data?.items ?? []).map((item) => ({ value: item.id, label: item.name })), onChange: setHostId },
          ...(tab === "recordings" ? [{ key: "recording-status", value: recordingStatus, allLabel: t("remoteSessions.filters.allStatuses"), options: ["recording", "available", "incomplete", "failed", "expired"].map((value) => ({ value, label: t(`remoteSessions.status.${value}`) })), onChange: setRecordingStatus }] : [
            { key: "account", value: accountId, allLabel: t("remoteSessions.filters.allAccounts"), options: (accounts.data ?? []).map((item) => ({ value: item.id, label: item.username })), onChange: setAccountId },
            { key: "protocol", value: protocol, allLabel: t("remoteSessions.filters.allProtocols"), options: [{ value: "ssh", label: "SSH" }, { value: "winrs", label: "WinRS" }], onChange: setProtocol },
            { key: "mode", value: connectionMode, allLabel: t("remoteSessions.filters.allModes"), options: ["via_bastion", "connector_local", "direct_ssh", "direct_winrm"].map((value) => ({ value, label: value })), onChange: setConnectionMode },
          ]),
        ]}
        onRefresh={() => void queryClient.invalidateQueries({ queryKey: ["remote-access", tab === "recordings" ? "recordings" : "sessions"] })}
        refreshing={sessions.isFetching || recordings.isFetching}
      />
      <TabsContent value="active">{sessions.isPending ? <Spinner label={t("common.loading")} /> : table(activeSessions, "remoteSessions.empty.active", true)}</TabsContent>
      <TabsContent value="history">{sessions.isPending ? <Spinner label={t("common.loading")} /> : table(all, "remoteSessions.empty.history", false)}</TabsContent>
      <TabsContent value="recordings">{recordings.isPending ? <Spinner label={t("common.loading")} /> : <Recordings onSelect={(row) => setRecordingId(row.id)} recordings={recordings.data?.items ?? []} />}</TabsContent>
    </Tabs>
    <RecordingDetailDialog onOpenChange={(open) => { if (!open) setRecordingId(null); }} recordingId={recordingId} />
  </PageShell>;
}
