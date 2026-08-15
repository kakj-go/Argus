import { Check } from "lucide-react";
import { type ReactNode } from "react";
import { Button } from "./button";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type WizardStep = {
  id: string;
  title: string;
  description?: string;
  /** Optional steps can be skipped via the skip button. */
  optional?: boolean;
};

export function Wizard({
  steps,
  current,
  children,
  canNext = true,
  submitting,
  onBack,
  onNext,
  onSkip,
  onSubmit,
  backLabel,
  nextLabel,
  skipLabel,
  submitLabel,
  className,
}: {
  steps: WizardStep[];
  /** Zero-based index of the current step (controlled). */
  current: number;
  /** Content of the current step. */
  children: ReactNode;
  /** Set false to disable advancing (e.g. failed validation). */
  canNext?: boolean;
  submitting?: boolean;
  onBack?: () => void;
  onNext?: () => void;
  onSkip?: () => void;
  onSubmit?: () => void;
  backLabel?: string;
  nextLabel?: string;
  skipLabel?: string;
  submitLabel?: string;
  className?: string;
}) {
  const text = useUiText();
  const isLast = current >= steps.length - 1;
  const step = steps[current];

  return (
    <div className={cx("argus-wizard", className)}>
      <ol className="argus-wizard__steps">
        {steps.map((item, index) => {
          const status =
            index < current
              ? "done"
              : index === current
                ? "current"
                : "pending";
          return (
            <li className={cx("argus-wizard__step", `is-${status}`)} key={item.id}>
              <span className="argus-wizard__marker">
                {status === "done" ? <Check aria-hidden size={12} /> : index + 1}
              </span>
              <span className="argus-wizard__step-text">
                <b>{item.title}</b>
                {item.description && <small>{item.description}</small>}
              </span>
            </li>
          );
        })}
      </ol>

      <div className="argus-wizard__content">{children}</div>

      <footer className="argus-wizard__footer">
        <Button
          disabled={current <= 0 || submitting}
          onClick={onBack}
          variant="secondary"
        >
          {backLabel ?? text("上一步", "Back")}
        </Button>
        <div className="argus-wizard__footer-right">
          {step?.optional && onSkip && (
            <Button disabled={submitting} onClick={onSkip} variant="ghost">
              {skipLabel ?? text("跳过", "Skip")}
            </Button>
          )}
          {isLast ? (
            <Button
              disabled={!canNext}
              loading={submitting}
              onClick={onSubmit}
              variant="primary"
            >
              {submitLabel ?? text("提交", "Submit")}
            </Button>
          ) : (
            <Button disabled={!canNext} onClick={onNext} variant="primary">
              {nextLabel ?? text("下一步", "Next")}
            </Button>
          )}
        </div>
      </footer>
    </div>
  );
}
