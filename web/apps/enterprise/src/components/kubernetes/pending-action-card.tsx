import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatErrorCode,
  useApi,
  type ConfirmActionResult,
  type PendingActionPublic,
} from "@argus/api-client";
import { PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";
import { presentPendingAction } from "../pending-action-presentation";

function toDiffLines(diff: PendingActionPublic["diff"]) {
  return diff.map((line) => ({
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
 * 统一的 PendingActionPublic 两阶段确认卡片：
 * 展示公开 preview/diff，确认走 approvals.confirm，取消走 approvals.cancel。
 * 结果展示约 1.2s 后通过 onSettled 回调交给父组件收尾（invalidate / 关闭）。
 */
export function PendingActionCard({
  action,
  claimOneTimeResult = false,
  onSettled,
}: {
  action: PendingActionPublic;
  claimOneTimeResult?: boolean;
  onSettled: (confirmed: boolean, result?: ConfirmActionResult) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const presented = presentPendingAction(action, t);
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string>();
  const timerRef = useRef<number | undefined>(undefined);

  useEffect(
    () => () => {
      window.clearTimeout(timerRef.current);
    },
    [],
  );

  const settle = (confirmed: boolean, result?: ConfirmActionResult) => {
    timerRef.current = window.setTimeout(
      () => onSettled(confirmed, result),
      1200,
    );
  };

  const confirm = useMutation({
    mutationFn: async () => {
      let result = await api.approvals.confirm(action.action_ref);
      if (!claimOneTimeResult || !result.execution || result.one_time_result) {
        return result;
      }
      const executionId = result.execution.execution_id;
      for (let attempt = 0; attempt < 120; attempt += 1) {
        const execution = await api.executions.get(executionId);
        result = { ...result, execution };
        if (execution.status === "succeeded") {
          if (execution.one_time_result_available) {
            return {
              ...result,
              one_time_result: await api.executions.claimOneTimeResult(
                execution.execution_id,
              ),
            };
          }
          return result;
        }
        if (execution.status === "failed" || execution.status === "cancelled") {
          throw new Error(formatErrorCode(execution.error_code, "Execution failed"));
        }
        await new Promise((resolve) => window.setTimeout(resolve, 500));
      }
      throw new Error("execution timed out");
    },
    onSuccess: (result) => {
      setStatus("success");
      setResultMessage(
        result.pending_action.status === "awaiting_approval"
          ? t("kubernetes.pendingAction.awaitingApproval")
          : t("kubernetes.pendingAction.executed"),
      );
      settle(true, result);
    },
    onError: () => setStatus("failed"),
  });

  const cancel = useMutation({
    mutationFn: () => api.approvals.cancel(action.action_ref),
    onSuccess: () => {
      setStatus("cancelled");
      settle(false);
    },
    onError: () => setStatus("failed"),
  });

  return (
    <PreviewCommitCard
      confirming={confirm.isPending || cancel.isPending}
      diff={toDiffLines(presented.diff)}
      expiresAt={action.expires_at}
      onCancel={() => cancel.mutate()}
      onConfirm={() => confirm.mutate()}
      resultMessage={resultMessage}
      risk={action.risk}
      riskLabel={presented.riskLabel}
      status={status}
      title={presented.title}
    >
      <p>{presented.summary}</p>
    </PreviewCommitCard>
  );
}
