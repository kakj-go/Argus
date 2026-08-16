import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import type { PendingActionPublic } from "@argus/api-client";
import type { CardInstance } from "./chat-view-model";
import { useApi } from "@argus/api-client";
import {
  PreviewCommitCard,
  Skeleton,
  type PreviewCommitStatus,
} from "@argus/ui";

function toPreviewStatus(action: PendingActionPublic): PreviewCommitStatus {
  switch (action.status) {
    case "succeeded":
      return "success";
    case "failed":
    case "rejected":
      return "failed";
    case "cancelled":
      return "cancelled";
    case "expired":
      return "expired";
    default:
      // awaiting_confirmation / awaiting_approval / ready / executing
      return "pending";
  }
}

/**
 * 消息中的 Pending Action 确认卡片（不用 iframe）：
 * 预览摘要、风险、Diff、影响对象、计划哈希、过期倒计时；
 * [确认执行] 直接调 approvals.confirm（不经过模型），状态原地流转。
 */
export function PendingActionCard({ card }: { card: CardInstance }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const actionRef = card.pendingActionRef ?? "";
  const [confirming, setConfirming] = useState(false);
  const [failedMessage, setFailedMessage] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ["approvals", actionRef],
    queryFn: () => api.approvals.get(actionRef),
    enabled: actionRef !== "",
    // 执行中/待审批时轮询，直到状态机进入终态（mock 侧 Task 步骤按定时器推进）。
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "executing" ||
        status === "ready" ||
        status === "awaiting_approval"
        ? 1_000
        : false;
    },
  });
  // mock 客户端就地改写同一对象引用，React Query 结构共享会据此跳过重渲染；
  // 把 dataUpdatedAt 计入追踪属性，保证每次轮询结果都刷新视图
  // （真实后端每次返回新对象，不受影响）。
  const { data: action, dataUpdatedAt } = query;

  if (!action) {
    return (
      <div className="argus-chat-action">
        <Skeleton height={120} />
      </div>
    );
  }

  const status = failedMessage ? "failed" : toPreviewStatus(action);
  const awaitingApproval = action.status === "awaiting_approval";
  const busy =
    confirming ||
    awaitingApproval ||
    action.status === "executing" ||
    action.status === "ready";
  const preview =
    typeof action.preview === "object" && action.preview !== null
      ? (action.preview as Record<string, unknown>)
      : {};

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ["approvals", actionRef] });

  const confirm = async () => {
    setConfirming(true);
    setFailedMessage(null);
    try {
      await api.approvals.confirm(actionRef);
      await refresh();
    } catch (error) {
      setFailedMessage(
        error instanceof Error ? error.message : t("chat.action.failed"),
      );
    } finally {
      setConfirming(false);
    }
  };

  const cancel = async () => {
    setConfirming(true);
    try {
      await api.approvals.cancel(actionRef);
      await refresh();
    } catch (error) {
      setFailedMessage(
        error instanceof Error ? error.message : t("chat.action.failed"),
      );
    } finally {
      setConfirming(false);
    }
  };

  const affectedName = String(preview["name"] ?? action.title);
  const affectedDetail = [preview["address"], preview["environment"]]
    .filter(Boolean)
    .join(" · ");

  const resultMessage =
    status === "success" ? (
      <span>
        {action.result_summary ?? t("chat.result.success")}
        {action.execution_ref && (
          <a className="argus-chat-action__task-link" href="/tasks">
            {t("chat.action.viewTask")}
            <ExternalLink aria-hidden size={11} />
          </a>
        )}
      </span>
    ) : status === "failed" ? (
      (failedMessage ?? action.result_summary ?? t("chat.result.failed"))
    ) : undefined;

  return (
    <div
      className="argus-chat-action"
      data-fetched={dataUpdatedAt}
      data-testid="pending-action-card"
    >
      <PreviewCommitCard
        affected={[
          {
            name: affectedName,
            detail: affectedDetail || action.summary,
          },
        ]}
        confirming={busy}
        confirmLabel={
          awaitingApproval ? t("chat.action.awaitingApproval") : undefined
        }
        diff={action.diff.map((line) => ({
          type:
            line.kind === "add"
              ? "add"
              : line.kind === "remove"
                ? "remove"
                : "context",
          content: line.text,
        }))}
        expiresAt={action.expires_at}
        onCancel={awaitingApproval ? undefined : cancel}
        onConfirm={awaitingApproval ? undefined : confirm}
        resultMessage={resultMessage}
        risk={action.risk}
        riskLabel={t(`chat.action.risk.${action.risk}`)}
        status={status}
        title={action.title}
      >
        <p className="argus-chat-action__summary">{action.summary}</p>
      </PreviewCommitCard>
    </div>
  );
}
