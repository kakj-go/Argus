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
  type EnterpriseAdmin,
} from "@argus/api-client";
import {
  Alert,
  Button,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

type AdminRow = {
  id: string;
  displayName: string;
  username: string;
  email: string;
  enterpriseId: string;
  enterpriseName: string;
  credentialStatus: EnterpriseAdmin["credentialStatus"];
  lastLoginAt?: string;
};

type AdminAction = "resetAuth" | "disable";

const adminCreateConstraints = {
  username: formConstraint("EnterpriseAdminCreate", "username"),
  displayName: formConstraint("EnterpriseAdminCreate", "display_name"),
  email: formConstraint("EnterpriseAdminCreate", "email"),
};

function credentialTone(status: EnterpriseAdmin["credentialStatus"]) {
  if (status === "active") return "success" as const;
  if (status === "temporary_password") return "warning" as const;
  return "danger" as const;
}

/**
 * 企业管理员：创建后一次性展示临时密码，并支持重置认证和禁用。
 * 明确不提供代登录。
 */
export function AdminsPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<EnterpriseAdmin | null>(null);
  const [pendingAction, setPendingAction] = useState<{
    type: AdminAction;
    admin: AdminRow;
  } | null>(null);

  const adminSchema = useMemo(
    () =>
      z.object({
        enterpriseId: z.string().min(1, t("admins.form.required")),
        username: z
          .string()
          .trim()
          .min(
            adminCreateConstraints.username.minLength ?? 3,
            t("admins.form.usernameInvalid"),
          )
          .max(
            adminCreateConstraints.username.maxLength ?? 128,
            t("admins.form.tooLong", {
              max: adminCreateConstraints.username.maxLength ?? 128,
            }),
          ),
        displayName: z
          .string()
          .trim()
          .min(
            adminCreateConstraints.displayName.minLength ?? 1,
            t("admins.form.required"),
          )
          .max(
            adminCreateConstraints.displayName.maxLength ?? 128,
            t("admins.form.tooLong", {
              max: adminCreateConstraints.displayName.maxLength ?? 128,
            }),
          ),
        email: z
          .string()
          .trim()
          .refine(
            (value) => value === "" || z.email().safeParse(value).success,
            t("admins.form.emailInvalid"),
          ),
      }),
    [t],
  );
  type AdminForm = z.infer<typeof adminSchema>;
  const {
    control,
    register,
    reset,
    setError,
    handleSubmit,
    formState: { errors },
  } = useForm<AdminForm>({
    resolver: zodResolver(adminSchema),
    defaultValues: {
      enterpriseId: "",
      username: "",
      displayName: "",
      email: "",
    },
  });

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const admins = useQuery({
    queryKey: ["platform", "admins"],
    queryFn: () => api.platform.admins.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "admins"] });

  const create = useMutation({
    mutationFn: (input: AdminForm) =>
      api.platform.admins.create({
        enterpriseId: input.enterpriseId,
        username: input.username,
        displayName: input.displayName,
        email: input.email || undefined,
      }),
    onSuccess: (admin) => {
      setCreateOpen(false);
      setCreated(admin);
      reset();
      void invalidate();
    },
    onError: (error) => {
      const field = apiErrorField(error);
      const formField =
        field === "enterprise_id"
          ? "enterpriseId"
          : field === "display_name"
            ? "displayName"
            : field === "username" || field === "email"
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

  const action = useMutation({
    mutationFn: (input: { type: AdminAction; id: string }) =>
      api.platform.admins[input.type](input.id),
    onSuccess: (admin, input) => {
      setPendingAction(null);
      if (input.type === "resetAuth") setCreated(admin);
      void invalidate();
    },
  });

  const enterpriseName = (id: string) =>
    enterprises.data?.items.find((item) => item.id === id)?.name ?? id;

  const rows: AdminRow[] = (admins.data ?? []).map((item) => ({
    id: item.id,
    displayName: item.displayName,
    username: item.username,
    email: item.email ?? "",
    enterpriseId: item.enterpriseId,
    enterpriseName: enterpriseName(item.enterpriseId),
    credentialStatus: item.credentialStatus,
    lastLoginAt: item.lastLoginAt,
  }));

  const activeEnterprises = (enterprises.data?.items ?? []).filter(
    (item) => item.status === "active",
  );

  return (
    <PageShell
      actions={
        <Button
          onClick={() => {
            reset({
              enterpriseId: activeEnterprises[0]?.id ?? "",
              username: "",
              displayName: "",
              email: "",
            });
            setCreateOpen(true);
          }}
          variant="primary"
        >
          {t("admins.invite")}
        </Button>
      }
      description={t("admins.description")}
      title={t("admins.title")}
    >
      <div className="argus-platform-stack">
        <Alert
          description={t("admins.noImpersonation.description")}
          title={t("admins.noImpersonation.title")}
          tone="info"
        />

        {admins.isPending ? (
          <Spinner />
        ) : rows.length === 0 ? (
          <EmptyState description="" title={t("admins.empty")} />
        ) : (
          <DataTable<AdminRow>
            columns={[
              { key: "displayName", header: t("admins.table.displayName") },
              {
                key: "username",
                header: t("admins.table.username"),
                render: (row) => (
                  <code className="argus-mono">{row.username}</code>
                ),
              },
              {
                key: "email",
                header: t("admins.table.email"),
                render: (row) => row.email || t("common.none"),
              },
              { key: "enterpriseName", header: t("admins.table.enterprise") },
              {
                key: "credentialStatus",
                header: t("admins.table.credentialStatus"),
                render: (row) => (
                  <StatusBadge tone={credentialTone(row.credentialStatus)}>
                    {t(`admins.status.${row.credentialStatus}`)}
                  </StatusBadge>
                ),
              },
              {
                key: "lastLoginAt",
                header: t("admins.table.lastLogin"),
                render: (row) => formatDateTime(row.lastLoginAt, i18n.language),
              },
              {
                key: "id",
                header: t("common.actions"),
                render: (row) => (
                  <div className="argus-row-actions">
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "resetAuth", admin: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("admins.action.resetAuth")}
                    </Button>
                    {row.credentialStatus !== "disabled" && (
                      <Button
                        onClick={() =>
                          setPendingAction({ type: "disable", admin: row })
                        }
                        size="sm"
                        variant="ghost"
                      >
                        {t("admins.action.disable")}
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
      </div>

      {/* 创建临时密码管理员 */}
      <FormDrawer
        description={t("admins.form.description")}
        loading={create.isPending}
        onOpenChange={(open) => {
          setCreateOpen(open);
          if (!open) reset();
        }}
        onSubmit={handleSubmit((values) => create.mutate(values))}
        open={createOpen}
        submitLabel={t("common.create")}
        title={t("admins.form.title")}
      >
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("common.saveFailed")}
            tone="danger"
          />
        )}
        <Field requirement="required"
          error={errors.enterpriseId?.message}
          label={t("admins.form.enterprise")}
        >
          <Controller
            control={control}
            name="enterpriseId"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={activeEnterprises.map((enterprise) => ({
                  value: enterprise.id,
                  label: enterprise.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field requirement="required"
          error={errors.displayName?.message}
          label={t("admins.form.displayName")}
        >
          <Input {...register("displayName")} maxLength={adminCreateConstraints.displayName.maxLength} required />
        </Field>
        <Field requirement="required"
          error={errors.username?.message}
          label={t("admins.form.username")}
        >
          <Input {...register("username")} maxLength={adminCreateConstraints.username.maxLength} required />
        </Field>
        <Field requirement="optional" error={errors.email?.message} label={t("admins.form.email")}>
          <Input {...register("email")} type="email" />
        </Field>
      </FormDrawer>

      {/* 创建或重置成功后只展示一次临时密码。 */}
      <FormDrawer
        footer={
          <Button onClick={() => setCreated(null)} variant="primary">
            {t("common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setCreated(null);
        }}
        open={created !== null}
        title={t("admins.created.title")}
      >
        {created && (
          <div className="argus-drawer-stack">
            <Alert
              description={t("admins.created.description")}
              title={t("admins.created.title")}
              tone="success"
            />
            <CodeBlock code={created.temporaryPassword ?? ""} language="text" />
          </div>
        )}
      </FormDrawer>

      {/* 操作确认 */}
      <ConfirmDialog
        danger
        description={
          pendingAction
            ? `${pendingAction.admin.displayName} (${pendingAction.admin.username}) — ${t(`admins.confirm.${pendingAction.type}.description`)}`
            : undefined
        }
        loading={action.isPending}
        onConfirm={() =>
          pendingAction &&
          action.mutate({
            type: pendingAction.type,
            id: pendingAction.admin.id,
          })
        }
        onOpenChange={(open) => {
          if (!open) setPendingAction(null);
        }}
        open={pendingAction !== null}
        title={
          pendingAction ? t(`admins.confirm.${pendingAction.type}.title`) : ""
        }
      />
    </PageShell>
  );
}
