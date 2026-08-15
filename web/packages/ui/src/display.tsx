import { AlertTriangle, Inbox, LoaderCircle } from "lucide-react";
import { type ReactNode } from "react";
import { cx } from "./lib";
import { useUiText } from "./locale";

export function Progress({
  value,
  tone = "accent",
  label,
}: {
  value: number;
  tone?: "accent" | "success" | "warning" | "danger";
  label?: string;
}) {
  return (
    <div className="argus-progress-wrap">
      {label && (
        <div className="argus-progress__label">
          <span>{label}</span>
          <span>{value}%</span>
        </div>
      )}
      <div
        aria-label={label}
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={value}
        className="argus-progress"
        role="progressbar"
      >
        <i
          className={`is-${tone}`}
          style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
        />
      </div>
    </div>
  );
}

export function Metric({
  label,
  value,
  unit,
  change,
  tone = "neutral",
}: {
  label: string;
  value: string;
  unit?: string;
  change?: string;
  tone?: "neutral" | "success" | "warning" | "danger";
}) {
  return (
    <div className="argus-metric">
      <span className="argus-metric__label">{label}</span>
      <strong className="argus-metric__value">
        {value}
        {unit && <small>{unit}</small>}
      </strong>
      {change && (
        <span className={cx("argus-metric__change", `is-${tone}`)}>
          {change}
        </span>
      )}
    </div>
  );
}

export function DescriptionList({
  items,
}: {
  items: Array<{ label: string; value: ReactNode }>;
}) {
  return (
    <dl className="argus-description-list">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export type TimelineItem = {
  title: ReactNode;
  meta?: ReactNode;
  status?: "done" | "current" | "pending" | "danger";
};

export function Timeline({ items }: { items: TimelineItem[] }) {
  return (
    <ol className="argus-timeline">
      {items.map((item, index) => (
        <li
          className={cx(
            "argus-timeline__item",
            `is-${item.status ?? "pending"}`,
          )}
          key={index}
        >
          <i aria-hidden />
          <div>
            <div className="argus-timeline__title">{item.title}</div>
            {item.meta && (
              <div className="argus-timeline__meta">{item.meta}</div>
            )}
          </div>
        </li>
      ))}
    </ol>
  );
}

export function EmptyState({
  title,
  description,
  action,
  kind = "empty",
  illustration,
}: {
  title: string;
  description: string;
  action?: ReactNode;
  kind?: "empty" | "error";
  /** Custom artwork replacing the default icon. */
  illustration?: ReactNode;
}) {
  return (
    <div className="argus-empty-state">
      {illustration ??
        (kind === "error" ? (
          <AlertTriangle aria-hidden />
        ) : (
          <Inbox aria-hidden />
        ))}
      <strong>{title}</strong>
      <p>{description}</p>
      {action}
    </div>
  );
}

export function Skeleton({
  width = "100%",
  height = 14,
}: {
  width?: string;
  height?: number;
}) {
  return <span className="argus-skeleton" style={{ width, height }} />;
}

export function Spinner({ label }: { label?: string }) {
  const text = useUiText();
  return (
    <span className="argus-spinner">
      <LoaderCircle className="argus-spin" size={16} />
      <span>{label ?? text("加载中", "Loading")}</span>
    </span>
  );
}

export function Divider({ label }: { label?: string }) {
  return <div className="argus-divider">{label && <span>{label}</span>}</div>;
}
