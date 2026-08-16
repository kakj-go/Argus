import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type Enterprise } from "@argus/api-client";
import {
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  KeyValueGrid,
  PageShell,
  Select,
  Spinner,
  StatusBadge,
  Textarea,
} from "@argus/ui";
import { QuotaEditor } from "../components/quota-editor";
import { formatDateTime } from "../lib/format";

type EnterpriseRow = {
  id: string;
  name: string;
  code: string;
  timezone: string;
  status: Enterprise["status"];
  sandboxQuotaProfile: string;
  createdAt: string;
};

type LifecycleAction = "suspend" | "activate" | "disable";

function statusTone(status: Enterprise["status"]) {
  if (status === "active") return "success" as const;
  if (status === "suspended") return "warning" as const;
  return "danger" as const;
}

const TIMEZONES = [
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Europe/Berlin",
  "America/New_York",
  "UTC",
];

/** 企业管理：生命周期（创建/暂停/激活/停用，无删除）+ 详情抽屉内配额编辑。 */
export function EnterprisesPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [detail, setDetail] = useState<Enterprise | null>(null);
  const [pendingAction, setPendingAction] = useState<{
    type: LifecycleAction;
    enterprise: EnterpriseRow;
  } | null>(null);

  // 创建表单
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [timezone, setTimezone] = useState(TIMEZONES[0]!);
  const [quotaProfile, setQuotaProfile] = useState("");
  const [remark, setRemark] = useState("");

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const profiles = useQuery({
    queryKey: ["platform", "profiles"],
    queryFn: () => api.platform.profiles.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "enterprises"] });

  const create = useMutation({
    mutationFn: () =>
      api.platform.enterprises.create({
        name: name.trim(),
        code: code.trim(),
        timezone,
        sandboxQuotaProfile: quotaProfile || undefined,
        remark: remark.trim() || undefined,
      }),
    onSuccess: () => {
      setCreateOpen(false);
      setName("");
      setCode("");
      setQuotaProfile("");
      setRemark("");
      void invalidate();
    },
  });

  const lifecycle = useMutation({
    mutationFn: (input: { type: LifecycleAction; id: string }) =>
      api.platform.enterprises[input.type](input.id),
    onSuccess: () => {
      setPendingAction(null);
      void invalidate();
    },
  });

  const rows: EnterpriseRow[] = (enterprises.data?.items ?? []).map((item) => ({
    id: item.id,
    name: item.name,
    code: item.code,
    timezone: item.timezone,
    status: item.status,
    sandboxQuotaProfile: item.sandboxQuotaProfile ?? "",
    createdAt: item.createdAt,
  }));

  const findEnterprise = (id: string) =>
    enterprises.data?.items.find((item) => item.id === id) ?? null;

  return (
    <PageShell
      actions={
        <Button onClick={() => setCreateOpen(true)} variant="primary">
          {t("enterprises.create")}
        </Button>
      }
      description={t("enterprises.description")}
      title={t("enterprises.title")}
    >
      {enterprises.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("enterprises.empty")} />
      ) : (
        <DataTable<EnterpriseRow>
          columns={[
            { key: "name", header: t("enterprises.table.name") },
            {
              key: "code",
              header: t("enterprises.table.code"),
              render: (row) => <code className="argus-mono">{row.code}</code>,
            },
            { key: "timezone", header: t("enterprises.table.timezone") },
            {
              key: "status",
              header: t("enterprises.table.status"),
              render: (row) => (
                <StatusBadge tone={statusTone(row.status)}>
                  {t(`enterprises.status.${row.status}`)}
                </StatusBadge>
              ),
            },
            {
              key: "sandboxQuotaProfile",
              header: t("enterprises.table.quotaProfile"),
              render: (row) => row.sandboxQuotaProfile || t("common.none"),
            },
            {
              key: "createdAt",
              header: t("enterprises.table.createdAt"),
              render: (row) => formatDateTime(row.createdAt, i18n.language),
            },
            {
              key: "id",
              header: t("common.actions"),
              render: (row) => (
                <div className="argus-row-actions">
                  <Button
                    onClick={() => setDetail(findEnterprise(row.id))}
                    size="sm"
                    variant="ghost"
                  >
                    {t("common.detail")}
                  </Button>
                  {row.status !== "active" && row.status !== "disabled" && (
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "activate", enterprise: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("enterprises.action.activate")}
                    </Button>
                  )}
                  {row.status === "active" && (
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "suspend", enterprise: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("enterprises.action.suspend")}
                    </Button>
                  )}
                  {row.status !== "disabled" && (
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "disable", enterprise: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("enterprises.action.disable")}
                    </Button>
                  )}
                </div>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      {/* 创建企业 */}
      <FormDrawer
        description={t("enterprises.form.create.description")}
        loading={create.isPending}
        onOpenChange={setCreateOpen}
        onSubmit={() => create.mutate()}
        open={createOpen}
        submitLabel={t("common.create")}
        title={t("enterprises.form.create.title")}
      >
        <Field label={t("enterprises.form.name")}>
          <Input
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </Field>
        <Field
          hint={t("enterprises.form.codeHint")}
          label={t("enterprises.form.code")}
        >
          <Input
            onChange={(event) => setCode(event.target.value)}
            pattern="[a-z0-9-]+"
            required
            value={code}
          />
        </Field>
        <Field label={t("enterprises.form.timezone")}>
          <Select
            onValueChange={setTimezone}
            options={TIMEZONES.map((zone) => ({ value: zone, label: zone }))}
            value={timezone}
          />
        </Field>
        <Field label={t("enterprises.form.quotaProfile")}>
          <Select
            onValueChange={setQuotaProfile}
            options={[
              { value: "", label: t("common.none") },
              ...(profiles.data ?? []).map((profile) => ({
                value: profile.name,
                label: profile.name,
              })),
            ]}
            value={quotaProfile}
          />
        </Field>
        <Field label={t("enterprises.form.remark")}>
          <Textarea
            onChange={(event) => setRemark(event.target.value)}
            rows={3}
            value={remark}
          />
        </Field>
      </FormDrawer>

      {/* 生命周期确认 */}
      <ConfirmDialog
        danger={pendingAction?.type !== "activate"}
        description={
          pendingAction
            ? `${pendingAction.enterprise.name} — ${t(`enterprises.confirm.${pendingAction.type}.description`)}`
            : undefined
        }
        loading={lifecycle.isPending}
        onConfirm={() =>
          pendingAction &&
          lifecycle.mutate({
            type: pendingAction.type,
            id: pendingAction.enterprise.id,
          })
        }
        onOpenChange={(open) => {
          if (!open) setPendingAction(null);
        }}
        open={pendingAction !== null}
        title={
          pendingAction
            ? t(`enterprises.confirm.${pendingAction.type}.title`)
            : ""
        }
      />

      {/* 详情抽屉 */}
      <FormDrawer
        footer={
          <Button onClick={() => setDetail(null)} variant="secondary">
            {t("common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setDetail(null);
        }}
        open={detail !== null}
        title={t("enterprises.detail.title")}
        width={560}
      >
        {detail && (
          <div className="argus-drawer-stack">
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: t("enterprises.detail.id"),
                  value: <code className="argus-mono">{detail.id}</code>,
                },
                { label: t("enterprises.table.name"), value: detail.name },
                {
                  label: t("enterprises.table.code"),
                  value: <code className="argus-mono">{detail.code}</code>,
                },
                {
                  label: t("enterprises.table.timezone"),
                  value: detail.timezone,
                },
                {
                  label: t("enterprises.table.status"),
                  value: (
                    <StatusBadge tone={statusTone(detail.status)}>
                      {t(`enterprises.status.${detail.status}`)}
                    </StatusBadge>
                  ),
                },
                {
                  label: t("enterprises.table.quotaProfile"),
                  value: detail.sandboxQuotaProfile ?? t("common.none"),
                },
                {
                  label: t("enterprises.table.createdAt"),
                  value: formatDateTime(detail.createdAt, i18n.language),
                },
                {
                  label: t("enterprises.detail.remark"),
                  value: detail.remark ?? t("common.none"),
                },
              ]}
            />
            <section className="argus-drawer-section">
              <h3>{t("enterprises.quota.title")}</h3>
              <QuotaEditor enterpriseId={detail.id} />
            </section>
          </div>
        )}
      </FormDrawer>
    </PageShell>
  );
}
