import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { type ReactNode } from "react";
import { Button } from "./button";
import { useUiText } from "./locale";

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  danger,
  confirmLabel,
  cancelLabel,
  loading,
  onConfirm,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  danger?: boolean;
  confirmLabel?: string;
  cancelLabel?: string;
  loading?: boolean;
  onConfirm: () => void;
  children?: ReactNode;
}) {
  const text = useUiText();
  return (
    <DialogPrimitive.Root onOpenChange={onOpenChange} open={open}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="argus-dialog__overlay" />
        <DialogPrimitive.Content className="argus-dialog argus-dialog--confirm">
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
              aria-label={text("关闭", "Close")}
              className="argus-dialog__close"
            >
              <X size={17} />
            </DialogPrimitive.Close>
          </div>
          {children && <div className="argus-dialog__body">{children}</div>}
          <div className="argus-dialog__footer">
            <Button
              disabled={loading}
              onClick={() => onOpenChange(false)}
              variant="secondary"
            >
              {cancelLabel ?? text("取消", "Cancel")}
            </Button>
            <Button
              loading={loading}
              onClick={onConfirm}
              variant={danger ? "danger" : "primary"}
            >
              {confirmLabel ?? text("确认", "Confirm")}
            </Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function FormDrawer({
  open,
  onOpenChange,
  title,
  description,
  children,
  submitLabel,
  cancelLabel,
  loading,
  onSubmit,
  footer,
  width = 480,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  submitLabel?: string;
  cancelLabel?: string;
  loading?: boolean;
  onSubmit?: () => void;
  /** Replaces the default cancel/submit footer when provided. */
  footer?: ReactNode;
  width?: number;
}) {
  const text = useUiText();
  return (
    <DialogPrimitive.Root onOpenChange={onOpenChange} open={open}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="argus-dialog__overlay" />
        <DialogPrimitive.Content
          className="argus-drawer"
          style={{ width: `min(${width}px, 100vw)` }}
        >
          <div className="argus-drawer__top">
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
              aria-label={text("关闭", "Close")}
              className="argus-dialog__close"
            >
              <X size={17} />
            </DialogPrimitive.Close>
          </div>
          <div className="argus-drawer__body">{children}</div>
          <div className="argus-drawer__footer">
            {footer ?? (
              <>
                <Button
                  disabled={loading}
                  onClick={() => onOpenChange(false)}
                  variant="secondary"
                >
                  {cancelLabel ?? text("取消", "Cancel")}
                </Button>
                <Button loading={loading} onClick={onSubmit} variant="primary">
                  {submitLabel ?? text("提交", "Submit")}
                </Button>
              </>
            )}
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
