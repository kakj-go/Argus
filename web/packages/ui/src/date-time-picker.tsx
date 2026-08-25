import { CalendarDays } from "lucide-react";
import { forwardRef, type InputHTMLAttributes } from "react";
import DatePicker from "react-datepicker";
import "react-datepicker/dist/react-datepicker.css";
import { Input } from "./form";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type DateTimePickerType = "date" | "datetime-local";

function parseLocalValue(value?: string): Date | null {
  if (!value) return null;
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/);
  if (!match) return null;
  const [, year, month, day, hours = "0", minutes = "0"] = match;
  const date = new Date(
    Number(year),
    Number(month) - 1,
    Number(day),
    Number(hours),
    Number(minutes),
  );
  return Number.isNaN(date.getTime()) ? null : date;
}

function pad(value: number) {
  return String(value).padStart(2, "0");
}

function formatLocalValue(date: Date, type: DateTimePickerType) {
  const value = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  return type === "date"
    ? value
    : `${value}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function toDate(value?: string) {
  return parseLocalValue(value) ?? undefined;
}

function toDateFromAttribute(value?: string | number) {
  return typeof value === "string" ? toDate(value) : undefined;
}

const PickerInput = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement>
>((props, ref) => <Input {...props} ref={ref} />);
PickerInput.displayName = "DateTimePickerInput";

export const DateTimePicker = forwardRef<
  HTMLInputElement,
  Omit<InputHTMLAttributes<HTMLInputElement>, "onChange" | "type" | "value"> & {
    onChange?: (value: string) => void;
    type?: DateTimePickerType;
    value?: string;
  }
>(
  (
    {
      className,
      disabled,
      id,
      max,
      min,
      name,
      onBlur,
      onChange,
      placeholder,
      required,
      type = "datetime-local",
      value,
      ...props
    },
    ref,
  ) => {
    const text = useUiText();
    const selected = parseLocalValue(value);
    const displayFormat = type === "date" ? "yyyy/MM/dd" : "yyyy/MM/dd HH:mm";
    return (
      <div className={cx("argus-date-time-picker", className)}>
        <DatePicker
          calendarClassName="argus-date-time-picker__calendar"
          dateFormat={displayFormat}
          disabled={disabled}
          isClearable={!required}
          minDate={toDateFromAttribute(min)}
          maxDate={toDateFromAttribute(max)}
          name={name}
          onBlur={onBlur}
          onChange={(date: Date | null) =>
            onChange?.(date ? formatLocalValue(date, type) : "")
          }
          placeholderText={placeholder}
          popperClassName="argus-date-time-picker__popper"
          selected={selected}
          showTimeSelect={type === "datetime-local"}
          timeCaption={text("时间", "Time")}
          timeFormat="HH:mm"
          timeIntervals={1}
          wrapperClassName="argus-date-time-picker__control"
          customInput={
            <PickerInput
              {...props}
              aria-label={props["aria-label"]}
              id={id}
              ref={ref}
              required={required}
              value={value}
            />
          }
        />
        <CalendarDays
          aria-hidden
          className="argus-date-time-picker__icon"
          size={18}
        />
      </div>
    );
  },
);
DateTimePicker.displayName = "DateTimePicker";
