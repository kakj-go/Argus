import { useQuery } from "@tanstack/react-query";
import { RefreshCw, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type {
  PKIBundleState,
  PKINodeStatus,
  PKINodeTrustStatus,
} from "@argus/api-client";
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  DataTable,
  EmptyState,
  KeyValueGrid,
  PageShell,
  Spinner,
  StatCard,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

function bundleTone(state: PKIBundleState) {
  if (state === "stable") return "success" as const;
  if (state === "failed") return "danger" as const;
  if (state === "overlapping" || state === "retiring") {
    return "warning" as const;
  }
  return "info" as const;
}

function nodeTone(status: PKINodeTrustStatus) {
  if (status === "acked") return "success" as const;
  if (status === "pending") return "warning" as const;
  return "danger" as const;
}

function Fingerprints({ values, empty }: { values: string[]; empty: string }) {
  if (values.length === 0) return <>{empty}</>;
  return (
    <span className="argus-pki-fingerprints">
      {values.map((value) => (
        <code className="argus-mono" key={value}>
          {value}
        </code>
      ))}
    </span>
  );
}

export function PKIPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const status = useQuery({
    queryKey: ["platform", "pki"],
    queryFn: () => api.platform.pki.get(),
    refetchInterval: 15_000,
  });
  const current = status.data?.bundles[0];

  return (
    <PageShell description={t("pki.description")} title={t("pki.title")}>
      <div className="argus-platform-stack">
        {status.isError && (
          <Alert
            description={t("pki.loadFailedDescription")}
            icon={<ShieldAlert aria-hidden size={16} />}
            title={t("pki.loadFailed")}
            tone="danger"
          />
        )}

        {status.isPending ? (
          <Spinner />
        ) : status.data ? (
          <>
            <div className="argus-stat-row">
              <StatCard
                label={t("pki.stats.acked")}
                tone="success"
                value={status.data.acknowledgedNodes}
              />
              <StatCard
                label={t("pki.stats.pending")}
                tone={status.data.pendingNodes > 0 ? "warning" : "neutral"}
                value={status.data.pendingNodes}
              />
              <StatCard
                label={t("pki.stats.failed")}
                tone={status.data.failedNodes > 0 ? "danger" : "neutral"}
                value={status.data.failedNodes}
              />
              <StatCard
                label={t("pki.stats.expired")}
                tone={status.data.trustExpiredNodes > 0 ? "danger" : "neutral"}
                value={status.data.trustExpiredNodes}
              />
            </div>

            <Card>
              <CardHeader
                action={
                  <Button
                    aria-label={t("pki.refresh")}
                    loading={status.isFetching}
                    onClick={() => void status.refetch()}
                    size="sm"
                    variant="secondary"
                  >
                    <RefreshCw aria-hidden size={14} />
                    {t("pki.refresh")}
                  </Button>
                }
                description={t("pki.current.description")}
                title={t("pki.current.title")}
              />
              <CardContent>
                {!current ? (
                  <EmptyState description="" title={t("pki.current.empty")} />
                ) : (
                  <KeyValueGrid
                    columns={2}
                    items={[
                      { label: t("pki.field.epoch"), value: current.epoch },
                      {
                        label: t("pki.field.state"),
                        value: (
                          <StatusBadge tone={bundleTone(current.state)}>
                            {t(`pki.state.${current.state}`)}
                          </StatusBadge>
                        ),
                      },
                      {
                        label: t("pki.field.direction"),
                        value: t(`pki.direction.${current.direction}`),
                      },
                      {
                        label: t("pki.field.bundleSha"),
                        value: (
                          <code className="argus-mono argus-pki-hash">
                            {current.bundleSha256}
                          </code>
                        ),
                      },
                      {
                        label: t("pki.field.startedAt"),
                        value: formatDateTime(current.startedAt, i18n.language),
                      },
                      {
                        label: t("pki.field.retireAt"),
                        value: current.retireAt
                          ? formatDateTime(current.retireAt, i18n.language)
                          : t("pki.field.none"),
                      },
                      {
                        label: t("pki.field.currentCa"),
                        value: (
                          <Fingerprints
                            empty={t("pki.field.none")}
                            values={current.currentCaFingerprints}
                          />
                        ),
                      },
                      {
                        label: t("pki.field.nextCa"),
                        value: (
                          <Fingerprints
                            empty={t("pki.field.none")}
                            values={current.nextCaFingerprints}
                          />
                        ),
                      },
                    ]}
                  />
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader title={t("pki.history.title")} />
              <CardContent>
                <DataTable
                  columns={[
                    { key: "epoch", header: t("pki.field.epoch") },
                    {
                      key: "state",
                      header: t("pki.field.state"),
                      render: (row) => (
                        <StatusBadge tone={bundleTone(row.state)}>
                          {t(`pki.state.${row.state}`)}
                        </StatusBadge>
                      ),
                    },
                    {
                      key: "direction",
                      header: t("pki.field.direction"),
                      render: (row) => t(`pki.direction.${row.direction}`),
                    },
                    {
                      key: "bundleSha256",
                      header: t("pki.field.bundleSha"),
                      render: (row) => (
                        <code className="argus-mono argus-pki-short-hash">
                          {row.bundleSha256}
                        </code>
                      ),
                    },
                    {
                      key: "startedAt",
                      header: t("pki.field.startedAt"),
                      render: (row) =>
                        formatDateTime(row.startedAt, i18n.language),
                    },
                  ]}
                  data={status.data.bundles}
                  getRowKey={(row) => String(row.epoch)}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader
                description={t("pki.nodes.description")}
                title={t("pki.nodes.title")}
              />
              <CardContent>
                {status.data.nodes.length === 0 ? (
                  <EmptyState description="" title={t("pki.nodes.empty")} />
                ) : (
                  <DataTable<PKINodeStatus>
                    columns={[
                      { key: "id", header: t("pki.table.node") },
                      {
                        key: "kind",
                        header: t("pki.table.kind"),
                        render: (row) => t(`pki.nodeKind.${row.kind}`),
                      },
                      { key: "epoch", header: t("pki.table.epoch") },
                      {
                        key: "status",
                        header: t("pki.table.status"),
                        render: (row) => (
                          <StatusBadge tone={nodeTone(row.status)}>
                            {t(`pki.nodeStatus.${row.status}`)}
                          </StatusBadge>
                        ),
                      },
                      {
                        key: "blocksCutover",
                        header: t("pki.table.cutoverGate"),
                        render: (row) =>
                          t(
                            row.blocksCutover
                              ? "pki.cutoverGate.required"
                              : "pki.cutoverGate.offline",
                          ),
                      },
                      {
                        key: "updatedAt",
                        header: t("pki.table.updatedAt"),
                        render: (row) =>
                          formatDateTime(row.updatedAt, i18n.language),
                      },
                      {
                        key: "error",
                        header: t("pki.table.error"),
                        render: (row) => row.error ?? "—",
                      },
                    ]}
                    data={status.data.nodes}
                    getRowKey={(row) => `${row.kind}/${row.id}`}
                  />
                )}
              </CardContent>
            </Card>
          </>
        ) : null}
      </div>
    </PageShell>
  );
}
