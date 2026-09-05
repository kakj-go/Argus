import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatErrorCode,
  useApi,
  type ConfirmActionResult,
  type PendingActionPublic,
} from "@argus/api-client";
import { Button, PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";
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
  onDismiss,
}: {
  action: PendingActionPublic;
  claimOneTimeResult?: boolean;
  onSettled: (confirmed: boolean, result?: ConfirmActionResult) => void;
  /** 中性关闭(不取消动作):等待审批场景下由用户手动关闭卡片。 */
  onDismiss?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const presented = presentPendingAction(action, t);
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string>();
  const [awaitingApproval, setAwaitingApproval] = useState(false);
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
      // 执行是异步的:轮询到终态再返回,确保父组件失效缓存时副作用已落库。
      if (!result.execution || result.one_time_result) {
        return result;
      }
      const executionId = result.execution.execution_id;
      for (let attempt = 0; attempt < 120; attempt += 1) {
        const execution = await api.executions.get(executionId);
        result = { ...result, execution };
        if (execution.status === "succeeded") {
          if (
            claimOneTimeResult &&
            execution.one_time_result_state === "available"
          ) {
            return {
              ...result,
              one_time_result: await api.executions.claimOneTimeResult(
                execution.execution_id,
              ),
            };
          }
          return result;
        }
        if (execution.status === "result_unknown" && execution.operation_ref) {
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
      const awaiting = result.pending_action.status === "awaiting_approval";
      setResultMessage(
        awaiting
          ? t("kubernetes.pendingAction.awaitingApproval")
          : t("kubernetes.pendingAction.executed"),
      );
      // 等待双人审批:动作尚未生效,保持卡片与引导,不触发 settle,
      // 避免用户误以为操作已执行而列表"没有刷新"。
      if (awaiting) {
        setAwaitingApproval(true);
        return;
      }
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
      {awaitingApproval && (
        <div className="argus-form-actions">
          <span className="argus-muted">
            {t("hosts.preview.awaitingApprovalHint")}
          </span>
          <Button onClick={() => onDismiss?.()} variant="secondary">
            {t("hosts.preview.close")}
          </Button>
        </div>
      )}
    </PreviewCommitCard>
  );
}
