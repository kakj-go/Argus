import { useMutation, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { presentApiFormError, useApi } from "@argus/api-client";
import type { Role } from "@argus/api-client";
import {
  ActionGroup,
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  RowAction,
  Spinner,
  Tooltip,
} from "@argus/ui";
import { roleDisplayName } from "../../lib/role-presentation";
import { formatDateTime, PermissionMatrix } from "./shared";
import { useOrgRoles } from "./org-users-tab";
import { DataAuthorizationDialog } from "./data-authorization-dialog";

type RoleRow = {
  id: string;
  name: string;
  description: string;
  builtin: boolean;
  permissionCount: number;
  createdAt: string;
};

export function OrgRolesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const roles = useOrgRoles();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [authorizationTarget, setAuthorizationTarget] = useState<Role | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "roles"] });

  const save = useMutation({
    mutationFn: (input: {
      id?: string;
      name: string;
      description?: string;
      permissions: string[];
    }) =>
      input.id
        ? api.org.updateRole(input.id, {
            name: input.name,
            description: input.description,
            permissions: input.permissions,
          })
        : api.org.createRole({
            name: input.name,
            description: input.description,
            permissions: input.permissions,
          }),
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.org.deleteRole(id),
    onSuccess: () => {
      setDeleting(null);
      void invalidate();
    },
  });

  const rows: RoleRow[] = (roles.data ?? []).map((role) => ({
    id: role.id,
    name: roleDisplayName(role, t),
    description: role.description ?? "",
    builtin: role.builtin,
    permissionCount: role.permissions.includes("*")
      ? -1
      : role.permissions.length,
    createdAt: role.created_at,
  }));

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.roles")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.org.rolesTab.create")}
        </Button>
      </div>
      {roles.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("settings.org.rolesTab.empty")} />
      ) : (
        <DataTable<RoleRow>
          columns={[
            {
              key: "name",
              header: t("settings.common.name"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  {row.name}
                  {row.builtin && (
                    <Badge tone="accent">{t("settings.common.builtin")}</Badge>
                  )}
                </span>
              ),
            },
            { key: "description", header: t("settings.common.description") },
            {
              key: "permissionCount",
              header: t("settings.org.rolesTab.permissions"),
              render: (row) =>
                row.permissionCount < 0 ? "*" : String(row.permissionCount),
            },
            {
              key: "createdAt",
              header: t("settings.common.createdAt"),
              render: (row) => formatDateTime(row.createdAt),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <ActionGroup>
                  <RowAction onClick={() => setAuthorizationTarget(roles.data?.find((role) => role.id === row.id) ?? null)}>数据授权</RowAction>
                  {!row.builtin && (
                    <>
                      <RowAction
                        onClick={() => {
                          setEditing(
                            roles.data?.find((role) => role.id === row.id) ??
                              null,
                          );
                          setDrawerOpen(true);
                        }}
                      >
                        {t("settings.common.edit")}
                      </RowAction>
                      <RowAction
                        danger
                        onClick={() =>
                          setDeleting(
                            roles.data?.find((role) => role.id === row.id) ??
                              null,
                          )
                        }
                      >
                        {t("settings.common.delete")}
                      </RowAction>
                    </>
                  )}
                  {row.builtin && (
                    <Tooltip content={t("settings.org.rolesTab.builtinLocked")}>
                      <span>
                        <RowAction disabled danger>
                          {t("settings.common.delete")}
                        </RowAction>
                      </span>
                    </Tooltip>
                  )}
                </ActionGroup>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}
      <DataAuthorizationDialog open={authorizationTarget !== null} onOpenChange={(open) => !open && setAuthorizationTarget(null)} subjectType="role" subjectId={authorizationTarget?.id ?? ""} subjectLabel={authorizationTarget?.name ?? ""} />

      <RoleDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutateAsync({ ...input, id: editing?.id })}
        open={drawerOpen}
        role={editing}
      />

      <ConfirmDialog
        danger
        description={t("settings.org.rolesTab.deleteDescription")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        open={deleting !== null}
        title={`${t("settings.org.rolesTab.deleteTitle")} · ${deleting?.name ?? ""}`}
      />
    </div>
  );
}

function RoleDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  role,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    name: string;
    description?: string;
    permissions: string[];
  }) => Promise<unknown>;
  loading: boolean;
  role: Role | null;
}) {
  const { t } = useTranslation();
  const roleSchema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t("settings.common.required")),
        description: z.string().trim(),
        permissions: z.array(z.string()),
      }),
    [t],
  );
  type RoleForm = z.infer<typeof roleSchema>;
  const {
    control,
    clearErrors,
    register,
    reset,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<RoleForm>({
    resolver: zodResolver(roleSchema),
    defaultValues: { name: "", description: "", permissions: [] },
  });

  useEffect(() => {
    if (!open) return;
    reset({
      name: role?.name ?? "",
      description: role?.description ?? "",
      permissions: role?.permissions ?? [],
    });
  }, [open, reset, role]);
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        name: values.name,
        description: values.description || undefined,
        permissions: values.permissions,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          description: "description",
          name: "name",
          permissions: "permissions",
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
        role
          ? t("settings.org.rolesTab.editTitle")
          : t("settings.org.rolesTab.createTitle")
      }
      width={640}
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          requirement="required"
          error={errors.name?.message}
          label={t("settings.common.name")}
        >
          <Input {...register("name")} required />
        </Field>
        <Field requirement="optional" label={t("settings.common.description")}>
          <Input {...register("description")} />
        </Field>
        <Field
          requirement="optional"
          hint={t("settings.org.rolesTab.permissionHint")}
          label={t("settings.org.rolesTab.permissions")}
        >
          <Controller
            control={control}
            name="permissions"
            render={({ field }) => (
              <PermissionMatrix onChange={field.onChange} value={field.value} />
            )}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
