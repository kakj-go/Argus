import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { Avatar, Dropdown, Tooltip } from "./primitives";
import { Button, type ButtonProps } from "./button";
import { cx } from "./lib";

export type UserMenuItem =
  | {
      label: string;
      shortcut?: string;
      danger?: boolean;
      onSelect?: () => void;
    }
  | "separator";

export function AppShell({
  sidebar,
  header,
  children,
  overlay,
  className,
}: {
  sidebar: ReactNode;
  header: ReactNode;
  children: ReactNode;
  overlay?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cx("argus-app-shell", className)}>
      {sidebar}
      <div className="argus-app-main">
        {header}
        <main className="argus-page-content">{children}</main>
      </div>
      {overlay}
    </div>
  );
}

export function PortalUserMenu({
  displayName,
  username,
  items,
}: {
  displayName: string;
  username: string;
  items: UserMenuItem[];
}) {
  return (
    <Dropdown
      items={items}
      trigger={
        <button className="argus-user-menu" type="button">
          <Avatar fallback={displayName.slice(0, 1)} />
          <span>
            <b>{displayName}</b>
            <small>{username}</small>
          </span>
          <ChevronDown aria-hidden size={13} />
        </button>
      }
    />
  );
}

export function IconButton({
  label,
  children,
  ...props
}: Omit<ButtonProps, "aria-label" | "size"> & {
  label: string;
  children: ReactNode;
}) {
  return (
    <Tooltip content={label}>
      <Button aria-label={label} size="icon" {...props}>
        {children}
      </Button>
    </Tooltip>
  );
}

export function AuthStatePage({
  status,
  title,
  message,
}: {
  status: "checking" | "unavailable";
  title?: string;
  message?: string | null;
}) {
  if (status === "checking") {
    return <main aria-busy="true" className="argus-auth-state" />;
  }
  return (
    <main className="argus-auth-state" role="alert">
      {title && <h1>{title}</h1>}
      {message && <p>{message}</p>}
    </main>
  );
}
