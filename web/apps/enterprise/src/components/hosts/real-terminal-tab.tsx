import { useEffect, useMemo, useRef, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  apiErrorPresentation,
  formConstraint,
  formatApiError,
  RemoteAccessConnection,
  useApi,
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
  StatusBadge,
  TerminalEmulator,
  type TerminalLine,
} from "@argus/ui";
import { RemoteRecordingPlayer } from "./remote-recording-player";
import { MfaStepUpDialog } from "../security/mfa-step-up-dialog";

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
  const connection = useRef<RemoteAccessConnection | null>(null);
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const [stepUpOpen, setStepUpOpen] = useState(false);
  const [live, setLive] = useState<RemoteAccessSession>();
  const [lines, setLines] = useState<TerminalLine[]>([]);
  const [recording, setRecording] = useState<RemoteAccessSession>();

  const accounts = useQuery({
    queryKey: ["managed-accounts", host.id],
    queryFn: async () =>
      (await api.secrets.listManagedAccounts()).filter(
        (item) => item.host_id === host.id && item.status === "active",
      ),
  });
  const sessions = useQuery({
    queryKey: ["remote-access-sessions", host.id],
    queryFn: async () =>
      (await api.remoteAccess.listSessions()).items.filter(
        (item) => item.host_id === host.id,
      ),
  });
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
  useEffect(() => () => connection.current?.close("component_destroyed"), []);

  const refresh = () =>
    void queryClient.invalidateQueries({
      queryKey: ["remote-access-sessions"],
    });
  const openSession = async ({ accountId, reason }: SessionForm) => {
    const request = await api.remoteAccess.createRequest({
      host_id: host.id,
      managed_account_id: accountId,
      protocol,
      action: "terminal",
      reason,
    });
    if (request.status !== "authorized") {
      setError(t("hosts.preview.awaitingApproval"));
      refresh();
      return;
    }
    const lease = (await api.remoteAccess.listLeases()).items.find(
      (item) => item.request_id === request.id && !item.revoked,
    );
    if (!lease) {
      setError(t("hosts.terminal.leaseExpired"));
      return;
    }
    const session = await api.remoteAccess.createSession({
      lease_id: lease.id,
      terminal_cols: 100,
      terminal_rows: 30,
    });
    const ticket = await api.remoteAccess.createTicket(session.id);
    setLive(session);
    connection.current = new RemoteAccessConnection(ticket, {
      cols: 100,
      rows: 30,
      onFrame(frame) {
        if (frame.type === "output")
          setLines((value) => [
            ...value,
            { kind: frame.stream, content: frame.data },
          ]);
        if (frame.type === "error") setError(frame.code);
        if (
          frame.type === "state" &&
          !activeStates.includes(frame.status as RemoteAccessSession["status"])
        )
          setLive(undefined);
      },
      onClose: () => {
        setLive(undefined);
        refresh();
      },
    });
    refresh();
  };

  const presentStartError = (value: unknown, allowStepUp: boolean) => {
    if (
      allowStepUp &&
      apiErrorPresentation(value)?.code === "REMOTE_ACCESS_MFA_REQUIRED"
    ) {
      setStepUpOpen(true);
      return;
    }
    setError(
      formatApiError(value, t("hosts.terminal.failed"), (requestId) =>
        t("common.requestReference", { requestId }),
      ),
    );
  };

  const start = form.handleSubmit(async (values) => {
    setStarting(true);
    setError("");
    try {
      await openSession(values);
    } catch (value) {
      presentStartError(value, true);
    } finally {
      setStarting(false);
    }
  });

  const retryAfterStepUp = async () => {
    setStarting(true);
    setError("");
    try {
      await openSession(form.getValues());
    } catch (value) {
      presentStartError(value, false);
    } finally {
      setStarting(false);
    }
  };

  const terminate = async (session: RemoteAccessSession) => {
    await api.remoteAccess.terminateSession(session.id, "user_requested");
    if (live?.id === session.id) {
      connection.current?.close("terminated");
      setLive(undefined);
    }
    refresh();
  };

  return (
    <div className="argus-hosts-stack">
      <Card>
        <CardHeader title={t("hosts.terminal.sessionConfirmTitle")} />
        <CardContent className="argus-session-confirm">
          {live ? (
            <TerminalEmulator
              host={host.name}
              lines={lines}
              mode={protocol === "ssh" ? "pty" : "line"}
              onCommand={(command) =>
                connection.current?.input(`${command}\r\n`)
              }
              onData={(data) => connection.current?.input(data)}
              onResize={(cols, rows) => connection.current?.resize(cols, rows)}
              prompt={protocol === "winrs" ? "PS>" : "$"}
              protocol={protocol === "winrs" ? "WinRS PowerShell" : "SSH PTY"}
              state="connected"
            />
          ) : (
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
              <Button loading={starting} type="submit" variant="primary">
                {t("hosts.terminal.start")}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader title={t("hosts.terminal.activeSessions")} />
        <CardContent>
          {(sessions.data?.length ?? 0) === 0 ? (
            <EmptyState
              description=""
              title={t("hosts.terminal.noActiveSessions")}
            />
          ) : (
            sessions.data?.map((session) => (
              <div className="argus-remote-session-row" key={session.id}>
                <StatusBadge
                  tone={
                    activeStates.includes(session.status) ? "info" : "neutral"
                  }
                >
                  {session.status}
                </StatusBadge>
                <span>
                  {session.protocol === "winrs"
                    ? "WinRS PowerShell"
                    : "SSH PTY"}
                </span>
                {activeStates.includes(session.status) ? (
                  <Button
                    onClick={() => void terminate(session)}
                    size="sm"
                    variant="danger"
                  >
                    {t("hosts.terminal.terminate")}
                  </Button>
                ) : (
                  <Button
                    onClick={() => setRecording(session)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("remoteAccess.recording")}
                  </Button>
                )}
              </div>
            ))
          )}
        </CardContent>
      </Card>
      {recording && (
        <RemoteRecordingPlayer
          host={host.name}
          onClose={() => setRecording(undefined)}
          protocol={recording.protocol}
          recordingId={recording.recording_id}
        />
      )}
      <MfaStepUpDialog
        onComplete={retryAfterStepUp}
        onOpenChange={setStepUpOpen}
        open={stepUpOpen}
      />
    </div>
  );
}
