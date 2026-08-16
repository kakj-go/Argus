import { useTranslation } from "react-i18next";
import { CheckCircle2, XCircle } from "lucide-react";
import type { ChatMessage } from "./chat-view-model";

/** card_action_result 事件消息的紧凑回显条（成功/失败 + 摘要）。 */
export function ActionResultBar({ message }: { message: ChatMessage }) {
  const { t } = useTranslation();
  const event = message.event;
  if (!event) return null;
  const success = event.status === "success";

  return (
    <div className="argus-chat-result" data-testid="card-action-result">
      <span
        className={`argus-chat-result__bar ${success ? "is-success" : "is-failed"}`}
      >
        {success ? (
          <CheckCircle2 aria-hidden size={13} />
        ) : (
          <XCircle aria-hidden size={13} />
        )}
        <span>
          {t("chat.result.action")} · {event.action} ·{" "}
          {success ? t("chat.result.success") : t("chat.result.failed")}
        </span>
        {event.resultRef && <code>{event.resultRef}</code>}
      </span>
    </div>
  );
}
