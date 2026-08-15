import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type CreateRoleBindingInput,
  type RoleBinding,
  type RoleBindingScopeType,
  type RoleBindingSubjectType,
} from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
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
import { useOrgDepartments, useOrgProjects, useOrgRoleBindings, useOrgRoles, useOrgUsers } from "./org-users-tab";
import { formatDateTime } from "./shared";

/** ISO 时间 → date input 值。 */
function toDateInput(iso?: string): string {
  return iso?.slice(0, 10) ?? "";
}

/** date input 值 → ISO 时间；空字符串表示不限制。validUntil 取当日结束。 */
function fromDateInput(value: string, endOfDay = false): string | undefined {
  if (!value) return undefined;
  return new Date(`${value}T${endOfDay ? "23:59:59.999" : "00:00:00.000"}`).toISOString();
}

export function OrgBindingsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const bindings = useOrgRoleBindings();
  const roles = useOrgRoles();
  const users = useOrgUsers();
  const departments = useOrgDepartments();
  const projects = useOrgProjects();
  const [editing, setEditing] = useState<RoleBinding | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<RoleBinding | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });

  const save = useMutation({
    mutationFn: (input: { id?: string; values: CreateRoleBindingInput }) =>
      input.id
        ? api.org.updateRoleBinding(input.id, {
            status: input.values.status,
            validFrom: input.values.validFrom,
            validUntil: input.values.validUntil,
          })
        : api.org.createRoleBinding(input.values),
    onSuccess: () => { setEditing(undefined); void invalidate(); },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.org.deleteRoleBinding(id),
    onSuccess: () => { setDeleting(null); void invalidate(); },
  });

  const subjectName = (binding: RoleBinding) => {
    if (binding.subjectType === "user") {
      const user = (users.data ?? []).find((entry) => entry.id === binding.subjectId);
      return user ? `${user.displayName} (@${user.username})` : binding.subjectId;
    }
    return (departments.data ?? []).find((entry) => entry.id === binding.subjectId)?.name ?? binding.subjectId;
  };
  const roleName = (id: string) => (roles.data ?? []).find((role) => role.id === id)?.name ?? id;
  const scopeName = (binding: RoleBinding) =>
    binding.scopeType === "enterprise"
      ? t("settings.org.bindingsTab.scopeTypes.enterprise")
      : (projects.data ?? []).find((project) => project.id === binding.scopeId)?.name ?? binding.scopeId;
  const validity = (binding: RoleBinding) =>
    binding.validFrom || binding.validUntil
      ? `${binding.validFrom ? formatDateTime(binding.validFrom) : "—"} ~ ${binding.validUntil ? formatDateTime(binding.validUntil) : "—"}`
      : t("settings.org.bindingsTab.permanent");

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">{t("settings.org.tabs.bindings")}</h2>
        <Button onClick={() => setEditing(null)} size="sm" variant="primary">{t("settings.org.bindingsTab.create")}</Button>
      </div>
      {bindings.isPending ? (
        <Spinner />
      ) : (bindings.data ?? []).length === 0 ? (
        <EmptyState description="" title={t("settings.org.bindingsTab.empty")} />
      ) : (
        <DataTable<RoleBinding & Record<string, unknown>>
          columns={[
            {
              key: "subjectId",
              header: t("settings.org.bindingsTab.subject"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Badge>{t(`settings.org.bindingsTab.subjectTypes.${row.subjectType}`)}</Badge>
                  {subjectName(row)}
                </span>
              ),
            },
            { key: "roleId", header: t("settings.org.bindingsTab.role"), render: (row) => roleName(row.roleId) },
            { key: "scopeId", header: t("settings.org.bindingsTab.scope"), render: (row) => scopeName(row) },
            { key: "validUntil", header: t("settings.org.bindingsTab.validity"), render: (row) => validity(row) },
            {
              key: "status",
              header: t("settings.common.status"),
              render: (row) => <StatusBadge tone={row.status === "active" ? "success" : "neutral"}>{t(`settings.common.${row.status}`)}</StatusBadge>,
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Button onClick={() => setEditing(row)} size="sm" variant="ghost">{t("settings.common.edit")}</Button>
                  <Button onClick={() => setDeleting(row)} size="sm" variant="ghost">{t("settings.common.delete")}</Button>
                </span>
              ),
            },
          ]}
          data={(bindings.data ?? []) as Array<RoleBinding & Record<string, unknown>>}
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
  const enterpriseId = useAuthStore((state) => state.session?.membership?.enterpriseId ?? "");
  const roles = useOrgRoles();
  const users = useOrgUsers();
  const departments = useOrgDepartments();
  const projects = useOrgProjects();
  const [subjectType, setSubjectType] = useState<RoleBindingSubjectType>(binding?.subjectType ?? "user");
  const [subjectId, setSubjectId] = useState(binding?.subjectId ?? "");
  const [roleId, setRoleId] = useState(binding?.roleId ?? "");
  const [scopeType, setScopeType] = useState<RoleBindingScopeType>(binding?.scopeType ?? "enterprise");
  const [projectId, setProjectId] = useState(binding?.scopeType === "project" ? binding.scopeId : "");
  const [validFrom, setValidFrom] = useState(toDateInput(binding?.validFrom));
  const [validUntil, setValidUntil] = useState(toDateInput(binding?.validUntil));
  const [active, setActive] = useState(binding?.status !== "disabled");

  const subjects = subjectType === "user" ? (users.data ?? []) : (departments.data ?? []);
  const subjectOptions = subjects.map((subject) => ({
    value: subject.id,
    label: "displayName" in subject ? `${subject.displayName} (@${subject.username})` : subject.name,
  }));

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={() =>
        onSubmit({
          subjectType,
          subjectId: subjectId || subjectOptions[0]?.value || "",
          roleId: roleId || (roles.data ?? [])[0]?.id || "",
          scopeType,
          scopeId: scopeType === "enterprise" ? enterpriseId : projectId || (projects.data ?? [])[0]?.id || "",
          validFrom: fromDateInput(validFrom),
          validUntil: fromDateInput(validUntil, true),
          status: active ? "active" : "disabled",
        })
      }
      open
      title={binding ? t("settings.org.bindingsTab.editTitle") : t("settings.org.bindingsTab.createTitle")}
    >
      <div className="argus-settings-form">
        {binding && <p className="argus-settings-section__hint">{t("settings.org.bindingsTab.immutableHint")}</p>}
        <Field label={t("settings.org.bindingsTab.subjectType")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={(value) => { setSubjectType(value as RoleBindingSubjectType); setSubjectId(""); }}
            options={[
              { value: "user", label: t("settings.org.bindingsTab.subjectTypes.user") },
              { value: "department", label: t("settings.org.bindingsTab.subjectTypes.department") },
            ]}
            value={subjectType}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.subject")}>
          <Select disabled={Boolean(binding)} onValueChange={setSubjectId} options={subjectOptions} value={subjectId || subjectOptions[0]?.value || ""} />
        </Field>
        <Field label={t("settings.org.bindingsTab.role")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={setRoleId}
            options={(roles.data ?? []).map((role) => ({ value: role.id, label: role.name }))}
            value={roleId || (roles.data ?? [])[0]?.id || ""}
          />
        </Field>
        <Field label={t("settings.org.bindingsTab.scopeType")}>
          <Select
            disabled={Boolean(binding)}
            onValueChange={(value) => setScopeType(value as RoleBindingScopeType)}
            options={[
              { value: "enterprise", label: t("settings.org.bindingsTab.scopeTypes.enterprise") },
              { value: "project", label: t("settings.org.bindingsTab.scopeTypes.project") },
            ]}
            value={scopeType}
          />
        </Field>
        {scopeType === "project" && (
          <Field label={t("settings.org.bindingsTab.project")}>
            <Select
              disabled={Boolean(binding)}
              onValueChange={setProjectId}
              options={(projects.data ?? []).map((project) => ({ value: project.id, label: project.name }))}
              value={projectId || (projects.data ?? [])[0]?.id || ""}
            />
          </Field>
        )}
        <Field label={t("settings.org.bindingsTab.validFrom")}>
          <Input onChange={(event) => setValidFrom(event.target.value)} type="date" value={validFrom} />
        </Field>
        <Field label={t("settings.org.bindingsTab.validUntil")}>
          <Input onChange={(event) => setValidUntil(event.target.value)} type="date" value={validUntil} />
        </Field>
        <Switch checked={active} label={active ? t("settings.common.enabled") : t("settings.common.disabled")} onChange={setActive} />
      </div>
    </FormDrawer>
  );
}
