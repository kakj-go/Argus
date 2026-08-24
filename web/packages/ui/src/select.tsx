import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { type AriaAttributes, type ReactNode } from "react";
import { mergeAriaIds, useFieldContext } from "./form";
import { cx } from "./lib";

const EMPTY_VALUE = "__argus_empty_value__";

export type SelectOption = {
  value: string;
  label: ReactNode;
  disabled?: boolean;
};

export function Select({
  value,
  onValueChange,
  options,
  placeholder,
  ariaLabel,
  className,
  disabled,
  id,
  required,
  ...ariaProps
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  ariaLabel?: string;
  className?: string;
  disabled?: boolean;
  id?: string;
  required?: boolean;
} & Pick<
  AriaAttributes,
  "aria-describedby" | "aria-invalid" | "aria-labelledby" | "aria-required"
>) {
  const field = useFieldContext();
  const normalizedValue = value === "" ? EMPTY_VALUE : value;
  return (
    <SelectPrimitive.Root
      disabled={disabled}
      onValueChange={(next) => onValueChange(next === EMPTY_VALUE ? "" : next)}
      required={required ?? field?.required}
      value={normalizedValue}
    >
      <SelectPrimitive.Trigger
        {...ariaProps}
        aria-describedby={mergeAriaIds(
          ariaProps["aria-describedby"],
          field?.descriptionId,
        )}
        aria-invalid={
          ariaProps["aria-invalid"] ?? (field?.invalid || undefined)
        }
        aria-label={ariaLabel}
        aria-labelledby={mergeAriaIds(
          ariaProps["aria-labelledby"],
          field?.labelId,
        )}
        aria-required={
          ariaProps["aria-required"] ?? (field?.required || undefined)
        }
        className={cx("argus-select", className)}
        id={id ?? field?.controlId}
      >
        <SelectPrimitive.Value placeholder={placeholder} />
        <SelectPrimitive.Icon className="argus-select__icon">
          <ChevronDown aria-hidden size={14} />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          className="argus-select__content"
          position="popper"
          sideOffset={5}
        >
          <SelectPrimitive.Viewport className="argus-select__viewport">
            {options.map((option) => (
              <SelectPrimitive.Item
                className="argus-select__item"
                disabled={option.disabled}
                key={option.value}
                value={option.value === "" ? EMPTY_VALUE : option.value}
              >
                <SelectPrimitive.ItemIndicator className="argus-select__check">
                  <Check aria-hidden size={14} />
                </SelectPrimitive.ItemIndicator>
                <SelectPrimitive.ItemText>
                  {option.label}
                </SelectPrimitive.ItemText>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}
