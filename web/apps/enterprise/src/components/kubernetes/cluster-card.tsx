import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type ConnectionTestResult,
  type K8sCluster,
} from "@argus/api-client";
import { Badge, Button, Card, CardContent, StatusBadge } from "@argus/ui";
import {
  bindingCoverage,
  collectorStatusTone,
  connectionStatusTone,
} from "./status";

/** 单个集群卡片：概览指标、Collector/绑定徽标、连接测试与卡片操作。 */
export function ClusterCard({
  cluster,
  onOpen,
  onEdit,
  onDelete,
  onInstallCollector,
  onOpenCollector,
}: {
  cluster: K8sCluster;
  onOpen: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onInstallCollector: () => void;
  onOpenCollector: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(
    null,
  );

  const collectorQuery = useQuery({
    queryKey: ["kubernetes", "collector", cluster.id],
    queryFn: () => api.kubernetes.getCollector(cluster.id),
  });
  const bindingsQuery = useQuery({
    queryKey: ["kubernetes", "nodeBindings", cluster.id],
    queryFn: () => api.kubernetes.listNodeBindings(cluster.id),
  });

  const test = useMutation({
    mutationFn: () => api.kubernetes.testClusterConnection(cluster.id),
    onSuccess: setTestResult,
  });

  const collectorStatus = collectorQuery.data?.status ?? "not_installed";
  const coverage = bindingCoverage(cluster, bindingsQuery.data);

  return (
    <Card>
      <CardContent>
        <div className="argus-k8s-cluster-card__head">
          <span className="argus-k8s-cluster-card__name">{cluster.name}</span>
          <Badge tone="accent">
            {t(`kubernetes.environment.${cluster.environment}`)}
          </Badge>
          <StatusBadge tone={connectionStatusTone(cluster.connectionStatus)}>
            {t(`kubernetes.status.${cluster.connectionStatus}`)}
          </StatusBadge>
        </div>
        <div className="argus-k8s-cluster-card__server">
          {cluster.apiServer}
        </div>
        <dl className="argus-k8s-kv">
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.version")}</dt>
            <dd>{cluster.version}</dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.nodes")}</dt>
            <dd>
              {cluster.readyNodeCount}/{cluster.nodeCount}
            </dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.connectionMode")}</dt>
            <dd>{t(`kubernetes.mode.${cluster.connectionMode}`)}</dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.collector")}</dt>
            <dd>
              <button
                aria-label={t(
                  collectorStatus === "not_installed"
                    ? "kubernetes.card.installCollector"
                    : "kubernetes.card.openCollector",
                  { name: cluster.name },
                )}
                className="argus-k8s-collector-action"
                onClick={
                  collectorStatus === "not_installed"
                    ? onInstallCollector
                    : onOpenCollector
                }
                type="button"
              >
                <StatusBadge tone={collectorStatusTone(collectorStatus)}>
                  {t(`kubernetes.collectorStatus.${collectorStatus}`)}
                </StatusBadge>
              </button>
            </dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.bindingCoverage")}</dt>
            <dd>
              {coverage.verified}/{coverage.total} ({coverage.percent}%)
            </dd>
          </div>
        </dl>
        {testResult && (
          <div className="argus-k8s-test-result">
            <div className="argus-k8s-test-result__summary">
              <StatusBadge tone={testResult.success ? "success" : "danger"}>
                {testResult.success
                  ? t("kubernetes.card.testSuccess")
                  : t("kubernetes.card.testFailed")}
              </StatusBadge>
              {testResult.success && (
                <span>
                  {t("kubernetes.card.latency")}: {testResult.latencyMs} ms
                </span>
              )}
            </div>
            <ul className="argus-k8s-test-result__checks">
              {testResult.checks.map((check) => (
                <li key={check.name}>
                  <StatusBadge
                    tone={
                      check.status === "passed"
                        ? "success"
                        : check.status === "failed"
                          ? "danger"
                          : "neutral"
                    }
                  >
                    {check.name}
                  </StatusBadge>
                  {check.detail && <code>{check.detail}</code>}
                </li>
              ))}
            </ul>
          </div>
        )}
        <div className="argus-k8s-card-actions">
          <Button onClick={onOpen} size="sm" variant="primary">
            {t("kubernetes.card.open")}
          </Button>
          <Button
            loading={test.isPending}
            onClick={() => test.mutate()}
            size="sm"
          >
            {test.isPending
              ? t("kubernetes.card.testing")
              : t("kubernetes.card.test")}
          </Button>
          <Button onClick={onEdit} size="sm">
            {t("kubernetes.card.edit")}
          </Button>
          <Button onClick={onDelete} size="sm" variant="danger">
            {t("kubernetes.card.delete")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
