import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useApi } from "@argus/api-client";
import { normalizeTerminalPlayerEvents, type TerminalPlayerEvent } from "@argus/ui";

export type RecordingEventsResult = {
  events: TerminalPlayerEvent[];
  /** 最后一个事件的时间戳（秒），没有事件时为 0。 */
  duration: number;
};

/** 稳定的空数组引用：播放器以事件流身份变化作为重放信号。 */
const NO_EVENTS: TerminalPlayerEvent[] = [];

/**
 * 会话录像事件的统一加载入口：自动翻页拉取全量事件并做防御性归一化。
 * 播放器需要完整时间轴，手动分页与 seek 语义冲突，因此一次拉全。
 * 空页（录像仍在写入时翻到末尾）直接截断，避免对同一 cursor 死循环。
 */
export function useRecordingEvents(recordingId: string | null | undefined) {
  const api = useApi();
  const query = useQuery({
    enabled: recordingId != null && recordingId !== "",
    queryKey: ["remote-access", "recording-events", recordingId],
    queryFn: async (): Promise<RecordingEventsResult> => {
      const collected: unknown[] = [];
      let cursor: string | undefined;
      for (;;) {
        const page = await api.remoteAccess.listRecordingEvents(recordingId!, cursor);
        const batch = Array.isArray(page.events) ? page.events : [];
        collected.push(...batch);
        if (page.complete || batch.length === 0) break;
        cursor = page.next_cursor;
      }
      const events = normalizeTerminalPlayerEvents(collected);
      return { events, duration: events.at(-1)?.time ?? 0 };
    },
  });
  const events = useMemo(() => query.data?.events ?? NO_EVENTS, [query.data?.events]);
  return { ...query, events, duration: query.data?.duration ?? 0 };
}
