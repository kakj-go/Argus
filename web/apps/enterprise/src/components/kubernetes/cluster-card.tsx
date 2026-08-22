import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useApi, type KubernetesCluster } from "@argus/api-client";
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
  cluster: KubernetesCluster;
  onOpen: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onInstallCollector?: () => void;
  onOpenCollector?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();

  const collectorQuery = useQuery({
    queryKey: ["kubernetes", "collector", cluster.id],
    queryFn: () => api.kubernetes.getCollector(cluster.id),
    enabled: Boolean(onInstallCollector || onOpenCollector),
  });
  const bindingsQuery = useQuery({
    queryKey: ["kubernetes", "nodeBindings", cluster.id],
    queryFn: () => api.kubernetes.listNodeBindings(cluster.id),
    enabled: Boolean(onInstallCollector || onOpenCollector),
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
          <StatusBadge tone={connectionStatusTone(cluster.connection_status)}>
            {t(`kubernetes.status.${cluster.connection_status}`)}
          </StatusBadge>
        </div>
        <div className="argus-k8s-cluster-card__server">
          {cluster.api_server}
        </div>
        <dl className="argus-k8s-kv">
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.version")}</dt>
            <dd>{cluster.kubernetes_version}</dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.nodes")}</dt>
            <dd>
              {cluster.ready_node_count}/{cluster.node_count}
            </dd>
          </div>
          <div className="argus-k8s-kv__item">
            <dt>{t("kubernetes.card.connectionMode")}</dt>
            <dd>{t(`kubernetes.mode.${cluster.connection_mode}`)}</dd>
          </div>
          {(onInstallCollector || onOpenCollector) && (
            <>
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
            </>
          )}
        </dl>
        <div className="argus-k8s-card-actions">
          <Button onClick={onOpen} size="sm" variant="primary">
            {t("kubernetes.card.open")}
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
