import { useEffect, useRef } from "react";
import type { ChatMessage } from "@argus/api-client";
import { ChatMessageItem } from "./message-item";

/**
 * 消息流：历史消息 + 乐观用户消息 + 流式中的 AI 消息；
 * 流式期间自动滚动到底。
 */
export function ChatMessageList({
  messages,
  pendingUser,
  streaming,
}: {
  messages: ChatMessage[];
  pendingUser: ChatMessage | null;
  streaming: ChatMessage | null;
}) {
  const streamRef = useRef<HTMLDivElement>(null);
  const persistedIds = new Set(messages.map((message) => message.id));
  const pendingUserPersisted =
    pendingUser !== null &&
    messages.some(
      (message) =>
        message.role === "user" &&
        message.content === pendingUser.content &&
        Math.abs(
          new Date(message.createdAt).getTime() -
            new Date(pendingUser.createdAt).getTime(),
        ) < 5_000,
    );

  useEffect(() => {
    const el = streamRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, pendingUser, streaming]);

  return (
    <div className="argus-chat__stream" ref={streamRef}>
      <div className="argus-chat-stream__inner">
        {messages.map((message) => (
          <ChatMessageItem key={message.id} message={message} />
        ))}
        {pendingUser && !pendingUserPersisted && (
          <ChatMessageItem key={pendingUser.id} message={pendingUser} />
        )}
        {streaming && !persistedIds.has(streaming.id) && (
          <ChatMessageItem key={streaming.id} message={streaming} streaming />
        )}
      </div>
    </div>
  );
}
