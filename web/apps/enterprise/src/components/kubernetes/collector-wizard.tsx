import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type K8sCluster,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  Button,
  DataTable,
  EmptyState,
  KeyValueGrid,
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
  status: "proposed" | "verified" | "rejected";
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
  cluster: K8sCluster;
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

  const verify = useMutation({
    mutationFn: (input: { bindingId: string; hostId: string }) =>
      api.kubernetes.verifyNodeBinding(input.bindingId, {
        hostId: input.hostId,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["kubernetes"] });
    },
  });

  const previewInstall = useMutation({
    mutationFn: () =>
      api.kubernetes.previewCollectorInstall(cluster.id, {
        profile: "k8s-daemonset",
      }),
    onSuccess: setPendingAction,
  });

  const bindings = useMemo(
    () => bindingsQuery.data ?? [],
    [bindingsQuery.data],
  );

  const bindingRows: BindingRow[] = bindings.map((binding) => ({
    id: binding.id,
    node: binding.nodeName,
    host: binding.hostId ?? "—",
    status: binding.status,
  }));

  const claimRows: ClaimRow[] = useMemo(() => {
    const rows: ClaimRow[] = [];
    const nodeByBindingId = new Map(
      bindings.map((binding) => [binding.id, binding.nodeName]),
    );
    const claimedBindingIds = new Set<string>();
    for (const claim of claimsQuery.data ?? []) {
      if (claim.nodeBindingId) claimedBindingIds.add(claim.nodeBindingId);
      const state: ClaimStateKey =
        claim.status === "conflict"
          ? "closeHostProfile"
          : claim.status === "released"
            ? "temporary"
            : "none";
      rows.push({
        id: claim.id,
        node:
          (claim.nodeBindingId && nodeByBindingId.get(claim.nodeBindingId)) ||
          claim.scope,
        signal: claim.signal,
        profile: claim.profile,
        owner: claim.collectorId,
        state,
        note:
          claim.status === "conflict"
            ? `${t("kubernetes.collector.wizard.conflictWith")}: ${claim.conflictWithId ?? "—"}`
            : "",
      });
    }
    // proposed 绑定上尚无 Claim：安装后 DaemonSet 将接管。
    for (const binding of bindings) {
      if (binding.status === "proposed" && !claimedBindingIds.has(binding.id)) {
        rows.push({
          id: `takeover-${binding.id}`,
          node: binding.nodeName,
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

  const unboundCount = Math.max(0, cluster.nodeCount - bindings.length);

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
          <Button
            disabled={row.host === "—"}
            loading={verify.isPending && verify.variables?.bindingId === row.id}
            onClick={() =>
              verify.mutate({ bindingId: row.id, hostId: row.host })
            }
            size="sm"
            variant="primary"
          >
            {t("kubernetes.collector.wizard.verify")}
          </Button>
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
