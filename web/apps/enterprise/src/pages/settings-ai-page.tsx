import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  Activity,
  CircleDollarSign,
  Gauge,
  Plus,
  RefreshCw,
} from "lucide-react";
import {
  apiErrorPresentation,
  formConstraint,
  formatApiError,
  useApi,
  type AIModel,
  type ModelQuota,
} from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import {
  Badge,
  Alert,
  Button,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  RowAction,
  Select,
  StatCard,
  StatusBadge,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import {
  useOrgDepartments,
  useOrgUsers,
} from "../components/settings/org-users-tab";
import { usePermission } from "../lib/permissions";
import "../styles/ai-settings.css";

type ModelDrawerState =
  { mode: "create" } | { mode: "edit"; model: AIModel } | null;

type ModelFormValues = {
  name: string;
  baseUrl: string;
  apiKey: string;
  modelId: string;
  apiProtocol: "chat_completions" | "responses";
  contextWindowTokens: number;
  maxOutputTokens: number;
  inputPricePerMillionTokens: number;
  outputPricePerMillionTokens: number;
};

const modelConstraints = {
  name: formConstraint("AIModelTestCreate", "name"),
  baseUrl: formConstraint("AIModelTestCreate", "base_url"),
  apiKey: formConstraint("AIModelTestCreate", "api_key"),
  contextWindowTokens: formConstraint(
    "AIModelTestCreate",
    "context_window_tokens",
  ),
  maxOutputTokens: formConstraint("AIModelTestCreate", "max_output_tokens"),
  modelId: formConstraint("AIModelTestCreate", "model_id"),
  inputPrice: formConstraint("AIModelTestCreate", "input_price_per_million"),
  outputPrice: formConstraint("AIModelTestCreate", "output_price_per_million"),
};

const modelFieldByApiName: Record<string, keyof ModelFormValues> = {
  api_key: "apiKey",
  api_protocol: "apiProtocol",
  base_url: "baseUrl",
  context_window_tokens: "contextWindowTokens",
  input_price_per_million: "inputPricePerMillionTokens",
  max_output_tokens: "maxOutputTokens",
  model_id: "modelId",
  name: "name",
  output_price_per_million: "outputPricePerMillionTokens",
};

export function SettingsAiPage() {
  const { t } = useTranslation();
  const canManageModels = usePermission("model.manage");
  const canManageQuota = usePermission("model_quota.manage");
  const departmentAdmin = !canManageModels && canManageQuota;
  const [tab, setTab] = useState("models");
  const [dashboardModelId, setDashboardModelId] = useState<string>();
  return (
    <PageShell
      description={t("aiSettings.description")}
      title={t("shell.nav.settingsAi")}
    >
      <Tabs onValueChange={setTab} value={tab}>
        <TabsList>
          {canManageModels && (
            <TabsTrigger value="models">
              {t("aiSettings.tabs.models")}
            </TabsTrigger>
          )}
          <TabsTrigger value="governance">
            {t("aiSettings.tabs.governance")}
          </TabsTrigger>
        </TabsList>
        {canManageModels && (
          <TabsContent value="models">
            <ModelsView
              onDashboard={(id) => {
                setDashboardModelId(id);
                setTab("governance");
              }}
            />
          </TabsContent>
        )}
        <TabsContent value="governance">
          <GovernanceView
            canManage={canManageModels || departmentAdmin}
            modelId={dashboardModelId}
            onModelChange={setDashboardModelId}
          />
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}

function ModelsView({ onDashboard }: { onDashboard: (id: string) => void }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [drawer, setDrawer] = useState<ModelDrawerState>(null);
  const [quotaModel, setQuotaModel] = useState<AIModel | null>(null);
  const models = useQuery({
    queryKey: ["models"],
    queryFn: () => api.models.list(),
  });
  const update = useMutation({
    mutationFn: ({
      id,
      patch,
    }: {
      id: string;
      patch: Parameters<typeof api.models.update>[1];
    }) => api.models.update(id, patch),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["models"] }),
  });
  return (
    <div className="argus-ai-stack">
      <div className="argus-ai-toolbar">
        <span />
        <Button onClick={() => setDrawer({ mode: "create" })} variant="primary">
          <Plus size={15} />
          {t("aiSettings.model.add")}
        </Button>
      </div>
      {(models.data ?? []).length === 0 ? (
        <EmptyState description="" title={t("aiSettings.model.empty")} />
      ) : (
        <DataTable<AIModel & Record<string, unknown>>
          columns={[
            {
              key: "name",
              header: t("aiSettings.model.name"),
              render: (model) => (
                <div className="argus-ai-model-name">
                  <b>{model.name}</b>
                  <small>{model.modelId}</small>
                </div>
              ),
            },
            {
              key: "baseUrl",
              header: t("aiSettings.model.baseUrl"),
              render: (model) => <code>{model.baseUrl}</code>,
            },
            {
              key: "inputPrice",
              header: t("aiSettings.model.inputPrice"),
              render: (model) => model.inputPricePerMillionTokens.toFixed(2),
            },
            {
              key: "outputPrice",
              header: t("aiSettings.model.outputPrice"),
              render: (model) => model.outputPricePerMillionTokens.toFixed(2),
            },
            {
              key: "health",
              header: t("settings.common.status"),
              render: (model) => (
                <StatusBadge
                  tone={model.healthStatus === "healthy" ? "success" : "danger"}
                >
                  {t(`aiSettings.model.${model.healthStatus}`)}
                </StatusBadge>
              ),
            },
            {
              key: "revision",
              header: t("aiSettings.model.revisionLabel"),
              render: (model) => <Badge>{model.revision}</Badge>,
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (model) => (
                <span className="argus-ai-actions">
                  <RowAction
                    onClick={() =>
                      void api.models
                        .test(model.id)
                        .then(() => models.refetch())
                    }
                  >
                    <RefreshCw size={14} />
                    {t("aiSettings.model.test")}
                  </RowAction>
                  <RowAction onClick={() => setDrawer({ mode: "edit", model })}>
                    {t("aiSettings.model.edit")}
                  </RowAction>
                  <RowAction onClick={() => setQuotaModel(model)}>
                    {t("aiSettings.model.quota")}
                  </RowAction>
                  <RowAction onClick={() => onDashboard(model.id)}>
                    {t("aiSettings.model.dashboard")}
                  </RowAction>
                  <Switch
                    checked={model.enabled}
                    label={
                      model.enabled
                        ? t("aiSettings.model.disable")
                        : t("aiSettings.model.enable")
                    }
                    onChange={(enabled) =>
                      update.mutate({ id: model.id, patch: { enabled } })
                    }
                  />
                </span>
              ),
            },
          ]}
          data={(models.data ?? []) as Array<AIModel & Record<string, unknown>>}
          getRowKey={(model) => model.id}
        />
      )}
      {drawer && <ModelDrawer onClose={() => setDrawer(null)} state={drawer} />}
      {quotaModel && (
        <QuotaDrawer model={quotaModel} onClose={() => setQuotaModel(null)} />
      )}
    </div>
  );
}

function ModelDrawer({
  state,
  onClose,
}: {
  state: NonNullable<ModelDrawerState>;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const existing = state.mode === "edit" ? state.model : undefined;
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("aiSettings.validation.required"))
          .max(
            modelConstraints.name.maxLength ?? 128,
            t("aiSettings.validation.maxLength", {
              max: modelConstraints.name.maxLength ?? 128,
            }),
          ),
        baseUrl: z
          .string()
          .trim()
          .min(1, t("aiSettings.validation.required"))
          .max(
            modelConstraints.baseUrl.maxLength ?? 2048,
            t("aiSettings.validation.maxLength", {
              max: modelConstraints.baseUrl.maxLength ?? 2048,
            }),
          )
          .refine((value) => {
            try {
              const url = new URL(value);
              return url.protocol === "http:" || url.protocol === "https:";
            } catch {
              return false;
            }
          }, t("aiSettings.validation.invalidUrl")),
        apiKey: existing
          ? z.string().max(
              modelConstraints.apiKey.maxLength ?? 8192,
              t("aiSettings.validation.maxLength", {
                max: modelConstraints.apiKey.maxLength ?? 8192,
              }),
            )
          : z
              .string()
              .min(1, t("aiSettings.validation.required"))
              .max(
                modelConstraints.apiKey.maxLength ?? 8192,
                t("aiSettings.validation.maxLength", {
                  max: modelConstraints.apiKey.maxLength ?? 8192,
                }),
              ),
        modelId: z
          .string()
          .trim()
          .min(1, t("aiSettings.validation.required"))
          .max(
            modelConstraints.modelId.maxLength ?? 256,
            t("aiSettings.validation.maxLength", {
              max: modelConstraints.modelId.maxLength ?? 256,
            }),
          ),
        apiProtocol: z.enum(["chat_completions", "responses"]),
        contextWindowTokens: z
          .number({ error: t("aiSettings.validation.required") })
          .int(t("aiSettings.validation.integer"))
          .min(
            modelConstraints.contextWindowTokens.minimum ?? 8192,
            t("aiSettings.validation.minimum", {
              min: modelConstraints.contextWindowTokens.minimum ?? 8192,
            }),
          ),
        maxOutputTokens: z
          .number({ error: t("aiSettings.validation.required") })
          .int(t("aiSettings.validation.integer"))
          .min(
            modelConstraints.maxOutputTokens.minimum ?? 1,
            t("aiSettings.validation.minimum", {
              min: modelConstraints.maxOutputTokens.minimum ?? 1,
            }),
          ),
        inputPricePerMillionTokens: z
          .number({ error: t("aiSettings.validation.required") })
          .min(
            modelConstraints.inputPrice.minimum ?? 0,
            t("aiSettings.validation.minimum", {
              min: modelConstraints.inputPrice.minimum ?? 0,
            }),
          ),
        outputPricePerMillionTokens: z
          .number({ error: t("aiSettings.validation.required") })
          .min(
            modelConstraints.outputPrice.minimum ?? 0,
            t("aiSettings.validation.minimum", {
              min: modelConstraints.outputPrice.minimum ?? 0,
            }),
          ),
      }),
    [existing, t],
  );
  const {
    control,
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<ModelFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: existing?.name ?? "",
      baseUrl: existing?.baseUrl ?? "",
      apiKey: "",
      modelId: existing?.modelId ?? "",
      apiProtocol: existing?.apiProtocol ?? "responses",
      contextWindowTokens: existing?.contextWindowTokens ?? 128_000,
      maxOutputTokens: existing?.maxOutputTokens ?? 8192,
      inputPricePerMillionTokens: existing?.inputPricePerMillionTokens,
      outputPricePerMillionTokens: existing?.outputPricePerMillionTokens,
    },
  });
  const mutation = useMutation({
    mutationFn: async (values: ModelFormValues) => {
      if (existing) {
        return api.models.update(existing.id, {
          ...values,
          apiKey: values.apiKey || undefined,
        });
      }
      const result = await api.models.testAndCreate(values);
      if (!result.created)
        throw new Error(result.compatibility.diagnostics.join("; "));
      return result.model;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["models"] });
      onClose();
    },
    onError: (caught) => {
      const presentation = apiErrorPresentation(caught);
      const apiField = presentation?.params?.field;
      const formField =
        typeof apiField === "string"
          ? modelFieldByApiName[apiField]
          : undefined;
      const message =
        presentation?.publicMessage ??
        t("aiSettings.model.compatibilityFailed");
      if (formField) {
        setError(formField, { message, type: "server" }, { shouldFocus: true });
        return;
      }
      setError("root", {
        message: formatApiError(
          caught,
          t("aiSettings.model.compatibilityFailed"),
          (requestId) => t("common.requestReference", { requestId }),
        ),
        type: "server",
      });
    },
  });
  return (
    <FormDrawer
      loading={mutation.isPending}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={handleSubmit((values) => mutation.mutate(values))}
      open
      submitLabel={
        existing ? t("settings.common.save") : t("aiSettings.model.testCreate")
      }
      title={
        existing
          ? t("aiSettings.model.editTitle")
          : t("aiSettings.model.createTitle")
      }
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("aiSettings.model.compatibilityFailed")}
            tone="danger"
          />
        )}
        <Field
          error={errors.name?.message}
          requirement="required"
          label={t("aiSettings.model.name")}
        >
          <Input
            {...register("name")}
            maxLength={modelConstraints.name.maxLength}
          />
        </Field>
        <Field
          error={errors.baseUrl?.message}
          requirement="required"
          label={t("aiSettings.model.baseUrl")}
        >
          <Input
            {...register("baseUrl")}
            maxLength={modelConstraints.baseUrl.maxLength}
            type="url"
          />
        </Field>
        <Field
          error={errors.apiKey?.message}
          requirement={existing ? "optional" : "required"}
          hint={t("aiSettings.model.apiKeyHint")}
          label={t("aiSettings.model.apiKey")}
        >
          <Input
            {...register("apiKey")}
            maxLength={modelConstraints.apiKey.maxLength}
            type="password"
          />
        </Field>
        <Field
          error={errors.modelId?.message}
          requirement="required"
          label={t("aiSettings.model.modelId")}
        >
          <Input
            {...register("modelId")}
            maxLength={modelConstraints.modelId.maxLength}
          />
        </Field>
        <Field
          error={errors.apiProtocol?.message}
          requirement="required"
          label={t("aiSettings.model.apiProtocol")}
        >
          <Controller
            control={control}
            name="apiProtocol"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={[
                  { value: "responses", label: "Responses" },
                  { value: "chat_completions", label: "Chat Completions" },
                ]}
                value={field.value}
              />
            )}
          />
        </Field>
        <div className="argus-form-row">
          <Field
            error={errors.contextWindowTokens?.message}
            requirement="required"
            label={t("aiSettings.model.contextWindowTokens")}
          >
            <Input
              {...register("contextWindowTokens", { valueAsNumber: true })}
              min={modelConstraints.contextWindowTokens.minimum}
              step="1"
              type="number"
            />
          </Field>
          <Field
            error={errors.maxOutputTokens?.message}
            requirement="required"
            label={t("aiSettings.model.maxOutputTokens")}
          >
            <Input
              {...register("maxOutputTokens", { valueAsNumber: true })}
              min={modelConstraints.maxOutputTokens.minimum}
              step="1"
              type="number"
            />
          </Field>
        </div>
        <div className="argus-form-row">
          <Field
            error={errors.inputPricePerMillionTokens?.message}
            requirement="required"
            label={t("aiSettings.model.inputPrice")}
          >
            <Input
              {...register("inputPricePerMillionTokens", {
                valueAsNumber: true,
              })}
              min={modelConstraints.inputPrice.minimum}
              step="0.01"
              type="number"
            />
          </Field>
          <Field
            error={errors.outputPricePerMillionTokens?.message}
            requirement="required"
            label={t("aiSettings.model.outputPrice")}
          >
            <Input
              {...register("outputPricePerMillionTokens", {
                valueAsNumber: true,
              })}
              min={modelConstraints.outputPrice.minimum}
              step="0.01"
              type="number"
            />
          </Field>
        </div>
      </div>
    </FormDrawer>
  );
}

function QuotaDrawer({
  model,
  onClose,
}: {
  model: AIModel;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const session = useEnterpriseAuthStore((state) => state.session);
  const enterpriseAdmin = usePermission("model.manage");
  const departments = useOrgDepartments();
  const users = useOrgUsers();
  const queryClient = useQueryClient();
  const schema = useMemo(
    () =>
      z.object({
        subjectType: z.enum(["department", "user"]),
        subjectId: z.string().min(1, t("aiSettings.validation.required")),
        amount: z
          .number({ error: t("aiSettings.validation.nonnegative") })
          .min(0, t("aiSettings.validation.nonnegative"))
          .optional(),
      }),
    [t],
  );
  type QuotaFormValues = z.infer<typeof schema>;
  const {
    control,
    handleSubmit,
    register,
    setError,
    setValue,
    watch,
    formState: { errors },
  } = useForm<QuotaFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      subjectType: enterpriseAdmin ? "department" : "user",
      subjectId: "",
      amount: undefined,
    },
  });
  const subjectType = watch("subjectType");
  const subjects = useMemo(() => {
    if (subjectType === "department") return departments.data ?? [];
    return (users.data ?? []).filter(
      (user) =>
        enterpriseAdmin ||
        user.department_id === session?.session.department_id,
    );
  }, [
    departments.data,
    enterpriseAdmin,
    session?.session.department_id,
    subjectType,
    users.data,
  ]);
  useEffect(() => {
    setValue("subjectId", subjects[0]?.id ?? "", { shouldValidate: false });
  }, [setValue, subjectType, subjects]);
  const save = useMutation({
    mutationFn: (values: QuotaFormValues) =>
      api.models.setQuota({
        modelId: model.id,
        subjectType: values.subjectType,
        subjectId: values.subjectId,
        monthlyAmount: values.amount,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["model-quotas"] });
      onClose();
    },
    onError: (caught) => {
      setError("root", {
        message: formatApiError(
          caught,
          t("aiSettings.quota.saveFailed"),
          (requestId) => t("common.requestReference", { requestId }),
        ),
        type: "server",
      });
    },
  });
  return (
    <FormDrawer
      loading={save.isPending}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={handleSubmit((values) => save.mutate(values))}
      open
      submitLabel={t("aiSettings.quota.save")}
      title={`${t("aiSettings.quota.title")} · ${model.name}`}
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("aiSettings.quota.saveFailed")}
            tone="danger"
          />
        )}
        {enterpriseAdmin && (
          <Field
            error={errors.subjectType?.message}
            requirement="required"
            label={t("aiSettings.quota.subjectType")}
          >
            <Controller
              control={control}
              name="subjectType"
              render={({ field }) => (
                <Select
                  onValueChange={field.onChange}
                  options={[
                    {
                      value: "department",
                      label: t("aiSettings.quota.department"),
                    },
                    { value: "user", label: t("aiSettings.quota.user") },
                  ]}
                  value={field.value}
                />
              )}
            />
          </Field>
        )}
        <Field
          error={errors.subjectId?.message}
          requirement="required"
          label={t("aiSettings.quota.subject")}
        >
          <Controller
            control={control}
            name="subjectId"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={subjects.map((subject) => ({
                  value: subject.id,
                  label:
                    (subject as { name?: string; displayName?: string }).name ??
                    (subject as { displayName?: string }).displayName ??
                    subject.id,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          error={errors.amount?.message}
          requirement="optional"
          hint={t("aiSettings.quota.unlimited")}
          label={t("aiSettings.quota.amount")}
        >
          <Input
            {...register("amount", {
              setValueAs: (value) =>
                value === "" || value === undefined ? undefined : Number(value),
            })}
            min="0"
            step="0.01"
            type="number"
          />
        </Field>
        {!enterpriseAdmin && (
          <p className="argus-settings-section__hint">
            {t("aiSettings.quota.departmentOnly")}
          </p>
        )}
      </div>
    </FormDrawer>
  );
}

function GovernanceView({
  modelId,
  onModelChange,
  canManage,
}: {
  modelId?: string;
  onModelChange: (id?: string) => void;
  canManage: boolean;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const models = useQuery({
    queryKey: ["models"],
    queryFn: () => api.models.list(),
  });
  const usage = useQuery({
    queryKey: ["model-usage", modelId],
    queryFn: () => api.models.usage({ modelId }),
  });
  const quotas = useQuery<ModelQuota[]>({
    queryKey: ["model-quotas", modelId],
    queryFn: () => api.models.listQuotas(modelId),
    enabled: canManage,
  });
  const data = usage.data;
  const ranking = useMemo(() => {
    const map = new Map<string, number>();
    for (const point of data?.points ?? [])
      map.set(
        `${point.departmentId} / ${point.userId}`,
        (map.get(`${point.departmentId} / ${point.userId}`) ?? 0) +
          point.amount,
      );
    return [...map.entries()]
      .sort((a, b) => b[1] - a[1])
      .map(([subject, amount]) => ({ subject, amount }));
  }, [data]);
  return (
    <div className="argus-ai-stack">
      <div className="argus-ai-toolbar">
        <Select
          className="argus-ai-model-filter"
          onValueChange={(value) => onModelChange(value || undefined)}
          options={[
            { value: "", label: t("aiSettings.dashboard.allModels") },
            ...(models.data ?? []).map((model) => ({
              value: model.id,
              label: model.name,
            })),
          ]}
          value={modelId ?? ""}
        />
        {canManage && (
          <Badge>
            {t("aiSettings.dashboard.quotaCount", {
              count: (quotas.data ?? []).length,
            })}
          </Badge>
        )}
      </div>
      <div className="argus-settings-stat-grid">
        <StatCard
          icon={<Activity size={16} />}
          label={t("aiSettings.dashboard.requests")}
          value={data?.totalRequests ?? 0}
        />
        <StatCard
          icon={<Gauge size={16} />}
          label={t("aiSettings.dashboard.tokens")}
          value={(
            (data?.totalInputTokens ?? 0) + (data?.totalOutputTokens ?? 0)
          ).toLocaleString()}
        />
        <StatCard
          icon={<CircleDollarSign size={16} />}
          label={t("aiSettings.dashboard.amount")}
          value={(data?.totalAmount ?? 0).toFixed(2)}
        />
        <StatCard
          label={t("aiSettings.dashboard.successRate")}
          value={`${((data?.successRate ?? 1) * 100).toFixed(1)}%`}
        />
        <StatCard
          label={t("aiSettings.dashboard.latency")}
          value={`${Math.round(data?.avgLatencyMs ?? 0)} ms`}
        />
        <StatCard
          label={t("aiSettings.dashboard.errors")}
          value={data?.errorCount ?? 0}
          tone="danger"
        />
        <StatCard
          label={t("aiSettings.dashboard.toolFailures")}
          value={data?.toolCallingFailures ?? 0}
          tone="warning"
        />
        <StatCard
          label={t("aiSettings.dashboard.structuredFailures")}
          value={data?.structuredOutputFailures ?? 0}
          tone="warning"
        />
      </div>
      <section className="argus-settings-section">
        <h2 className="argus-settings-section__title">
          {t("aiSettings.dashboard.ranking")}
        </h2>
        {ranking.length ? (
          <DataTable
            columns={[
              { key: "subject", header: t("aiSettings.quota.subject") },
              {
                key: "amount",
                header: t("aiSettings.dashboard.amount"),
                render: (row) => row.amount.toFixed(2),
              },
            ]}
            data={ranking}
            getRowKey={(row) => row.subject}
          />
        ) : (
          <EmptyState description="" title={t("aiSettings.dashboard.noData")} />
        )}
      </section>
    </div>
  );
}
