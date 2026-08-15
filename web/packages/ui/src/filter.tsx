import { RefreshCw, Search } from "lucide-react";
import { forwardRef, type InputHTMLAttributes } from "react";
import { Button } from "./button";
import { cx } from "./lib";
import { useUiText } from "./locale";
import { Select } from "./select";

export type SearchInputProps = InputHTMLAttributes<HTMLInputElement>;

export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(
  ({ className, ...props }, ref) => (
    <div className={cx("argus-search-input", className)}>
      <Search aria-hidden size={15} />
      <input ref={ref} type="search" {...props} />
    </div>
  ),
);
SearchInput.displayName = "SearchInput";

export type FilterBarFilter = {
  key: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  /** Placeholder option shown with an empty value, e.g. "All statuses". */
  allLabel?: string;
  ariaLabel?: string;
  onChange: (value: string) => void;
};

export function FilterBar({
  search,
  filters = [],
  onRefresh,
  refreshing,
  refreshLabel,
  className,
}: {
  search?: {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
  };
  filters?: FilterBarFilter[];
  onRefresh?: () => void;
  refreshing?: boolean;
  refreshLabel?: string;
  className?: string;
}) {
  const text = useUiText();
  return (
    <div className={cx("argus-filter-bar", className)}>
      {search && (
        <SearchInput
          aria-label={search.placeholder ?? text("搜索", "Search")}
          className="argus-filter-bar__search"
          onChange={(event) => search.onChange(event.target.value)}
          placeholder={search.placeholder ?? text("搜索…", "Search…")}
          value={search.value}
        />
      )}
      {filters.map((filter) => (
        <div className="argus-filter-bar__select" key={filter.key}>
          <Select
            ariaLabel={filter.ariaLabel ?? filter.allLabel ?? filter.key}
            onValueChange={filter.onChange}
            options={[
              ...(filter.allLabel !== undefined
                ? [{ value: "", label: filter.allLabel }]
                : []),
              ...filter.options,
            ]}
            value={filter.value}
          />
        </div>
      ))}
      {onRefresh && (
        <Button
          aria-label={refreshLabel ?? text("刷新", "Refresh")}
          className="argus-filter-bar__refresh"
          onClick={onRefresh}
          size="icon"
          variant="ghost"
        >
          <RefreshCw
            aria-hidden
            className={refreshing ? "argus-spin" : undefined}
            size={15}
          />
        </Button>
      )}
    </div>
  );
}
