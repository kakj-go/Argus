import type { PendingActionPublic } from "@argus/api-client";

type Translate = (key: string, options?: Record<string, unknown>) => string;

export type PresentedPendingAction = {
  title: string;
  summary: string;
  riskLabel: string;
  diff: PendingActionPublic["diff"];
};

function displayName(action: PendingActionPublic, fallback: string): string {
  const preview = (action.preview ?? {}) as Record<string, unknown>;
  const name = preview["name"];
  if (typeof name === "string" && name.trim()) return name;
  const match = action.title.match(/(?:host|cluster|主机|集群)\s+(.+)$/i);
  if (match?.[1]?.trim()) return match[1].trim();
  const diffMatch = action.diff
    .map((line) => line.text)
    .join(" ")
    .match(/(?:host|cluster|主机|集群)(?:\s+resource)?\s+(.+?)(?:$|\s)/i);
  return diffMatch?.[1]?.trim() || fallback;
}

function isHostCreate(action: PendingActionPublic): boolean {
  const preview = (action.preview ?? {}) as Record<string, unknown>;
  return (
    /(?:create|add)\s+host|(?:新增|创建)主机|主机资源|validated host/i.test(
      `${action.title} ${action.summary}`,
    ) ||
    (typeof preview["address"] === "string" &&
      preview["port"] !== undefined &&
      !/(?:update|edit|delete|更新|编辑|删除)\s*主机?/i.test(action.title))
  );
}

function isKubernetesCreate(action: PendingActionPublic): boolean {
  const preview = (action.preview ?? {}) as Record<string, unknown>;
  return (
    typeof preview["api_server"] === "string" ||
    typeof preview["apiServer"] === "string" ||
    /(?:create|创建)\s+(?:kubernetes\s+)?cluster|集群资源/i.test(`${action.title} ${action.summary}`)
  );
}

export function presentPendingAction(action: PendingActionPublic, t: Translate): PresentedPendingAction {
  const name = displayName(action, t("common.unknown"));
  const kubernetesCreate = isKubernetesCreate(action);
  const hostCreate = !kubernetesCreate && isHostCreate(action);
  const riskLabel = t(`governance.approvals.risk.${action.risk}`);

  if (hostCreate) {
    return {
      title: t("hosts.preview.createTitle", { name }),
      summary: t("hosts.preview.createSummary"),
      riskLabel,
      diff: action.diff.map((line, index) => ({
        ...line,
        text: index === 0 ? t("hosts.preview.createResourceDiff", { name }) : index === 1 ? t("hosts.preview.collectorDiff") : line.text,
      })),
    };
  }

  if (kubernetesCreate) {
    return {
      title: t("kubernetes.pendingAction.createTitle", { name }),
      summary: t("kubernetes.pendingAction.createSummary"),
      riskLabel,
      diff: action.diff.map((line, index) => ({
        ...line,
        text: index === 0 ? t("kubernetes.pendingAction.createResourceDiff", { name }) : line.text,
      })),
    };
  }

  return { title: action.title, summary: action.summary, riskLabel, diff: action.diff };
}
