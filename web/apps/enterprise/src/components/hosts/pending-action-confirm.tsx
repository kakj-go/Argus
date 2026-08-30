import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatErrorCode,
  useApi,
  type ConfirmActionResult,
  type PendingActionPublic,
} from "@argus/api-client";
import { Button, PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";
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
  onDismiss,
}: {
  action: PendingActionPublic;
  claimOneTimeResult?: boolean;
  onDone?: (result: ConfirmActionResult) => void;
  onCancel?: () => void;
  /** 中性关闭(不取消动作):等待审批场景下由用户手动关闭卡片。 */
  onDismiss?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const presented = presentPendingAction(action, t);
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string | undefined>();
  const [confirming, setConfirming] = useState(false);
  const [awaitingApproval, setAwaitingApproval] = useState(false);

  const confirm = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      let result = await api.approvals.confirm(action.action_ref);
      // 执行是异步的:必须轮询到终态再回调,否则父组件失效缓存时副作用
      // 尚未落库,列表刷了个寂寞(如新增主机后短暂看不到新主机)。
      if (result.execution && !result.one_time_result) {
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
      const awaiting = result.pending_action.status === "awaiting_approval";
      setResultMessage(
        awaiting
          ? t("hosts.preview.awaitingApproval")
          : t("hosts.preview.submitted"),
      );
      if (awaiting) {
        // 等待双人审批:动作尚未生效,保持卡片与引导,不触发完成回调,
        // 避免用户误以为操作已执行而列表"没有刷新"。
        setAwaitingApproval(true);
        return;
      }
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
      {awaitingApproval && (
        <div className="argus-form-actions">
          <span className="argus-muted">
            {t("hosts.preview.awaitingApprovalHint")}
          </span>
          <Button
            onClick={() => (onDismiss ?? onCancel)?.()}
            variant="secondary"
          >
            {t("hosts.preview.close")}
          </Button>
        </div>
      )}
    </PreviewCommitCard>
  );
}
