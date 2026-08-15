import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type ConfirmActionResult, type PendingAction } from "@argus/api-client";
import { PreviewCommitCard, type PreviewCommitStatus } from "@argus/ui";

function diffLinesOf(action: PendingAction) {
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
 * Pending Action 确认卡：渲染 preview/diff，确认走 approvals.confirm。
 * confirm 后若无 task 返回，说明进入 awaiting_approval。
 */
export function PendingActionConfirm({
  action,
  onDone,
  onCancel,
}: {
  action: PendingAction;
  onDone?: (result: ConfirmActionResult) => void;
  onCancel?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [status, setStatus] = useState<PreviewCommitStatus>("pending");
  const [resultMessage, setResultMessage] = useState<string | undefined>();
  const [confirming, setConfirming] = useState(false);

  const confirm = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      const result = await api.approvals.confirm(action.actionRef);
      setStatus("success");
      setResultMessage(
        result.task
          ? t("hosts.preview.submitted")
          : t("hosts.preview.awaitingApproval"),
      );
      onDone?.(result);
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
      diff={diffLinesOf(action)}
      expiresAt={action.expiresAt}
      onCancel={onCancel}
      onConfirm={() => void confirm()}
      planHash={action.planHash}
      resultMessage={resultMessage}
      risk={action.riskLevel}
      status={status}
      title={action.title}
    >
      <p className="argus-muted">{action.summary}</p>
    </PreviewCommitCard>
  );
}
