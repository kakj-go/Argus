import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { ApprovalPolicy, Environment, RiskLevel } from "@argus/api-client";
import {
  Badge,
  Button,
  CheckItem,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Spinner,
  StatusBadge,
  Switch,
  Textarea,
} from "@argus/ui";
import { useOrgRoles } from "./org-users-tab";

const RISK_LEVELS: RiskLevel[] = ["read", "write", "dangerous", "critical"];
const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

type PolicyRow = {
  id: string;
  name: string;
  riskLevels: RiskLevel[];
  environments: Environment[];
  minApprovers: number;
  approverRoleIds: string[];
  separationOfDuty: boolean;
  enabled: boolean;
};

export function OrgPoliciesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const policies = useQuery({
    queryKey: ["org", "approvalPolicies"],
    queryFn: () => api.org.listApprovalPolicies(),
  });
  const roles = useOrgRoles();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<ApprovalPolicy | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "approvalPolicies"] });

  const save = useMutation({
    mutationFn: (
      input: Omit<ApprovalPolicy, "id" | "enterpriseId" | "createdAt"> & {
        id?: string;
      },
    ) => api.org.saveApprovalPolicy(input),
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const roleName = (id: string) =>
    roles.data?.find((role) => role.id === id)?.name ?? id;

  const rows: PolicyRow[] = (policies.data ?? []).map((policy) => ({
    id: policy.id,
    name: policy.name,
    riskLevels: policy.matchRiskLevels,
    environments: policy.matchEnvironments,
    minApprovers: policy.minApprovers,
    approverRoleIds: policy.approverRoleIds,
    separationOfDuty: policy.separationOfDuty,
    enabled: policy.enabled,
  }));

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.policies")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.org.policiesTab.create")}
        </Button>
      </div>
      {policies.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState
          description=""
          title={t("settings.org.policiesTab.empty")}
        />
      ) : (
        <DataTable<PolicyRow>
          columns={[
            { key: "name", header: t("settings.common.name") },
            {
              key: "riskLevels",
              header: t("settings.org.policiesTab.matchRiskLevels"),
              render: (row) =>
                row.riskLevels.length === 0 ? (
                  t("settings.common.all")
                ) : (
                  <span className="argus-settings-inline-actions">
                    {row.riskLevels.map((level) => (
                      <Badge
                        key={level}
                        tone={
                          level === "critical"
                            ? "danger"
                            : level === "dangerous"
                              ? "warning"
                              : "neutral"
                        }
                      >
                        {t(`settings.org.policiesTab.riskLevels.${level}`)}
                      </Badge>
                    ))}
                  </span>
                ),
            },
            {
              key: "environments",
              header: t("settings.org.policiesTab.matchEnvironments"),
              render: (row) =>
                row.environments.length === 0
                  ? t("settings.common.all")
                  : row.environments
                      .map((env) => t(`settings.org.env.${env}`))
                      .join(", "),
            },
            {
              key: "minApprovers",
              header: t("settings.org.policiesTab.minApprovers"),
              render: (row) => String(row.minApprovers),
            },
            {
              key: "approverRoleIds",
              header: t("settings.org.policiesTab.approverRoles"),
              render: (row) =>
                row.approverRoleIds.map(roleName).join(", ") || "—",
            },
            {
              key: "enabled",
              header: t("settings.common.status"),
              render: (row) => (
                <StatusBadge tone={row.enabled ? "success" : "neutral"}>
                  {row.enabled
                    ? t("settings.common.enabled")
                    : t("settings.common.disabled")}
                </StatusBadge>
              ),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <Button
                  onClick={() => {
                    setEditing(
                      policies.data?.find((policy) => policy.id === row.id) ??
                        null,
                    );
                    setDrawerOpen(true);
                  }}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.common.edit")}
                </Button>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <PolicyDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutate({ ...input, id: editing?.id })}
        open={drawerOpen}
        policy={editing}
        roles={(roles.data ?? []).map((role) => ({
          id: role.id,
          label: role.name,
        }))}
      />
    </div>
  );
}

function toggleInList<T>(list: T[], item: T): T[] {
  return list.includes(item)
    ? list.filter((entry) => entry !== item)
    : [...list, item];
}

function PolicyDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  policy,
  roles,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (
    input: Omit<ApprovalPolicy, "id" | "enterpriseId" | "createdAt">,
  ) => void;
  loading: boolean;
  policy: ApprovalPolicy | null;
  roles: Array<{ id: string; label: string }>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [riskLevels, setRiskLevels] = useState<RiskLevel[]>([]);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [minApprovers, setMinApprovers] = useState(1);
  const [approverRoleIds, setApproverRoleIds] = useState<string[]>([]);
  const [separationOfDuty, setSeparationOfDuty] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [loadedFor, setLoadedFor] = useState<string | null>(null);

  const key = open ? (policy?.id ?? "__new__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setName(policy?.name ?? "");
    setDescription(policy?.description ?? "");
    setRiskLevels(policy?.matchRiskLevels ?? []);
    setEnvironments(policy?.matchEnvironments ?? []);
    setMinApprovers(policy?.minApprovers ?? 1);
    setApproverRoleIds(policy?.approverRoleIds ?? []);
    setSeparationOfDuty(policy?.separationOfDuty ?? false);
    setEnabled(policy?.enabled ?? true);
  }

  const checkRow = <T,>(
    list: T[],
    setList: (next: T[]) => void,
    options: Array<{ value: T; label: string }>,
  ) => (
    <div className="argus-settings-check-row">
      {options.map((option) => (
        <span
          aria-checked={list.includes(option.value)}
          key={String(option.value)}
          onClick={() => setList(toggleInList(list, option.value))}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setList(toggleInList(list, option.value));
            }
          }}
          role="checkbox"
          tabIndex={0}
        >
          <CheckItem checked={list.includes(option.value)}>
            {option.label}
          </CheckItem>
        </span>
      ))}
    </div>
  );

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          description: description.trim() || undefined,
          matchRiskLevels: riskLevels,
          matchEnvironments: environments,
          minApprovers: Math.max(1, minApprovers),
          approverRoleIds,
          separationOfDuty,
          enabled,
        })
      }
      open={open}
      title={
        policy
          ? t("settings.org.policiesTab.editTitle")
          : t("settings.org.policiesTab.createTitle")
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
        <Field label={t("settings.common.description")}>
          <Textarea
            onChange={(event) => setDescription(event.target.value)}
            rows={2}
            value={description}
          />
        </Field>
        <Field label={t("settings.org.policiesTab.matchRiskLevels")}>
          {checkRow(
            riskLevels,
            setRiskLevels,
            RISK_LEVELS.map((level) => ({
              value: level,
              label: t(`settings.org.policiesTab.riskLevels.${level}`),
            })),
          )}
        </Field>
        <Field label={t("settings.org.policiesTab.matchEnvironments")}>
          {checkRow(
            environments,
            setEnvironments,
            ENVIRONMENTS.map((env) => ({
              value: env,
              label: t(`settings.org.env.${env}`),
            })),
          )}
        </Field>
        <Field label={t("settings.org.policiesTab.minApprovers")}>
          <Input
            min={1}
            onChange={(event) =>
              setMinApprovers(Number.parseInt(event.target.value, 10) || 1)
            }
            type="number"
            value={minApprovers}
          />
        </Field>
        <Field label={t("settings.org.policiesTab.approverRoles")}>
          {checkRow(
            approverRoleIds,
            setApproverRoleIds,
            roles.map((role) => ({ value: role.id, label: role.label })),
          )}
        </Field>
        <Field label={t("settings.org.policiesTab.separationOfDuty")}>
          <Switch
            checked={separationOfDuty}
            label={t("settings.org.policiesTab.separationOfDuty")}
            onChange={setSeparationOfDuty}
          />
        </Field>
        <Field label={t("settings.common.enabled")}>
          <Switch
            checked={enabled}
            label={t("settings.common.enabled")}
            onChange={setEnabled}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
