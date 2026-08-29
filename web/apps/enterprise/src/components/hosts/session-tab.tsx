import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cx } from "@argus/ui";
import type { SessionState } from "@argus/api-client";

type SessionTabProps = {
  session: SessionState;
  isActive: boolean;
  isTerminating: boolean;
  onSelect: () => void;
  onClose: () => void;
};

export function SessionTab({
  session,
  isActive,
  isTerminating,
  onSelect,
  onClose,
}: SessionTabProps) {
  const { t } = useTranslation();
  const label = `${session.accountName}@${session.hostName}`;

  return (
    <div
      className={cx("argus-terminal-tab", isActive && "is-active")}
      onClick={onSelect}
      role="tab"
      aria-selected={isActive}
      tabIndex={0}
      title={label}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
    >
      <span
        className={cx("argus-terminal-tab__status", `is-${session.state}`)}
        title={t(`hosts.terminal.connectionState.${session.state}`)}
      />
      <span className="argus-terminal-tab__label" title={label}>
        {label}
      </span>
      <button
        className="argus-terminal-tab__close"
        onClick={(e) => {
          e.stopPropagation();
          if (!isTerminating) onClose();
        }}
        disabled={isTerminating}
        aria-label={t("hosts.terminal.closeSession", "关闭会话")}
        title={t("hosts.terminal.closeSession", "关闭会话")}
      >
        <X size={12} />
      </button>
    </div>
  );
}
