import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  formConstraint,
  formValueConstraint,
  presentApiFormError,
  useApi,
  type DataScope,
  type SaveDataScopeInput,
} from "@argus/api-client";
import {
  Alert,
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
  key: string;
  operator: RequirementOperator;
  values: string;
};

const RESOURCE_TYPES: ResourceType[] = ["host", "kubernetes_cluster"];
const OPERATORS: RequirementOperator[] = ["eq", "in", "exists", "not_exists"];
const labelKeyConstraint = formValueConstraint("UserLabelKey");
const labelValueConstraint = formValueConstraint("LabelValue");
const LABEL_KEY_PATTERN = new RegExp(labelKeyConstraint.pattern ?? "(?!)");
const LABEL_VALUE_PATTERN = new RegExp(labelValueConstraint.pattern ?? "(?!)");
const scopeConstraints = {
  description: formConstraint("DataScopeCreate", "description"),
  name: formConstraint("DataScopeCreate", "name"),
  requirements: formConstraint("LabelSelector", "requirements"),
  resourceTypes: formConstraint("DataScopeCreate", "resource_types"),
};
function createRequirementDraft(
  requirement?: LabelRequirement,
): RequirementDraft {
  return {
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
        onSubmit={(input) => save.mutateAsync({ ...input, id: editing?.id })}
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
  onSubmit: (input: Omit<SaveDataScopeInput, "id">) => Promise<unknown>;
  loading: boolean;
  scope: DataScope | null;
}) {
  const { t } = useTranslation();
  const scopeSchema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(
            scopeConstraints.name.minLength ?? 1,
            t("settings.common.required"),
          )
          .max(scopeConstraints.name.maxLength ?? 128),
        description: z
          .string()
          .trim()
          .max(scopeConstraints.description.maxLength ?? 1024),
        resource_types: z
          .array(z.enum(RESOURCE_TYPES))
          .min(
            scopeConstraints.resourceTypes.minItems ?? 1,
            t("settings.org.scopesTab.resourceTypeRequired"),
          ),
        resource_ids: z.string(),
        requirements: z
          .array(
            z
              .object({
                key: z
                  .string()
                  .trim()
                  .regex(
                    LABEL_KEY_PATTERN,
                    t("settings.org.scopesTab.labelKeyInvalid"),
                  ),
                operator: z.enum(OPERATORS),
                values: z.string(),
              })
              .superRefine((value, context) => {
                if (value.operator !== "eq" && value.operator !== "in") return;
                const values = parseList(value.values);
                if (
                  values.length === 0 ||
                  (value.operator === "eq" && values.length !== 1) ||
                  values.length > 32 ||
                  values.some((entry) => !LABEL_VALUE_PATTERN.test(entry))
                ) {
                  context.addIssue({
                    code: "custom",
                    path: ["values"],
                    message: t("settings.org.scopesTab.labelValuesInvalid"),
                  });
                }
              }),
          )
          .max(scopeConstraints.requirements.maxItems ?? 16)
          .superRefine((requirements, context) => {
            const keys = new Set<string>();
            let valueCount = 0;
            requirements.forEach((requirement, index) => {
              if (keys.has(requirement.key))
                context.addIssue({
                  code: "custom",
                  path: [index, "key"],
                  message: t("settings.org.scopesTab.labelKeyDuplicate"),
                });
              keys.add(requirement.key);
              valueCount += parseList(requirement.values).length;
            });
            if (valueCount > 128)
              context.addIssue({
                code: "custom",
                message: t("settings.org.scopesTab.labelValuesTooMany"),
              });
          }),
        status: z.enum(["active", "disabled"]),
      }),
    [t],
  );
  type ScopeForm = z.infer<typeof scopeSchema>;
  const {
    clearErrors,
    control,
    register,
    reset,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<ScopeForm>({
    resolver: zodResolver(scopeSchema),
    defaultValues: {
      name: "",
      description: "",
      resource_types: [],
      resource_ids: "",
      requirements: [],
      status: "active",
    },
  });
  const {
    fields: requirements,
    append,
    remove,
  } = useFieldArray({
    control,
    name: "requirements",
  });
  const requirementValues = useWatch({ control, name: "requirements" });

  useEffect(() => {
    if (!open) return;
    reset({
      name: scope?.name ?? "",
      description: scope?.description ?? "",
      resource_types: scope?.resource_types ?? [],
      resource_ids: (scope?.explicit_resource_ids ?? []).join("\n"),
      requirements: (scope?.label_selector?.requirements ?? []).map(
        createRequirementDraft,
      ),
      status: scope?.status ?? "active",
    });
  }, [open, reset, scope]);
  const submit = handleSubmit(async (values) => {
    const normalizedRequirements = values.requirements
      .map(toRequirement)
      .filter(
        (requirement): requirement is LabelRequirement => requirement !== null,
      );
    clearErrors();
    try {
      await onSubmit({
        name: values.name,
        description: values.description || undefined,
        resource_types: values.resource_types,
        explicit_resource_ids: parseList(values.resource_ids),
        label_selector:
          normalizedRequirements.length > 0
            ? {
                schema_version: "argus.label_selector/v1",
                requirements: normalizedRequirements,
              }
            : undefined,
        status: values.status,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          description: "description",
          explicit_resource_ids: "resource_ids",
          label_selector: "requirements",
          name: "name",
          resource_types: "resource_types",
          status: "status",
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
        scope
          ? t("settings.org.scopesTab.editTitle")
          : t("settings.org.scopesTab.createTitle")
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
        <Field
          requirement="required"
          error={errors.name?.message}
          label={t("settings.common.name")}
        >
          <Input
            {...register("name")}
            maxLength={scopeConstraints.name.maxLength}
            required
          />
        </Field>
        <Field requirement="optional" label={t("settings.common.description")}>
          <Textarea
            {...register("description")}
            maxLength={scopeConstraints.description.maxLength}
            rows={2}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.resource_types?.message}
          label={t("settings.org.scopesTab.resourceTypes")}
        >
          <Controller
            control={control}
            name="resource_types"
            render={({ field }) => (
              <div className="argus-settings-check-list">
                {RESOURCE_TYPES.map((resourceType) => (
                  <button
                    className="argus-settings-check-option"
                    key={resourceType}
                    onClick={() =>
                      field.onChange(
                        field.value.includes(resourceType)
                          ? field.value.filter((type) => type !== resourceType)
                          : [...field.value, resourceType],
                      )
                    }
                    type="button"
                  >
                    <CheckItem checked={field.value.includes(resourceType)}>
                      {t(`settings.org.scopesTab.resourceType.${resourceType}`)}
                    </CheckItem>
                  </button>
                ))}
              </div>
            )}
          />
        </Field>
        <Field
          requirement="optional"
          hint={t("settings.org.scopesTab.resourceIdsHint")}
          label={t("settings.org.scopesTab.resourceIds")}
        >
          <Textarea {...register("resource_ids")} rows={3} />
        </Field>
        <Field
          controlMode="group"
          requirement="optional"
          hint={t("settings.org.scopesTab.labelSelectorHint")}
          label={t("settings.org.scopesTab.labelSelector")}
        >
          <div className="argus-settings-selector-list">
            {requirements.map((requirement, index) => (
              <div className="argus-settings-selector-row" key={requirement.id}>
                <Input
                  {...register(`requirements.${index}.key`)}
                  aria-label={t("settings.org.scopesTab.labelKey")}
                  maxLength={labelKeyConstraint.maxLength}
                  placeholder={t("settings.org.scopesTab.labelKey")}
                />
                <Controller
                  control={control}
                  name={`requirements.${index}.operator`}
                  render={({ field }) => (
                    <Select
                      aria-label={t("settings.org.scopesTab.operator")}
                      onValueChange={field.onChange}
                      options={OPERATORS.map((operator) => ({
                        value: operator,
                        label: operator,
                      }))}
                      value={field.value}
                    />
                  )}
                />
                {requirementValues[index]?.operator === "eq" ||
                requirementValues[index]?.operator === "in" ? (
                  <Input
                    {...register(`requirements.${index}.values`)}
                    aria-label={t("settings.org.scopesTab.labelValues")}
                    placeholder={t("settings.org.scopesTab.labelValues")}
                  />
                ) : (
                  <span aria-hidden="true" />
                )}
                <Button onClick={() => remove(index)} size="sm" variant="ghost">
                  {t("settings.common.delete")}
                </Button>
                {(errors.requirements?.[index]?.key?.message ||
                  errors.requirements?.[index]?.values?.message) && (
                  <small className="argus-field__error" role="alert">
                    {errors.requirements[index]?.key?.message ??
                      errors.requirements[index]?.values?.message}
                  </small>
                )}
              </div>
            ))}
            <Button
              disabled={
                requirements.length >=
                (scopeConstraints.requirements.maxItems ?? 16)
              }
              onClick={() => append(createRequirementDraft())}
              size="sm"
              variant="ghost"
            >
              {t("settings.org.scopesTab.addRequirement")}
            </Button>
          </div>
        </Field>
        {errors.requirements?.root?.message && (
          <p className="argus-field__error" role="alert">
            {errors.requirements.root.message}
          </p>
        )}
        <Field requirement="required" label={t("settings.common.status")}>
          <Controller
            control={control}
            name="status"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={[
                  { value: "active", label: t("settings.common.active") },
                  { value: "disabled", label: t("settings.common.disabled") },
                ]}
                value={field.value}
              />
            )}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
