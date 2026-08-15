import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { Role } from "@argus/api-client";
import {
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Spinner,
  Tooltip,
} from "@argus/ui";
import { formatDateTime, PermissionMatrix } from "./shared";
import { useOrgRoles } from "./org-users-tab";

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
    name: role.name,
    description: role.description ?? "",
    builtin: role.builtin,
    permissionCount: role.permissions.includes("*")
      ? -1
      : role.permissions.length,
    createdAt: role.createdAt,
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
                <span className="argus-settings-inline-actions">
                  {!row.builtin && (
                    <>
                      <Button
                        onClick={() => {
                          setEditing(
                            roles.data?.find((role) => role.id === row.id) ??
                              null,
                          );
                          setDrawerOpen(true);
                        }}
                        size="sm"
                        variant="ghost"
                      >
                        {t("settings.common.edit")}
                      </Button>
                      <Button
                        onClick={() =>
                          setDeleting(
                            roles.data?.find((role) => role.id === row.id) ??
                              null,
                          )
                        }
                        size="sm"
                        variant="ghost"
                      >
                        {t("settings.common.delete")}
                      </Button>
                    </>
                  )}
                  {row.builtin && (
                    <Tooltip content={t("settings.org.rolesTab.builtinLocked")}>
                      <span>
                        <Button disabled size="sm" variant="ghost">
                          {t("settings.common.delete")}
                        </Button>
                      </span>
                    </Tooltip>
                  )}
                </span>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <RoleDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutate({ ...input, id: editing?.id })}
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
  }) => void;
  loading: boolean;
  role: Role | null;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [permissions, setPermissions] = useState<string[]>([]);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);

  const key = open ? (role?.id ?? "__new__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setName(role?.name ?? "");
    setDescription(role?.description ?? "");
    setPermissions(role?.permissions ?? []);
  }

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          description: description.trim() || undefined,
          permissions,
        })
      }
      open={open}
      title={
        role
          ? t("settings.org.rolesTab.editTitle")
          : t("settings.org.rolesTab.createTitle")
      }
      width={640}
    >
      <div className="argus-settings-form">
        <Field label={t("settings.common.name")}>
          <Input
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </Field>
        <Field label={t("settings.common.description")}>
          <Input
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
        </Field>
        <Field
          hint={t("settings.org.rolesTab.permissionHint")}
          label={t("settings.org.rolesTab.permissions")}
        >
          <PermissionMatrix onChange={setPermissions} value={permissions} />
        </Field>
      </div>
    </FormDrawer>
  );
}
