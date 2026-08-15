import { ChevronRight } from "lucide-react";
import { type ReactNode } from "react";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type BreadcrumbItem = {
  label: ReactNode;
  href?: string;
  onClick?: () => void;
};

export function PageShell({
  breadcrumbs,
  title,
  description,
  actions,
  children,
  className,
}: {
  breadcrumbs?: BreadcrumbItem[];
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const text = useUiText();
  return (
    <div className={cx("argus-page", className)}>
      <header className="argus-page__header">
        {breadcrumbs && breadcrumbs.length > 0 && (
          <nav
            aria-label={text("面包屑", "Breadcrumb")}
            className="argus-breadcrumb"
          >
            {breadcrumbs.map((item, index) => {
              const isLast = index === breadcrumbs.length - 1;
              return (
                <span className="argus-breadcrumb__item" key={index}>
                  {index > 0 && (
                    <ChevronRight aria-hidden size={12} />
                  )}
                  {isLast || (!item.href && !item.onClick) ? (
                    <span
                      aria-current={isLast ? "page" : undefined}
                      className={isLast ? "is-current" : undefined}
                    >
                      {item.label}
                    </span>
                  ) : item.href ? (
                    <a href={item.href}>{item.label}</a>
                  ) : (
                    <button onClick={item.onClick} type="button">
                      {item.label}
                    </button>
                  )}
                </span>
              );
            })}
          </nav>
        )}
        <div className="argus-page__titlebar">
          <div className="argus-page__heading">
            <h1 className="argus-page__title">{title}</h1>
            {description && (
              <p className="argus-page__description">{description}</p>
            )}
          </div>
          {actions && <div className="argus-page__actions">{actions}</div>}
        </div>
      </header>
      <div className="argus-page__content">{children}</div>
    </div>
  );
}
