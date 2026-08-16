import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type PendingActionPublic } from "@argus/api-client";
import { PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";

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
  onSettled,
}: {
  action: PendingActionPublic;
  onSettled: (confirmed: boolean) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string>();
  const timerRef = useRef<number | undefined>(undefined);

  useEffect(
    () => () => {
      window.clearTimeout(timerRef.current);
    },
    [],
  );

  const settle = (confirmed: boolean) => {
    timerRef.current = window.setTimeout(() => onSettled(confirmed), 1200);
  };

  const confirm = useMutation({
    mutationFn: () => api.approvals.confirm(action.action_ref),
    onSuccess: (result) => {
      setStatus("success");
      setResultMessage(
        result.pending_action.status === "awaiting_approval"
          ? t("kubernetes.pendingAction.awaitingApproval")
          : t("kubernetes.pendingAction.executed"),
      );
      settle(true);
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
      diff={toDiffLines(action.diff)}
      expiresAt={action.expires_at}
      onCancel={() => cancel.mutate()}
      onConfirm={() => confirm.mutate()}
      resultMessage={resultMessage}
      risk={action.risk}
      riskLabel={t(`kubernetes.risk.${action.risk}`)}
      status={status}
      title={action.title}
    >
      <p>{action.summary}</p>
    </PreviewCommitCard>
  );
}
