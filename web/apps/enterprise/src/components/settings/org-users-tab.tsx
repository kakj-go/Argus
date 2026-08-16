import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type Role,
  type RoleBinding,
  type User,
} from "@argus/api-client";
import type { EnterpriseUser } from "@argus/api-client/contracts";
import {
  Badge,
  Button,
  CheckItem,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "./shared";

export type UserRow = {
  id: string;
  username: string;
  displayName: string;
  email?: string;
  status: User["status"];
  lastLoginAt?: string;
  /** 由 user 主体且 active 的 RoleBinding 派生，仅用于展示。 */
  role_ids: string[];
  department_id: string;
};

function toRow(
  user: User,
  enterpriseUser: EnterpriseUser | null,
  bindings: RoleBinding[],
): UserRow {
  return {
    id: user.id,
    username: user.username,
    displayName: user.displayName,
    email: user.email,
    status: user.status,
    lastLoginAt: user.lastLoginAt,
    role_ids: bindings
      .filter(
        (binding) =>
          binding.subject_type === "user" &&
          binding.subject_id === user.id &&
          binding.status === "active",
      )
      .map((binding) => binding.role_id),
    department_id: enterpriseUser?.department_id ?? "",
  };
}

export function useOrgUsers() {
  const api = useApi();
  return useQuery({
    queryKey: ["org", "users"],
    queryFn: async () => {
      const [users, bindings] = await Promise.all([
        api.org.listUsers(),
        api.org.listRoleBindings(),
      ]);
      return Promise.all(
        users.map(async (user) =>
          toRow(user, await api.org.getEnterpriseUser(user.id), bindings),
        ),
      );
    },
  });
}

export function useOrgRoles() {
  const api = useApi();
  return useQuery({
    queryKey: ["org", "roles"],
    queryFn: () => api.org.listRoles(),
  });
}

export function useOrgDepartments() {
  const api = useApi();
  return useQuery({
    queryKey: ["org", "departments"],
    queryFn: () => api.org.listDepartments(),
  });
}

export function useOrgRoleBindings() {
  const api = useApi();
  return useQuery({
    queryKey: ["org", "role-bindings"],
    queryFn: () => api.org.listRoleBindings(),
  });
}

export function CheckList({
  options,
  value,
  onChange,
}: {
  options: Array<{ id: string; label: string }>;
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const selected = new Set(value);
  return (
    <div className="argus-settings-check-list">
      {options.map((option) => (
        <button
          className="argus-settings-check-option"
          key={option.id}
          onClick={() => {
            const next = new Set(selected);
            if (next.has(option.id)) next.delete(option.id);
            else next.add(option.id);
            onChange([...next]);
          }}
          type="button"
        >
          <CheckItem checked={selected.has(option.id)}>
            {option.label}
          </CheckItem>
        </button>
      ))}
    </div>
  );
}

export function OrgUsersTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const users = useOrgUsers();
  const roles = useOrgRoles();
  const departments = useOrgDepartments();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [editing, setEditing] = useState<UserRow | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });

  const invite = useMutation({
    mutationFn: (input: {
      username: string;
      display_name: string;
      email?: string;
      role_ids: string[];
      department_id: string;
    }) => api.org.inviteUser(input),
    onSuccess: () => {
      setInviteOpen(false);
      void invalidate();
    },
  });

  const save = useMutation({
    mutationFn: (input: { userId: string; department_id: string }) =>
      api.org.updateEnterpriseUser(input.userId, {
        department_id: input.department_id,
      }),
    onSuccess: () => {
      setEditing(null);
      void invalidate();
    },
  });

  const roleName = (id: string, list?: Role[]) =>
    list?.find((role) => role.id === id)?.name ?? id;
  const departmentName = (id: string) =>
    departments.data?.find((department) => department.id === id)?.name ?? id;

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.users")}
        </h2>
        <Button onClick={() => setInviteOpen(true)} size="sm" variant="primary">
          {t("settings.org.users.invite")}
        </Button>
      </div>
      {users.isPending ? (
        <Spinner />
      ) : (users.data ?? []).length === 0 ? (
        <EmptyState description="" title={t("settings.org.users.empty")} />
      ) : (
        <DataTable<UserRow>
          columns={[
            {
              key: "displayName",
              header: t("settings.common.name"),
              render: (row) => (
                <span>
                  {row.displayName} <small>@{row.username}</small>
                </span>
              ),
            },
            {
              key: "department_id",
              header: t("settings.org.users.department"),
              render: (row) => departmentName(row.department_id),
            },
            {
              key: "role_ids",
              header: t("settings.org.users.roles"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  {row.role_ids.map((id) => (
                    <Badge key={id}>{roleName(id, roles.data)}</Badge>
                  ))}
                </span>
              ),
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
              key: "lastLoginAt",
              header: t("settings.org.users.lastActive"),
              render: (row) =>
                row.lastLoginAt
                  ? formatDateTime(row.lastLoginAt)
                  : t("settings.common.never"),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <Button
                  onClick={() => setEditing(row)}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.common.edit")}
                </Button>
              ),
            },
          ]}
          data={users.data ?? []}
          getRowKey={(row) => row.id}
        />
      )}
      <EnterpriseUserDrawer
        departments={departments.data ?? []}
        loading={invite.isPending}
        onOpenChange={setInviteOpen}
        onSubmit={(input) => invite.mutate(input)}
        open={inviteOpen}
        roles={roles.data ?? []}
      />
      <EnterpriseUserDrawer
        departments={departments.data ?? []}
        editing={editing}
        loading={save.isPending}
        onOpenChange={(open) => !open && setEditing(null)}
        onSubmit={(input) =>
          editing &&
          save.mutate({
            userId: editing.id,
            department_id: input.department_id,
          })
        }
        open={editing !== null}
        roles={roles.data ?? []}
      />
    </div>
  );
}

function EnterpriseUserDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  roles,
  departments,
  editing,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    username: string;
    display_name: string;
    email?: string;
    role_ids: string[];
    department_id: string;
  }) => void;
  loading: boolean;
  roles: Role[];
  departments: Array<{ id: string; name: string }>;
  editing?: UserRow | null;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [role_ids, setRoleIds] = useState<string[]>([]);
  const [departmentId, setDepartmentId] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = open ? (editing?.id ?? "__new__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setUsername(editing?.username ?? "");
    setDisplayName(editing?.displayName ?? "");
    setEmail(editing?.email ?? "");
    setRoleIds(editing?.role_ids ?? []);
    setDepartmentId(editing?.department_id ?? departments[0]?.id ?? "");
  }
  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          username,
          display_name: displayName,
          email: email || undefined,
          role_ids,
          department_id: departmentId,
        })
      }
      open={open}
      title={
        editing
          ? t("settings.org.users.editTitle")
          : t("settings.org.users.inviteTitle")
      }
    >
      <div className="argus-settings-form">
        {!editing && (
          <>
            <Field label={t("settings.org.users.username")}>
              <Input
                onChange={(event) => setUsername(event.target.value)}
                required
                value={username}
              />
            </Field>
            <Field label={t("settings.org.users.displayName")}>
              <Input
                onChange={(event) => setDisplayName(event.target.value)}
                required
                value={displayName}
              />
            </Field>
            <Field label={t("settings.org.users.email")}>
              <Input
                onChange={(event) => setEmail(event.target.value)}
                type="email"
                value={email}
              />
            </Field>
          </>
        )}
        <Field label={t("settings.org.users.department")}>
          <Select
            onValueChange={setDepartmentId}
            options={departments.map((department) => ({
              value: department.id,
              label: department.name,
            }))}
            value={departmentId}
          />
        </Field>
        {!editing && (
          <Field label={t("settings.org.users.roles")}>
            <CheckList
              onChange={setRoleIds}
              options={roles.map((role) => ({ id: role.id, label: role.name }))}
              value={role_ids}
            />
          </Field>
        )}
      </div>
    </FormDrawer>
  );
}
