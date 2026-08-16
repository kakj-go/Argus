import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type CreateRoleBindingInput,
  type RoleBinding,
  type RoleBindingSubjectType,
} from "@argus/api-client";
import {
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Spinner,
  StatusBadge,
  Switch,
} from "@argus/ui";
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
  const roleName = (id: string) =>
    (roles.data ?? []).find((role) => role.id === id)?.name ?? id;
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
          onSubmit={(values) => save.mutate({ id: editing?.id, values })}
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
  onSubmit: (values: CreateRoleBindingInput) => void;
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
  const [subject_type, setSubjectType] = useState<RoleBindingSubjectType>(
    binding?.subject_type ?? "user",
  );
  const [subject_id, setSubjectId] = useState(binding?.subject_id ?? "");
  const [role_id, setRoleId] = useState(binding?.role_id ?? "");
  const [data_scope_ids, setDataScopeIds] = useState(
    binding?.data_scope_ids ?? [],
  );
  const [valid_from, setValidFrom] = useState(toDateInput(binding?.valid_from));
  const [valid_until, setValidUntil] = useState(
    toDateInput(binding?.valid_until),
  );
  const [active, setActive] = useState(binding?.status !== "disabled");

  const subjectOptions =
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
          }));

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={() =>
        onSubmit({
          subject_type,
          subject_id: subject_id || subjectOptions[0]?.value || "",
          role_id: role_id || (roles.data ?? [])[0]?.id || "",
          data_scope_ids,
          valid_from: fromDateInput(valid_from),
          valid_until: fromDateInput(valid_until, true),
          status: active ? "active" : "disabled",
        })
      }
      open
      title={
        binding
          ? t("settings.org.bindingsTab.editTitle")
          : t("settings.org.bindingsTab.createTitle")
      }
    >
      <div className="argus-settings-form">
        {binding && (
          <p className="argus-settings-section__hint">
            {t("settings.org.bindingsTab.immutableHint")}
          </p>
        )}
        <Field label={t("settings.org.bindingsTab.subject_type")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={(value) => {
              setSubjectType(value as RoleBindingSubjectType);
              setSubjectId("");
            }}
            options={[
              {
                value: "user",
                label: t("settings.org.bindingsTab.subjectTypes.user"),
              },
              {
                value: "department",
                label: t("settings.org.bindingsTab.subjectTypes.department"),
              },
              {
                value: "service_account",
                label: t(
                  "settings.org.bindingsTab.subjectTypes.serviceAccount",
                ),
              },
            ]}
            value={subject_type}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.subject")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={setSubjectId}
            options={subjectOptions}
            value={subject_id || subjectOptions[0]?.value || ""}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.role")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={setRoleId}
            options={(roles.data ?? []).map((role) => ({
              value: role.id,
              label: role.name,
            }))}
            value={role_id || (roles.data ?? [])[0]?.id || ""}
          />
        </Field>
        <Field label={t("settings.org.tabs.scopes")}>
          <CheckList
            onChange={setDataScopeIds}
            options={(dataScopes.data ?? []).map((scope) => ({
              id: scope.id,
              label: scope.name,
            }))}
            value={data_scope_ids}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.valid_from")}>
          <Input
            onChange={(event) => setValidFrom(event.target.value)}
            type="date"
            value={valid_from}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.valid_until")}>
          <Input
            onChange={(event) => setValidUntil(event.target.value)}
            type="date"
            value={valid_until}
          />
        </Field>
        <Switch
          checked={active}
          label={
            active
              ? t("settings.common.enabled")
              : t("settings.common.disabled")
          }
          onChange={setActive}
        />
      </div>
    </FormDrawer>
  );
}
