/** 时间格式化与确定性演示数据。 */

const DAY_MS = 24 * 3600_000;

/** 统一的日期时间展示；locale 来自 i18n 当前语言。 */
export function formatDateTime(iso: string | undefined, locale: string): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return new Intl.DateTimeFormat(locale === "en-US" ? "en-US" : "zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export type UsagePoint = {
  label: string;
  sessions: number;
  sessionMinutes: number;
  cpuMinutes: number;
};

/**
 * 近 14 天平台用量序列：mock 环境没有平台级用量 API，
 * 这里用与种子数据相同的确定性公式生成，保证每次渲染一致。
 */
export function platformUsageSeries(days = 14): UsagePoint[] {
  const now = Date.now();
  return Array.from({ length: days }, (_, index) => {
    const day = days - 1 - index;
    const date = new Date(now - day * DAY_MS);
    return {
      label: `${date.getMonth() + 1}/${date.getDate()}`,
      sessions: 3 + (day % 4),
      sessionMinutes: 45 + day * 6,
      cpuMinutes: 90 + day * 11,
    };
  });
}
