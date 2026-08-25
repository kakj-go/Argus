import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  presentApiFormError,
  useApi,
  type CreateRoleBindingInput,
  type RoleBinding,
  type RoleBindingSubjectType,
} from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  DateTimePicker,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Spinner,
  StatusBadge,
  Switch,
} from "@argus/ui";
import { roleDisplayName } from "../../lib/role-presentation";
import {
  CheckList,
  useOrgDepartments,
  useOrgRoleBindings,
  useOrgRoles,
  useOrgUsers,
} from "./org-users-tab";
import { formatDateTime } from "./shared";

/** ISO 时间 → date input 值。 */
function toDateInput(iso?: string): string {
  return iso?.slice(0, 10) ?? "";
}

/** date input 值 → ISO 时间；空字符串表示不限制。valid_until 取当日结束。 */
function fromDateInput(value: string, endOfDay = false): string | undefined {
  if (!value) return undefined;
  return new Date(
    `${value}T${endOfDay ? "23:59:59.999" : "00:00:00.000"}`,
  ).toISOString();
}

export function OrgBindingsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const bindings = useOrgRoleBindings();
  const roles = useOrgRoles();
  const users = useOrgUsers();
  const departments = useOrgDepartments();
  const serviceAccounts = useQuery({
    queryKey: ["org", "serviceAccounts"],
    queryFn: () => api.org.listServiceAccounts(),
  });
  const [editing, setEditing] = useState<RoleBinding | null | undefined>(
    undefined,
  );
  const [deleting, setDeleting] = useState<RoleBinding | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });

  const save = useMutation({
    mutationFn: (input: { id?: string; values: CreateRoleBindingInput }) =>
      input.id
        ? api.org.updateRoleBinding(input.id, {
            data_scope_ids: input.values.data_scope_ids,
            status: input.values.status,
            valid_from: input.values.valid_from,
            valid_until: input.values.valid_until,
          })
        : api.org.createRoleBinding(input.values),
    onSuccess: () => {
      setEditing(undefined);
      void invalidate();
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.org.deleteRoleBinding(id),
    onSuccess: () => {
      setDeleting(null);
      void invalidate();
    },
  });

  const subjectName = (binding: RoleBinding) => {
    if (binding.subject_type === "user") {
      const user = (users.data ?? []).find(
        (entry) => entry.id === binding.subject_id,
      );
      return user
        ? `${user.displayName} (@${user.username})`
        : binding.subject_id;
    }
    if (binding.subject_type === "department") {
      return (
        (departments.data ?? []).find(
          (entry) => entry.id === binding.subject_id,
        )?.name ?? binding.subject_id
      );
    }
    return (
      (serviceAccounts.data ?? []).find(
        (entry) => entry.id === binding.subject_id,
      )?.name ?? binding.subject_id
    );
  };
  const roleName = (id: string) => {
    const role = (roles.data ?? []).find((item) => item.id === id);
    return role ? roleDisplayName(role, t) : id;
  };
  const scopeName = (binding: RoleBinding) =>
    binding.data_scope_ids.length === 0
      ? t("settings.org.bindingsTab.scopeTypes.enterprise")
      : binding.data_scope_ids.join(", ");
  const validity = (binding: RoleBinding) =>
    binding.valid_from || binding.valid_until
      ? `${binding.valid_from ? formatDateTime(binding.valid_from) : "—"} ~ ${binding.valid_until ? formatDateTime(binding.valid_until) : "—"}`
      : t("settings.org.bindingsTab.permanent");

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.bindings")}
        </h2>
        <Button onClick={() => setEditing(null)} size="sm" variant="primary">
          {t("settings.org.bindingsTab.create")}
        </Button>
      </div>
      {bindings.isPending ? (
        <Spinner />
      ) : (bindings.data ?? []).length === 0 ? (
        <EmptyState
          description=""
          title={t("settings.org.bindingsTab.empty")}
        />
      ) : (
        <DataTable<RoleBinding & Record<string, unknown>>
          columns={[
            {
              key: "subject_id",
              header: t("settings.org.bindingsTab.subject"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Badge>
                    {t(
                      `settings.org.bindingsTab.subjectTypes.${row.subject_type === "service_account" ? "serviceAccount" : row.subject_type}`,
                    )}
                  </Badge>
                  {subjectName(row)}
                </span>
              ),
            },
            {
              key: "role_id",
              header: t("settings.org.bindingsTab.role"),
              render: (row) => roleName(row.role_id),
            },
            {
              key: "scopeId",
              header: t("settings.org.bindingsTab.scope"),
              render: (row) => scopeName(row),
            },
            {
              key: "valid_until",
              header: t("settings.org.bindingsTab.validity"),
              render: (row) => validity(row),
            },
            {
              key: "status",
              header: t("settings.common.status"),
              render: (row) => (
                <StatusBadge
                  tone={row.status === "active" ? "success" : "neutral"}
                >
                  {t(`settings.common.${row.status}`)}
                </StatusBadge>
              ),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Button
                    onClick={() => setEditing(row)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.common.edit")}
                  </Button>
                  <Button
                    onClick={() => setDeleting(row)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.common.delete")}
                  </Button>
                </span>
              ),
            },
          ]}
          data={
            (bindings.data ?? []) as Array<
              RoleBinding & Record<string, unknown>
            >
          }
          getRowKey={(row) => row.id}
        />
      )}
      {editing !== undefined && (
        <BindingDrawer
          binding={editing}
          loading={save.isPending}
          onClose={() => setEditing(undefined)}
          onSubmit={(values) => save.mutateAsync({ id: editing?.id, values })}
        />
      )}
      <ConfirmDialog
        danger
        description={t("settings.org.bindingsTab.deleteDescription")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
        onOpenChange={(open) => !open && setDeleting(null)}
        open={Boolean(deleting)}
        title={t("settings.org.bindingsTab.deleteTitle")}
      />
    </div>
  );
}

function BindingDrawer({
  binding,
  loading,
  onClose,
  onSubmit,
}: {
  binding: RoleBinding | null;
  loading: boolean;
  onClose: () => void;
  onSubmit: (values: CreateRoleBindingInput) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const roles = useOrgRoles();
  const users = useOrgUsers();
  const departments = useOrgDepartments();
  const serviceAccounts = useQuery({
    queryKey: ["org", "serviceAccounts"],
    queryFn: () => api.org.listServiceAccounts(),
  });
  const dataScopes = useQuery({
    queryKey: ["org", "data-scopes"],
    queryFn: () => api.org.listDataScopes(),
  });
  const bindingSchema = useMemo(
    () =>
      z
        .object({
          subject_type: z.enum(["user", "department", "service_account"]),
          subject_id: z.string().min(1, t("settings.common.required")),
          role_id: z.string().min(1, t("settings.common.required")),
          data_scope_ids: z.array(z.string()),
          valid_from: z.string(),
          valid_until: z.string(),
          active: z.boolean(),
        })
        .refine(
          (values) =>
            !values.valid_from ||
            !values.valid_until ||
            values.valid_from <= values.valid_until,
          {
            path: ["valid_until"],
            message: t("settings.org.bindingsTab.dateRangeInvalid"),
          },
        ),
    [t],
  );
  type BindingForm = z.infer<typeof bindingSchema>;
  const {
    clearErrors,
    control,
    setError,
    setValue,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<BindingForm>({
    resolver: zodResolver(bindingSchema),
    defaultValues: {
      subject_type: binding?.subject_type ?? "user",
      subject_id: binding?.subject_id ?? "",
      role_id: binding?.role_id ?? "",
      data_scope_ids: binding?.data_scope_ids ?? [],
      valid_from: toDateInput(binding?.valid_from),
      valid_until: toDateInput(binding?.valid_until),
      active: binding?.status !== "disabled",
    },
  });
  const subject_type = watch("subject_type") as RoleBindingSubjectType;

  const subjectOptions = useMemo(
    () =>
      subject_type === "user"
        ? (users.data ?? []).map((subject) => ({
            value: subject.id,
            label: `${subject.displayName} (@${subject.username})`,
          }))
        : subject_type === "department"
          ? (departments.data ?? []).map((subject) => ({
              value: subject.id,
              label: subject.name,
            }))
          : (serviceAccounts.data ?? []).map((subject) => ({
              value: subject.id,
              label: subject.name,
            })),
    [departments.data, serviceAccounts.data, subject_type, users.data],
  );

  useEffect(() => {
    if (!binding && subjectOptions[0])
      setValue("subject_id", subjectOptions[0].value, {
        shouldValidate: true,
      });
  }, [binding, setValue, subjectOptions]);

  useEffect(() => {
    if (!binding && roles.data?.[0])
      setValue("role_id", roles.data[0].id, { shouldValidate: true });
  }, [binding, roles.data, setValue]);
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        subject_type: values.subject_type,
        subject_id: values.subject_id,
        role_id: values.role_id,
        data_scope_ids: values.data_scope_ids,
        valid_from: fromDateInput(values.valid_from),
        valid_until: fromDateInput(values.valid_until, true),
        status: values.active ? "active" : "disabled",
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          data_scope_ids: "data_scope_ids",
          role_id: "role_id",
          subject_id: "subject_id",
          subject_type: "subject_type",
          valid_from: "valid_from",
          valid_until: "valid_until",
        },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          setError(field, { message, type: "server" }, { shouldFocus: true }),
        setFormError: (message) =>
          setError("root", { message, type: "server" }),
      });
    }
  });

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={submit}
      open
      title={
        binding
          ? t("settings.org.bindingsTab.editTitle")
          : t("settings.org.bindingsTab.createTitle")
      }
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        {binding && (
          <p className="argus-settings-section__hint">
            {t("settings.org.bindingsTab.immutableHint")}
          </p>
        )}
        <Field
          requirement="required"
          label={t("settings.org.bindingsTab.subjectType")}
        >
          <Controller
            control={control}
            name="subject_type"
            render={({ field }) => (
              <Select
                disabled={Boolean(binding)}
                onValueChange={(value) => {
                  field.onChange(value);
                  setValue("subject_id", "", { shouldValidate: true });
                }}
                options={[
                  {
                    value: "user",
                    label: t("settings.org.bindingsTab.subjectTypes.user"),
                  },
                  {
                    value: "department",
                    label: t(
                      "settings.org.bindingsTab.subjectTypes.department",
                    ),
                  },
                  {
                    value: "service_account",
                    label: t(
                      "settings.org.bindingsTab.subjectTypes.serviceAccount",
                    ),
                  },
                ]}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.subject_id?.message}
          label={t("settings.org.bindingsTab.subject")}
        >
          <Controller
            control={control}
            name="subject_id"
            render={({ field }) => (
              <Select
                disabled={Boolean(binding)}
                onValueChange={field.onChange}
                options={subjectOptions}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.role_id?.message}
          label={t("settings.org.bindingsTab.role")}
        >
          <Controller
            control={control}
            name="role_id"
            render={({ field }) => (
              <Select
                disabled={Boolean(binding)}
                onValueChange={field.onChange}
                options={(roles.data ?? []).map((role) => ({
                  value: role.id,
                  label: roleDisplayName(role, t),
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field requirement="optional" label={t("settings.org.tabs.scopes")}>
          <Controller
            control={control}
            name="data_scope_ids"
            render={({ field }) => (
              <CheckList
                onChange={field.onChange}
                options={(dataScopes.data ?? []).map((scope) => ({
                  id: scope.id,
                  label: scope.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          requirement="optional"
          label={t("settings.org.bindingsTab.validFrom")}
        >
          <Controller
            control={control}
            name="valid_from"
            render={({ field }) => <DateTimePicker {...field} type="date" />}
          />
        </Field>
        <Field
          requirement="optional"
          error={errors.valid_until?.message}
          label={t("settings.org.bindingsTab.validUntil")}
        >
          <Controller
            control={control}
            name="valid_until"
            render={({ field }) => <DateTimePicker {...field} type="date" />}
          />
        </Field>
        <Controller
          control={control}
          name="active"
          render={({ field }) => (
            <Switch
              checked={field.value}
              label={
                field.value
                  ? t("settings.common.enabled")
                  : t("settings.common.disabled")
              }
              onChange={field.onChange}
            />
          )}
        />
      </div>
    </FormDrawer>
  );
}
