import { type HTMLAttributes } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cx } from "./lib";

const badgeVariants = cva("argus-badge", {
  variants: {
    tone: {
      neutral: "argus-badge--neutral",
      accent: "argus-badge--accent",
      success: "argus-badge--success",
      warning: "argus-badge--warning",
      danger: "argus-badge--danger",
      info: "argus-badge--info",
    },
    dot: { true: "argus-badge--dot" },
  },
  defaultVariants: { tone: "neutral" },
});

export type BadgeProps = HTMLAttributes<HTMLSpanElement> &
  VariantProps<typeof badgeVariants>;

export function Badge({
  className,
  tone,
  dot,
  children,
  ...props
}: BadgeProps) {
  return (
    <span className={cx(badgeVariants({ tone, dot }), className)} {...props}>
      {dot && <i aria-hidden />}
      {children}
    </span>
  );
}
