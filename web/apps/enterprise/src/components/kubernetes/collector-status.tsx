import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type CollectorInstance,
  type KubernetesCluster,
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
  cluster: KubernetesCluster;
  collector: CollectorInstance;
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
  const profilesQuery = useQuery({
    queryKey: ["telemetry", "profiles"],
    queryFn: () => api.telemetry.listProfiles(),
  });

  const [profiles, setProfiles] = useState(() =>
    initialProfiles("k8s-daemonset"),
  );
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [routeTested, setRouteTested] = useState(false);

  const baseline = useMemo(
    () => initialProfiles("k8s-daemonset"),
    [],
  );

  const diffLines = useMemo(() => {
    const lines: Array<{
      type: "add" | "remove" | "context";
      content: string;
    }> = [
      {
        type: "context",
        content: `distribution: ${collector.distribution_version_id}`,
      },
    ];
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
  }, [profiles, baseline, collector.distribution_version_id, t]);

  const changed = diffLines.length > 1;

  const previewChange = useMutation({
    mutationFn: () => {
      const keys = toProfileString(profiles).split(",");
      const catalogKeys = new Set<string>();
      if (keys.includes("k8s-daemonset") || keys.includes("node-metrics")) {
        catalogKeys.add("k8s-node-container");
      }
      if (keys.includes("cluster-traces")) {
        catalogKeys.add("k8s-otlp-gateway");
      }
      const profileIds = (profilesQuery.data ?? [])
        .filter((profile) => catalogKeys.has(profile.key))
        .map((profile) => profile.id);
      return api.kubernetes.previewCollectorAction(cluster.id, "configure", {
        distribution_version_id: collector.distribution_version_id,
        profile_ids: profileIds,
        route_kind: collector.route?.kind ?? "direct_argus",
        gateway_collector_id: collector.route?.gateway_collector_id,
        expected_version: collector.version,
      });
    },
    onSuccess: setPendingAction,
  });

  const previewLifecycle = useMutation({
    mutationFn: (action: "upgrade" | "repair" | "uninstall") => {
      const profileIds = [...new Set((claimsQuery.data ?? [])
        .filter((claim) => claim.collector_id === collector.id && claim.status === "active")
        .map((claim) => claim.profile_id)
        .filter((id): id is string => Boolean(id)))];
      return api.kubernetes.previewCollectorAction(cluster.id, action, {
        distribution_version_id: collector.distribution_version_id,
        profile_ids: profileIds,
        route_kind: collector.route?.kind ?? "direct_argus",
        gateway_collector_id: collector.route?.gateway_collector_id,
        expected_version: collector.version,
      });
    },
    onSuccess: setPendingAction,
  });

  const testRoute = useMutation({
    mutationFn: () => api.telemetry.testRoute({
      collector_id: collector.id,
      route_kind: collector.route?.kind ?? "direct_argus",
      gateway_collector_id: collector.route?.gateway_collector_id,
    }),
    onSuccess: (result) => setRouteTested(result.status === "succeeded"),
  });

  const coverage = bindingCoverage(cluster, bindingsQuery.data);
  const conflicts = (claimsQuery.data ?? []).filter(
    (claim) => claim.status === "conflict",
  );
  const egressOk = cluster.connection_status === "connected";

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
          value={collector.platform}
        />
      </div>

      <Card>
        <CardHeader title={t("kubernetes.collector.status.readyReplicas")} />
        <CardContent>
          <Progress
            label={collector.status === "converged" ? "100%" : "60%"}
            tone={collector.status === "converged" ? "success" : "accent"}
            value={collector.status === "converged" ? 100 : 60}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader title={t("kubernetes.collector.status.lifecycleTitle")} />
        <CardContent>
          {pendingAction ? (
            <PendingActionCard action={pendingAction} onSettled={() => {
              setPendingAction(null);
              void queryClient.invalidateQueries({ queryKey: ["kubernetes"] });
            }} />
          ) : (
            <div className="argus-form-actions">
              <Button loading={testRoute.isPending} onClick={() => testRoute.mutate()} variant="secondary">
                {t("kubernetes.collector.status.testRoute")}
              </Button>
              {routeTested && <StatusBadge tone="success">{t("kubernetes.collector.status.routeOk")}</StatusBadge>}
              <Button loading={previewLifecycle.isPending} onClick={() => previewLifecycle.mutate("upgrade")} variant="secondary">{t("kubernetes.collector.status.upgrade")}</Button>
              <Button loading={previewLifecycle.isPending} onClick={() => previewLifecycle.mutate("repair")} variant="secondary">{t("kubernetes.collector.status.repair")}</Button>
              <Button loading={previewLifecycle.isPending} onClick={() => previewLifecycle.mutate("uninstall")} variant="danger">{t("kubernetes.collector.status.uninstall")}</Button>
            </div>
          )}
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
                value: `${collector.effective_revision} → ${collector.desired_revision}`,
              },
              {
                label: t("kubernetes.collector.wizard.profile"),
                value: collector.distribution_version_id,
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
                    {claim.profile_id ?? claim.claim_type} · {claim.signal} ·{" "}
                    {claim.physical_resource_ref}
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
