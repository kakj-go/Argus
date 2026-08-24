import {
  createContext,
  forwardRef,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
  useContext,
  useId,
} from "react";
import { cx } from "./lib";

export type FieldRequirement = "required" | "optional" | "none";

export type FieldContextValue = {
  controlId?: string;
  descriptionId?: string;
  invalid: boolean;
  labelId?: string;
  required: boolean;
};

const FieldContext = createContext<FieldContextValue | null>(null);

export function mergeAriaIds(...values: Array<string | undefined>) {
  const result = values
    .flatMap((value) => value?.split(/\s+/) ?? [])
    .filter(Boolean);
  return result.length > 0 ? [...new Set(result)].join(" ") : undefined;
}

export function useFieldContext() {
  return useContext(FieldContext);
}

export const Input = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement>
>(({ className, ...props }, ref) => {
  const field = useContext(FieldContext);
  return (
    <input
      ref={ref}
      {...props}
      aria-describedby={mergeAriaIds(
        props["aria-describedby"],
        field?.descriptionId,
      )}
      aria-invalid={props["aria-invalid"] ?? (field?.invalid || undefined)}
      aria-labelledby={mergeAriaIds(
        props["aria-labelledby"],
        field?.controlId ? undefined : field?.labelId,
      )}
      aria-required={props["aria-required"] ?? (field?.required || undefined)}
      className={cx("argus-input", className)}
      id={props.id ?? field?.controlId}
      required={props.required ?? field?.required}
    />
  );
});
Input.displayName = "Input";

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, ...props }, ref) => {
  const field = useContext(FieldContext);
  return (
    <textarea
      ref={ref}
      {...props}
      aria-describedby={mergeAriaIds(
        props["aria-describedby"],
        field?.descriptionId,
      )}
      aria-invalid={props["aria-invalid"] ?? (field?.invalid || undefined)}
      aria-labelledby={mergeAriaIds(
        props["aria-labelledby"],
        field?.controlId ? undefined : field?.labelId,
      )}
      aria-required={props["aria-required"] ?? (field?.required || undefined)}
      className={cx("argus-textarea", className)}
      id={props.id ?? field?.controlId}
      required={props.required ?? field?.required}
    />
  );
});
Textarea.displayName = "Textarea";

export function Field({
  label,
  hint,
  error,
  children,
  className,
  controlId: providedControlId,
  controlMode = "single",
  requirement,
}: {
  label: string;
  requirement: FieldRequirement;
  hint?: string;
  error?: string;
  children: ReactNode;
  className?: string;
  controlId?: string;
  controlMode?: "single" | "group";
}) {
  const generatedId = useId();
  const controlId =
    controlMode === "single"
      ? (providedControlId ?? `${generatedId}-control`)
      : undefined;
  const labelId = `${generatedId}-label`;
  const messageId = `${generatedId}-message`;
  const required = requirement === "required";
  const context: FieldContextValue = {
    controlId,
    descriptionId: hint || error ? messageId : undefined,
    invalid: Boolean(error),
    labelId: controlMode === "single" ? labelId : undefined,
    required,
  };
  return (
    <div
      aria-labelledby={controlMode === "group" ? labelId : undefined}
      className={cx("argus-field", className)}
      role={controlMode === "group" ? "group" : undefined}
    >
      <label className="argus-field__label" htmlFor={controlId} id={labelId}>
        {label}
        {required && (
          <span aria-hidden className="argus-field__required">
            *
          </span>
        )}
      </label>
      <FieldContext.Provider value={context}>{children}</FieldContext.Provider>
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
