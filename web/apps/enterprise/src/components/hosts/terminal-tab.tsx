import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useApi,
  type BastionScope,
  type Host,
  type RemoteSession,
} from "@argus/api-client";
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  Input,
  KeyValueGrid,
  StatusBadge,
  TerminalEmulator,
  type Column,
  type TerminalLine,
} from "@argus/ui";
import { Table } from "./data-table";
import {
  connectionPathKey,
  formatDateTime,
  formatDuration,
  scopeOf,
} from "./host-utils";

/** 常见命令的演示输出（mock 终端脚本）。 */
function commandOutput(
  command: string,
  host: Host,
  account: string,
): TerminalLine[] {
  const out = (content: string): TerminalLine => ({ kind: "stdout", content });
  const err = (content: string): TerminalLine => ({ kind: "stderr", content });
  const [raw] = command.trim().split(/\s+/);
  const cmd = raw ?? "";
  switch (cmd) {
    case "ls":
      return [out("app  etc  home  opt  srv  usr  var")];
    case "ll":
    case "ls -la":
      return [
        out("drwxr-xr-x  2 root root 4096 Jun 12 09:41 app"),
        out("drwxr-xr-x 88 root root 4096 Jun 10 18:02 etc"),
        out("drwxr-xr-x  3 root root 4096 May 30 11:20 home"),
        out("drwxr-xr-x  4 root root 4096 Jun 01 03:12 var"),
      ];
    case "top":
      return [
        out(`top - up 42 days, 3:17, 2 users, load average: 0.42, 0.38, 0.31`),
        out("%Cpu(s):  6.2 us,  2.1 sy,  0.0 ni, 90.9 id"),
        out("MiB Mem :  15920.4 total,   9041.2 free,   4412.8 used"),
        out("  PID USER      %CPU %MEM  COMMAND"),
        out(" 1842 app        4.3 12.1  java -jar app.jar"),
        out("  971 root       1.2  0.6  argus-otelcol"),
      ];
    case "df":
      return [
        out("Filesystem      Size  Used Avail Use% Mounted on"),
        out("/dev/vda1        80G   47G   33G  59% /"),
        out("/dev/vdb1       200G  122G   78G  61% /var/lib/data"),
      ];
    case "uname":
      return [
        out(
          `Linux ${host.hostname || host.name} 5.15.0-91-generic #101-Ubuntu SMP x86_64 GNU/Linux`,
        ),
      ];
    case "whoami":
      return [out(account)];
    case "pwd":
      return [out(`/home/${account}`)];
    case "free":
      return [
        out("               total        used        free"),
        out("Mem:        15920432     4412800     9041920"),
        out("Swap:        2097152           0     2097152"),
      ];
    case "uptime":
      return [
        out(
          " 10:42:01 up 42 days,  3:17,  2 users,  load average: 0.42, 0.38, 0.31",
        ),
      ];
    case "clear":
      return [];
    default:
      return [err(`bash: ${command}: command not found`)];
  }
}

/** 历史会话录像的预设回放内容（确定性生成）。 */
function replayLines(session: RemoteSession, host: Host): TerminalLine[] {
  const at = (offsetMinutes: number) =>
    new Date(
      new Date(session.startedAt ?? session.createdAt).getTime() +
        offsetMinutes * 60_000,
    ).toLocaleTimeString();
  const target = `${session.targetAccount}@${host.hostname || host.name}`;
  return [
    {
      kind: "stdout",
      time: at(0),
      content: `# session ${session.id} → ${target} (${session.protocol})`,
    },
    { kind: "stdin", time: at(1), content: "uname -a" },
    {
      kind: "stdout",
      time: at(1),
      content: `Linux ${host.hostname || host.name} 5.15.0-91-generic x86_64`,
    },
    { kind: "stdin", time: at(3), content: "df -h" },
    {
      kind: "stdout",
      time: at(3),
      content: "/dev/vda1   80G   47G   33G  59% /",
    },
    { kind: "stdin", time: at(6), content: "top -b -n1 | head -5" },
    {
      kind: "stdout",
      time: at(6),
      content: "%Cpu(s):  6.2 us,  2.1 sy, 90.9 id",
    },
    { kind: "stdin", time: at(9), content: "exit" },
    { kind: "stdout", time: at(9), content: `# session ${session.id} closed` },
  ];
}

const ACTIVE_STATUSES: RemoteSession["status"][] = [
  "requested",
  "awaiting_approval",
  "authorized",
  "connecting",
  "active",
];

/** 详情页「终端与会话」Tab：确认卡 → TerminalEmulator 会话 + 活动/历史会话表。 */
export function TerminalTab({
  host,
  scopes,
}: {
  host: Host;
  scopes: BastionScope[];
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [account, setAccount] = useState("ops");
  const [reason, setReason] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [liveSession, setLiveSession] = useState<RemoteSession | null>(null);
  const [liveLines, setLiveLines] = useState<TerminalLine[]>([]);
  const [ending, setEnding] = useState(false);
  const [terminateTarget, setTerminateTarget] = useState<RemoteSession | null>(
    null,
  );
  const [terminating, setTerminating] = useState(false);
  const [replayTarget, setReplayTarget] = useState<RemoteSession | null>(null);

  const sessionsQuery = useQuery({
    queryKey: ["remote-sessions", "host", host.id],
    queryFn: () => api.hosts.listSessions({ hostId: host.id }),
  });
  const sessions = sessionsQuery.data ?? [];
  const activeSessions = sessions.filter((session) =>
    ACTIVE_STATUSES.includes(session.status),
  );
  const historySessions = sessions.filter(
    (session) => !ACTIVE_STATUSES.includes(session.status),
  );

  const invalidateSessions = () => {
    void queryClient.invalidateQueries({ queryKey: ["remote-sessions"] });
  };

  const protocol = host.connectionMode === "direct_winrm" ? "winrm" : "ssh";
  const scope = scopeOf(host, scopes);
  const path = t(`hosts.path.${connectionPathKey(host)}`, {
    scope: scope?.name ?? host.bastionScopeId ?? "",
    address: `${host.address}:${host.port}`,
  });

  const startSession = async () => {
    if (!account.trim()) {
      setFormError(t("hosts.terminal.needAccount"));
      return;
    }
    if (!reason.trim()) {
      setFormError(t("hosts.terminal.needReason"));
      return;
    }
    setFormError(null);
    setStarting(true);
    try {
      const session = await api.hosts.createSession({
        hostId: host.id,
        targetAccount: account.trim(),
        reason: reason.trim(),
        protocol,
      });
      setLiveSession(session);
      setLiveLines([
        {
          kind: "stdout",
          content: `# ${t("hosts.terminal.connectedAs", { account: session.targetAccount, host: host.hostname || host.name })} · ${session.protocol}`,
        },
      ]);
      invalidateSessions();
    } finally {
      setStarting(false);
    }
  };

  const endLiveSession = async () => {
    if (!liveSession || ending) return;
    setEnding(true);
    try {
      await api.hosts.terminateSession(liveSession.id);
      setLiveSession(null);
      setLiveLines([]);
      invalidateSessions();
    } finally {
      setEnding(false);
    }
  };

  const onCommand = (command: string) => {
    if (!liveSession) return;
    if (command.trim() === "exit") {
      void endLiveSession();
      return;
    }
    setLiveLines((previous) => [
      ...previous,
      ...commandOutput(command, host, liveSession.targetAccount),
    ]);
  };

  const confirmTerminate = async () => {
    if (!terminateTarget || terminating) return;
    setTerminating(true);
    try {
      await api.hosts.terminateSession(terminateTarget.id);
      setTerminateTarget(null);
      invalidateSessions();
    } finally {
      setTerminating(false);
    }
  };

  const sessionColumns: Column<RemoteSession>[] = [
    {
      key: "user",
      header: t("hosts.terminal.user"),
      render: (session) => session.userName ?? session.userId,
    },
    { key: "targetAccount", header: t("hosts.terminal.identity") },
    { key: "protocol", header: t("hosts.terminal.protocol") },
    {
      key: "startedAt",
      header: t("hosts.terminal.startedAt"),
      render: (session) => formatDateTime(session.startedAt),
    },
    {
      key: "duration",
      header: t("hosts.terminal.duration"),
      render: (session) => formatDuration(session.startedAt, session.endedAt),
    },
  ];

  return (
    <div className="argus-hosts-stack">
      {liveSession ? (
        <Card>
          <CardHeader
            action={
              <Button
                loading={ending}
                onClick={() => void endLiveSession()}
                variant="danger"
              >
                {t("hosts.terminal.end")}
              </Button>
            }
            title={t("hosts.terminal.newSession")}
          />
          <CardContent>
            <TerminalEmulator
              host={host.name}
              lines={liveLines}
              onCommand={onCommand}
              prompt={protocol === "winrm" ? "PS>" : "$"}
              protocol={protocol}
              startedAt={liveSession.startedAt}
              state="connected"
            />
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader title={t("hosts.terminal.sessionConfirmTitle")} />
          <CardContent className="argus-session-confirm">
            <KeyValueGrid
              columns={3}
              items={[
                {
                  label: t("hosts.terminal.target"),
                  value: `${host.name}（${host.address}:${host.port}）`,
                },
                { label: t("hosts.row.path"), value: path },
                {
                  label: t("hosts.terminal.protocol"),
                  value: protocol.toUpperCase(),
                },
                {
                  label: t("hosts.terminal.maxDuration"),
                  value: t("hosts.terminal.maxDurationValue"),
                },
                {
                  label: t("hosts.terminal.recordingPolicy"),
                  value: t("hosts.terminal.recordingPolicyValue"),
                },
              ]}
            />
            <div className="argus-form-row">
              <Field label={t("hosts.terminal.account")}>
                <Input
                  onChange={(event) => setAccount(event.target.value)}
                  placeholder={t("hosts.terminal.accountPlaceholder")}
                  value={account}
                />
              </Field>
              <Field
                error={formError ?? undefined}
                label={t("hosts.terminal.reason")}
              >
                <Input
                  onChange={(event) => setReason(event.target.value)}
                  placeholder={t("hosts.terminal.reasonPlaceholder")}
                  value={reason}
                />
              </Field>
            </div>
            <div className="argus-form-actions">
              <Button
                loading={starting}
                onClick={() => void startSession()}
                variant="primary"
              >
                {starting
                  ? t("hosts.terminal.starting")
                  : t("hosts.terminal.start")}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader title={t("hosts.terminal.activeSessions")} />
        <CardContent>
          {activeSessions.length > 0 ? (
            <Table
              columns={[
                ...sessionColumns,
                {
                  key: "actions",
                  header: t("hosts.row.actions"),
                  render: (session) => (
                    <Button
                      onClick={() => setTerminateTarget(session)}
                      size="sm"
                      variant="danger"
                    >
                      {t("hosts.terminal.terminate")}
                    </Button>
                  ),
                },
              ]}
              data={activeSessions}
              getRowKey={(session) => session.id}
            />
          ) : (
            <EmptyState
              description=""
              title={t("hosts.terminal.noActiveSessions")}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader title={t("hosts.terminal.historySessions")} />
        <CardContent>
          {historySessions.length > 0 ? (
            <Table
              columns={[
                ...sessionColumns,
                {
                  key: "status",
                  header: t("hosts.row.status"),
                  render: (session) => (
                    <StatusBadge tone="neutral">{session.status}</StatusBadge>
                  ),
                },
                {
                  key: "actions",
                  header: t("hosts.row.actions"),
                  render: (session) =>
                    session.recordingRef ? (
                      <Button
                        onClick={() => setReplayTarget(session)}
                        size="sm"
                        variant="secondary"
                      >
                        {t("hosts.terminal.replay")}
                      </Button>
                    ) : null,
                },
              ]}
              data={historySessions}
              getRowKey={(session) => session.id}
            />
          ) : (
            <EmptyState
              description=""
              title={t("hosts.terminal.noHistorySessions")}
            />
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        danger
        description={t("hosts.terminal.terminateDesc")}
        loading={terminating}
        onConfirm={() => void confirmTerminate()}
        onOpenChange={(open) => {
          if (!open) setTerminateTarget(null);
        }}
        open={terminateTarget !== null}
        title={t("hosts.terminal.terminateTitle")}
      >
        {terminateTarget && (
          <p className="argus-mono">
            {terminateTarget.id} · {terminateTarget.userName} →{" "}
            {terminateTarget.targetAccount}@{host.name}
          </p>
        )}
      </ConfirmDialog>

      <Dialog
        onOpenChange={(open) => {
          if (!open) setReplayTarget(null);
        }}
        open={replayTarget !== null}
        title={t("hosts.terminal.replayTitle", {
          id: replayTarget?.id ?? "",
        })}
      >
        {replayTarget && (
          <TerminalEmulator
            host={host.name}
            lines={replayLines(replayTarget, host)}
            protocol={replayTarget.protocol}
            readOnly
            state="disconnected"
          />
        )}
      </Dialog>
    </div>
  );
}
