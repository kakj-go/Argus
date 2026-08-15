import * as AvatarPrimitive from "@radix-ui/react-avatar";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as DropdownPrimitive from "@radix-ui/react-dropdown-menu";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { Check, ChevronRight, X } from "lucide-react";
import { type ReactNode } from "react";
import { cx } from "./lib";
import { useUiText } from "./locale";

export function Avatar({
  fallback,
  src,
  size = "md",
}: {
  fallback: string;
  src?: string;
  size?: "sm" | "md" | "lg";
}) {
  return (
    <AvatarPrimitive.Root
      className={cx("argus-avatar", `argus-avatar--${size}`)}
    >
      <AvatarPrimitive.Image alt="" className="argus-avatar__image" src={src} />
      <AvatarPrimitive.Fallback
        className="argus-avatar__fallback"
        delayMs={200}
      >
        {fallback}
      </AvatarPrimitive.Fallback>
    </AvatarPrimitive.Root>
  );
}

export const Tabs = TabsPrimitive.Root;

export function TabsList({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <TabsPrimitive.List className={cx("argus-tabs", className)}>
      {children}
    </TabsPrimitive.List>
  );
}

export function TabsTrigger({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  return (
    <TabsPrimitive.Trigger className="argus-tabs__trigger" value={value}>
      {children}
    </TabsPrimitive.Trigger>
  );
}

export function TabsContent({
  value,
  children,
  className,
}: {
  value: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <TabsPrimitive.Content
      className={cx("argus-tabs__content", className)}
      value={value}
    >
      {children}
    </TabsPrimitive.Content>
  );
}

export function Tooltip({
  children,
  content,
}: {
  children: ReactNode;
  content: ReactNode;
}) {
  return (
    <TooltipPrimitive.Provider delayDuration={350}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content className="argus-tooltip" sideOffset={7}>
            {content}
            <TooltipPrimitive.Arrow className="argus-tooltip__arrow" />
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
}

export function Dialog({
  trigger,
  title,
  description,
  children,
  footer,
  open,
  onOpenChange,
}: {
  trigger?: ReactNode;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const text = useUiText();
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      {trigger && (
        <DialogPrimitive.Trigger asChild>{trigger}</DialogPrimitive.Trigger>
      )}
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="argus-dialog__overlay" />
        <DialogPrimitive.Content className="argus-dialog">
          <div className="argus-dialog__top">
            <div>
              <DialogPrimitive.Title className="argus-dialog__title">
                {title}
              </DialogPrimitive.Title>
              {description && (
                <DialogPrimitive.Description className="argus-dialog__description">
                  {description}
                </DialogPrimitive.Description>
              )}
            </div>
            <DialogPrimitive.Close
              className="argus-dialog__close"
              aria-label={text("关闭", "Close")}
            >
              <X size={17} />
            </DialogPrimitive.Close>
          </div>
          <div className="argus-dialog__body">{children}</div>
          {footer && <div className="argus-dialog__footer">{footer}</div>}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Dropdown({
  trigger,
  items,
}: {
  trigger: ReactNode;
  items: Array<
    | {
        label: string;
        shortcut?: string;
        danger?: boolean;
        onSelect?: () => void;
      }
    | "separator"
  >;
}) {
  return (
    <DropdownPrimitive.Root>
      <DropdownPrimitive.Trigger asChild>{trigger}</DropdownPrimitive.Trigger>
      <DropdownPrimitive.Portal>
        <DropdownPrimitive.Content
          className="argus-dropdown"
          sideOffset={6}
          align="end"
        >
          {items.map((item, index) =>
            item === "separator" ? (
              <DropdownPrimitive.Separator
                className="argus-dropdown__separator"
                key={index}
              />
            ) : (
              <DropdownPrimitive.Item
                className={cx(
                  "argus-dropdown__item",
                  item.danger && "is-danger",
                )}
                key={item.label}
                onSelect={item.onSelect}
              >
                <span>{item.label}</span>
                {item.shortcut && <kbd>{item.shortcut}</kbd>}
              </DropdownPrimitive.Item>
            ),
          )}
        </DropdownPrimitive.Content>
      </DropdownPrimitive.Portal>
    </DropdownPrimitive.Root>
  );
}

export function MenuItem({
  active,
  children,
  end,
}: {
  active?: boolean;
  children: ReactNode;
  end?: ReactNode;
}) {
  return (
    <div className={cx("argus-menu-item", active && "is-active")}>
      <span>{children}</span>
      {end ?? <ChevronRight size={14} />}
    </div>
  );
}

export function CheckItem({
  checked,
  children,
}: {
  checked: boolean;
  children: ReactNode;
}) {
  return (
    <div className="argus-check-item">
      <span className={cx("argus-check-item__box", checked && "is-checked")}>
        {checked && <Check size={12} />}
      </span>
      {children}
    </div>
  );
}
