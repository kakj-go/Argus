import { type HTMLAttributes, type ReactNode } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cx } from "./lib";

const statCardVariants = cva("argus-stat-card", {
  variants: {
    tone: {
      neutral: "argus-stat-card--neutral",
      accent: "argus-stat-card--accent",
      success: "argus-stat-card--success",
      warning: "argus-stat-card--warning",
      danger: "argus-stat-card--danger",
      info: "argus-stat-card--info",
    },
  },
  defaultVariants: { tone: "neutral" },
});

export type StatCardProps = HTMLAttributes<HTMLDivElement> &
  VariantProps<typeof statCardVariants> & {
    label: ReactNode;
    value: ReactNode;
    detail?: ReactNode;
    icon?: ReactNode;
  };

export function StatCard({
  className,
  tone,
  label,
  value,
  detail,
  icon,
  ...props
}: StatCardProps) {
  return (
    <div className={cx(statCardVariants({ tone }), className)} {...props}>
      <div className="argus-stat-card__top">
        <span className="argus-stat-card__label">{label}</span>
        {icon && <span className="argus-stat-card__icon">{icon}</span>}
      </div>
      <strong className="argus-stat-card__value">{value}</strong>
      {detail && <span className="argus-stat-card__detail">{detail}</span>}
    </div>
  );
}
