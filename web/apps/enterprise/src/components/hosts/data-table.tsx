import { DataTable, type Column } from "@argus/ui";

/**
 * DataTable 的泛型约束是 `Record<string, unknown>`，接口类型没有索引签名
 * 无法直接满足；这里做一层类型适配，调用方保持完整的行类型。
 * （与 components/ai-settings/shared.tsx 中的 Table 同一模式。）
 */
export function Table<T extends object>({
  columns,
  data,
  getRowKey,
}: {
  columns: Column<T>[];
  data: T[];
  getRowKey: (item: T) => string;
}) {
  return (
    <DataTable
      columns={columns as unknown as Column<Record<string, unknown>>[]}
      data={data as unknown as Record<string, unknown>[]}
      getRowKey={(item) => getRowKey(item as unknown as T)}
    />
  );
}
