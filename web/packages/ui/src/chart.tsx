import {
  type MouseEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { cx } from "./lib";

export type MetricChartType = "line" | "area" | "bar";

export type MetricChartSeries = {
  name: string;
  points: number[];
  /** Optional CSS color override; defaults to the chart palette. */
  color?: string;
};

const PALETTE = [
  "var(--accent)",
  "var(--info)",
  "var(--success)",
  "var(--warning)",
  "var(--danger)",
];

const PAD = { top: 10, right: 10, bottom: 24, left: 46 };
const TICKS = 5;

function defaultFormat(value: number): string {
  return String(Math.round(value * 100) / 100);
}

export function MetricChart({
  type = "line",
  series,
  labels = [],
  height = 220,
  formatValue = defaultFormat,
  showLegend,
  className,
}: {
  type?: MetricChartType;
  series: MetricChartSeries[];
  labels?: string[];
  height?: number;
  formatValue?: (value: number) => string;
  showLegend?: boolean;
  className?: string;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);

  useEffect(() => {
    const element = wrapRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver((entries) => {
      const next = entries[0]?.contentRect.width;
      if (next && next > 0) setWidth(next);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const count = Math.max(
    labels.length,
    ...series.map((item) => item.points.length),
    0,
  );
  const allValues = series.flatMap((item) => item.points);

  const plotW = Math.max(1, width - PAD.left - PAD.right);
  const plotH = Math.max(1, height - PAD.top - PAD.bottom);

  let yMin = allValues.length > 0 ? Math.min(0, ...allValues) : 0;
  let yMax = allValues.length > 0 ? Math.max(...allValues) : 1;
  if (yMax === yMin) yMax = yMin + 1;
  const headroom = (yMax - yMin) * 0.06;
  if (type !== "bar" || yMin < 0) yMin -= type === "bar" ? 0 : headroom;
  yMax += headroom;

  const xAt = (index: number) =>
    PAD.left + (count <= 1 ? plotW / 2 : (plotW * index) / (count - 1));
  const yAt = (value: number) =>
    PAD.top + plotH - ((value - yMin) / (yMax - yMin)) * plotH;

  const ticks = Array.from(
    { length: TICKS },
    (_, index) => yMin + ((yMax - yMin) * index) / (TICKS - 1),
  );

  const colorOf = (item: MetricChartSeries, index: number) =>
    item.color ?? PALETTE[index % PALETTE.length]!;

  const onMove = (event: MouseEvent<SVGSVGElement>) => {
    if (count === 0) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const relative = event.clientX - rect.left - PAD.left;
    const index = Math.round(
      (relative / plotW) * (count <= 1 ? 0 : count - 1),
    );
    setHoverIndex(Math.max(0, Math.min(count - 1, index)));
  };

  const xLabelIndexes = Array.from({ length: Math.min(count, 6) }, (_, i) =>
    Math.round((i * (count - 1)) / Math.max(1, Math.min(count, 6) - 1)),
  ).filter((value, index, arr) => arr.indexOf(value) === index);

  const zeroY = yAt(Math.max(yMin, Math.min(yMax, 0)));

  return (
    <div className={cx("argus-chart", className)} ref={wrapRef}>
      {count === 0 ? (
        <div className="argus-chart__empty" style={{ height }} />
      ) : (
        <div className="argus-chart__plot">
          <svg
            height={height}
            onMouseLeave={() => setHoverIndex(null)}
            onMouseMove={onMove}
            role="img"
            width={width}
          >
            {ticks.map((tick, index) => (
              <g key={index}>
                <line
                  className="argus-chart__grid"
                  x1={PAD.left}
                  x2={width - PAD.right}
                  y1={yAt(tick)}
                  y2={yAt(tick)}
                />
                <text
                  className="argus-chart__tick"
                  textAnchor="end"
                  x={PAD.left - 7}
                  y={yAt(tick) + 3}
                >
                  {formatValue(tick)}
                </text>
              </g>
            ))}
            {xLabelIndexes.map((index) => (
              <text
                className="argus-chart__tick"
                key={index}
                textAnchor="middle"
                x={xAt(index)}
                y={height - 7}
              >
                {labels[index] ?? index + 1}
              </text>
            ))}

            {series.map((item, seriesIndex) => {
              const color = colorOf(item, seriesIndex);
              if (type === "bar") {
                const groupW = plotW / count;
                const barW = Math.max(
                  2,
                  (groupW / series.length) * 0.72,
                );
                const gap = (groupW / series.length) * 0.14;
                const blockW =
                  series.length * barW + (series.length - 1) * gap;
                return (
                  <g key={item.name}>
                    {item.points.map((value, pointIndex) => {
                      const x =
                        PAD.left +
                        pointIndex * groupW +
                        groupW / 2 -
                        blockW / 2 +
                        seriesIndex * (barW + gap);
                      const y = yAt(Math.max(0, value));
                      const barHeight = Math.abs(yAt(value) - zeroY);
                      return (
                        <rect
                          className={cx(
                            "argus-chart__bar",
                            hoverIndex === pointIndex && "is-hovered",
                          )}
                          fill={color}
                          height={barHeight}
                          key={pointIndex}
                          rx={2}
                          width={barW}
                          x={x}
                          y={y}
                        />
                      );
                    })}
                  </g>
                );
              }
              const coords = item.points
                .map((value, pointIndex) => `${xAt(pointIndex)},${yAt(value)}`)
                .join(" ");
              const linePath = `M ${coords.replaceAll(" ", " L ")}`;
              const lastX = xAt(item.points.length - 1);
              const firstX = xAt(0);
              return (
                <g key={item.name}>
                  {type === "area" && (
                    <path
                      className="argus-chart__area"
                      d={`${linePath} L ${lastX},${yAt(Math.max(yMin, 0))} L ${firstX},${yAt(Math.max(yMin, 0))} Z`}
                      fill={color}
                    />
                  )}
                  <path
                    className="argus-chart__line"
                    d={linePath}
                    fill="none"
                    stroke={color}
                  />
                  {hoverIndex !== null &&
                    item.points[hoverIndex] !== undefined && (
                      <circle
                        className="argus-chart__dot"
                        cx={xAt(hoverIndex)}
                        cy={yAt(item.points[hoverIndex]!)}
                        fill={color}
                        r={3.5}
                      />
                    )}
                </g>
              );
            })}

            {hoverIndex !== null && (
              <line
                className="argus-chart__crosshair"
                x1={xAt(hoverIndex)}
                x2={xAt(hoverIndex)}
                y1={PAD.top}
                y2={height - PAD.bottom}
              />
            )}
          </svg>

          {hoverIndex !== null && (
            <div
              className="argus-chart__tooltip"
              style={{
                left: Math.min(
                  Math.max(xAt(hoverIndex), 60),
                  Math.max(60, width - 60),
                ),
              }}
            >
              <b>{labels[hoverIndex] ?? `#${hoverIndex + 1}`}</b>
              {series.map((item, seriesIndex) => (
                <span key={item.name}>
                  <i style={{ background: colorOf(item, seriesIndex) }} />
                  {item.name}
                  <em>{formatValue(item.points[hoverIndex] ?? 0)}</em>
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      {showLegend && series.length > 0 && (
        <div className="argus-chart__legend">
          {series.map((item, index) => (
            <span key={item.name}>
              <i style={{ background: colorOf(item, index) }} />
              {item.name}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
