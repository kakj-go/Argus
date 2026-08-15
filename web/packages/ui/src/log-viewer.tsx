import { Copy, Download, Pause, Play } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "./button";
import { Switch } from "./form";
import { cx } from "./lib";
import { useUiText } from "./locale";

export type LogLevel = "info" | "warn" | "error" | "debug";

export type LogLine = {
  timestamp?: string;
  level?: LogLevel;
  content: string;
};

function serialize(lines: LogLine[]): string {
  return lines
    .map((line) =>
      [line.timestamp, line.level?.toUpperCase(), line.content]
        .filter(Boolean)
        .join(" "),
    )
    .join("\n");
}

export function LogViewer({
  lines,
  height = 320,
  autoScroll = true,
  showLineNumbers = true,
  fileName = "argus.log",
  followLabel,
  pauseLabel,
  copyLabel,
  downloadLabel,
  className,
}: {
  lines: LogLine[];
  height?: number;
  /** Initial follow state; also re-applied when it changes. */
  autoScroll?: boolean;
  showLineNumbers?: boolean;
  fileName?: string;
  followLabel?: string;
  pauseLabel?: string;
  copyLabel?: string;
  downloadLabel?: string;
  className?: string;
}) {
  const text = useUiText();
  const outputRef = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(autoScroll);
  const [paused, setPaused] = useState(false);
  // Snapshot of lines frozen while paused.
  const frozenRef = useRef<LogLine[]>(lines);

  useEffect(() => setFollowing(autoScroll), [autoScroll]);

  useEffect(() => {
    if (!paused) frozenRef.current = lines;
  }, [lines, paused]);

  const visible = paused ? frozenRef.current : lines;

  useEffect(() => {
    const element = outputRef.current;
    if (following && !paused && element) {
      element.scrollTop = element.scrollHeight;
    }
  }, [visible.length, following, paused]);

  const copy = () => {
    void navigator.clipboard?.writeText(serialize(visible));
  };

  const download = () => {
    const blob = new Blob([serialize(visible)], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  return (
    <section className={cx("argus-log-viewer", className)}>
      <header className="argus-log-viewer__toolbar">
        <span className="argus-log-viewer__follow">
          <Switch
            checked={following}
            label={followLabel ?? text("自动滚动", "Follow")}
            onChange={setFollowing}
          />
          <span>{followLabel ?? text("自动滚动", "Follow")}</span>
        </span>
        <div className="argus-log-viewer__actions">
          <Button
            aria-label={pauseLabel ?? text("暂停", "Pause")}
            onClick={() => setPaused((value) => !value)}
            size="icon"
            variant={paused ? "secondary" : "ghost"}
          >
            {paused ? <Play size={14} /> : <Pause size={14} />}
          </Button>
          <Button
            aria-label={copyLabel ?? text("复制", "Copy")}
            onClick={copy}
            size="icon"
            variant="ghost"
          >
            <Copy size={14} />
          </Button>
          <Button
            aria-label={downloadLabel ?? text("下载", "Download")}
            onClick={download}
            size="icon"
            variant="ghost"
          >
            <Download size={14} />
          </Button>
        </div>
      </header>
      <div
        className="argus-log-viewer__output"
        ref={outputRef}
        style={{ height }}
      >
        {visible.map((line, index) => (
          <div
            className={cx(
              "argus-log-viewer__line",
              line.level && `is-${line.level}`,
            )}
            key={index}
          >
            {showLineNumbers && (
              <span className="argus-log-viewer__ln">{index + 1}</span>
            )}
            {line.timestamp && <time>{line.timestamp}</time>}
            {line.level && (
              <span className="argus-log-viewer__level">
                {line.level.toUpperCase()}
              </span>
            )}
            <span className="argus-log-viewer__content">{line.content}</span>
          </div>
        ))}
      </div>
    </section>
  );
}
