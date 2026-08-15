import {
  forwardRef,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from "react";
import { cx } from "./lib";

export const Input = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement>
>(({ className, ...props }, ref) => (
  <input ref={ref} className={cx("argus-input", className)} {...props} />
));
Input.displayName = "Input";

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, ...props }, ref) => (
  <textarea ref={ref} className={cx("argus-textarea", className)} {...props} />
));
Textarea.displayName = "Textarea";

export function Field({
  label,
  hint,
  error,
  children,
  className,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <label className={cx("argus-field", className)}>
      <span className="argus-field__label">{label}</span>
      {children}
      {(hint || error) && (
        <span className={cx("argus-field__hint", error && "is-error")}>
          {error || hint}
        </span>
      )}
    </label>
  );
}

export function Switch({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
}) {
  return (
    <button
      aria-checked={checked}
      className={cx("argus-switch", checked && "is-on")}
      onClick={() => onChange(!checked)}
      role="switch"
      type="button"
    >
      <span aria-hidden />
      <span className="sr-only">{label}</span>
    </button>
  );
}
