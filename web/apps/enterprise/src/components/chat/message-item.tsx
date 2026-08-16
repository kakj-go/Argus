import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { Bot } from "lucide-react";
import { Avatar, Badge } from "@argus/ui";
import { useEnterpriseAuthStore } from "@argus/auth";
import { ActionResultBar } from "./action-result-bar";
import { PendingActionCard } from "./pending-action-card";
import { ToolTrace } from "./tool-trace";
import { SandboxCardFrame } from "./sandbox-card-frame";
import type { ChatMessage } from "./chat-view-model";

function formatTime(value: string, locale: string): string {
  return new Date(value).toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** 单条消息：用户 / AI（含工具 trace 与卡片）/ card_action_result 事件。 */
export function ChatMessageItem({
  message,
  streaming = false,
}: {
  message: ChatMessage;
  /** 流式生成中：内容末尾显示光标。 */
  streaming?: boolean;
}) {
  const { t, i18n } = useTranslation();
  const user = useEnterpriseAuthStore((state) => state.session?.user);

  if (message.event) {
    return <ActionResultBar message={message} />;
  }

  if (message.role === "user") {
    return (
      <div
        className="argus-chat-message argus-chat-message--user"
        data-testid="chat-message-user"
      >
        <div className="argus-chat-message__who">
          <Avatar
            fallback={(user?.display_name ?? t("chat.you")).slice(0, 1)}
            size="sm"
          />
          <b>{user?.display_name ?? t("chat.you")}</b>
          <time>{formatTime(message.createdAt, i18n.language)}</time>
        </div>
        <div className="argus-chat-message__body">{message.content}</div>
      </div>
    );
  }

  return (
    <div className="argus-chat-message" data-testid="chat-message-assistant">
      <div className="argus-chat-message__who">
        <span className="argus-chat-message__avatar">
          <Bot aria-hidden size={14} />
        </span>
        <b>{t("chat.assistant")}</b>
        {message.modelId && <Badge tone="accent">{message.modelId}</Badge>}
        <time>{formatTime(message.createdAt, i18n.language)}</time>
      </div>
      {message.createdInteractiveCardId && (
        <div className="argus-chat-message__body">
          <Link
            className="argus-chat-message__card-link"
            to="/settings/interactive-cards"
          >
            {t("chat.card.openCreated")}
          </Link>
        </div>
      )}
      <div
        className={`argus-chat-message__body ${streaming && message.content ? "argus-chat-message__caret" : ""}`}
      >
        {message.content}
      </div>
      {(message.toolCalls?.length || message.cards?.length) && (
        <div className="argus-chat-message__extras">
          {message.toolCalls && message.toolCalls.length > 0 && (
            <ToolTrace toolCalls={message.toolCalls} />
          )}
          {message.cards?.map((card) =>
            card.pendingActionRef ? (
              <PendingActionCard card={card} key={card.id} />
            ) : (
              <SandboxCardFrame card={card} key={card.id} />
            ),
          )}
        </div>
      )}
    </div>
  );
}
