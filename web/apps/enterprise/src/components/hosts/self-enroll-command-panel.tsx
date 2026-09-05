import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  formatApiError,
  useApi,
  type ActionOneTimeResult,
  type ConfirmActionResult,
  type Host,
  type PendingActionPublic,
} from "@argus/api-client";
import { Alert, Button } from "@argus/ui";

import { formatDateTime } from "../settings/shared";
import { PendingActionConfirm } from "./pending-action-confirm";
import { InstallInstructionPanel } from "./install-instruction-panel";

/** Maintenance actions for a registered self-enrolled host. */
export function SelfEnrollCommandPanel({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [kind, setKind] = useState<"install" | "uninstall" | null>(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [result, setResult] = useState<ActionOneTimeResult | null>(null);
  const [loading, setLoading] = useState<"install" | "uninstall" | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setResult(null);
    setPendingAction(null);
    setKind(null);
  }, [host.id]);

  if (host.connection_mode !== "self_enrolled") return null;

  const preview = async (nextKind: "install" | "uninstall") => {
    setLoading(nextKind);
    setError(null);
    setResult(null);
    setKind(nextKind);
    try {
      setPendingAction(
        nextKind === "install"
          ? await api.hosts.previewEnrollmentRotate(
              host.id,
              host.resource_version,
            )
          : await api.hosts.previewUninstallCommand(
              host.id,
              host.resource_version,
            ),
      );
    } catch (cause) {
      setError(
        formatApiError(cause, t("hosts.selfEnrollPanel.failed"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
      setKind(null);
    } finally {
      setLoading(null);
    }
  };

  const done = (value: ConfirmActionResult) => {
    if (value.one_time_result) setResult(value.one_time_result);
    setPendingAction(null);
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["executions"] });
  };

  return (
    <div className="argus-detail-section">
      <h3>{t("hosts.selfEnrollPanel.title")}</h3>
      <p className="argus-muted">{t("hosts.selfEnrollPanel.desc")}</p>
      {error && (
        <Alert
          description={error}
          title={t("hosts.selfEnrollPanel.failed")}
          tone="danger"
        />
      )}
      {result ? (
        <>
          <Alert
            description={t("hosts.wizard.commandOnce")}
            title={
              kind === "uninstall"
                ? t("hosts.selfEnrollPanel.uninstallResult")
                : t("hosts.wizard.commandTitle")
            }
            tone={kind === "uninstall" ? "warning" : "success"}
          />
          <InstallInstructionPanel result={result} />
          <p className="argus-muted">
            {t("hosts.wizard.commandExpires", {
              time: formatDateTime(result.expires_at),
            })}
          </p>
          <div className="argus-form-actions">
            <Button
              onClick={() => {
                setResult(null);
                setKind(null);
              }}
              variant="secondary"
            >
              {t("hosts.selfEnrollPanel.dismiss")}
            </Button>
          </div>
        </>
      ) : pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult
          onCancel={() => {
            setPendingAction(null);
            setKind(null);
          }}
          onDone={done}
        />
      ) : (
        <div className="argus-form-actions">
          <Button
            loading={loading === "install"}
            onClick={() => void preview("install")}
            variant="secondary"
          >
            {t("hosts.selfEnrollPanel.install")}
          </Button>
          <Button
            loading={loading === "uninstall"}
            onClick={() => void preview("uninstall")}
            variant="secondary"
          >
            {t("hosts.selfEnrollPanel.uninstall")}
          </Button>
        </div>
      )}
    </div>
  );
}
