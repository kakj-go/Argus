import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { StreamTerminatedError, useApi } from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import type { ChatMessage } from "./chat-view-model";
import {
  initialConversationProjection,
  reduceStreamEnvelope,
  type CompactionView,
} from "./event-reducer";

export type ChatStreamState = {
  /** 正在流式生成的 AI 消息（未落库，message_done 后清空并改用查询数据）。 */
  streaming: ChatMessage | null;
  /** 刚发送、等待查询刷新的用户消息（乐观展示）。 */
  pendingUser: ChatMessage | null;
  sending: boolean;
  error: string | null;
  stopReason: "completed" | "cancelled" | "failed" | "output_limit" | null;
  compaction: CompactionView;
  activeRunId: string | null;
  send: (
    conversationId: string,
    text: string,
    mockIntent?: "interactive_card.create",
  ) => Promise<void>;
  /** 中断当前网络流和本地消费循环。 */
  stop: () => void;
  compact: () => void;
};

/**
 * 消费冻结的 StreamEventEnvelope/AgentEvent，并通过纯 reducer 生成展示模型。
 */
export function useChatStream(): ChatStreamState {
  const api = useApi();
  const queryClient = useQueryClient();
  const [streaming, setStreaming] = useState<ChatMessage | null>(null);
  const [pendingUser, setPendingUser] = useState<ChatMessage | null>(null);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const runRef = useRef<string | null>(null);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [stopReason, setStopReason] = useState<
    "completed" | "cancelled" | "failed" | "output_limit" | null
  >(null);
  const [compaction, setCompaction] = useState<CompactionView>({
    status: "idle",
  });

  const stop = useCallback(() => {
    abortRef.current?.abort();
    const runId = runRef.current;
    if (runId) void api.runs.cancel(runId).catch(() => undefined);
    setStopReason("cancelled");
  }, [api]);

  const compact = useCallback(() => {
    const runId = runRef.current;
    if (runId) void api.runs.compact(runId).catch(() => undefined);
  }, [api]);

  const send = useCallback(
    async (
      conversationId: string,
      text: string,
      mockIntent?: "interactive_card.create",
    ) => {
      if (sending) return;
      const controller = new AbortController();
      abortRef.current = controller;
      setSending(true);
      setError(null);
      setStopReason(null);
      setCompaction({ status: "idle" });
      setPendingUser({
        id: `local-user-${Date.now()}`,
        conversationId,
        role: "user",
        content: text,
        createdAt: new Date().toISOString(),
      });

      let projection = initialConversationProjection;

      try {
        const stream = api.conversations.sendMessage(
          conversationId,
          {
            content: text,
            ...(mockIntent ? { command: { type: mockIntent } } : {}),
          },
          { signal: controller.signal, mock_intent: mockIntent },
        );
        for await (const envelope of stream) {
          const streamed = envelope.data as { run_id?: string };
          if (streamed.run_id && streamed.run_id !== runRef.current) {
            runRef.current = streamed.run_id;
            setActiveRunId(streamed.run_id);
          }
          projection = reduceStreamEnvelope(projection, envelope);
          setCompaction(projection.compaction);
          const completed = projection.completed_message;
          setStreaming(
            completed ?? {
              id: projection.message_id ?? `stream-${conversationId}`,
              conversationId,
              role: "assistant",
              content: projection.message_text,
              createdAt: envelope.occurred_at,
              toolCalls: [...projection.tool_calls.values()],
              cards: [...projection.cards],
            },
          );
          if (projection.stop_reason) setStopReason(projection.stop_reason);
        }
        if (!controller.signal.aborted && !projection.stop_reason) {
          setStopReason("completed");
        }
      } catch (streamError) {
        if (!controller.signal.aborted) {
          if (
            streamError instanceof StreamTerminatedError &&
            streamError.code === "AUTHORIZATION_VERSION_STALE"
          ) {
            useEnterpriseAuthStore.getState().clear();
            window.location.assign("/login");
            return;
          }
          setError("send failed");
          setStopReason("failed");
        }
      } finally {
        // 等待列表刷新完成再清掉本地乐观消息，避免流式内容闪烁消失。
        await queryClient.invalidateQueries({ queryKey: ["conversations"] });
        setStreaming(null);
        setPendingUser(null);
        setSending(false);
        abortRef.current = null;
        runRef.current = null;
        setActiveRunId(null);
      }
    },
    [api, queryClient, sending],
  );

  return {
    streaming,
    pendingUser,
    sending,
    error,
    stopReason,
    compaction,
    activeRunId,
    send,
    stop,
    compact,
  };
}
