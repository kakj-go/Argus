import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, LoaderCircle, Wrench, X } from "lucide-react";
import type { ToolCallTrace } from "./chat-view-model";

function formatDuration(durationMs?: number): string {
  if (durationMs === undefined) return "";
  return durationMs >= 1000
    ? `${(durationMs / 1000).toFixed(1)}s`
    : `${durationMs}ms`;
}

function StatusIcon({ status }: { status: ToolCallTrace["status"] }) {
  if (status === "running") {
    return (
      <span className="argus-chat-trace__status is-running">
        <LoaderCircle aria-hidden className="argus-spin" size={13} />
      </span>
    );
  }
  if (status === "failed") {
    return (
      <span className="argus-chat-trace__status is-failed">
        <X aria-hidden size={13} />
      </span>
    );
  }
  return (
    <span className="argus-chat-trace__status is-success">
      <Check aria-hidden size={13} />
    </span>
  );
}

/** 工具调用 trace：默认折叠的可展开区块（名称、参数摘要、状态、耗时）。 */
export function ToolTrace({ toolCalls }: { toolCalls: ToolCallTrace[] }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  if (toolCalls.length === 0) return null;

  return (
    <div className="argus-chat-trace" data-testid="tool-trace">
      <button
        aria-expanded={expanded}
        aria-label={
          expanded ? t("chat.trace.collapse") : t("chat.trace.expand")
        }
        className="argus-chat-trace__toggle"
        onClick={() => setExpanded((value) => !value)}
        type="button"
      >
        <Wrench aria-hidden size={12} />
        <span>{t("chat.trace.title", { count: toolCalls.length })}</span>
        <ChevronDown aria-hidden size={13} />
      </button>
      {expanded && (
        <div className="argus-chat-trace__list">
          {toolCalls.map((call) => (
            <div className="argus-chat-trace__row" key={call.callId}>
              <StatusIcon status={call.status} />
              <code>{call.toolName}</code>
              {call.summary && <small>{call.summary}</small>}
              <time>{formatDuration(call.durationMs)}</time>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
