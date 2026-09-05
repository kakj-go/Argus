import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import {
  formatApiError,
  formatErrorCode,
  useApi,
  type ActionOneTimeResult,
  type BastionScope,
  type ConfirmActionResult,
  type Host,
  type PendingActionPublic,
} from "@argus/api-client";
import { Alert, Badge, Button, Dialog } from "@argus/ui";

import { formatDateTime } from "../settings/shared";
import { PendingActionConfirm } from "./pending-action-confirm";
import { InstallInstructionPanel } from "./install-instruction-panel";
import {
  connectorInstallEventStatusLabel,
  connectorInstallStageLabel,
  connectorInstallStatusLabel,
} from "./connector-install-presentation";

function useInvalidateResources() {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["bastion-scopes"] });
    void queryClient.invalidateQueries({ queryKey: ["connectors"] });
    void queryClient.invalidateQueries({ queryKey: ["executions"] });
  };
}

function OneTimeCommandDialog({
  result,
  onClose,
}: {
  result: ActionOneTimeResult;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog
      description={t("hosts.wizard.commandOnce")}
      onOpenChange={(next) => !next && onClose()}
      open
      title={t("hosts.wizard.commandTitle")}
      width={680}
    >
      <InstallInstructionPanel result={result} />
      <p className="argus-muted">
        {t("hosts.wizard.commandExpires", {
          time: formatDateTime(result.expires_at),
        })}
      </p>
    </Dialog>
  );
}

function ScopeOperationDialog({
  scope,
  open,
  onOpenChange,
}: {
  scope: BastionScope;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const operationId = scope.onboarding.operation_id;
  const operationQuery = useQuery({
    queryKey: ["connector-install-operation", operationId],
    queryFn: () => api.connectors.getInstallOperation(operationId!),
    enabled: open && Boolean(operationId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return open &&
        (!status ||
          !["succeeded", "failed", "expired", "result_unknown"].includes(
            status,
          ))
        ? 1000
        : false;
    },
  });
  const operation = operationQuery.data;
  return (
    <Dialog
      description={t("hosts.bastionForm.installProgressHint")}
      onOpenChange={onOpenChange}
      open={open}
      title={t("hosts.bastionForm.installProgress")}
      width={760}
    >
      {!operationId && (
        <Alert
          description={t("hosts.pendingActions.operationMissing")}
          title={t("hosts.pendingActions.failed")}
          tone="danger"
        />
      )}
      {operationQuery.isError && (
        <Alert
          description={t("hosts.pendingActions.operationLoadFailed")}
          title={t("hosts.pendingActions.failed")}
          tone="danger"
        />
      )}
      {operation && (
        <div className="argus-operation-progress">
          <div className="argus-operation-progress__head">
            <strong>{connectorInstallStageLabel(t, operation.stage)}</strong>
            <Badge
              tone={
                ["failed", "expired", "result_unknown"].includes(
                  operation.status,
                )
                  ? "danger"
                  : operation.status === "succeeded"
                    ? "success"
                    : "info"
              }
            >
              {connectorInstallStatusLabel(t, operation.status)}
            </Badge>
          </div>
          <ol className="argus-operation-timeline">
            {operation.events.map((event) => (
              <li className={`is-${event.status}`} key={event.id}>
                <strong>{connectorInstallStageLabel(t, event.stage)}</strong>
                <span>
                  {event.error_code
                    ? formatErrorCode(
                        event.error_code,
                        t("hosts.pendingActions.failed"),
                      )
                    : connectorInstallEventStatusLabel(t, event.status)}
                </span>
              </li>
            ))}
          </ol>
        </div>
      )}
    </Dialog>
  );
}

/** Pending Bastion Scope actions are derived only from the server projection. */
export function PendingScopeActions({ scope }: { scope: BastionScope }) {
  const { t } = useTranslation();
  const api = useApi();
  const invalidate = useInvalidateResources();
  const [actionKind, setActionKind] = useState<
    "rotate" | "retry" | "delete" | null
  >(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [command, setCommand] = useState<ActionOneTimeResult | null>(null);
  const [operationOpen, setOperationOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const onboarding = scope.onboarding;

  const fail = (cause: unknown) =>
    setError(
      formatApiError(cause, t("hosts.pendingActions.failed"), (requestId) =>
        t("common.requestReference", { requestId }),
      ),
    );

  const claimExisting = async () => {
    if (!onboarding.execution_id) {
      setError(t("hosts.pendingActions.resultMissing"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      setCommand(
        await api.executions.claimOneTimeResult(onboarding.execution_id),
      );
      invalidate();
    } catch (cause) {
      fail(cause);
      invalidate();
    } finally {
      setBusy(false);
    }
  };

  const preview = async (kind: "rotate" | "retry" | "delete") => {
    setBusy(true);
    setError(null);
    setActionKind(kind);
    try {
      const action =
        kind === "rotate"
          ? await api.connectors.previewEnrollmentRotate(
              scope.id,
              scope.resource_version,
            )
          : kind === "retry" && onboarding.operation_id
            ? await api.connectors.previewRetryInstallOperation(
                onboarding.operation_id,
              )
            : await api.connectors.previewDeleteBastionScope(
                scope.id,
                scope.resource_version,
              );
      setPendingAction(action);
    } catch (cause) {
      fail(cause);
      setActionKind(null);
    } finally {
      setBusy(false);
    }
  };

  const done = (result: ConfirmActionResult) => {
    if (actionKind === "rotate" && result.one_time_result) {
      setCommand(result.one_time_result);
    }
    setPendingAction(null);
    setActionKind(null);
    invalidate();
  };

  return (
    <div className="argus-scope-card__actions">
      {error && (
        <Alert
          description={error}
          title={t("hosts.pendingActions.failed")}
          tone="danger"
        />
      )}
      {onboarding.state === "command_available" && (
        <Button
          disabled={busy || !onboarding.execution_id}
          onClick={() => void claimExisting()}
          variant="secondary"
        >
          {t("hosts.pendingActions.claimCommand")}
        </Button>
      )}
      {(onboarding.state === "command_consumed" ||
        onboarding.state === "command_expired") &&
        scope.onboarding_mode === "command" && (
          <Button
            disabled={busy}
            onClick={() => void preview("rotate")}
            variant="secondary"
          >
            {t("hosts.pendingActions.rotateCommand")}
          </Button>
        )}
      {onboarding.state === "awaiting_approval" && (
        <Link
          className="argus-button argus-button--secondary"
          search={{ approval: "operation", scope: "mine" }}
          to="/approvals"
        >
          {t("hosts.pendingActions.viewApproval")}
        </Link>
      )}
      {onboarding.state === "installing" && (
        <Button onClick={() => setOperationOpen(true)} variant="secondary">
          {t("hosts.pendingActions.viewProgress")}
        </Button>
      )}
      {onboarding.state === "install_failed" && (
        <>
          <Button
            disabled={busy || !onboarding.operation_id}
            onClick={() => void preview("retry")}
            variant="primary"
          >
            {t("hosts.bastionForm.retryInstall")}
          </Button>
          <Button onClick={() => setOperationOpen(true)} variant="secondary">
            {t("hosts.pendingActions.viewFailure")}
          </Button>
        </>
      )}
      <Button
        disabled={busy}
        onClick={() => void preview("delete")}
        variant="ghost"
      >
        {t("hosts.pendingActions.delete")}
      </Button>
      {pendingAction && actionKind && (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult={actionKind === "rotate"}
          onCancel={() => {
            setPendingAction(null);
            setActionKind(null);
          }}
          onDone={done}
        />
      )}
      {command && (
        <OneTimeCommandDialog
          result={command}
          onClose={() => setCommand(null)}
        />
      )}
      <ScopeOperationDialog
        onOpenChange={setOperationOpen}
        open={operationOpen}
        scope={scope}
      />
    </div>
  );
}

/** Pending self-enrolled host actions are derived only from onboarding. */
export function SelfEnrollHostActions({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();
  const invalidate = useInvalidateResources();
  const [actionKind, setActionKind] = useState<"rotate" | "delete" | null>(
    null,
  );
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [command, setCommand] = useState<ActionOneTimeResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (
    host.connection_mode !== "self_enrolled" ||
    host.connection_status !== "onboarding"
  ) {
    return null;
  }

  const claimExisting = async () => {
    if (!host.onboarding.execution_id) {
      setError(t("hosts.pendingActions.resultMissing"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      setCommand(
        await api.executions.claimOneTimeResult(host.onboarding.execution_id),
      );
      invalidate();
    } catch (cause) {
      setError(
        formatApiError(cause, t("hosts.pendingActions.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
      invalidate();
    } finally {
      setBusy(false);
    }
  };

  const preview = async (kind: "rotate" | "delete") => {
    setBusy(true);
    setError(null);
    setActionKind(kind);
    try {
      setPendingAction(
        kind === "rotate"
          ? await api.hosts.previewEnrollmentRotate(
              host.id,
              host.resource_version,
            )
          : await api.hosts.previewDeleteResource(
              host.id,
              host.resource_version,
            ),
      );
    } catch (cause) {
      setError(
        formatApiError(cause, t("hosts.pendingActions.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
      setActionKind(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="argus-scope-card__actions">
      {error && (
        <Alert
          description={error}
          title={t("hosts.pendingActions.failed")}
          tone="danger"
        />
      )}
      {host.onboarding.state === "command_available" && (
        <Button
          disabled={busy || !host.onboarding.execution_id}
          onClick={() => void claimExisting()}
          variant="secondary"
        >
          {t("hosts.pendingActions.claimCommand")}
        </Button>
      )}
      {(host.onboarding.state === "command_consumed" ||
        host.onboarding.state === "command_expired") && (
        <Button
          disabled={busy}
          onClick={() => void preview("rotate")}
          variant="secondary"
        >
          {t("hosts.pendingActions.rotateCommand")}
        </Button>
      )}
      {host.onboarding.state === "awaiting_approval" && (
        <Link
          className="argus-button argus-button--secondary"
          search={{ approval: "operation", scope: "mine" }}
          to="/approvals"
        >
          {t("hosts.pendingActions.viewApproval")}
        </Link>
      )}
      <Button
        disabled={busy}
        onClick={() => void preview("delete")}
        variant="ghost"
      >
        {t("hosts.pendingActions.delete")}
      </Button>
      {pendingAction && actionKind && (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult={actionKind === "rotate"}
          onCancel={() => {
            setPendingAction(null);
            setActionKind(null);
          }}
          onDone={(result) => {
            if (actionKind === "rotate" && result.one_time_result) {
              setCommand(result.one_time_result);
            }
            setPendingAction(null);
            setActionKind(null);
            invalidate();
          }}
        />
      )}
      {command && (
        <OneTimeCommandDialog
          result={command}
          onClose={() => setCommand(null)}
        />
      )}
    </div>
  );
}
