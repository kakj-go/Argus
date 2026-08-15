import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { CreatedApiKey, ServiceAccount } from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "./shared";
import { CheckList, useOrgRoles } from "./org-users-tab";

type SaRow = {
  id: string;
  name: string;
  description: string;
  roleIds: string[];
  status: ServiceAccount["status"];
  lastUsedAt?: string;
};

export function OrgAccessTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const roles = useOrgRoles();
  const serviceAccounts = useQuery({
    queryKey: ["org", "serviceAccounts"],
    queryFn: () => api.org.listServiceAccounts(),
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [keysFor, setKeysFor] = useState<ServiceAccount | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "serviceAccounts"] });

  const create = useMutation({
    mutationFn: (input: {
      name: string;
      description?: string;
      roleIds: string[];
    }) => api.org.createServiceAccount(input),
    onSuccess: () => {
      setCreateOpen(false);
      void invalidate();
    },
  });

  const toggleStatus = useMutation({
    mutationFn: (account: ServiceAccount) =>
      api.org.updateServiceAccount(account.id, {
        status: account.status === "active" ? "disabled" : "active",
      }),
    onSuccess: () => void invalidate(),
  });

  const roleName = (id: string) =>
    roles.data?.find((role) => role.id === id)?.name ?? id;

  const rows: SaRow[] = (serviceAccounts.data ?? []).map((account) => ({
    id: account.id,
    name: account.name,
    description: account.description ?? "",
    roleIds: account.roleIds,
    status: account.status,
    lastUsedAt: account.lastUsedAt,
  }));

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <div>
          <h2 className="argus-settings-section__title">
            {t("settings.org.tabs.access")}
          </h2>
          <p className="argus-settings-section__hint">
            {t("settings.org.accessTab.saDescription")}
          </p>
        </div>
        <Button
          onClick={() => setCreateOpen(true)}
          size="sm"
          variant="primary"
        >
          {t("settings.org.accessTab.createSa")}
        </Button>
      </div>
      {serviceAccounts.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("settings.org.accessTab.empty")} />
      ) : (
        <DataTable<SaRow>
          columns={[
            { key: "name", header: t("settings.common.name") },
            { key: "description", header: t("settings.common.description") },
            {
              key: "roleIds",
              header: t("settings.org.users.roles"),
              render: (row) =>
                row.roleIds.length === 0 ? (
                  "—"
                ) : (
                  <span className="argus-settings-inline-actions">
                    {row.roleIds.map((id) => (
                      <Badge key={id}>{roleName(id)}</Badge>
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
                  {row.status === "active"
                    ? t("settings.common.active")
                    : t("settings.common.disabled")}
                </StatusBadge>
              ),
            },
            {
              key: "lastUsedAt",
              header: t("settings.org.accessTab.lastUsedAt"),
              render: (row) =>
                row.lastUsedAt
                  ? formatDateTime(row.lastUsedAt)
                  : t("settings.common.never"),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Button
                    onClick={() =>
                      setKeysFor(
                        serviceAccounts.data?.find(
                          (account) => account.id === row.id,
                        ) ?? null,
                      )
                    }
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.org.accessTab.apiKeys")}
                  </Button>
                  <Button
                    onClick={() => {
                      const account = serviceAccounts.data?.find(
                        (entry) => entry.id === row.id,
                      );
                      if (account) toggleStatus.mutate(account);
                    }}
                    size="sm"
                    variant="ghost"
                  >
                    {row.status === "active"
                      ? t("settings.org.accessTab.disableSa")
                      : t("settings.org.accessTab.enableSa")}
                  </Button>
                </span>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <CreateSaDrawer
        loading={create.isPending}
        onOpenChange={setCreateOpen}
        onSubmit={(input) => create.mutate(input)}
        open={createOpen}
        roles={(roles.data ?? []).map((role) => ({
          id: role.id,
          label: role.name,
        }))}
      />

      {keysFor && (
        <ApiKeysDrawer
          account={keysFor}
          onOpenChange={(open) => {
            if (!open) setKeysFor(null);
          }}
        />
      )}
    </div>
  );
}

function CreateSaDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  roles,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    name: string;
    description?: string;
    roleIds: string[];
  }) => void;
  loading: boolean;
  roles: Array<{ id: string; label: string }>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [roleIds, setRoleIds] = useState<string[]>([]);

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          description: description.trim() || undefined,
          roleIds,
        })
      }
      open={open}
      title={t("settings.org.accessTab.createSaTitle")}
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
        <Field label={t("settings.org.users.roles")}>
          <CheckList onChange={setRoleIds} options={roles} value={roleIds} />
        </Field>
      </div>
    </FormDrawer>
  );
}

function ApiKeysDrawer({
  account,
  onOpenChange,
}: {
  account: ServiceAccount;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const keys = useQuery({
    queryKey: ["org", "apiKeys", account.id],
    queryFn: () => api.org.listApiKeys(account.id),
  });
  const [keyName, setKeyName] = useState("");
  const [scopes, setScopes] = useState("");
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ["org", "apiKeys", account.id],
    });

  const create = useMutation({
    mutationFn: () =>
      api.org.createApiKey(account.id, {
        name: keyName.trim(),
        scopes: scopes
          .split(/[,\n]+/)
          .map((scope) => scope.trim())
          .filter(Boolean),
      }),
    onSuccess: (result) => {
      setCreated(result);
      setKeyName("");
      setScopes("");
      void invalidate();
    },
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.org.revokeApiKey(id),
    onSuccess: () => {
      setRevoking(null);
      void invalidate();
    },
  });

  const activeKeys = (keys.data ?? []).filter(
    (key) => key.status === "active",
  );

  return (
    <FormDrawer
      footer={<></>}
      onOpenChange={onOpenChange}
      open
      title={`${t("settings.org.accessTab.apiKeysTitle")} · ${account.name}`}
    >
      <div className="argus-settings-form">
        {created && (
          <div className="argus-settings-section">
            <Alert
              description={t("settings.org.accessTab.keyCreatedWarning")}
              title={t("settings.org.accessTab.keyCreatedTitle")}
              tone="warning"
            />
            <CodeBlock code={created.secret} language="apikey" />
          </div>
        )}
        {keys.isPending ? (
          <Spinner />
        ) : activeKeys.length === 0 ? (
          <p className="argus-settings-section__hint">
            {t("settings.org.accessTab.noKeys")}
          </p>
        ) : (
          <div className="argus-settings-key-list">
            {activeKeys.map((key) => (
              <div className="argus-settings-key-row" key={key.id}>
                <div className="argus-settings-key-row__meta">
                  <span>
                    {key.name}{" "}
                    <Badge tone="accent">{key.prefix}…</Badge>
                  </span>
                  <small>
                    {key.scopes.join(", ") || "—"} ·{" "}
                    {t("settings.org.accessTab.lastUsedAt")}:{" "}
                    {key.lastUsedAt
                      ? formatDateTime(key.lastUsedAt)
                      : t("settings.common.never")}
                  </small>
                </div>
                <Button
                  onClick={() => setRevoking(key.id)}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.org.accessTab.revoke")}
                </Button>
              </div>
            ))}
          </div>
        )}
        <Field label={t("settings.org.accessTab.keyName")}>
          <Input
            onChange={(event) => setKeyName(event.target.value)}
            value={keyName}
          />
        </Field>
        <Field
          hint={t("settings.org.accessTab.scopesHint")}
          label={t("settings.org.accessTab.scopes")}
        >
          <Input
            onChange={(event) => setScopes(event.target.value)}
            placeholder="host.read, telemetry.read"
            value={scopes}
          />
        </Field>
        <Button
          disabled={!keyName.trim()}
          loading={create.isPending}
          onClick={() => create.mutate()}
          variant="primary"
        >
          {t("settings.org.accessTab.createKey")}
        </Button>
      </div>

      <ConfirmDialog
        danger
        description={t("settings.org.accessTab.revokeDescription")}
        loading={revoke.isPending}
        onConfirm={() => revoking && revoke.mutate(revoking)}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
        }}
        open={revoking !== null}
        title={t("settings.org.accessTab.revokeTitle")}
      />
    </FormDrawer>
  );
}
