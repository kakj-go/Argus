import { useEffect, useMemo, useRef } from "react";
import { LineChart } from "echarts/charts";
import {
  AriaComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { cx } from "./lib";
import { useLocale, useUiText } from "./locale";

echarts.use([
  AriaComponent,
  CanvasRenderer,
  GridComponent,
  LegendComponent,
  LineChart,
  TooltipComponent,
]);

export type TelemetrySeries = {
  name: string;
  unit?: string;
  points: Array<{ timestamp: string; value: number }>;
};

export function TelemetryTimeSeries({
  ariaLabel,
  className,
  height = 280,
  series,
}: {
  ariaLabel: string;
  className?: string;
  height?: number;
  series: TelemetrySeries[];
}) {
  const root = useRef<HTMLDivElement>(null);
  const text = useUiText();
  const { locale } = useLocale();

  useEffect(() => {
    if (!root.current) return;
    const chart = echarts.init(root.current);
    const styles = getComputedStyle(root.current);
    const color = (token: string) => styles.getPropertyValue(token).trim();
    chart.setOption({
      animationDuration: 180,
      aria: { enabled: true, description: ariaLabel },
      grid: { left: 52, right: 18, top: 28, bottom: 42 },
      color: [
        color("--accent"),
        color("--info"),
        color("--success"),
        color("--warning"),
        color("--danger"),
      ],
      legend: { top: 0, textStyle: { color: color("--text-tertiary") } },
      tooltip: { trigger: "axis" },
      xAxis: { type: "time", axisLabel: { hideOverlap: true } },
      yAxis: { type: "value", scale: true },
      series: series.map((item) => ({
        name: item.name,
        type: "line",
        showSymbol: false,
        sampling: "lttb",
        data: item.points.map((point) => [point.timestamp, point.value]),
      })),
    });
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(root.current);
    return () => {
      observer.disconnect();
      chart.dispose();
    };
  }, [ariaLabel, series]);

  const rows = useMemo(
    () =>
      series.flatMap((item) =>
        item.points.map((point) => ({
          ...point,
          name: item.name,
          unit: item.unit,
        })),
      ),
    [series],
  );

  return (
    <div className={cx("argus-telemetry-chart", className)}>
      <div aria-label={ariaLabel} ref={root} role="img" style={{ height }} />
      <details className="argus-telemetry-alternative">
        <summary>{text("查看数据表", "View data table")}</summary>
        <div className="argus-table-wrap" tabIndex={0}>
          <table className="argus-table">
            <thead>
              <tr>
                <th>{text("序列", "Series")}</th>
                <th>{text("时间", "Time")}</th>
                <th>{text("值", "Value")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => (
                <tr key={`${row.name}:${row.timestamp}:${index}`}>
                  <td>{row.name}</td>
                  <td>{new Date(row.timestamp).toLocaleString(locale)}</td>
                  <td>
                    {row.value}
                    {row.unit ? ` ${row.unit}` : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  );
}

export type TelemetryLogRow = {
  timestamp: string;
  resource_id: string;
  severity: string;
  service_name?: string;
  body: string;
  trace_id?: string;
};

export function TelemetryLogTable({ rows }: { rows: TelemetryLogRow[] }) {
  const text = useUiText();
  const { locale } = useLocale();
  return (
    <div
      aria-label={text(`${rows.length} 条日志`, `${rows.length} log records`)}
      className="argus-table-wrap"
      role="region"
      tabIndex={0}
    >
      <table className="argus-table argus-telemetry-log-table">
        <thead>
          <tr>
            <th>{text("时间", "Time")}</th>
            <th>{text("级别", "Severity")}</th>
            <th>{text("服务", "Service")}</th>
            <th>{text("正文", "Message")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={`${row.resource_id}:${row.timestamp}:${index}`}>
              <td className="argus-mono">
                {new Date(row.timestamp).toLocaleString(locale)}
              </td>
              <td>{row.severity}</td>
              <td>{row.service_name ?? "-"}</td>
              <td className="argus-telemetry-log-body">{row.body}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export type TelemetryTraceRow = {
  trace_id: string;
  service_name: string;
  root_span_name: string;
  started_at: string;
  duration_ms: number;
  span_count: number;
  status: string;
};

export function TelemetryTraceTimeline({
  rows,
}: {
  rows: TelemetryTraceRow[];
}) {
  const text = useUiText();
  const { locale } = useLocale();
  const maxDuration = Math.max(1, ...rows.map((row) => row.duration_ms));
  const statusLabel = (status: string) => {
    switch (status.toLowerCase()) {
      case "ok":
      case "success":
        return text("正常", "OK");
      case "error":
      case "failed":
        return text("错误", "Error");
      case "unset":
        return text("未设置", "Unset");
      default:
        return text("未知", "Unknown");
    }
  };
  return (
    <ol
      aria-label={text(`${rows.length} 条 Trace`, `${rows.length} traces`)}
      className="argus-telemetry-traces"
    >
      {rows.map((row) => (
        <li className="argus-telemetry-trace" key={row.trace_id} tabIndex={0}>
          <div className="argus-telemetry-trace__head">
            <b>
              {row.service_name} / {row.root_span_name}
            </b>
            <span>{Math.round(row.duration_ms)} ms</span>
          </div>
          <div
            aria-hidden
            className="argus-telemetry-trace__bar"
            style={{
              width: `${Math.max(3, (row.duration_ms / maxDuration) * 100)}%`,
            }}
          />
          <small>
            {new Date(row.started_at).toLocaleString(locale)} ·{" "}
            {text(`${row.span_count} 个 Span`, `${row.span_count} spans`)} ·{" "}
            {statusLabel(row.status)}
          </small>
        </li>
      ))}
    </ol>
  );
}
