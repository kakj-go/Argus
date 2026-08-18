import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  RemoteAccessConnection,
  useApi,
  type Host,
  type RemoteAccessSession,
} from "@argus/api-client";
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  Field,
  Input,
  Select,
  StatusBadge,
  TerminalEmulator,
  type TerminalLine,
} from "@argus/ui";
import { RemoteRecordingPlayer } from "./remote-recording-player";

const activeStates: RemoteAccessSession["status"][] = ["authorized", "connecting", "active", "terminating"];

export function RealTerminalTab({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const connection = useRef<RemoteAccessConnection | null>(null);
  const [accountId, setAccountId] = useState("");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const [live, setLive] = useState<RemoteAccessSession>();
  const [lines, setLines] = useState<TerminalLine[]>([]);
  const [recording, setRecording] = useState<RemoteAccessSession>();

  const accounts = useQuery({ queryKey: ["managed-accounts", host.id], queryFn: async () => (await api.secrets.listManagedAccounts()).filter((item) => item.host_id === host.id && item.status === "active") });
  const sessions = useQuery({ queryKey: ["remote-access-sessions", host.id], queryFn: async () => (await api.remoteAccess.listSessions()).items.filter((item) => item.host_id === host.id) });
  const protocol = host.connection_mode === "direct_winrm" ? "winrs" : "ssh";
  const options = (accounts.data ?? []).filter((item) => item.allowed_protocols.includes(protocol === "winrs" ? "winrm" : "ssh")).map((item) => ({ value: item.id, label: item.username }));
  useEffect(() => () => connection.current?.close("component_destroyed"), []);

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["remote-access-sessions"] });
  const start = async () => {
    if (!accountId || !reason.trim()) { setError(!accountId ? t("hosts.terminal.needAccount") : t("hosts.terminal.needReason")); return; }
    setStarting(true); setError("");
    try {
      const request = await api.remoteAccess.createRequest({ host_id: host.id, managed_account_id: accountId, protocol, action: "terminal", reason: reason.trim() });
      if (request.status !== "authorized") { setError(t("hosts.preview.awaitingApproval")); refresh(); return; }
      const lease = (await api.remoteAccess.listLeases()).items.find((item) => item.request_id === request.id && !item.revoked);
      if (!lease) { setError("REMOTE_ACCESS_LEASE_EXPIRED"); return; }
      const session = await api.remoteAccess.createSession({ lease_id: lease.id, terminal_cols: 100, terminal_rows: 30 });
      const ticket = await api.remoteAccess.createTicket(session.id);
      setLive(session);
      connection.current = new RemoteAccessConnection(ticket, {
        cols: 100,
        rows: 30,
        onFrame(frame) {
          if (frame.type === "output") setLines((value) => [...value, { kind: frame.stream, content: frame.data }]);
          if (frame.type === "error") setError(frame.code);
          if (frame.type === "state" && !activeStates.includes(frame.status as RemoteAccessSession["status"])) setLive(undefined);
        },
        onClose: () => { setLive(undefined); refresh(); },
      });
      refresh();
    } catch (value) { setError(value instanceof Error ? value.message : "REMOTE_ACCESS_CONNECTION_LOST"); }
    finally { setStarting(false); }
  };

  const terminate = async (session: RemoteAccessSession) => {
    await api.remoteAccess.terminateSession(session.id, "user_requested");
    if (live?.id === session.id) { connection.current?.close("terminated"); setLive(undefined); }
    refresh();
  };

  return <div className="argus-hosts-stack">
    <Card>
      <CardHeader title={t("hosts.terminal.sessionConfirmTitle")} />
      <CardContent className="argus-session-confirm">
        {live ? <TerminalEmulator
          host={host.name}
          lines={lines}
          mode={protocol === "ssh" ? "pty" : "line"}
          onCommand={(command) => connection.current?.input(`${command}\r\n`)}
          onData={(data) => connection.current?.input(data)}
          onResize={(cols, rows) => connection.current?.resize(cols, rows)}
          prompt={protocol === "winrs" ? "PS>" : "$"}
          protocol={protocol === "winrs" ? "WinRS PowerShell" : "SSH PTY"}
          state="connected"
        /> : <>
          <div className="argus-form-row">
            <Field label={t("hosts.terminal.account")}><Select ariaLabel={t("hosts.terminal.account")} onValueChange={setAccountId} options={options} placeholder={t("hosts.terminal.accountPlaceholder")} value={accountId} /></Field>
            <Field error={error || undefined} label={t("hosts.terminal.reason")}><Input onChange={(event) => setReason(event.target.value)} value={reason} /></Field>
          </div>
          <Button loading={starting} onClick={() => void start()} variant="primary">{t("hosts.terminal.start")}</Button>
        </>}
      </CardContent>
    </Card>
    <Card><CardHeader title={t("hosts.terminal.activeSessions")} /><CardContent>
      {(sessions.data?.length ?? 0) === 0 ? <EmptyState description="" title={t("hosts.terminal.noActiveSessions")} /> : sessions.data?.map((session) => <div className="argus-remote-session-row" key={session.id}><StatusBadge tone={activeStates.includes(session.status) ? "info" : "neutral"}>{session.status}</StatusBadge><span>{session.protocol === "winrs" ? "WinRS PowerShell" : "SSH PTY"}</span>{activeStates.includes(session.status) ? <Button onClick={() => void terminate(session)} size="sm" variant="danger">{t("hosts.terminal.terminate")}</Button> : <Button onClick={() => setRecording(session)} size="sm" variant="ghost">{t("remoteAccess.recording")}</Button>}</div>)}
    </CardContent></Card>
    {recording && <RemoteRecordingPlayer host={host.name} onClose={() => setRecording(undefined)} protocol={recording.protocol} recordingId={recording.recording_id} />}
  </div>;
}
