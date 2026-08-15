import { type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "./button";
import { useUiText } from "./locale";

export type Column<T> = {
  key: keyof T | string;
  header: string;
  render?: (item: T) => ReactNode;
  align?: "left" | "right";
};

export function DataTable<T extends Record<string, unknown>>({
  columns,
  data,
  getRowKey,
}: {
  columns: Column<T>[];
  data: T[];
  getRowKey: (item: T) => string;
}) {
  return (
    <div className="argus-table-wrap">
      <table className="argus-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th
                className={column.align === "right" ? "is-right" : ""}
                key={String(column.key)}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((item) => (
            <tr key={getRowKey(item)}>
              {columns.map((column) => (
                <td
                  className={column.align === "right" ? "is-right" : ""}
                  key={String(column.key)}
                >
                  {column.render
                    ? column.render(item)
                    : String(item[column.key as keyof T] ?? "")}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function Pagination({
  page,
  totalPages,
  onChange,
}: {
  page: number;
  totalPages: number;
  onChange: (page: number) => void;
}) {
  const text = useUiText();
  return (
    <nav aria-label={text("分页", "Pagination")} className="argus-pagination">
      <span>
        {text(`第 ${page} / ${totalPages} 页`, `Page ${page} of ${totalPages}`)}
      </span>
      <div>
        <Button
          aria-label={text("上一页", "Previous page")}
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
          size="icon"
          variant="ghost"
        >
          <ChevronLeft size={16} />
        </Button>
        <Button
          aria-label={text("下一页", "Next page")}
          disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}
          size="icon"
          variant="ghost"
        >
          <ChevronRight size={16} />
        </Button>
      </div>
    </nav>
  );
}
