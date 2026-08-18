import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pause, Play, RotateCcw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useApi, type RecordingEventPage } from "@argus/api-client";
import { Button, Card, CardContent, CardHeader, Select, TerminalEmulator, type TerminalLine } from "@argus/ui";

type RecordingEvent = RecordingEventPage["events"][number];

async function loadRecording(api: ReturnType<typeof useApi>, recordingId: string): Promise<RecordingEventPage> {
  const events: RecordingEvent[] = [];
  let cursor: string | undefined;
  let page: RecordingEventPage;
  do {
    page = await api.remoteAccess.listRecordingEvents(recordingId, cursor);
    events.push(...page.events);
    cursor = page.complete ? undefined : page.next_cursor;
  } while (cursor);
  return { ...page, events };
}

export function RemoteRecordingPlayer({ recordingId, host, protocol, onClose }: { recordingId: string; host: string; protocol: "ssh" | "winrs"; onClose(): void }) {
  const api = useApi();
  const { t } = useTranslation();
  const recording = useQuery({ queryKey: ["remote-access", "recording", recordingId], queryFn: () => loadRecording(api, recordingId) });
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [position, setPosition] = useState(0);
  const events = useMemo(() => recording.data?.events ?? [], [recording.data?.events]);
  const duration = events.at(-1)?.time ?? 0;

  useEffect(() => {
    if (!playing || position >= duration) return;
    const started = performance.now();
    const initial = position;
    const timer = window.setInterval(() => {
      const next = Math.min(duration, initial + ((performance.now() - started) / 1000) * speed);
      setPosition(next);
      if (next >= duration) setPlaying(false);
    }, 50);
    return () => window.clearInterval(timer);
  }, [duration, playing, position, speed]);

  const lines = useMemo<TerminalLine[]>(() => events.filter((event) => event.time <= position && (event.type === "i" || event.type === "o")).map((event) => ({
    kind: event.type === "i" ? "stdin" : "stdout",
    content: typeof event.data === "string" ? event.data : JSON.stringify(event.data),
    time: `${event.time.toFixed(1)}s`,
  })), [events, position]);

  return <Card>
    <CardHeader action={<Button onClick={onClose} size="sm" variant="ghost">{t("remoteAccess.close")}</Button>} title={t("remoteAccess.recordingTitle", { host })} />
    <CardContent>
      {recording.isError && <p role="alert">{t("remoteAccess.recordingUnavailable")}</p>}
      {!recording.isLoading && !recording.isError && events.length === 0 && <p>{t("remoteAccess.recordingEmpty")}</p>}
      <div className="argus-recording-controls">
        <Button aria-label={t(playing ? "remoteAccess.pause" : "remoteAccess.play")} onClick={() => setPlaying((value) => !value)} size="icon" variant="secondary">{playing ? <Pause size={16} /> : <Play size={16} />}</Button>
        <Button aria-label={t("remoteAccess.restart")} onClick={() => { setPlaying(false); setPosition(0); }} size="icon" variant="ghost"><RotateCcw size={16} /></Button>
        <input aria-label={t("remoteAccess.position")} max={Math.max(duration, 0.1)} min="0" onChange={(event) => setPosition(Number(event.target.value))} step="0.1" type="range" value={position} />
        <span>{position.toFixed(1)}s / {duration.toFixed(1)}s</span>
        <Select ariaLabel={t("remoteAccess.speed")} onValueChange={(value) => setSpeed(Number(value))} options={[0.5, 1, 1.5, 2].map((value) => ({ value: String(value), label: `${value}x` }))} value={String(speed)} />
      </div>
      <TerminalEmulator host={host} lines={lines} mode={protocol === "ssh" ? "pty" : "line"} protocol={protocol === "ssh" ? "SSH PTY recording" : "WinRS PowerShell recording"} readOnly state="disconnected" />
    </CardContent>
  </Card>;
}
