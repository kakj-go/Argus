import { ChevronsDown, ChevronsLeft, ChevronsRight, ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { SessionTab } from "./session-tab";
import type { SessionState } from "@argus/api-client";

type TerminalTabBarProps = {
  sessions: SessionState[];
  activeSessionId: string | null;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
  terminatingIds: Set<string>;
  starting: boolean;
  position?: "bottom" | "left" | "right" | "window";
  onPositionChange?: (position: "bottom" | "left" | "right" | "window") => void;
};

export function TerminalTabBar({
  sessions,
  activeSessionId,
  onSelectTab,
  onCloseTab,
  terminatingIds,
  position = "bottom",
  onPositionChange,
}: TerminalTabBarProps) {
  const { t } = useTranslation();

  return (
    <div className="argus-terminal-tab-bar">
      <div className="argus-terminal-tab-bar__tabs">
        {sessions.map((session) => (
          <SessionTab
            key={session.id}
            session={session}
            isActive={session.id === activeSessionId}
            isTerminating={terminatingIds.has(session.id)}
            onSelect={() => onSelectTab(session.id)}
            onClose={() => onCloseTab(session.id)}
          />
        ))}
      </div>
      {onPositionChange && (
        <div className="argus-terminal-tab-bar__actions">
          <button
            className={`argus-terminal-tab-bar__action ${position === "bottom" ? "is-active" : ""}`}
            onClick={() => onPositionChange("bottom")}
            title={t("hosts.terminal.positionBottom", "底部")}
            aria-label={t("hosts.terminal.positionBottom", "底部")}
          >
            <ChevronsDown size={14} />
          </button>
          <button
            className={`argus-terminal-tab-bar__action ${position === "left" ? "is-active" : ""}`}
            onClick={() => onPositionChange("left")}
            title={t("hosts.terminal.positionLeft", "左侧")}
            aria-label={t("hosts.terminal.positionLeft", "左侧")}
          >
            <ChevronsLeft size={14} />
          </button>
          <button
            className={`argus-terminal-tab-bar__action ${position === "right" ? "is-active" : ""}`}
            onClick={() => onPositionChange("right")}
            title={t("hosts.terminal.positionRight", "右侧")}
            aria-label={t("hosts.terminal.positionRight", "右侧")}
          >
            <ChevronsRight size={14} />
          </button>
          <button
            className={`argus-terminal-tab-bar__action ${position === "window" ? "is-active" : ""}`}
            onClick={() => onPositionChange("window")}
            title={t("hosts.terminal.positionWindow", "独立窗口")}
            aria-label={t("hosts.terminal.positionWindow", "独立窗口")}
          >
            <ExternalLink size={14} />
          </button>
        </div>
      )}
    </div>
  );
}
