import { type ReactNode } from "react";
import { cx } from "./lib";

export type KeyValueItem = {
  label: string;
  value: ReactNode;
};

/**
 * Detail-page description grid; shares the DescriptionList typography but
 * lays items out in 1-3 columns.
 */
export function KeyValueGrid({
  items,
  columns = 2,
  className,
}: {
  items: KeyValueItem[];
  columns?: 1 | 2 | 3;
  className?: string;
}) {
  return (
    <dl
      className={cx(
        "argus-kv-grid",
        `argus-kv-grid--${columns}`,
        className,
      )}
    >
      {items.map((item) => (
        <div className="argus-kv-grid__item" key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}
