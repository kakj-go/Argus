import type { ReactNode } from "react";

type TerminalViewportProps = {
  children: ReactNode;
};

export function TerminalViewport({ children }: TerminalViewportProps) {
  return <div className="argus-terminal-viewport">{children}</div>;
}
