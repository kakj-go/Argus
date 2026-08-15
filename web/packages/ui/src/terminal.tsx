import { Ban } from "lucide-react";
import {
  type KeyboardEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { Button } from "./button";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type TerminalLine = {
  kind: "stdin" | "stdout" | "stderr";
  content: string;
  time?: string;
};

export type TerminalState = "connected" | "connecting" | "disconnected";

function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

export function TerminalEmulator({
  lines = [],
  host,
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
}: {
  /** Full output buffer; the source of truth for playback and live output. */
  lines?: TerminalLine[];
  host?: string;
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
}) {
  const text = useUiText();
  const outputRef = useRef<HTMLDivElement>(null);
  const [typed, setTyped] = useState<TerminalLine[]>([]);
  const [draft, setDraft] = useState("");
  // Number of prop lines hidden by the last clear action.
  const [cleared, setCleared] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (startedAt === undefined) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [startedAt]);

  const visible = [...lines.slice(cleared), ...typed];

  useEffect(() => {
    const element = outputRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [visible.length]);

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
          {host && <b>{host}</b>}
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
          }}
          size="icon"
          variant="ghost"
        >
          <Ban size={13} />
        </Button>
      </header>

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

      {!readOnly && (
        <div className="argus-terminal__input">
          <span className="argus-terminal__prompt">{prompt}</span>
          <input
            aria-label={placeholder ?? text("输入命令", "Type a command")}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder={placeholder ?? text("输入命令并回车…", "Type a command and press Enter…")}
            spellCheck={false}
            value={draft}
          />
        </div>
      )}
    </section>
  );
}
