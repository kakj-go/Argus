import { forwardRef, useCallback, useImperativeHandle, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatApiError,
  useApi,
  useTerminalSessions,
  type AccessRequest,
  type Host,
  type RemoteAccessSession,
  type SessionTicketResult,
} from "@argus/api-client";
import { MfaStepUpDialog } from "../security/mfa-step-up-dialog";

type TerminalPanelProps = { host: Host };

export type TerminalPanelHandle = {
  createSession: (accountId: string, reason: string) => Promise<void>;
  attachSession: (
    session: RemoteAccessSession,
    ticket: SessionTicketResult,
  ) => Promise<void>;
  /** 重新显示本地已连接会话的 Dock 标签页（不新建连接）。 */
  showSession: (sessionId: string) => void;
};

export const TerminalPanel = forwardRef<
  TerminalPanelHandle,
  TerminalPanelProps
>(({ host }, ref) => {
  const { t } = useTranslation();
  const api = useApi();
  const { sessions, attachSession, showSession } = useTerminalSessions();
  const [stepUpOpen, setStepUpOpen] = useState(false);
  const [pendingRequestId, setPendingRequestId] = useState<string>();
  const [pendingAccountName, setPendingAccountName] = useState("unknown");

  const protocol = host.connection_mode === "direct_winrm" ? "winrs" : "ssh";

  const continueRequest = useCallback(
    async (request: AccessRequest, accountName: string) => {
      if (request.status === "awaiting_mfa") {
        setPendingRequestId(request.id);
        setPendingAccountName(accountName);
        setStepUpOpen(true);
        return;
      }
      setPendingRequestId(undefined);
      let resolved = request;
      if (request.status === "awaiting_approval") {
        for (let attempt = 0; attempt < 300; attempt += 1) {
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
          resolved = await api.remoteAccess.getRequest(request.id);
          if (resolved.status !== "awaiting_approval") break;
        }
      }
      if (resolved.status !== "authorized")
        throw new Error(t("hosts.preview.awaitingApproval"));
      const lease = (await api.remoteAccess.listLeases()).items.find(
        (item) => item.request_id === request.id && !item.revoked,
      );
      if (!lease) throw new Error(t("hosts.terminal.leaseExpired"));
      const session = await api.remoteAccess.createSession({
        lease_id: lease.id,
        terminal_cols: 100,
        terminal_rows: 30,
      });
      const ticket = await api.remoteAccess.createTicket(session.id);
      await attachSession(session.id, session, ticket, host.name, accountName);
    },
    [api, attachSession, host.name, t],
  );

  useImperativeHandle(
    ref,
    () => ({
      createSession: async (accountId: string, reason: string) => {
        const account = (await api.secrets.listManagedAccounts()).find(
          (item) => item.id === accountId,
        );
        const request = await api.remoteAccess.createRequest({
          host_id: host.id,
          managed_account_id: accountId,
          protocol,
          action: "terminal",
          reason,
        });
        await continueRequest(request, account?.username || "unknown");
      },
      showSession: (sessionId: string) => showSession(sessionId),
      attachSession: async (
        session: RemoteAccessSession,
        ticket: SessionTicketResult,
      ) => {
        if (sessions.has(session.id)) {
          const current = sessions.get(session.id)!;
          await attachSession(
            session.id,
            session,
            ticket,
            current.hostName,
            current.accountName,
          );
          return;
        }
        const account = (await api.secrets.listManagedAccounts()).find(
          (item) => item.id === session.managed_account_id,
        );
        await attachSession(
          session.id,
          session,
          ticket,
          host.name,
          account?.username || "unknown",
        );
      },
    }),
    [
      api,
      attachSession,
      continueRequest,
      host.id,
      host.name,
      protocol,
      sessions,
      showSession,
    ],
  );

  const retryAfterStepUp = async () => {
    if (!pendingRequestId) return;
    try {
      await continueRequest(
        await api.remoteAccess.resumeRequest(pendingRequestId),
        pendingAccountName,
      );
    } catch (error) {
      throw new Error(
        formatApiError(error, t("hosts.terminal.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
    }
  };

  return (
    <MfaStepUpDialog
      onComplete={retryAfterStepUp}
      onOpenChange={setStepUpOpen}
      open={stepUpOpen}
    />
  );
});

TerminalPanel.displayName = "TerminalPanel";
