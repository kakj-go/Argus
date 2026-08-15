import { type ButtonHTMLAttributes, forwardRef } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { LoaderCircle } from "lucide-react";
import { cx } from "./lib";

const buttonVariants = cva("argus-button", {
  variants: {
    variant: {
      primary: "argus-button--primary",
      secondary: "argus-button--secondary",
      ghost: "argus-button--ghost",
      danger: "argus-button--danger",
    },
    size: {
      sm: "argus-button--sm",
      md: "argus-button--md",
      lg: "argus-button--lg",
      icon: "argus-button--icon",
    },
  },
  defaultVariants: { variant: "secondary", size: "md" },
});

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants> & { loading?: boolean };

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className, variant, size, loading, disabled, children, ...props },
    ref,
  ) => (
    <button
      ref={ref}
      className={cx(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <LoaderCircle aria-hidden className="argus-spin" size={15} />}
      {children}
    </button>
  ),
);
Button.displayName = "Button";
