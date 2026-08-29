import type { ReactNode } from "react";
import type { ButtonProps } from "./button";
import { Button } from "./button";
import { cx } from "./lib";

/**
 * ActionGroup - 统一的操作按钮容器组件
 *
 * 用于表格行操作、表单按钮组、卡片操作等场景，确保按钮间距一致。
 *
 * @example
 * ```tsx
 * <ActionGroup>
 *   <RowAction>编辑</RowAction>
 *   <RowAction danger>删除</RowAction>
 * </ActionGroup>
 * ```
 */
export function ActionGroup({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={className ? `argus-action-group ${className}` : "argus-action-group"}>
      {children}
    </div>
  );
}

export type RowActionProps = ButtonProps & { danger?: boolean };

/**
 * RowAction - 列表/表格行内操作按钮
 *
 * 统一为文字型态（ghost + sm），与"详情"链接保持一致；破坏性操作
 * （删除、卸载、终止等不可逆动作）传 `danger` 以红色文字呈现。
 * 页面级主操作、表单/对话框底部按钮仍使用 Button，不要用本组件。
 *
 * @example
 * ```tsx
 * <ActionGroup>
 *   <RowAction onClick={openDetail}>详情</RowAction>
 *   <RowAction danger onClick={remove}>删除</RowAction>
 * </ActionGroup>
 * ```
 */
export function RowAction({ danger, className, ...props }: RowActionProps) {
  return (
    <Button
      {...props}
      className={cx("argus-row-action", danger && "argus-row-action--danger", className)}
      size="sm"
      variant="ghost"
    />
  );
}
