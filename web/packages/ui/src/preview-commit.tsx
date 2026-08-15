import { CheckCircle2, CircleSlash, Clock, XCircle } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Button } from "./button";
import { DiffViewer } from "./code";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type PreviewRiskLevel = "read" | "write" | "dangerous" | "critical";

export type PreviewCommitStatus =
  | "pending"
  | "success"
  | "failed"
  | "cancelled"
  | "expired";

export type PreviewAffectedItem = {
  name: string;
  detail?: string;
};

function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function toMs(value: Date | string | number): number {
  return value instanceof Date ? value.getTime() : new Date(value).getTime();
}

export function PreviewCommitCard({
  title,
  risk,
  riskLabel,
  children,
  diff,
  affected = [],
  planHash,
  expiresAt,
  status = "pending",
  resultMessage,
  confirming,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
  className,
}: {
  title: ReactNode;
  risk: PreviewRiskLevel;
  riskLabel?: string;
  /** Preview summary area, rendered between the header and the diff. */
  children?: ReactNode;
  diff?: Array<{ type: "add" | "remove" | "context"; content: string }>;
  affected?: PreviewAffectedItem[];
  planHash?: string;
  /** When passed, a live countdown is shown and the card expires on its own. */
  expiresAt?: Date | string | number;
  status?: PreviewCommitStatus;
  /** Custom message for the result state; falls back to a localized default. */
  resultMessage?: ReactNode;
  confirming?: boolean;
  confirmLabel?: string;
  cancelLabel?: string;
  onConfirm?: () => void;
  onCancel?: () => void;
  className?: string;
}) {
  const text = useUiText();
  const expiresAtMs = expiresAt !== undefined ? toMs(expiresAt) : undefined;
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (expiresAtMs === undefined || status !== "pending") return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [expiresAtMs, status]);

  const locallyExpired =
    expiresAtMs !== undefined && now >= expiresAtMs && status === "pending";
  const effectiveStatus: PreviewCommitStatus = locallyExpired
    ? "expired"
    : status;

  const defaultResult: Record<Exclude<PreviewCommitStatus, "pending">, string> =
    {
      success: text("执行成功", "Executed successfully"),
      failed: text("执行失败", "Execution failed"),
      cancelled: text("操作已取消", "Operation cancelled"),
      expired: text("预览已过期", "Preview expired"),
    };

  return (
    <section
      className={cx(
        "argus-preview-card",
        effectiveStatus !== "pending" &&
          `is-${effectiveStatus === "success" ? "success" : "muted"}`,
        className,
      )}
    >
      <header className="argus-preview-card__header">
        <span className={cx("argus-risk-badge", `is-${risk}`)}>
          {riskLabel ?? risk}
        </span>
        <strong className="argus-preview-card__title">{title}</strong>
        {expiresAtMs !== undefined && effectiveStatus === "pending" && (
          <span className="argus-preview-card__countdown">
            <Clock aria-hidden size={12} />
            {formatRemaining(expiresAtMs - now)}
          </span>
        )}
      </header>

      {children && <div className="argus-preview-card__body">{children}</div>}

      {diff && diff.length > 0 && (
        <div className="argus-preview-card__diff">
          <DiffViewer lines={diff} />
        </div>
      )}

      {affected.length > 0 && (
        <ul className="argus-preview-card__affected">
          {affected.map((item) => (
            <li key={item.name}>
              <span>{item.name}</span>
              {item.detail && <small>{item.detail}</small>}
            </li>
          ))}
        </ul>
      )}

      {planHash && (
        <div className="argus-preview-card__hash">
          <span>{text("计划哈希", "Plan hash")}</span>
          <code>{planHash}</code>
        </div>
      )}

      {effectiveStatus === "pending" ? (
        <footer className="argus-preview-card__footer">
          <Button
            disabled={confirming}
            onClick={onCancel}
            variant="secondary"
          >
            {cancelLabel ?? text("取消", "Cancel")}
          </Button>
          <Button loading={confirming} onClick={onConfirm} variant="primary">
            {confirmLabel ?? text("确认执行", "Confirm")}
          </Button>
        </footer>
      ) : (
        <footer
          className={cx(
            "argus-preview-card__result",
            `is-${effectiveStatus}`,
          )}
        >
          {effectiveStatus === "success" ? (
            <CheckCircle2 aria-hidden size={15} />
          ) : effectiveStatus === "failed" ? (
            <XCircle aria-hidden size={15} />
          ) : (
            <CircleSlash aria-hidden size={15} />
          )}
          <span>{resultMessage ?? defaultResult[effectiveStatus]}</span>
        </footer>
      )}
    </section>
  );
}
