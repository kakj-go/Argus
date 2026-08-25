import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatErrorCode,
  useApi,
  type ConfirmActionResult,
  type PendingActionPublic,
} from "@argus/api-client";
import { PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";
import { presentPendingAction } from "../pending-action-presentation";

function diffLinesOf(action: PendingActionPublic) {
  return action.diff.map((line) => ({
    type:
      line.kind === "add"
        ? ("add" as const)
        : line.kind === "remove"
          ? ("remove" as const)
          : ("context" as const),
    content: line.text,
  }));
}

/**
 * Pending Action 确认卡：渲染 preview/diff，确认和取消都提交服务端状态。
 * confirm 后若无 task 返回，说明进入 awaiting_approval。
 */
export function PendingActionConfirm({
  action,
  claimOneTimeResult = false,
  onDone,
  onCancel,
}: {
  action: PendingActionPublic;
  claimOneTimeResult?: boolean;
  onDone?: (result: ConfirmActionResult) => void;
  onCancel?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const presented = presentPendingAction(action, t);
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string | undefined>();
  const [confirming, setConfirming] = useState(false);

  const confirm = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      let result = await api.approvals.confirm(action.action_ref);
      if (claimOneTimeResult && result.execution && !result.one_time_result) {
        const executionId = result.execution.execution_id;
        for (let attempt = 0; attempt < 120; attempt += 1) {
          const execution = await api.executions.get(executionId);
          result = { ...result, execution };
          if (execution.status === "succeeded") {
            if (execution.one_time_result_available) {
              result = {
                ...result,
                one_time_result: await api.executions.claimOneTimeResult(
                  execution.execution_id,
                ),
              };
            }
            break;
          }
          if (
            execution.status === "failed" ||
            execution.status === "cancelled"
          ) {
            throw new Error(formatErrorCode(execution.error_code, "Execution failed"));
          }
          await new Promise((resolve) => window.setTimeout(resolve, 500));
        }
      }
      setStatus("success");
      setResultMessage(
        result.pending_action.status === "awaiting_approval"
          ? t("hosts.preview.awaitingApproval")
          : t("hosts.preview.submitted"),
      );
      onDone?.(result);
    } catch {
      setStatus("failed");
    } finally {
      setConfirming(false);
    }
  };

  const cancel = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      await api.approvals.cancel(action.action_ref);
      setStatus("cancelled");
      onCancel?.();
    } catch {
      setStatus("failed");
    } finally {
      setConfirming(false);
    }
  };

  return (
    <PreviewCommitCard
      affected={[]}
      confirming={confirming}
      diff={diffLinesOf({ ...action, diff: presented.diff })}
      expiresAt={action.expires_at}
      onCancel={() => void cancel()}
      onConfirm={() => void confirm()}
      resultMessage={resultMessage}
      risk={action.risk}
      riskLabel={presented.riskLabel}
      status={status}
      title={presented.title}
    >
      <p className="argus-muted">{presented.summary}</p>
    </PreviewCommitCard>
  );
}
