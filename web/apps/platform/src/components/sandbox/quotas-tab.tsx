import { useQueries, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { Button, DataTable, EmptyState, FormDrawer, RowAction, Spinner } from "@argus/ui";
import { QuotaEditor } from "../quota-editor";

type QuotaRow = {
  enterpriseId: string;
  enterpriseName: string;
  allowedProfiles: string;
  maxConcurrentSessions: number;
  maxDailySessionMinutes: number;
  maxDailyCpuMinutes: number;
  maxArtifactStorageMb: number;
  artifactRetentionDays: number;
};

/** 企业配额 Tab：按企业聚合展示 Sandbox 配额，抽屉内编辑。 */
export function QuotasTab() {
  const { t } = useTranslation();
  const api = useApi();
  const [editing, setEditing] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });

  const enterpriseItems = enterprises.data?.items ?? [];
  const quotas = useQueries({
    queries: enterpriseItems.map((enterprise) => ({
      queryKey: ["platform", "quota", enterprise.id],
      queryFn: () => api.platform.quotas.get(enterprise.id),
      retry: false,
    })),
  });

  const isPending =
    enterprises.isPending || quotas.some((query) => query.isPending);

  const rows: QuotaRow[] = enterpriseItems.flatMap((enterprise, index) => {
    const quota = quotas[index]?.data;
    if (!quota) return [];
    return [
      {
        enterpriseId: enterprise.id,
        enterpriseName: enterprise.name,
        allowedProfiles: quota.allowedProfiles.join(", "),
        maxConcurrentSessions: quota.maxConcurrentSessions,
        maxDailySessionMinutes: quota.maxDailySessionMinutes,
        maxDailyCpuMinutes: quota.maxDailyCpuMinutes,
        maxArtifactStorageMb: quota.maxArtifactStorageMb,
        artifactRetentionDays: quota.artifactRetentionDays,
      },
    ];
  });

  return (
    <div className="argus-platform-stack">
      {isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("sandbox.quotas.empty")} />
      ) : (
        <DataTable<QuotaRow>
          columns={[
            {
              key: "enterpriseName",
              header: t("sandbox.quotas.table.enterprise"),
            },
            {
              key: "allowedProfiles",
              header: t("sandbox.quotas.table.allowedProfiles"),
              render: (row) => (
                <code className="argus-mono">{row.allowedProfiles}</code>
              ),
            },
            {
              key: "maxConcurrentSessions",
              header: t("sandbox.quotas.table.concurrent"),
              align: "right",
            },
            {
              key: "maxDailySessionMinutes",
              header: t("sandbox.quotas.table.dailyMinutes"),
              align: "right",
            },
            {
              key: "maxDailyCpuMinutes",
              header: t("sandbox.quotas.table.dailyCpu"),
              align: "right",
            },
            {
              key: "maxArtifactStorageMb",
              header: t("sandbox.quotas.table.storage"),
              align: "right",
            },
            {
              key: "artifactRetentionDays",
              header: t("sandbox.quotas.table.retention"),
              align: "right",
            },
            {
              key: "enterpriseId",
              header: t("common.actions"),
              render: (row) => (
                <RowAction
                  onClick={() =>
                    setEditing({
                      id: row.enterpriseId,
                      name: row.enterpriseName,
                    })
                  }
                >
                  {t("sandbox.quotas.edit")}
                </RowAction>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.enterpriseId}
        />
      )}

      <FormDrawer
        footer={
          <Button onClick={() => setEditing(null)} variant="secondary">
            {t("common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setEditing(null);
        }}
        open={editing !== null}
        title={`${t("sandbox.quotas.edit")} — ${editing?.name ?? ""}`}
        width={560}
      >
        {editing && <QuotaEditor enterpriseId={editing.id} />}
      </FormDrawer>
    </div>
  );
}
