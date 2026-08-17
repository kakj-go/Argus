import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type KubernetesCluster,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Button,
  ConfirmDialog,
  EmptyState,
  FormDrawer,
  PageShell,
  Spinner,
} from "@argus/ui";
import { ClusterCard } from "../components/kubernetes/cluster-card";
import { CollectorWizard } from "../components/kubernetes/collector-wizard";
import {
  ClusterFormDrawer,
  type ClusterFormState,
} from "../components/kubernetes/cluster-form-drawer";
import { PendingActionCard } from "../components/kubernetes/pending-action-card";
import "../styles/kubernetes.css";

const realMode = import.meta.env.VITE_API_MODE === "real";

/** Kubernetes 集群列表：卡片网格 + 接入/编辑/删除/连接测试。 */
export function KubernetesPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [formState, setFormState] = useState<ClusterFormState | null>(null);
  const [deleting, setDeleting] = useState<KubernetesCluster | null>(null);
  const [deleteAction, setDeleteAction] =
    useState<PendingActionPublic | null>(null);
  const [installTarget, setInstallTarget] = useState<KubernetesCluster | null>(null);

  const clustersQuery = useQuery({
    queryKey: ["kubernetes", "clusters"],
    queryFn: () => api.kubernetes.listClusters(),
  });

  const deleteCluster = useMutation({
    mutationFn: (cluster: KubernetesCluster) =>
      api.kubernetes.previewDeleteResource(
        cluster.id,
        cluster.resource_version,
      ),
    onSuccess: (action) => {
      setDeleteAction(action);
      setDeleting(null);
    },
  });

  const clusters = clustersQuery.data?.items ?? [];

  return (
    <PageShell
      actions={
        <Button
          onClick={() => setFormState({ mode: "create" })}
          variant="primary"
        >
          {t("kubernetes.addCluster")}
        </Button>
      }
      description={t("kubernetes.subtitle")}
      title={t("kubernetes.title")}
    >
      {clustersQuery.isLoading ? (
        <Spinner label={t("common.loading")} />
      ) : clustersQuery.isError ? (
        <EmptyState
          description={t("kubernetes.loadFailed")}
          kind="error"
          title={t("kubernetes.title")}
        />
      ) : clusters.length === 0 ? (
        <EmptyState
          action={
            <Button
              onClick={() => setFormState({ mode: "create" })}
              variant="primary"
            >
              {t("kubernetes.addCluster")}
            </Button>
          }
          description={t("kubernetes.empty.description")}
          title={t("kubernetes.empty.title")}
        />
      ) : (
        <div className="argus-k8s-cluster-grid">
          {clusters.map((cluster) => (
            <ClusterCard
              cluster={cluster}
              key={cluster.id}
              onDelete={() => setDeleting(cluster)}
              onEdit={() => setFormState({ mode: "edit", cluster })}
              onInstallCollector={realMode ? undefined : () => setInstallTarget(cluster)}
              onOpen={() =>
                void navigate({
                  to: "/kubernetes/$clusterId",
                  params: { clusterId: cluster.id },
                })
              }
              onOpenCollector={realMode ? undefined : () =>
                void navigate({
                  to: "/kubernetes/$clusterId",
                  params: { clusterId: cluster.id },
                  hash: "otlp-collector",
                })
              }
            />
          ))}
        </div>
      )}

      <ClusterFormDrawer onClose={() => setFormState(null)} state={formState} />

      {!realMode && <FormDrawer
        footer={<></>}
        onOpenChange={(open) => {
          if (!open) setInstallTarget(null);
        }}
        open={installTarget !== null}
        title={`${t("kubernetes.collector.startInstall")} · ${installTarget?.name ?? ""}`}
        width={720}
      >
        {installTarget && (
          <CollectorWizard
            cluster={installTarget}
            onInstalled={() => setInstallTarget(null)}
          />
        )}
      </FormDrawer>}

      <ConfirmDialog
        confirmLabel={t("kubernetes.deleteDialog.confirm")}
        danger
        description={t("kubernetes.deleteDialog.description")}
        loading={deleteCluster.isPending}
        onConfirm={() => deleting && deleteCluster.mutate(deleting)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        open={deleting !== null}
        title={`${t("kubernetes.deleteDialog.title")} · ${deleting?.name ?? ""}`}
      />

      <FormDrawer
        footer={<></>}
        onOpenChange={(open) => {
          if (!open) setDeleteAction(null);
        }}
        open={deleteAction !== null}
        title={t("kubernetes.deleteDialog.title")}
      >
        {deleteAction && (
          <PendingActionCard
            action={deleteAction}
            onSettled={() => {
              setDeleteAction(null);
              void queryClient.invalidateQueries({ queryKey: ["kubernetes"] });
            }}
          />
        )}
      </FormDrawer>
    </PageShell>
  );
}
