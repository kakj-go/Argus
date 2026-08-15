import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { ChatMessage, InteractiveCardCreateCommand } from "@argus/api-client";
import { useApi } from "@argus/api-client";

export type ChatStreamState = {
  /** 正在流式生成的 AI 消息（未落库，message_done 后清空并改用查询数据）。 */
  streaming: ChatMessage | null;
  /** 刚发送、等待查询刷新的用户消息（乐观展示）。 */
  pendingUser: ChatMessage | null;
  sending: boolean;
  error: string | null;
  send: (
    conversationId: string,
    text: string,
    command?: InteractiveCardCreateCommand,
  ) => Promise<void>;
  /** 中断当前的流式消费循环。 */
  stop: () => void;
};

/**
 * 消费 conversations.sendMessage 的 AsyncIterable：
 * message_start → tool_call/tool_call_update → token* → card → message_done。
 * 完成后 invalidate ["conversations"]，让消息列表与侧栏会话列表一起刷新。
 */
export function useChatStream(): ChatStreamState {
  const api = useApi();
  const queryClient = useQueryClient();
  const [streaming, setStreaming] = useState<ChatMessage | null>(null);
  const [pendingUser, setPendingUser] = useState<ChatMessage | null>(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const stopRef = useRef(false);

  const stop = useCallback(() => {
    stopRef.current = true;
  }, []);

  const send = useCallback(
    async (
      conversationId: string,
      text: string,
      command?: InteractiveCardCreateCommand,
    ) => {
      if (sending) return;
      stopRef.current = false;
      setSending(true);
      setError(null);
      setPendingUser({
        id: `local-user-${Date.now()}`,
        conversationId,
        role: "user",
        content: text,
        createdAt: new Date().toISOString(),
      });

      let acc: ChatMessage | null = null;
      const patch = () => {
        if (acc) setStreaming({ ...acc });
      };

      try {
        const stream = api.conversations.sendMessage(conversationId, {
          text,
          command,
        });
        for await (const event of stream) {
          if (stopRef.current) break;
          switch (event.type) {
            case "message_start":
              acc = {
                id: event.messageId,
                conversationId,
                role: "assistant",
                content: "",
                createdAt: new Date().toISOString(),
                toolCalls: [],
                cards: [],
              };
              patch();
              break;
            case "token":
              if (acc) {
                acc.content += event.delta;
                patch();
              }
              break;
            case "tool_call":
              if (acc) {
                acc.toolCalls = [...(acc.toolCalls ?? []), event.toolCall];
                patch();
              }
              break;
            case "tool_call_update":
              if (acc) {
                acc.toolCalls = (acc.toolCalls ?? []).map((call) =>
                  call.callId === event.callId
                    ? {
                        ...call,
                        status: event.status,
                        durationMs: event.durationMs,
                        summary: event.summary,
                      }
                    : call,
                );
                patch();
              }
              break;
            case "card":
              if (acc) {
                acc.cards = [...(acc.cards ?? []), event.card];
                patch();
              }
              break;
            case "error":
              setError(event.message);
              break;
            case "message_done":
            case "card_action_result":
            case "interactive_card_created":
              // message_done 后消息已落库，统一由下面的 invalidate 刷新。
              break;
          }
        }
      } catch {
        setError("send failed");
      } finally {
        // 等待列表刷新完成再清掉本地乐观消息，避免流式内容闪烁消失。
        await queryClient.invalidateQueries({ queryKey: ["conversations"] });
        acc = null;
        setStreaming(null);
        setPendingUser(null);
        setSending(false);
      }
    },
    [api, queryClient, sending],
  );

  return { streaming, pendingUser, sending, error, send, stop };
}
