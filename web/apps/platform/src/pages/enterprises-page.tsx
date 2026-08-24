import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  apiErrorField,
  formConstraint,
  formatApiError,
  useApi,
  type Enterprise,
} from "@argus/api-client";
import {
  Alert,
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
import { formatDateTime } from "../lib/format";

type EnterpriseRow = {
  id: string;
  name: string;
  code: string;
  timezone: string;
  status: Enterprise["status"];
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

const enterpriseCreateConstraints = {
  name: formConstraint("EnterpriseCreate", "name"),
  code: formConstraint("EnterpriseCreate", "code"),
  timezone: formConstraint("EnterpriseCreate", "timezone"),
  remark: formConstraint("EnterpriseCreate", "remark"),
};

const enterpriseCodePattern = new RegExp(
  enterpriseCreateConstraints.code.pattern ?? "^(?!)$",
);

/** 企业管理：生命周期（创建/暂停/激活/停用，无删除）+ 详情抽屉内配额编辑。 */
export function EnterprisesPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<Enterprise | null>(null);
  const [detail, setDetail] = useState<Enterprise | null>(null);
  const [pendingAction, setPendingAction] = useState<{
    type: LifecycleAction;
    enterprise: EnterpriseRow;
  } | null>(null);

  const enterpriseSchema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(
            enterpriseCreateConstraints.name.minLength ?? 1,
            t("enterprises.form.required"),
          )
          .max(
            enterpriseCreateConstraints.name.maxLength ?? 128,
            t("enterprises.form.tooLong", {
              max: enterpriseCreateConstraints.name.maxLength ?? 128,
            }),
          ),
        code: z
          .string()
          .trim()
          .min(
            enterpriseCreateConstraints.code.minLength ?? 1,
            t("enterprises.form.required"),
          )
          .max(
            enterpriseCreateConstraints.code.maxLength ?? 63,
            t("enterprises.form.codeInvalid"),
          )
          .regex(enterpriseCodePattern, t("enterprises.form.codeInvalid")),
        timezone: z
          .string()
          .min(
            enterpriseCreateConstraints.timezone.minLength ?? 1,
            t("enterprises.form.required"),
          )
          .max(
            enterpriseCreateConstraints.timezone.maxLength ?? 128,
            t("enterprises.form.tooLong", {
              max: enterpriseCreateConstraints.timezone.maxLength ?? 128,
            }),
          ),
        remark: z.string().max(
          enterpriseCreateConstraints.remark.maxLength ?? 2048,
          t("enterprises.form.tooLong", {
            max: enterpriseCreateConstraints.remark.maxLength ?? 2048,
          }),
        ),
      }),
    [t],
  );
  type EnterpriseForm = z.infer<typeof enterpriseSchema>;
  const {
    control,
    register,
    reset,
    setError,
    handleSubmit,
    formState: { errors },
  } = useForm<EnterpriseForm>({
    resolver: zodResolver(enterpriseSchema),
    defaultValues: {
      name: "",
      code: "",
      timezone: TIMEZONES[0]!,
      remark: "",
    },
  });

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "enterprises"] });

  const create = useMutation({
    mutationFn: (input: EnterpriseForm) =>
      api.platform.enterprises.create({
        name: input.name,
        code: input.code,
        timezone: input.timezone,
        remark: input.remark.trim() || undefined,
      }),
    onSuccess: () => {
      setCreateOpen(false);
      reset();
      void invalidate();
    },
    onError: (error) => {
      const field = apiErrorField(error);
      const formField =
        field === "name" ||
        field === "code" ||
        field === "timezone" ||
        field === "remark"
          ? field
          : undefined;
      const message = formatApiError(
        error,
        t("common.saveFailed"),
        (requestId) => t("common.requestReference", { requestId }),
      );
      if (formField) {
        setError(formField, { message, type: "server" }, { shouldFocus: true });
      } else {
        setError("root", { message, type: "server" });
      }
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
  const update = useMutation({
    mutationFn: (input: {
      id: string;
      name: string;
      timezone: string;
      remark?: string;
    }) =>
      api.platform.enterprises.update(input.id, {
        name: input.name,
        timezone: input.timezone,
        remark: input.remark,
      }),
    onSuccess: () => {
      setEditing(null);
      void invalidate();
    },
  });

  const rows: EnterpriseRow[] = (enterprises.data?.items ?? []).map((item) => ({
    id: item.id,
    name: item.name,
    code: item.code,
    timezone: item.timezone,
    status: item.status,
    createdAt: item.createdAt,
  }));

  const findEnterprise = (id: string) =>
    enterprises.data?.items.find((item) => item.id === id) ?? null;

  return (
    <PageShell
      actions={
        <Button
          onClick={() => {
            reset();
            setCreateOpen(true);
          }}
          variant="primary"
        >
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
                  <Button
                    onClick={() => setEditing(findEnterprise(row.id))}
                    size="sm"
                    variant="ghost"
                  >
                    {t("common.edit")}
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
        onOpenChange={(open) => {
          setCreateOpen(open);
          if (!open) reset();
        }}
        onSubmit={handleSubmit((values) => create.mutate(values))}
        open={createOpen}
        submitLabel={t("common.create")}
        title={t("enterprises.form.create.title")}
      >
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("common.saveFailed")}
            tone="danger"
          />
        )}
        <Field requirement="required" error={errors.name?.message} label={t("enterprises.form.name")}>
          <Input {...register("name")} maxLength={enterpriseCreateConstraints.name.maxLength} required />
        </Field>
        <Field requirement="required"
          hint={t("enterprises.form.codeHint")}
          error={errors.code?.message}
          label={t("enterprises.form.code")}
        >
          <Input {...register("code")} maxLength={enterpriseCreateConstraints.code.maxLength} required />
        </Field>
        <Field requirement="required"
          error={errors.timezone?.message}
          label={t("enterprises.form.timezone")}
        >
          <Controller
            control={control}
            name="timezone"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={TIMEZONES.map((zone) => ({
                  value: zone,
                  label: zone,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field requirement="optional" error={errors.remark?.message} label={t("enterprises.form.remark")}>
          <Textarea {...register("remark")} maxLength={enterpriseCreateConstraints.remark.maxLength} rows={3} />
        </Field>
      </FormDrawer>

      {editing && (
        <EnterpriseEditDrawer
          enterprise={editing}
          loading={update.isPending}
          onClose={() => setEditing(null)}
          onSubmit={(input) =>
            update.mutateAsync({ id: editing.id, ...input })
          }
        />
      )}

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
                  label: t("enterprises.table.createdAt"),
                  value: formatDateTime(detail.createdAt, i18n.language),
                },
                {
                  label: t("enterprises.detail.remark"),
                  value: detail.remark ?? t("common.none"),
                },
              ]}
            />
          </div>
        )}
      </FormDrawer>
    </PageShell>
  );
}

function EnterpriseEditDrawer({
  enterprise,
  loading,
  onClose,
  onSubmit,
}: {
  enterprise: Enterprise;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: {
    name: string;
    timezone: string;
    remark?: string;
  }) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(
            enterpriseCreateConstraints.name.minLength ?? 1,
            t("enterprises.form.required"),
          )
          .max(
            enterpriseCreateConstraints.name.maxLength ?? 128,
            t("enterprises.form.tooLong", {
              max: enterpriseCreateConstraints.name.maxLength ?? 128,
            }),
          ),
        timezone: z
          .string()
          .min(
            enterpriseCreateConstraints.timezone.minLength ?? 1,
            t("enterprises.form.required"),
          )
          .max(
            enterpriseCreateConstraints.timezone.maxLength ?? 128,
            t("enterprises.form.tooLong", {
              max: enterpriseCreateConstraints.timezone.maxLength ?? 128,
            }),
          ),
        remark: z
          .string()
          .trim()
          .max(
            enterpriseCreateConstraints.remark.maxLength ?? 2048,
            t("enterprises.form.tooLong", {
              max: enterpriseCreateConstraints.remark.maxLength ?? 2048,
            }),
          ),
      }),
    [t],
  );
  type EditForm = z.infer<typeof schema>;
  const {
    control,
    register,
    setError,
    handleSubmit,
    formState: { errors },
  } = useForm<EditForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: enterprise.name,
      timezone: enterprise.timezone,
      remark: enterprise.remark ?? "",
    },
  });
  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={handleSubmit(async (values) => {
        try {
          await onSubmit({
            name: values.name,
            timezone: values.timezone,
            remark: values.remark || undefined,
          });
        } catch (error) {
          const field = apiErrorField(error);
          const formField =
            field === "name" || field === "timezone" || field === "remark"
              ? field
              : undefined;
          const message = formatApiError(
            error,
            t("common.saveFailed"),
            (requestId) => t("common.requestReference", { requestId }),
          );
          if (formField) {
            setError(formField, { message, type: "server" }, { shouldFocus: true });
          } else {
            setError("root", { message, type: "server" });
          }
        }
      })}
      open
      title={t("enterprises.form.edit.title")}
    >
      {errors.root?.message && (
        <Alert
          description={errors.root.message}
          title={t("common.saveFailed")}
          tone="danger"
        />
      )}
      <Field requirement="required" error={errors.name?.message} label={t("enterprises.form.name")}>
        <Input {...register("name")} maxLength={enterpriseCreateConstraints.name.maxLength} required />
      </Field>
      <Field requirement="required"
        error={errors.timezone?.message}
        label={t("enterprises.form.timezone")}
      >
        <Controller
          control={control}
          name="timezone"
          render={({ field }) => (
            <Select
              onValueChange={field.onChange}
              options={TIMEZONES.map((zone) => ({ value: zone, label: zone }))}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field requirement="optional" error={errors.remark?.message} label={t("enterprises.form.remark")}>
        <Textarea {...register("remark")} maxLength={enterpriseCreateConstraints.remark.maxLength} rows={3} />
      </Field>
    </FormDrawer>
  );
}
