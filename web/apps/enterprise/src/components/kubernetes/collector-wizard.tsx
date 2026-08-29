import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type KubernetesCluster,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  DataTable,
  EmptyState,
  KeyValueGrid,
  RowAction,
  Spinner,
  StatusBadge,
  Wizard,
  type Column,
} from "@argus/ui";
import { PendingActionCard } from "./pending-action-card";
import { bindingStatusTone } from "./status";

type ClaimStateKey =
  "none" | "takeover" | "closeHostProfile" | "unbound" | "temporary";

const CLAIM_STATE_TONE: Record<
  ClaimStateKey,
  "success" | "info" | "warning" | "neutral" | "danger"
> = {
  none: "success",
  takeover: "info",
  closeHostProfile: "warning",
  unbound: "neutral",
  temporary: "danger",
};

type BindingRow = {
  id: string;
  node: string;
  host: string;
  status: "proposed" | "verified" | "rejected" | "stale";
  version: number;
};

type ClaimRow = {
  id: string;
  node: string;
  signal: string;
  profile: string;
  owner: string;
  state: ClaimStateKey;
  note: string;
};

/**
 * DaemonSet Collector 安装向导：
 * ① Node/Host 绑定确认 → ② Collection Claim 冲突矩阵 → ③ 安装预览 + 两阶段确认。
 */
export function CollectorWizard({
  cluster,
  onInstalled,
}: {
  cluster: KubernetesCluster;
  onInstalled?: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [step, setStep] = useState(0);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);

  const bindingsQuery = useQuery({
    queryKey: ["kubernetes", "nodeBindings", cluster.id],
    queryFn: () => api.kubernetes.listNodeBindings(cluster.id),
  });
  const claimsQuery = useQuery({
    queryKey: ["kubernetes", "collectionClaims", cluster.id],
    queryFn: () => api.kubernetes.listCollectionClaims(cluster.id),
  });
  const distributionsQuery = useQuery({
    queryKey: ["telemetry", "distributions"],
    queryFn: () => api.telemetry.listDistributions(),
  });
  const profilesQuery = useQuery({
    queryKey: ["telemetry", "profiles"],
    queryFn: () => api.telemetry.listProfiles(),
  });

  const verify = useMutation({
    mutationFn: (input: { bindingId: string; hostId: string; version: number }) =>
      api.kubernetes.verifyNodeBinding(input.bindingId, {
        host_id: input.hostId,
        expected_version: input.version,
      }),
    onSuccess: setPendingAction,
  });

  const previewInstall = useMutation({
    mutationFn: () => {
      const distribution = distributionsQuery.data?.find(
        (item) =>
          item.support_status === "supported" &&
          item.artifacts.some((artifact) => artifact.platform === "linux_arm64"),
      );
      const profileIds = (profilesQuery.data ?? [])
        .filter((item) =>
          ["k8s-node-container", "k8s-cluster", "collector-self"].includes(
            item.key,
          ),
        )
        .map((item) => item.id);
      if (!distribution || profileIds.length === 0) {
        throw new Error("supported collector catalog is unavailable");
      }
      return api.kubernetes.previewCollectorInstall(cluster.id, {
        distribution_version_id: distribution.id,
        profile_ids: profileIds,
        route_kind: "direct_argus",
      });
    },
    onSuccess: setPendingAction,
  });

  const bindings = useMemo(
    () => bindingsQuery.data ?? [],
    [bindingsQuery.data],
  );

  const bindingRows: BindingRow[] = bindings.map((binding) => ({
    id: binding.id,
    node: binding.node_name,
    host: binding.host_id ?? "—",
    status: binding.status,
    version: binding.version,
  }));

  const claimRows: ClaimRow[] = useMemo(() => {
    const rows: ClaimRow[] = [];
    for (const claim of claimsQuery.data ?? []) {
      const state: ClaimStateKey =
        claim.status === "conflict"
          ? "closeHostProfile"
          : claim.status === "released"
            ? "temporary"
            : "none";
      rows.push({
        id: claim.id,
        node: claim.physical_resource_ref,
        signal: claim.signal,
        profile: claim.profile_id ?? claim.claim_type,
        owner: claim.collector_id,
        state,
        note:
          claim.status === "conflict"
            ? t("kubernetes.collector.wizard.conflictWith")
            : "",
      });
    }
    // proposed 绑定上尚无 Claim：安装后 DaemonSet 将接管。
    for (const binding of bindings) {
      if (binding.status === "proposed") {
        rows.push({
          id: `takeover-${binding.id}`,
          node: binding.node_name,
          signal: "metrics, logs",
          profile: "k8s-daemonset",
          owner: "—",
          state: "takeover",
          note: t("kubernetes.collector.wizard.takeoverNote"),
        });
      }
    }
    return rows;
  }, [bindings, claimsQuery.data, t]);

  const unboundCount = Math.max(0, (cluster.node_count ?? 0) - bindings.length);

  const bindingColumns: Column<BindingRow>[] = [
    { key: "node", header: t("kubernetes.table.node") },
    { key: "host", header: t("kubernetes.table.host") },
    {
      key: "status",
      header: t("kubernetes.table.status"),
      render: (row) => (
        <StatusBadge tone={bindingStatusTone(row.status)}>
          {t(`kubernetes.bindingStatus.${row.status}`)}
        </StatusBadge>
      ),
    },
    {
      key: "actions",
      header: t("kubernetes.table.actions"),
      render: (row) =>
        row.status === "proposed" ? (
          <RowAction
            disabled={row.host === "—"}
            loading={verify.isPending && verify.variables?.bindingId === row.id}
            onClick={() =>
              verify.mutate({
                bindingId: row.id,
                hostId: row.host,
                version: row.version,
              })
            }
          >
            {t("kubernetes.collector.wizard.verify")}
          </RowAction>
        ) : null,
    },
  ];

  const claimColumns: Column<ClaimRow>[] = [
    { key: "node", header: t("kubernetes.table.node") },
    { key: "signal", header: t("kubernetes.table.signal") },
    { key: "profile", header: t("kubernetes.table.profile") },
    { key: "owner", header: t("kubernetes.table.owner") },
    {
      key: "state",
      header: t("kubernetes.table.status"),
      render: (row) => (
        <StatusBadge tone={CLAIM_STATE_TONE[row.state]}>
          {t(`kubernetes.claimState.${row.state}`)}
        </StatusBadge>
      ),
    },
    { key: "note", header: t("kubernetes.table.note") },
  ];

  if (bindingsQuery.isLoading || claimsQuery.isLoading) {
    return <Spinner label={t("common.loading")} />;
  }

  if (pendingAction) {
    return (
      <PendingActionCard
        action={pendingAction}
        onSettled={() => {
          setPendingAction(null);
          void queryClient.invalidateQueries({ queryKey: ["kubernetes"] });
          onInstalled?.();
        }}
      />
    );
  }

  return (
    <Wizard
      current={step}
      onBack={() => setStep((value) => Math.max(0, value - 1))}
      onNext={() => setStep((value) => value + 1)}
      onSubmit={() => previewInstall.mutate()}
      steps={[
        {
          id: "bindings",
          title: t("kubernetes.collector.wizard.step1"),
          description: t("kubernetes.collector.wizard.step1Desc"),
        },
        {
          id: "claims",
          title: t("kubernetes.collector.wizard.step2"),
          description: t("kubernetes.collector.wizard.step2Desc"),
        },
        {
          id: "preview",
          title: t("kubernetes.collector.wizard.step3"),
          description: t("kubernetes.collector.wizard.step3Desc"),
        },
      ]}
      submitting={previewInstall.isPending}
      submitLabel={t("kubernetes.collector.wizard.submit")}
    >
      {step === 0 && (
        <div className="argus-k8s-stack">
          <Alert
            description={t("kubernetes.collector.wizard.evidenceHint")}
            title={t("kubernetes.collector.wizard.evidenceTitle")}
            tone="info"
          />
          {unboundCount > 0 && (
            <div>
              <StatusBadge tone="neutral">
                {t("kubernetes.claimState.unbound")}
              </StatusBadge>{" "}
              <span>
                {unboundCount} {t("kubernetes.collector.wizard.unboundNodes")}
              </span>
            </div>
          )}
          {bindingRows.length === 0 ? (
            <EmptyState
              description={t("kubernetes.collector.wizard.noBindingsHint")}
              title={t("kubernetes.collector.wizard.noBindings")}
            />
          ) : (
            <DataTable
              columns={bindingColumns}
              data={bindingRows}
              getRowKey={(row) => row.id}
            />
          )}
        </div>
      )}
      {step === 1 && (
        <div className="argus-k8s-stack">
          <Alert
            description={t("kubernetes.collector.wizard.recommendation")}
            title={t("kubernetes.collector.wizard.recommendationTitle")}
            tone="info"
          />
          {claimRows.length === 0 ? (
            <EmptyState
              description={t("kubernetes.collector.wizard.claimsEmpty")}
              title={t("kubernetes.collector.wizard.step2")}
            />
          ) : (
            <DataTable
              columns={claimColumns}
              data={claimRows}
              getRowKey={(row) => row.id}
            />
          )}
        </div>
      )}
      {step === 2 && (
        <div className="argus-k8s-stack">
          <h3 className="argus-k8s-section-title">
            {t("kubernetes.collector.wizard.previewTitle")}
          </h3>
          <KeyValueGrid
            columns={1}
            items={[
              {
                label: "DaemonSet",
                value: t("kubernetes.collector.wizard.daemonsetSummary"),
              },
              {
                label: "Gateway Deployment",
                value: t("kubernetes.collector.wizard.gatewaySummary"),
              },
              {
                label: t("kubernetes.collector.wizard.imageDigest"),
                value: t("kubernetes.collector.wizard.imageDigestValue"),
              },
              {
                label: t("kubernetes.collector.wizard.rbac"),
                value: t("kubernetes.collector.wizard.rbacValue"),
              },
              {
                label: t("kubernetes.collector.wizard.profile"),
                value: "k8s-daemonset",
              },
            ]}
          />
        </div>
      )}
    </Wizard>
  );
}
