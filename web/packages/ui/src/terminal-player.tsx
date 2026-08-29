import { Pause, Play, RotateCcw } from "lucide-react";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Button } from "./button";
import { cx } from "./lib";
import { useUiText } from "./locale";

/** Asciicast v2 event tuple carried by session recordings. */
export type TerminalPlayerEvent = {
  /** Seconds relative to session start. */
  time: number;
  type: "i" | "o" | "r" | "m";
  data: unknown;
};

const VALID_TYPES = new Set(["i", "o", "r", "m"]);
const SPEEDS = [1, 2, 4, 8] as const;

/**
 * Defensive normalization: the API contract guarantees an event array, but a
 * malformed page must degrade to a shorter replay instead of crashing render.
 */
export function normalizeTerminalPlayerEvents(
  raw: readonly unknown[],
): TerminalPlayerEvent[] {
  const events: TerminalPlayerEvent[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue;
    const candidate = item as { time?: unknown; type?: unknown; data?: unknown };
    const time = Number(candidate.time);
    if (!Number.isFinite(time) || time < 0) continue;
    if (typeof candidate.type !== "string" || !VALID_TYPES.has(candidate.type)) continue;
    events.push({ time, type: candidate.type as TerminalPlayerEvent["type"], data: candidate.data });
  }
  events.sort((a, b) => a.time - b.time);
  return events;
}

function formatClock(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  const mm = String(minutes).padStart(2, "0");
  const ss = String(secs).padStart(2, "0");
  return hours > 0 ? `${hours}:${mm}:${ss}` : `${mm}:${ss}`;
}

function parseResize(data: unknown): { cols: number; rows: number } | null {
  if (typeof data !== "string") return null;
  const match = /^(\d+)x(\d+)$/.exec(data);
  if (!match) return null;
  const cols = Number(match[1]);
  const rows = Number(match[2]);
  if (cols < 2 || cols > 500 || rows < 2 || rows > 500) return null;
  return { cols, rows };
}

type XtermLike = {
  dispose(): void;
  reset(): void;
  resize(cols: number, rows: number): void;
  write(value: string): void;
};

/**
 * JumpServer-style session recording player: a terminal emulator driven by a
 * wall-clock scheduler over asciicast events. Output events repaint the screen
 * exactly as the live session did; seeking replays the stream from the start,
 * so the progress bar behaves like a video timeline.
 */
export function TerminalPlayer({
  events,
  loading = false,
  height = 420,
  className,
  emptyLabel,
  loadingLabel,
}: {
  /** Stable identity expected (memoize at the call site); changes restart playback. */
  events: readonly TerminalPlayerEvent[];
  loading?: boolean;
  height?: number;
  className?: string;
  emptyLabel?: string;
  loadingLabel?: string;
}) {
  const text = useUiText();
  const screenRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<XtermLike | null>(null);
  const cursorRef = useRef(0);
  // The scheduler loop reads live values through refs so it is not rebuilt per tick.
  const positionRef = useRef(0);
  const speedRef = useRef(1);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<number>(1);
  const [position, setPosition] = useState(0);
  const duration = events.at(-1)?.time ?? 0;
  const eventsRef = useRef(events);
  eventsRef.current = events;
  speedRef.current = speed;

  const emit = (event: TerminalPlayerEvent) => {
    const terminal = terminalRef.current;
    if (!terminal) return;
    if (event.type === "o" && typeof event.data === "string") {
      terminal.write(event.data);
    } else if (event.type === "r") {
      const size = parseResize(event.data);
      if (size) terminal.resize(size.cols, size.rows);
    }
  };

  useEffect(() => {
    const element = screenRef.current;
    if (!element) return;
    let disposed = false;
    let disposeTerminal: (() => void) | undefined;
    void Promise.all([
      import("@xterm/xterm"),
      import("@xterm/xterm/css/xterm.css"),
    ]).then(([xterm]) => {
      if (disposed) return;
      const terminal = new xterm.Terminal({
        convertEol: false,
        cursorBlink: false,
        disableStdin: true,
        fontFamily: "var(--font-mono)",
        fontSize: 13,
        scrollback: 10000,
        theme: { background: "#11100e", foreground: "#f4f1ea" },
      });
      terminal.open(element);
      terminalRef.current = terminal as unknown as XtermLike;
      disposeTerminal = () => {
        terminal.dispose();
        terminalRef.current = null;
      };
    });
    return () => {
      disposed = true;
      disposeTerminal?.();
    };
  }, []);

  // Re-emit from the beginning whenever the loaded event stream changes identity.
  useEffect(() => {
    cursorRef.current = 0;
    positionRef.current = 0;
    setPosition(0);
    setPlaying(false);
    terminalRef.current?.reset();
  }, [events]);

  useEffect(() => {
    if (!playing) return;
    let frame = 0;
    let last = performance.now();
    const tick = (now: number) => {
      const terminal = terminalRef.current;
      const list = eventsRef.current;
      const next = Math.min(duration, positionRef.current + ((now - last) / 1000) * speedRef.current);
      last = now;
      if (terminal) {
        for (let event = list[cursorRef.current]; event !== undefined && event.time <= next; event = list[cursorRef.current]) {
          emit(event);
          cursorRef.current++;
        }
      }
      positionRef.current = next;
      setPosition(next);
      if (next >= duration) {
        setPlaying(false);
        return;
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [playing, duration]);

  const seek = (target: number) => {
    const clamped = Math.min(Math.max(target, 0), duration);
    const terminal = terminalRef.current;
    if (terminal) {
      terminal.reset();
      cursorRef.current = 0;
      const list = eventsRef.current;
      for (let event = list[cursorRef.current]; event !== undefined && event.time <= clamped; event = list[cursorRef.current]) {
        emit(event);
        cursorRef.current++;
      }
    }
    positionRef.current = clamped;
    setPosition(clamped);
    if (clamped >= duration) setPlaying(false);
  };

  const toggle = () => {
    if (duration > 0 && positionRef.current >= duration) seek(0);
    setPlaying((value) => !value);
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === " " || event.key === "k") {
      event.preventDefault();
      toggle();
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      seek(positionRef.current - 5);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      seek(positionRef.current + 5);
    }
  };

  const summary = useMemo(
    () => `${formatClock(position)} / ${formatClock(duration)}`,
    [position, duration],
  );

  return (
    <div className={cx("argus-terminal-player", className)} onKeyDown={onKeyDown} tabIndex={0}>
      <div className="argus-terminal-player__screen" ref={screenRef} style={{ height }} />
      {(loading || (!loading && events.length === 0)) && (
        <div className="argus-terminal-player__overlay" role="status">
          {loading
            ? (loadingLabel ?? text("录像加载中…", "Loading recording…"))
            : (emptyLabel ?? text("录像中暂无事件", "No recording events"))}
        </div>
      )}
      <div className="argus-terminal-player__controls">
        <Button
          aria-label={playing ? text("暂停", "Pause") : text("播放", "Play")}
          onClick={toggle}
          size="icon"
          variant="secondary"
        >
          {playing ? <Pause size={16} /> : <Play size={16} />}
        </Button>
        <Button
          aria-label={text("从头播放", "Restart")}
          onClick={() => { setPlaying(false); seek(0); }}
          size="icon"
          variant="ghost"
        >
          <RotateCcw size={16} />
        </Button>
        <input
          aria-label={text("录像进度", "Recording position")}
          aria-valuetext={summary}
          max={Math.max(duration, 0.1)}
          min={0}
          onChange={(event) => seek(Number(event.target.value))}
          step={0.1}
          type="range"
          value={position}
        />
        <span className="argus-terminal-player__clock">{summary}</span>
        <div aria-label={text("倍速", "Speed")} className="argus-terminal-player__speeds" role="group">
          {SPEEDS.map((value) => (
            <button
              aria-pressed={speed === value}
              className={cx("argus-terminal-player__speed", speed === value && "is-active")}
              key={value}
              onClick={() => setSpeed(value)}
              type="button"
            >
              {value}x
            </button>
          ))}
        </div>
        {position >= duration && duration > 0 && (
          <span className="argus-terminal-player__ended">{text("回放结束", "Replay ended")}</span>
        )}
      </div>
    </div>
  );
}
