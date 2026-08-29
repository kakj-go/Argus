import { Ban } from "lucide-react";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Button } from "./button";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type TerminalLine = {
  kind: "stdin" | "stdout" | "stderr";
  content: string;
  time?: string;
};

export type TerminalState = "connected" | "connecting" | "disconnected";

type TerminalInstance = {
  clear(): void;
  dispose(): void;
  write(value: string): void;
  focus(): void;
};

function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

export function TerminalEmulator({
  lines = [],
  host,
  account,
  protocol = "ssh",
  state = "connected",
  startedAt,
  readOnly,
  onCommand,
  prompt = "$",
  placeholder,
  clearLabel,
  height = 320,
  className,
  mode = "line",
  onData,
  onResize,
  sessionId,
  autoFocusKey,
}: {
  /** Full output buffer; the source of truth for playback and live output. */
  lines?: TerminalLine[];
  host?: string;
  /** Remote account shown in the status bar, e.g. root@web-01. */
  account?: string;
  protocol?: string;
  state?: TerminalState;
  /** Session start time; when set, a live duration counter is shown. */
  startedAt?: Date | string | number;
  /** Playback mode: hides the command input. */
  readOnly?: boolean;
  onCommand?: (command: string) => void;
  prompt?: string;
  placeholder?: string;
  clearLabel?: string;
  height?: number;
  className?: string;
  /** PTY mode uses xterm and emits raw input; line mode preserves WinRS semantics. */
  mode?: "line" | "pty";
  onData?: (data: string) => void;
  onResize?: (cols: number, rows: number) => void;
  /** Stable identity used to preserve an xterm instance while output changes. */
  sessionId?: string;
  /** When this key changes the terminal is refocused (dock reopen, tab switch). */
  autoFocusKey?: string;
}) {
  const text = useUiText();
  const outputRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<TerminalInstance | null>(null);
  const onDataRef = useRef(onData);
  const onResizeRef = useRef(onResize);
  const linesRef = useRef(lines);
  const writtenChunksRef = useRef(0);
  const [typed, setTyped] = useState<TerminalLine[]>([]);
  const [draft, setDraft] = useState("");
  // Number of prop lines hidden by the last clear action.
  const [cleared, setCleared] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  onDataRef.current = onData;
  onResizeRef.current = onResize;
  linesRef.current = lines;

  useEffect(() => {
    if (startedAt === undefined) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [startedAt]);

  useEffect(() => {
    if (autoFocusKey === undefined) return;
    terminalRef.current?.focus();
  }, [autoFocusKey]);

  const visible = [...lines.slice(cleared), ...typed];

  useEffect(() => {
    const element = outputRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [visible.length]);

  useEffect(() => {
    if (mode !== "pty" || !xtermRef.current) return;
    const element = xtermRef.current;
    let disposed = false;
    let disposeTerminal: (() => void) | undefined;
    void Promise.all([
      import("@xterm/xterm"),
      import("@xterm/addon-fit"),
      import("@xterm/xterm/css/xterm.css"),
    ]).then(([xterm, fitModule]) => {
      if (disposed) return;
      const terminal = new xterm.Terminal({
        // PTY data already contains the remote terminal's exact control bytes.
        convertEol: false,
        cursorBlink: !readOnly,
        disableStdin: Boolean(readOnly),
        fontFamily: "var(--font-mono)",
        fontSize: 13,
        scrollback: 5000,
        theme: { background: "#11100e", foreground: "#f4f1ea" },
      });
      const fit = new fitModule.FitAddon();
      terminal.loadAddon(fit);
      terminal.open(element);
      fit.fit();
      terminalRef.current = terminal as unknown as TerminalInstance;
      for (const line of linesRef.current) terminal.write(line.content);
      writtenChunksRef.current = linesRef.current.length;
      terminal.focus();
      const data = terminal.onData((value) => onDataRef.current?.(value));
      const resize = terminal.onResize(({ cols, rows }) =>
        onResizeRef.current?.(cols, rows),
      );
      const observer = new ResizeObserver(() => fit.fit());
      observer.observe(element);
      disposeTerminal = () => {
        observer.disconnect();
        data.dispose();
        resize.dispose();
        terminal.dispose();
        terminalRef.current = null;
      };
    });
    return () => {
      disposed = true;
      disposeTerminal?.();
    };
  }, [mode, readOnly, sessionId]);

  useEffect(() => {
    if (mode !== "pty" || !terminalRef.current) return;
    const terminal = terminalRef.current;
    if (!terminal) return;
    if (lines.length < writtenChunksRef.current) {
      terminal.clear();
      writtenChunksRef.current = 0;
    }
    for (const line of lines.slice(writtenChunksRef.current))
      terminal.write(line.content);
    writtenChunksRef.current = lines.length;
  }, [lines, mode]);

  const stateText: Record<TerminalState, string> = {
    connected: text("已连接", "Connected"),
    connecting: text("连接中", "Connecting"),
    disconnected: text("已断开", "Disconnected"),
  };

  const submit = () => {
    const command = draft.trim();
    if (!command) return;
    setTyped((previous) => [...previous, { kind: "stdin", content: command }]);
    onCommand?.(command);
    setDraft("");
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") submit();
  };

  const startedAtMs =
    startedAt !== undefined ? new Date(startedAt).getTime() : undefined;

  return (
    <section className={cx("argus-terminal", className)}>
      <header className="argus-terminal__bar">
        <span
          className={cx("argus-terminal__state", `is-${state}`)}
          title={stateText[state]}
        >
          <i aria-hidden />
          {host && <b>{account ? `${account}@${host}` : host}</b>}
          <em>{protocol}</em>
        </span>
        <span className="argus-terminal__meta">
          {stateText[state]}
          {startedAtMs !== undefined && (
            <>
              {" · "}
              {formatDuration(now - startedAtMs)}
            </>
          )}
        </span>
        <Button
          aria-label={clearLabel ?? text("清屏", "Clear")}
          onClick={() => {
            setCleared(lines.length);
            setTyped([]);
            if (mode === "pty") {
              terminalRef.current?.clear();
              writtenChunksRef.current = lines.length;
            }
          }}
          size="icon"
          variant="ghost"
        >
          <Ban size={13} />
        </Button>
      </header>

      {mode === "pty" ? (
        <div
          className="argus-terminal__xterm"
          ref={xtermRef}
          style={{ height }}
        />
      ) : (
        <div
          className="argus-terminal__output"
          ref={outputRef}
          style={{ height }}
        >
          {visible.map((line, index) => (
            <div
              className={cx("argus-terminal__line", `is-${line.kind}`)}
              key={index}
            >
              {line.kind === "stdin" && (
                <span className="argus-terminal__prompt">{prompt}</span>
              )}
              {line.time && <time>{line.time}</time>}
              <span>{line.content}</span>
            </div>
          ))}
        </div>
      )}

      {!readOnly && mode === "line" && (
        <div className="argus-terminal__input">
          <span className="argus-terminal__prompt">{prompt}</span>
          <input
            aria-label={placeholder ?? text("输入命令", "Type a command")}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder={
              placeholder ??
              text("输入命令并回车…", "Type a command and press Enter…")
            }
            spellCheck={false}
            value={draft}
          />
        </div>
      )}
    </section>
  );
}
