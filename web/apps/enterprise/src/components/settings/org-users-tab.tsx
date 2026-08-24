import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  presentApiFormError,
  useApi,
  type Role,
  type RoleBinding,
  type User,
} from "@argus/api-client";
import type { EnterpriseUser } from "@argus/api-client/contracts";
import {
  Alert,
  Badge,
  Button,
  CheckItem,
  CodeBlock,
  ConfirmDialog,
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
  const [created, setCreated] = useState<User | null>(null);
  const [statusTarget, setStatusTarget] = useState<UserRow | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });

  const invite = useMutation({
    mutationFn: (input: {
      username: string;
      display_name: string;
      email?: string;
      role_ids: string[];
      department_id: string;
    }) => api.org.inviteUser(input),
    onSuccess: (user) => {
      setInviteOpen(false);
      setCreated(user);
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

  const toggleStatus = useMutation({
    mutationFn: (user: UserRow) =>
      api.org.updateEnterpriseUser(user.id, {
        status: user.status === "disabled" ? "active" : "disabled",
      }),
    onSuccess: () => {
      setStatusTarget(null);
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
                <span className="argus-settings-inline-actions">
                  <Button
                    onClick={() => setEditing(row)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.common.edit")}
                  </Button>
                  <Button
                    onClick={() => setStatusTarget(row)}
                    size="sm"
                    variant="ghost"
                  >
                    {row.status === "disabled"
                      ? t("settings.org.users.enable")
                      : t("settings.org.users.disable")}
                  </Button>
                </span>
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
        onSubmit={(input) => invite.mutateAsync(input)}
        open={inviteOpen}
        roles={roles.data ?? []}
      />
      <FormDrawer
        footer={
          <Button onClick={() => setCreated(null)} variant="primary">
            {t("settings.common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setCreated(null);
        }}
        open={created !== null}
        title={t("settings.org.users.credentialTitle")}
      >
        {created && (
          <div className="argus-settings-form">
            <Alert
              description={t("settings.org.users.credentialDescription")}
              title={t("settings.org.users.credentialTitle")}
              tone="warning"
            />
            <CodeBlock code={created.temporaryPassword ?? ""} language="text" />
          </div>
        )}
      </FormDrawer>
      <EnterpriseUserDrawer
        departments={departments.data ?? []}
        editing={editing}
        loading={save.isPending}
        onOpenChange={(open) => !open && setEditing(null)}
        onSubmit={(input) =>
          editing
            ? save.mutateAsync({
                userId: editing.id,
                department_id: input.department_id,
              })
            : Promise.resolve()
        }
        open={editing !== null}
        roles={roles.data ?? []}
      />
      <ConfirmDialog
        danger={statusTarget?.status !== "disabled"}
        description={
          statusTarget?.status === "disabled"
            ? t("settings.org.users.enableDescription")
            : t("settings.org.users.disableDescription")
        }
        loading={toggleStatus.isPending}
        onConfirm={() => statusTarget && toggleStatus.mutate(statusTarget)}
        onOpenChange={(open) => !open && setStatusTarget(null)}
        open={statusTarget !== null}
        title={
          statusTarget?.status === "disabled"
            ? t("settings.org.users.enableTitle")
            : t("settings.org.users.disableTitle")
        }
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
  }) => Promise<unknown>;
  loading: boolean;
  roles: Role[];
  departments: Array<{ id: string; name: string }>;
  editing?: UserRow | null;
}) {
  const { t } = useTranslation();
  const userSchema = useMemo(
    () =>
      z.object({
        username: z.string().trim().min(3, t("settings.common.required")),
        display_name: z.string().trim().min(1, t("settings.common.required")),
        email: z
          .string()
          .trim()
          .refine(
            (value) => value === "" || z.email().safeParse(value).success,
            t("settings.common.emailInvalid"),
          ),
        role_ids: z.array(z.string()),
        department_id: z.string().min(1, t("settings.common.required")),
      }),
    [t],
  );
  type UserForm = z.infer<typeof userSchema>;
  const {
    clearErrors,
    control,
    register,
    reset,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<UserForm>({
    resolver: zodResolver(userSchema),
    defaultValues: {
      username: "",
      display_name: "",
      email: "",
      role_ids: [],
      department_id: "",
    },
  });

  useEffect(() => {
    if (!open) return;
    reset({
      username: editing?.username ?? "",
      display_name: editing?.displayName ?? "",
      email: editing?.email ?? "",
      role_ids: editing?.role_ids ?? [],
      department_id: editing?.department_id ?? departments[0]?.id ?? "",
    });
  }, [departments, editing, open, reset]);
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        ...values,
        email: values.email || undefined,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          department_id: "department_id",
          display_name: "display_name",
          email: "email",
          role_ids: "role_ids",
          username: "username",
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
      onOpenChange={onOpenChange}
      onSubmit={submit}
      open={open}
      title={
        editing
          ? t("settings.org.users.editTitle")
          : t("settings.org.users.inviteTitle")
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
        {!editing && (
          <>
            <Field
              requirement="required"
              error={errors.username?.message}
              label={t("settings.org.users.username")}
            >
              <Input {...register("username")} required />
            </Field>
            <Field
              requirement="required"
              error={errors.display_name?.message}
              label={t("settings.org.users.displayName")}
            >
              <Input {...register("display_name")} required />
            </Field>
            <Field
              requirement="optional"
              error={errors.email?.message}
              label={t("settings.org.users.email")}
            >
              <Input {...register("email")} type="email" />
            </Field>
          </>
        )}
        <Field
          requirement="required"
          error={errors.department_id?.message}
          label={t("settings.org.users.department")}
        >
          <Controller
            control={control}
            name="department_id"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={departments.map((department) => ({
                  value: department.id,
                  label: department.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        {!editing && (
          <Field requirement="optional" label={t("settings.org.users.roles")}>
            <Controller
              control={control}
              name="role_ids"
              render={({ field }) => (
                <CheckList
                  onChange={field.onChange}
                  options={roles.map((role) => ({
                    id: role.id,
                    label: role.name,
                  }))}
                  value={field.value}
                />
              )}
            />
          </Field>
        )}
      </div>
    </FormDrawer>
  );
}
