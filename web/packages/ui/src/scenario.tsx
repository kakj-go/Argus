import type { ReactNode } from "react";

import { cx } from "./lib";
import { Badge } from "./badge";

export type ScenarioLinkMode = "ok" | "blocked" | "tunnel";
export type ScenarioLayout = "pair" | "member";
/**
 * pair 布局:目标主机(左)与 Argus(右)水平排列,连线槽位 top/mid/bottom。
 * member 布局:堡垒机(左上)与成员主机(左下)垂直排列,Argus 居右;
 * 竖线槽位 left(下行)/right(上行),横线槽位 h(堡垒机 → Argus)。
 */
export type ScenarioSlot = "top" | "mid" | "bottom" | "left" | "right" | "h";
export type ScenarioLinkDirection = "both" | "left" | "right" | "up" | "down" | "none";

export type ScenarioLink = {
  mode: ScenarioLinkMode;
  direction: ScenarioLinkDirection;
  label: string;
  slot: ScenarioSlot;
};

export type ScenarioNode = {
  label: string;
  kind?: "host" | "bastion" | "argus";
};

const NODE_W = 104;
const NODE_H = 34;
// 坐标与交互参考稿(docs/planv4/network-scenario-wizard.html)逐像素一致。
const LAYOUTS = {
  pair: {
    nodes: [
      { x: 12, y: 12 },
      { x: 212, y: 12 },
    ],
    hSlots: { top: 26, mid: 62, bottom: 88 },
    height: 112,
  },
  member: {
    nodes: [
      { x: 12, y: 6 },
      { x: 12, y: 80 },
      { x: 212, y: 26 },
    ],
    vSlots: { left: 36, right: 88 },
    hSlot: 30,
    height: 122,
  },
} as const;

const MODE_COLORS = { ok: "var(--success)", blocked: "var(--danger)", tunnel: "var(--warning)" } as const;
const MODE_DASHES = { ok: undefined, blocked: "5 4", tunnel: "5 4" } as const;

/**
 * 网络环境示例小图:绿实线=直连可达、红虚线=阻断、橙虚线=经 SSH 隧道承载。
 * 连线一律从节点边缘起止,标签位于连线上方/右侧,不与节点重叠。
 */
export function TopologyDiagram({
  layout,
  nodes,
  links,
  label,
}: {
  layout: ScenarioLayout;
  nodes: ScenarioNode[];
  links: ScenarioLink[];
  label: string;
}) {
  const geometry = LAYOUTS[layout];
  const geometryNodes = geometry.nodes;
  const geometryHSlots = "hSlots" in geometry ? geometry.hSlots : undefined;
  const geometryVSlots = "vSlots" in geometry ? geometry.vSlots : undefined;
  const geometryHSlot = "hSlot" in geometry ? geometry.hSlot : undefined;
  const line = (x1: number, y1: number, x2: number, y2: number, mode: ScenarioLinkMode, key: number) => (
    <line
      key={`l${key}`}
      stroke={MODE_COLORS[mode]}
      strokeDasharray={MODE_DASHES[mode]}
      strokeWidth={1.6}
      x1={x1}
      x2={x2}
      y1={y1}
      y2={y2}
    />
  );
  return (
    <svg
      aria-label={label}
      className="argus-topology"
      role="img"
      viewBox={`0 0 316 ${geometry.height}`}
    >
      {links.map((link, index) => {
        const color = MODE_COLORS[link.mode];
        if (layout === "pair") {
          const y = geometryHSlots![link.slot as "top" | "mid" | "bottom"];
          const x1 = 122;
          const x2 = 210;
          return (
            <g key={index}>
              {line(x1, y, x2, y, link.mode, index)}
              {(link.direction === "both" || link.direction === "left") && (
                <path d={`M ${x1} ${y} L ${x1 + 6} ${y - 3.5} L ${x1 + 6} ${y + 3.5} Z`} fill={color} />
              )}
              {(link.direction === "both" || link.direction === "right") && (
                <path d={`M ${x2} ${y} L ${x2 - 6} ${y - 3.5} L ${x2 - 6} ${y + 3.5} Z`} fill={color} />
              )}
              <text fill={color} fontSize={9} textAnchor="middle" x={(x1 + x2) / 2} y={y - 6}>
                {link.label}
              </text>
            </g>
          );
        }
        if (link.slot === "h") {
          const y = geometryHSlot!;
          const x1 = 122;
          const x2 = 210;
          return (
            <g key={index}>
              {line(x1, y, x2, y, link.mode, index)}
              {(link.direction === "both" || link.direction === "right") && (
                <path d={`M ${x2} ${y} L ${x2 - 6} ${y - 3.5} L ${x2 - 6} ${y + 3.5} Z`} fill={color} />
              )}
              <text fill={color} fontSize={9} textAnchor="middle" x={(x1 + x2) / 2} y={y - 6}>
                {link.label}
              </text>
            </g>
          );
        }
        const x = geometryVSlots![link.slot as "left" | "right"];
        const y1 = 44;
        const y2 = 76;
        const labelY = link.slot === "left" ? 56 : 72;
        return (
          <g key={index}>
            {line(x, y1, x, y2, link.mode, index)}
            {link.direction === "down" && (
              <path d={`M ${x} ${y2} L ${x - 3.5} ${y2 - 6} L ${x + 3.5} ${y2 - 6} Z`} fill={color} />
            )}
            {link.direction === "up" && (
              <path d={`M ${x} ${y1} L ${x - 3.5} ${y1 + 6} L ${x + 3.5} ${y1 + 6} Z`} fill={color} />
            )}
            <text fill={color} fontSize={9} x={100} y={labelY}>
              {link.label}
            </text>
          </g>
        );
      })}
      {geometryNodes.map((position, index) => {
        const node = nodes[index];
        if (!node) return null;
        return (
          <g key={`${node.label}-${index}`}>
            <rect
              fill="var(--bg-surface)"
              height={NODE_H}
              rx={7}
              stroke={node.kind === "argus" ? "var(--accent)" : "var(--border-strong)"}
              strokeWidth={1.1}
              width={NODE_W}
              x={position.x}
              y={position.y}
            />
            <text
              fill="var(--text-primary)"
              fontSize={11}
              textAnchor="middle"
              x={position.x + NODE_W / 2}
              y={position.y + 21}
            >
              {node.label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

/**
 * 网络环境场景卡:向导第一步的选择单元。选中态由受控 selected 驱动;
 * 规划中的场景以 disabled + badge 呈现,可查看但不可提交。
 */
export function ScenarioCard({
  title,
  refLabel,
  description,
  selected,
  onSelect,
  status = "supported",
  statusLabel,
  diagram,
  footer,
}: {
  title: string;
  /** 讨论场景编号徽章,如「场景 ①」;缺省不渲染。 */
  refLabel?: string;
  description: string;
  selected: boolean;
  onSelect: () => void;
  status?: "supported" | "planned" | "unavailable";
  statusLabel?: string;
  diagram: ReactNode;
  footer?: ReactNode;
}) {
  const tone =
    status === "supported" ? "success" : status === "planned" ? "warning" : "danger";
  return (
    <button
      aria-pressed={selected}
      className={cx("argus-scenario-card", selected && "is-selected")}
      disabled={status !== "supported"}
      onClick={onSelect}
      type="button"
    >
      <span className="argus-scenario-card__head">
        <span className="argus-scenario-card__title">{title}</span>
        {refLabel && <span className="argus-scenario-card__ref">{refLabel}</span>}
        {statusLabel && (
          <Badge className="argus-scenario-card__badge" tone={tone}>
            {statusLabel}
          </Badge>
        )}
      </span>
      <span className="argus-scenario-card__desc">{description}</span>
      {diagram}
      {footer}
    </button>
  );
}

export type ModeGridItem = {
  label: string;
  value: string;
  tone?: "ok" | "warn" | "bad" | "info";
};

/** 模式解读五格:接入方式/安装与配置/指标回传/远程终端/在线检测。 */
export function ModeGrid({ items }: { items: ModeGridItem[] }) {
  return (
    <div className="argus-mode-grid">
      {items.map((item) => (
        <div className="argus-mode-grid__cell" key={item.label}>
          <span className="argus-mode-grid__label">
            <i
              aria-hidden
              className={`argus-mode-grid__dot argus-mode-grid__dot--${item.tone ?? "info"}`}
            />
            {item.label}
          </span>
          <span className="argus-mode-grid__value">{item.value}</span>
        </div>
      ))}
    </div>
  );
}
