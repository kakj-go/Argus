import {
  cloneElement,
  forwardRef,
  isValidElement,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
  useId,
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
  const generatedId = useId();
  const controlId =
    isValidElement<Record<string, unknown>>(children) &&
    typeof children.props.id === "string"
      ? children.props.id
      : `${generatedId}-control`;
  const messageId = `${generatedId}-message`;
  const control = isValidElement<Record<string, unknown>>(children)
    ? cloneElement(children, {
        "aria-describedby": hint || error ? messageId : undefined,
        "aria-invalid": error ? true : undefined,
        id: controlId,
      })
    : children;
  return (
    <div className={cx("argus-field", className)}>
      <label className="argus-field__label" htmlFor={controlId}>{label}</label>
      {control}
      {(hint || error) && (
        <span
          className={cx("argus-field__hint", error && "is-error")}
          id={messageId}
        >
          {error || hint}
        </span>
      )}
    </div>
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
