import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type DataScope,
  type SaveDataScopeInput,
} from "@argus/api-client";
import {
  Badge,
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
  StatusBadge,
  Textarea,
} from "@argus/ui";

type ResourceType = DataScope["resource_types"][number];
type LabelRequirement = NonNullable<
  DataScope["label_selector"]
>["requirements"][number];
type RequirementOperator = LabelRequirement["operator"];

type ScopeRow = {
  id: string;
  name: string;
  description: string;
  resource_types: ResourceType[];
  explicit_resource_ids: string[];
  selector_summary: string;
  status: DataScope["status"];
};

type RequirementDraft = {
  id: number;
  key: string;
  operator: RequirementOperator;
  values: string;
};

const RESOURCE_TYPES: ResourceType[] = ["host", "kubernetes_cluster"];
const OPERATORS: RequirementOperator[] = ["eq", "in", "exists", "not_exists"];
let nextRequirementId = 1;

function createRequirementDraft(
  requirement?: LabelRequirement,
): RequirementDraft {
  return {
    id: nextRequirementId++,
    key: requirement?.key ?? "",
    operator: requirement?.operator ?? "eq",
    values:
      requirement && "values" in requirement
        ? requirement.values.join(", ")
        : "",
  };
}

function parseList(text: string): string[] {
  return [
    ...new Set(
      text
        .split(/[\n,]+/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  ];
}

function toRequirement(draft: RequirementDraft): LabelRequirement | null {
  const key = draft.key.trim();
  if (!key) return null;
  if (draft.operator === "exists" || draft.operator === "not_exists") {
    return { key, operator: draft.operator };
  }
  return { key, operator: draft.operator, values: parseList(draft.values) };
}

function selectorSummary(scope: DataScope): string {
  const requirements = scope.label_selector?.requirements ?? [];
  return requirements
    .map((requirement) => {
      if (!("values" in requirement)) {
        return `${requirement.key} ${requirement.operator}`;
      }
      return `${requirement.key} ${requirement.operator} (${requirement.values.join(", ")})`;
    })
    .join(" AND ");
}

export function OrgScopesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const scopes = useQuery({
    queryKey: ["org", "dataScopes"],
    queryFn: () => api.org.listDataScopes(),
  });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<DataScope | null>(null);
  const [deleting, setDeleting] = useState<DataScope | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "dataScopes"] });

  const save = useMutation({
    mutationFn: (input: SaveDataScopeInput) => api.org.saveDataScope(input),
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

  const rows: ScopeRow[] = (scopes.data ?? []).map((scope) => ({
    id: scope.id,
    name: scope.name,
    description: scope.description ?? "",
    resource_types: scope.resource_types,
    explicit_resource_ids: scope.explicit_resource_ids,
    selector_summary: selectorSummary(scope),
    status: scope.status,
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
            { key: "description", header: t("settings.common.description") },
            {
              key: "resource_types",
              header: t("settings.org.scopesTab.resourceTypes"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  {row.resource_types.map((type) => (
                    <Badge key={type}>
                      {t(`settings.org.scopesTab.resourceType.${type}`)}
                    </Badge>
                  ))}
                </span>
              ),
            },
            {
              key: "selector_summary",
              header: t("settings.org.scopesTab.labelSelector"),
              render: (row) =>
                row.selector_summary ? (
                  <code>{row.selector_summary}</code>
                ) : (
                  "—"
                ),
            },
            {
              key: "explicit_resource_ids",
              header: t("settings.org.scopesTab.resourceIds"),
              render: (row) =>
                row.explicit_resource_ids.length === 0
                  ? "—"
                  : String(row.explicit_resource_ids.length),
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
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutate({ ...input, id: editing?.id })}
        open={drawerOpen}
        scope={editing}
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
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: Omit<SaveDataScopeInput, "id">) => void;
  loading: boolean;
  scope: DataScope | null;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [resourceTypes, setResourceTypes] = useState<ResourceType[]>([]);
  const [resourceIds, setResourceIds] = useState("");
  const [requirements, setRequirements] = useState<RequirementDraft[]>([]);
  const [status, setStatus] = useState<DataScope["status"]>("active");

  useEffect(() => {
    if (!open) return;
    setName(scope?.name ?? "");
    setDescription(scope?.description ?? "");
    setResourceTypes(scope?.resource_types ?? []);
    setResourceIds((scope?.explicit_resource_ids ?? []).join("\n"));
    setRequirements(
      (scope?.label_selector?.requirements ?? []).map(createRequirementDraft),
    );
    setStatus(scope?.status ?? "active");
  }, [open, scope]);

  const updateRequirement = (
    id: number,
    patch: Partial<Omit<RequirementDraft, "id">>,
  ) => {
    setRequirements((current) =>
      current.map((requirement) =>
        requirement.id === id ? { ...requirement, ...patch } : requirement,
      ),
    );
  };

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() => {
        const normalizedRequirements = requirements
          .map(toRequirement)
          .filter(
            (requirement): requirement is LabelRequirement =>
              requirement !== null,
          );
        onSubmit({
          name: name.trim(),
          description: description.trim() || undefined,
          resource_types: resourceTypes,
          explicit_resource_ids: parseList(resourceIds),
          label_selector:
            normalizedRequirements.length > 0
              ? {
                  schema_version: "argus.label_selector/v1",
                  requirements: normalizedRequirements,
                }
              : undefined,
          status,
        });
      }}
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
        <Field label={t("settings.common.description")}>
          <Textarea
            onChange={(event) => setDescription(event.target.value)}
            rows={2}
            value={description}
          />
        </Field>
        <Field label={t("settings.org.scopesTab.resourceTypes")}>
          <div className="argus-settings-check-list">
            {RESOURCE_TYPES.map((resourceType) => (
              <button
                className="argus-settings-check-option"
                key={resourceType}
                onClick={() =>
                  setResourceTypes((current) =>
                    current.includes(resourceType)
                      ? current.filter((type) => type !== resourceType)
                      : [...current, resourceType],
                  )
                }
                type="button"
              >
                <CheckItem checked={resourceTypes.includes(resourceType)}>
                  {t(`settings.org.scopesTab.resourceType.${resourceType}`)}
                </CheckItem>
              </button>
            ))}
          </div>
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
        <Field
          hint={t("settings.org.scopesTab.labelSelectorHint")}
          label={t("settings.org.scopesTab.labelSelector")}
        >
          <div className="argus-settings-selector-list">
            {requirements.map((requirement) => (
              <div className="argus-settings-selector-row" key={requirement.id}>
                <Input
                  aria-label={t("settings.org.scopesTab.labelKey")}
                  maxLength={63}
                  onChange={(event) =>
                    updateRequirement(requirement.id, {
                      key: event.target.value,
                    })
                  }
                  pattern="[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*"
                  placeholder={t("settings.org.scopesTab.labelKey")}
                  value={requirement.key}
                />
                <Select
                  aria-label={t("settings.org.scopesTab.operator")}
                  onValueChange={(operator) =>
                    updateRequirement(requirement.id, {
                      operator: operator as RequirementOperator,
                    })
                  }
                  options={OPERATORS.map((operator) => ({
                    value: operator,
                    label: operator,
                  }))}
                  value={requirement.operator}
                />
                {requirement.operator === "eq" ||
                requirement.operator === "in" ? (
                  <Input
                    aria-label={t("settings.org.scopesTab.labelValues")}
                    onChange={(event) =>
                      updateRequirement(requirement.id, {
                        values: event.target.value,
                      })
                    }
                    placeholder={t("settings.org.scopesTab.labelValues")}
                    required
                    value={requirement.values}
                  />
                ) : (
                  <span aria-hidden="true" />
                )}
                <Button
                  onClick={() =>
                    setRequirements((current) =>
                      current.filter((item) => item.id !== requirement.id),
                    )
                  }
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.common.delete")}
                </Button>
              </div>
            ))}
            <Button
              disabled={requirements.length >= 16}
              onClick={() =>
                setRequirements((current) => [
                  ...current,
                  createRequirementDraft(),
                ])
              }
              size="sm"
              variant="ghost"
            >
              {t("settings.org.scopesTab.addRequirement")}
            </Button>
          </div>
        </Field>
        <Field label={t("settings.common.status")}>
          <Select
            onValueChange={(value) => setStatus(value as DataScope["status"])}
            options={[
              { value: "active", label: t("settings.common.active") },
              { value: "disabled", label: t("settings.common.disabled") },
            ]}
            value={status}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
