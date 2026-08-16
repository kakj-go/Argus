import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type CollectorInstallState,
  type K8sCluster,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  DiffViewer,
  KeyValueGrid,
  Progress,
  StatCard,
  StatusBadge,
  Switch,
} from "@argus/ui";
import { PendingActionCard } from "./pending-action-card";
import { bindingCoverage, collectorStatusTone } from "./status";

type ProfileKey = "nodeMetrics" | "containerLogs" | "clusterTraces";

const PROFILE_TOKEN: Record<ProfileKey, string> = {
  nodeMetrics: "node-metrics",
  containerLogs: "container-logs",
  clusterTraces: "cluster-traces",
};
const PROFILE_KEYS: ProfileKey[] = [
  "nodeMetrics",
  "containerLogs",
  "clusterTraces",
];

/** 依据当前 profile 推导开关初始态：DaemonSet 默认负责节点/容器指标与容器日志。 */
function initialProfiles(profile: string): Record<ProfileKey, boolean> {
  const tokens = profile.split(",").map((token) => token.trim());
  return {
    nodeMetrics:
      tokens.includes(PROFILE_TOKEN.nodeMetrics) ||
      tokens.includes("k8s-daemonset"),
    containerLogs:
      tokens.includes(PROFILE_TOKEN.containerLogs) ||
      tokens.includes("k8s-daemonset"),
    clusterTraces: tokens.includes(PROFILE_TOKEN.clusterTraces),
  };
}

function toProfileString(enabled: Record<ProfileKey, boolean>): string {
  const tokens = ["k8s-daemonset"];
  for (const key of PROFILE_KEYS) {
    if (enabled[key]) tokens.push(PROFILE_TOKEN[key]);
  }
  return tokens.join(",");
}

/** 已安装 OTLP 收集器：状态面板（副本/覆盖率/冲突/出口）+ Profile 开关配置。 */
export function CollectorStatusPanel({
  cluster,
  collector,
}: {
  cluster: K8sCluster;
  collector: CollectorInstallState;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const bindingsQuery = useQuery({
    queryKey: ["kubernetes", "nodeBindings", cluster.id],
    queryFn: () => api.kubernetes.listNodeBindings(cluster.id),
  });
  const claimsQuery = useQuery({
    queryKey: ["kubernetes", "collectionClaims", cluster.id],
    queryFn: () => api.kubernetes.listCollectionClaims(cluster.id),
  });

  const [profiles, setProfiles] = useState(() =>
    initialProfiles(collector.profile),
  );
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);

  const baseline = useMemo(
    () => initialProfiles(collector.profile),
    [collector.profile],
  );

  const diffLines = useMemo(() => {
    const lines: Array<{
      type: "add" | "remove" | "context";
      content: string;
    }> = [{ type: "context", content: `profile: ${collector.profile}` }];
    for (const key of PROFILE_KEYS) {
      if (profiles[key] === baseline[key]) continue;
      lines.push({
        type: profiles[key] ? "add" : "remove",
        content: `${t(`kubernetes.collector.status.profiles.${key}`)}: ${
          profiles[key] ? "on" : "off"
        }`,
      });
    }
    return lines;
  }, [profiles, baseline, collector.profile, t]);

  const changed = diffLines.length > 1;

  const previewChange = useMutation({
    mutationFn: () =>
      api.kubernetes.previewCollectorInstall(cluster.id, {
        profile: toProfileString(profiles),
      }),
    onSuccess: setPendingAction,
  });

  const coverage = bindingCoverage(cluster, bindingsQuery.data);
  const conflicts = (claimsQuery.data ?? []).filter(
    (claim) => claim.status === "conflict",
  );
  const egressOk = cluster.connectionStatus === "connected";

  return (
    <div className="argus-k8s-stack">
      <div className="argus-k8s-stat-grid">
        <StatCard
          label={t("kubernetes.collector.status.state")}
          value={
            <StatusBadge tone={collectorStatusTone(collector.status)}>
              {t(`kubernetes.collectorStatus.${collector.status}`)}
            </StatusBadge>
          }
        />
        <StatCard
          detail={`${coverage.verified}/${coverage.total}`}
          label={t("kubernetes.collector.status.bindingCoverage")}
          tone={coverage.percent >= 100 ? "success" : "warning"}
          value={`${coverage.percent}%`}
        />
        <StatCard
          label={t("kubernetes.collector.status.claimConflicts")}
          tone={conflicts.length > 0 ? "danger" : "success"}
          value={
            conflicts.length > 0
              ? String(conflicts.length)
              : t("kubernetes.collector.status.noConflicts")
          }
        />
        <StatCard
          label={t("kubernetes.collector.status.version")}
          value={collector.version}
        />
      </div>

      <Card>
        <CardHeader title={t("kubernetes.collector.status.readyReplicas")} />
        <CardContent>
          <Progress
            label={`${collector.progress}%`}
            tone={collector.progress >= 100 ? "success" : "accent"}
            value={collector.progress}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader title={t("kubernetes.detail.collectorTab")} />
        <CardContent>
          <KeyValueGrid
            columns={2}
            items={[
              {
                label: t("kubernetes.collector.status.revision"),
                value: `${collector.effectiveRevision} → ${collector.desiredRevision}`,
              },
              {
                label: t("kubernetes.collector.wizard.profile"),
                value: collector.profile,
              },
              {
                label: t("kubernetes.collector.status.imageDigest"),
                value: t("kubernetes.collector.wizard.imageDigestValue"),
              },
              {
                label: t("kubernetes.collector.status.egress"),
                value: (
                  <StatusBadge tone={egressOk ? "success" : "warning"}>
                    {egressOk
                      ? t("kubernetes.collector.status.egressOk")
                      : t("kubernetes.collector.status.egressDegraded")}
                  </StatusBadge>
                ),
              },
            ]}
          />
        </CardContent>
      </Card>

      {conflicts.length > 0 && (
        <Card>
          <CardHeader title={t("kubernetes.collector.status.claimConflicts")} />
          <CardContent>
            <ul className="argus-k8s-events">
              {conflicts.map((claim) => (
                <li key={claim.id}>
                  <StatusBadge tone="warning">
                    {t("kubernetes.claimState.closeHostProfile")}
                  </StatusBadge>
                  <span>
                    {claim.profile} · {claim.signal} ·{" "}
                    {t("kubernetes.collector.wizard.conflictWith")}:{" "}
                    {claim.conflictWithId ?? "—"}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader
          description={t("kubernetes.collector.status.configHint")}
          title={t("kubernetes.collector.status.configTitle")}
        />
        <CardContent>
          <div className="argus-k8s-stack">
            <div className="argus-k8s-profile-switches">
              {PROFILE_KEYS.map((key) => (
                <div className="argus-k8s-profile-switches__row" key={key}>
                  <span>
                    {t(`kubernetes.collector.status.profiles.${key}`)}
                  </span>
                  <Switch
                    checked={profiles[key]}
                    label={t(`kubernetes.collector.status.profiles.${key}`)}
                    onChange={(checked) =>
                      setProfiles((current) => ({
                        ...current,
                        [key]: checked,
                      }))
                    }
                  />
                </div>
              ))}
            </div>
            <div>
              <h3 className="argus-k8s-section-title">
                {t("kubernetes.collector.status.diffTitle")}
              </h3>
              {changed ? (
                <DiffViewer lines={diffLines} />
              ) : (
                <p className="argus-k8s-section-title">
                  {t("kubernetes.collector.status.noChange")}
                </p>
              )}
            </div>
            {pendingAction ? (
              <PendingActionCard
                action={pendingAction}
                onSettled={() => {
                  setPendingAction(null);
                  void queryClient.invalidateQueries({
                    queryKey: ["kubernetes"],
                  });
                }}
              />
            ) : (
              <div>
                <Button
                  disabled={!changed}
                  loading={previewChange.isPending}
                  onClick={() => previewChange.mutate()}
                  variant="primary"
                >
                  {t("kubernetes.collector.status.previewChange")}
                </Button>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
