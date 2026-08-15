import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { DataScope, Environment } from "@argus/api-client";
import {
  Button,
  CheckItem,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Spinner,
  Switch,
  Textarea,
} from "@argus/ui";
import { useOrgDepartments, useOrgRoles, useOrgUsers } from "./org-users-tab";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

type ScopeRow = {
  id: string;
  name: string;
  subject: string;
  environments: Environment[];
  tagExpression: string;
  resourceIds: string[];
  onlyOwned: boolean;
  createdAt: string;
};

function parseIdList(text: string): string[] {
  return text
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function OrgScopesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const scopes = useQuery({
    queryKey: ["org", "dataScopes"],
    queryFn: () => api.org.listDataScopes(),
  });
  const roles = useOrgRoles();
  const departments = useOrgDepartments();
  const users = useOrgUsers();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<DataScope | null>(null);
  const [deleting, setDeleting] = useState<DataScope | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "dataScopes"] });

  const save = useMutation({
    mutationFn: (
      input: Omit<DataScope, "id" | "enterpriseId" | "createdAt"> & {
        id?: string;
      },
    ) => api.org.saveDataScope(input),
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.org.deleteDataScope(id),
    onSuccess: () => {
      setDeleting(null);
      void invalidate();
    },
  });

  const subjectLabel = (scope: DataScope) => {
    const name =
      scope.subjectType === "role"
        ? roles.data?.find((role) => role.id === scope.subjectId)?.name
        : scope.subjectType === "department"
          ? departments.data?.find((department) => department.id === scope.subjectId)?.name
          : users.data?.find((user) => user.id === scope.subjectId)
              ?.displayName;
    return `${t(`settings.org.scopesTab.subjectTypes.${scope.subjectType}`)} · ${name ?? scope.subjectId}`;
  };

  const rows: ScopeRow[] = (scopes.data ?? []).map((scope) => ({
    id: scope.id,
    name: scope.name,
    subject: subjectLabel(scope),
    environments: scope.environments,
    tagExpression: scope.tagExpression ?? "",
    resourceIds: scope.resourceIds ?? [],
    onlyOwned: scope.onlyOwned,
    createdAt: scope.createdAt,
  }));

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.scopes")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.org.scopesTab.create")}
        </Button>
      </div>
      {scopes.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("settings.org.scopesTab.empty")} />
      ) : (
        <DataTable<ScopeRow>
          columns={[
            { key: "name", header: t("settings.common.name") },
            { key: "subject", header: t("settings.org.scopesTab.subject") },
            {
              key: "environments",
              header: t("settings.org.scopesTab.environments"),
              render: (row) =>
                row.environments.length === 0
                  ? t("settings.common.all")
                  : row.environments
                      .map((env) => t(`settings.org.env.${env}`))
                      .join(", "),
            },
            {
              key: "tagExpression",
              header: t("settings.org.scopesTab.tagExpression"),
              render: (row) =>
                row.tagExpression ? (
                  <code className="mono">{row.tagExpression}</code>
                ) : (
                  "—"
                ),
            },
            {
              key: "resourceIds",
              header: t("settings.org.scopesTab.resourceIds"),
              render: (row) =>
                row.resourceIds.length === 0
                  ? "—"
                  : `${row.resourceIds.length}`,
            },
            {
              key: "onlyOwned",
              header: t("settings.org.scopesTab.onlyOwned"),
              render: (row) =>
                row.onlyOwned
                  ? t("settings.common.yes")
                  : t("settings.common.no"),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Button
                    onClick={() => {
                      setEditing(
                        scopes.data?.find((scope) => scope.id === row.id) ??
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
                        scopes.data?.find((scope) => scope.id === row.id) ??
                          null,
                      )
                    }
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.common.delete")}
                  </Button>
                </span>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <ScopeDrawer
        departments={(departments.data ?? []).map((department) => ({
          id: department.id,
          label: department.name,
        }))}
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutate({ ...input, id: editing?.id })}
        open={drawerOpen}
        roles={(roles.data ?? []).map((role) => ({
          id: role.id,
          label: role.name,
        }))}
        scope={editing}
        users={(users.data ?? []).map((user) => ({
          id: user.id,
          label: `${user.displayName} (@${user.username})`,
        }))}
      />

      <ConfirmDialog
        danger
        description={t("settings.org.scopesTab.deleteDescription")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        open={deleting !== null}
        title={`${t("settings.org.scopesTab.deleteTitle")} · ${deleting?.name ?? ""}`}
      />
    </div>
  );
}

function ScopeDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  scope,
  roles,
  departments,
  users,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (
    input: Omit<DataScope, "id" | "enterpriseId" | "createdAt">,
  ) => void;
  loading: boolean;
  scope: DataScope | null;
  roles: Array<{ id: string; label: string }>;
  departments: Array<{ id: string; label: string }>;
  users: Array<{ id: string; label: string }>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [subjectType, setSubjectType] =
    useState<DataScope["subjectType"]>("role");
  const [subjectId, setSubjectId] = useState("");
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [tagExpression, setTagExpression] = useState("");
  const [resourceIds, setResourceIds] = useState("");
  const [onlyOwned, setOnlyOwned] = useState(false);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);

  const key = open ? (scope?.id ?? "__new__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setName(scope?.name ?? "");
    setSubjectType(scope?.subjectType ?? "role");
    setSubjectId(scope?.subjectId ?? "");
    setEnvironments(scope?.environments ?? []);
    setTagExpression(scope?.tagExpression ?? "");
    setResourceIds((scope?.resourceIds ?? []).join("\n"));
    setOnlyOwned(scope?.onlyOwned ?? false);
  }

  const subjectOptions =
    subjectType === "role" ? roles : subjectType === "department" ? departments : users;

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          subjectType,
          subjectId,
          environments,
          tagExpression: tagExpression.trim() || undefined,
          resourceIds: parseIdList(resourceIds),
          onlyOwned,
        })
      }
      open={open}
      title={
        scope
          ? t("settings.org.scopesTab.editTitle")
          : t("settings.org.scopesTab.createTitle")
      }
    >
      <div className="argus-settings-form">
        <Field label={t("settings.common.name")}>
          <Input
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </Field>
        <Field label={t("settings.org.scopesTab.subjectType")}>
          <div className="argus-settings-check-row">
            {(["role", "department", "user"] as const).map((type) => (
              <span
                key={type}
                onClick={() => {
                  setSubjectType(type);
                  setSubjectId("");
                }}
                role="radio"
                aria-checked={subjectType === type}
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setSubjectType(type);
                    setSubjectId("");
                  }
                }}
              >
                <CheckItem checked={subjectType === type}>
                  {t(`settings.org.scopesTab.subjectTypes.${type}`)}
                </CheckItem>
              </span>
            ))}
          </div>
        </Field>
        <Field label={t("settings.org.scopesTab.subject")}>
          <Select
            onValueChange={setSubjectId}
            options={[
              { value: "", label: "—" },
              ...subjectOptions.map((option) => ({
                value: option.id,
                label: option.label,
              })),
            ]}
            value={subjectId}
          />
        </Field>
        <Field label={t("settings.org.scopesTab.environments")}>
          <div className="argus-settings-check-row">
            {ENVIRONMENTS.map((env) => (
              <span
                key={env}
                onClick={() =>
                  setEnvironments((prev) =>
                    prev.includes(env)
                      ? prev.filter((item) => item !== env)
                      : [...prev, env],
                  )
                }
                role="checkbox"
                aria-checked={environments.includes(env)}
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setEnvironments((prev) =>
                      prev.includes(env)
                        ? prev.filter((item) => item !== env)
                        : [...prev, env],
                    );
                  }
                }}
              >
                <CheckItem checked={environments.includes(env)}>
                  {t(`settings.org.env.${env}`)}
                </CheckItem>
              </span>
            ))}
          </div>
        </Field>
        <Field
          hint={t("settings.org.scopesTab.tagExpressionHint")}
          label={t("settings.org.scopesTab.tagExpression")}
        >
          <Input
            onChange={(event) => setTagExpression(event.target.value)}
            placeholder="criticality!=high"
            value={tagExpression}
          />
        </Field>
        <Field
          hint={t("settings.org.scopesTab.resourceIdsHint")}
          label={t("settings.org.scopesTab.resourceIds")}
        >
          <Textarea
            onChange={(event) => setResourceIds(event.target.value)}
            rows={3}
            value={resourceIds}
          />
        </Field>
        <Field label={t("settings.org.scopesTab.onlyOwned")}>
          <Switch
            checked={onlyOwned}
            label={t("settings.org.scopesTab.onlyOwned")}
            onChange={setOnlyOwned}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
