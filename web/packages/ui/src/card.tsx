import { type HTMLAttributes, type ReactNode } from "react";
import { cx } from "./lib";

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <section className={cx("argus-card", className)} {...props} />;
}

export function CardHeader({
  title,
  description,
  action,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <header className={cx("argus-card__header", className)}>
      <div className="argus-card__heading">
        <div className="argus-card__title">{title}</div>
        {description && (
          <div className="argus-card__description">{description}</div>
        )}
      </div>
      {action && <div className="argus-card__action">{action}</div>}
    </header>
  );
}

export function CardContent({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return <div className={cx("argus-card__content", className)} {...props} />;
}

export function CardFooter({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return <footer className={cx("argus-card__footer", className)} {...props} />;
}
