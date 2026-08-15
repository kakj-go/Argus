import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Activity, CircleDollarSign, Gauge, Plus, RefreshCw } from "lucide-react";
import { useApi, type AIModel, type ModelQuota } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  StatCard,
  StatusBadge,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import { useOrgDepartments, useOrgUsers } from "../components/settings/org-users-tab";
import { usePermission } from "../lib/permissions";
import "../styles/ai-settings.css";

type ModelDrawerState = { mode: "create" } | { mode: "edit"; model: AIModel } | null;

export function SettingsAiPage() {
  const { t } = useTranslation();
  const admin = usePermission("*");
  const canManageQuota = usePermission("model_quota.manage");
  const departmentAdmin = !admin && canManageQuota;
  const [tab, setTab] = useState("models");
  const [dashboardModelId, setDashboardModelId] = useState<string>();
  return (
    <PageShell description={t("aiSettings.description")} title={t("shell.nav.settingsAi")}>
      <Tabs onValueChange={setTab} value={tab}>
        <TabsList>
          {admin && <TabsTrigger value="models">{t("aiSettings.tabs.models")}</TabsTrigger>}
          <TabsTrigger value="governance">{t("aiSettings.tabs.governance")}</TabsTrigger>
        </TabsList>
        {admin && <TabsContent value="models"><ModelsView onDashboard={(id) => { setDashboardModelId(id); setTab("governance"); }} /></TabsContent>}
        <TabsContent value="governance"><GovernanceView canManage={admin || departmentAdmin} modelId={dashboardModelId} onModelChange={setDashboardModelId} /></TabsContent>
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
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models.list() });
  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Parameters<typeof api.models.update>[1] }) => api.models.update(id, patch),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["models"] }),
  });
  return (
    <div className="argus-ai-stack">
      <div className="argus-ai-toolbar">
        <span />
        <Button onClick={() => setDrawer({ mode: "create" })} variant="primary"><Plus size={15} />{t("aiSettings.model.add")}</Button>
      </div>
      {(models.data ?? []).length === 0 ? (
        <EmptyState description="" title={t("aiSettings.model.empty")} />
      ) : (
        <DataTable<AIModel & Record<string, unknown>>
          columns={[
            { key: "name", header: t("aiSettings.model.name"), render: (model) => <div className="argus-ai-model-name"><b>{model.name}</b><small>{model.modelId}</small></div> },
            { key: "baseUrl", header: t("aiSettings.model.baseUrl"), render: (model) => <code>{model.baseUrl}</code> },
            { key: "inputPrice", header: t("aiSettings.model.inputPrice"), render: (model) => model.inputPricePerMillionTokens.toFixed(2) },
            { key: "outputPrice", header: t("aiSettings.model.outputPrice"), render: (model) => model.outputPricePerMillionTokens.toFixed(2) },
            { key: "health", header: t("settings.common.status"), render: (model) => <StatusBadge tone={model.healthStatus === "healthy" ? "success" : "danger"}>{t(`aiSettings.model.${model.healthStatus}`)}</StatusBadge> },
            { key: "revision", header: "Revision", render: (model) => <Badge>{model.revision}</Badge> },
            {
              key: "actions", header: t("settings.common.actions"), render: (model) => (
                <span className="argus-ai-actions">
                  <Button onClick={() => void api.models.test(model.id).then(() => models.refetch())} size="sm" variant="ghost"><RefreshCw size={14} />{t("aiSettings.model.test")}</Button>
                  <Button onClick={() => setDrawer({ mode: "edit", model })} size="sm" variant="ghost">{t("aiSettings.model.edit")}</Button>
                  <Button onClick={() => setQuotaModel(model)} size="sm" variant="ghost">{t("aiSettings.model.quota")}</Button>
                  <Button onClick={() => onDashboard(model.id)} size="sm" variant="ghost">{t("aiSettings.model.dashboard")}</Button>
                  <Switch checked={model.enabled} label={model.enabled ? t("aiSettings.model.disable") : t("aiSettings.model.enable")} onChange={(enabled) => update.mutate({ id: model.id, patch: { enabled } })} />
                </span>
              ),
            },
          ]}
          data={(models.data ?? []) as Array<AIModel & Record<string, unknown>>}
          getRowKey={(model) => model.id}
        />
      )}
      {drawer && <ModelDrawer onClose={() => setDrawer(null)} state={drawer} />}
      {quotaModel && <QuotaDrawer model={quotaModel} onClose={() => setQuotaModel(null)} />}
    </div>
  );
}

function ModelDrawer({ state, onClose }: { state: NonNullable<ModelDrawerState>; onClose: () => void }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const existing = state.mode === "edit" ? state.model : undefined;
  const [name, setName] = useState(existing?.name ?? "");
  const [baseUrl, setBaseUrl] = useState(existing?.baseUrl ?? "");
  const [apiKey, setApiKey] = useState("");
  const [modelId, setModelId] = useState(existing?.modelId ?? "");
  const [inputPrice, setInputPrice] = useState(String(existing?.inputPricePerMillionTokens ?? ""));
  const [outputPrice, setOutputPrice] = useState(String(existing?.outputPricePerMillionTokens ?? ""));
  const [error, setError] = useState("");
  const mutation = useMutation({
    mutationFn: async () => {
      if (existing) {
        return api.models.update(existing.id, { name, baseUrl, apiKey: apiKey || undefined, modelId, inputPricePerMillionTokens: Number(inputPrice), outputPricePerMillionTokens: Number(outputPrice) });
      }
      const result = await api.models.testAndCreate({ name, baseUrl, apiKey, modelId, inputPricePerMillionTokens: Number(inputPrice), outputPricePerMillionTokens: Number(outputPrice) });
      if (!result.created) throw new Error(result.compatibility.diagnostics.join("; "));
      return result.model;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["models"] });
      onClose();
    },
    onError: () => setError(t("aiSettings.model.compatibilityFailed")),
  });
  return (
    <FormDrawer description={error || undefined} loading={mutation.isPending} onOpenChange={(open) => !open && onClose()} onSubmit={() => mutation.mutate()} open submitLabel={existing ? t("settings.common.save") : t("aiSettings.model.testCreate")} title={existing ? t("aiSettings.model.editTitle") : t("aiSettings.model.createTitle")}>
      <div className="argus-settings-form">
        <Field label={t("aiSettings.model.name")}><Input onChange={(event) => setName(event.target.value)} required value={name} /></Field>
        <Field label={t("aiSettings.model.baseUrl")}><Input onChange={(event) => setBaseUrl(event.target.value)} required type="url" value={baseUrl} /></Field>
        <Field hint={t("aiSettings.model.apiKeyHint")} label={t("aiSettings.model.apiKey")}><Input onChange={(event) => setApiKey(event.target.value)} required={!existing} type="password" value={apiKey} /></Field>
        <Field label={t("aiSettings.model.modelId")}><Input onChange={(event) => setModelId(event.target.value)} required value={modelId} /></Field>
        <div className="argus-form-row">
          <Field label={t("aiSettings.model.inputPrice")}><Input min="0" onChange={(event) => setInputPrice(event.target.value)} required step="0.01" type="number" value={inputPrice} /></Field>
          <Field label={t("aiSettings.model.outputPrice")}><Input min="0" onChange={(event) => setOutputPrice(event.target.value)} required step="0.01" type="number" value={outputPrice} /></Field>
        </div>
      </div>
    </FormDrawer>
  );
}

function QuotaDrawer({ model, onClose }: { model: AIModel; onClose: () => void }) {
  const { t } = useTranslation();
  const api = useApi();
  const session = useAuthStore((state) => state.session);
  const enterpriseAdmin = usePermission("*");
  const departments = useOrgDepartments();
  const users = useOrgUsers();
  const queryClient = useQueryClient();
  const [subjectType, setSubjectType] = useState<"department" | "user">(enterpriseAdmin ? "department" : "user");
  const eligibleUsers = (users.data ?? []).filter((user) => enterpriseAdmin || user.departmentId === session?.membership?.departmentId);
  const subjects = subjectType === "department" ? (departments.data ?? []) : eligibleUsers;
  const [subjectId, setSubjectId] = useState("");
  const [amount, setAmount] = useState("");
  const save = useMutation({
    mutationFn: () => api.models.setQuota({ modelId: model.id, subjectType, subjectId: subjectId || subjects[0]?.id || "", monthlyAmount: amount === "" ? undefined : Number(amount) }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ["model-quotas"] }); onClose(); },
  });
  return (
    <FormDrawer loading={save.isPending} onOpenChange={(open) => !open && onClose()} onSubmit={() => save.mutate()} open submitLabel={t("aiSettings.quota.save")} title={`${t("aiSettings.quota.title")} · ${model.name}`}>
      <div className="argus-settings-form">
        {enterpriseAdmin && <Field label={t("aiSettings.quota.subjectType")}><Select onValueChange={(value) => { setSubjectType(value as "department" | "user"); setSubjectId(""); }} options={[{ value: "department", label: t("aiSettings.quota.department") }, { value: "user", label: t("aiSettings.quota.user") }]} value={subjectType} /></Field>}
        <Field label={t("aiSettings.quota.subject")}><Select onValueChange={setSubjectId} options={subjects.map((subject) => ({ value: subject.id, label: (subject as { name?: string; displayName?: string }).name ?? (subject as { displayName?: string }).displayName ?? subject.id }))} value={subjectId || subjects[0]?.id || ""} /></Field>
        <Field hint={t("aiSettings.quota.unlimited")} label={t("aiSettings.quota.amount")}><Input min="0" onChange={(event) => setAmount(event.target.value)} step="0.01" type="number" value={amount} /></Field>
        {!enterpriseAdmin && <p className="argus-settings-section__hint">{t("aiSettings.quota.departmentOnly")}</p>}
      </div>
    </FormDrawer>
  );
}

function GovernanceView({ modelId, onModelChange, canManage }: { modelId?: string; onModelChange: (id?: string) => void; canManage: boolean }) {
  const { t } = useTranslation();
  const api = useApi();
  const models = useQuery({ queryKey: ["models"], queryFn: () => api.models.list() });
  const usage = useQuery({ queryKey: ["model-usage", modelId], queryFn: () => api.models.usage({ modelId }) });
  const quotas = useQuery<ModelQuota[]>({ queryKey: ["model-quotas", modelId], queryFn: () => api.models.listQuotas(modelId), enabled: canManage });
  const data = usage.data;
  const ranking = useMemo(() => {
    const map = new Map<string, number>();
    for (const point of data?.points ?? []) map.set(`${point.departmentId} / ${point.userId}`, (map.get(`${point.departmentId} / ${point.userId}`) ?? 0) + point.amount);
    return [...map.entries()].sort((a, b) => b[1] - a[1]).map(([subject, amount]) => ({ subject, amount }));
  }, [data]);
  return (
    <div className="argus-ai-stack">
      <div className="argus-ai-toolbar">
        <Select className="argus-ai-model-filter" onValueChange={(value) => onModelChange(value || undefined)} options={[{ value: "", label: t("aiSettings.dashboard.allModels") }, ...(models.data ?? []).map((model) => ({ value: model.id, label: model.name }))]} value={modelId ?? ""} />
        {canManage && <Badge>{(quotas.data ?? []).length} quotas</Badge>}
      </div>
      <div className="argus-settings-stat-grid">
        <StatCard icon={<Activity size={16} />} label={t("aiSettings.dashboard.requests")} value={data?.totalRequests ?? 0} />
        <StatCard icon={<Gauge size={16} />} label={t("aiSettings.dashboard.tokens")} value={((data?.totalInputTokens ?? 0) + (data?.totalOutputTokens ?? 0)).toLocaleString()} />
        <StatCard icon={<CircleDollarSign size={16} />} label={t("aiSettings.dashboard.amount")} value={(data?.totalAmount ?? 0).toFixed(2)} />
        <StatCard label={t("aiSettings.dashboard.successRate")} value={`${((data?.successRate ?? 1) * 100).toFixed(1)}%`} />
        <StatCard label={t("aiSettings.dashboard.latency")} value={`${Math.round(data?.avgLatencyMs ?? 0)} ms`} />
        <StatCard label={t("aiSettings.dashboard.errors")} value={data?.errorCount ?? 0} tone="danger" />
        <StatCard label={t("aiSettings.dashboard.toolFailures")} value={data?.toolCallingFailures ?? 0} tone="warning" />
        <StatCard label={t("aiSettings.dashboard.structuredFailures")} value={data?.structuredOutputFailures ?? 0} tone="warning" />
      </div>
      <section className="argus-settings-section">
        <h2 className="argus-settings-section__title">{t("aiSettings.dashboard.ranking")}</h2>
        {ranking.length ? <DataTable columns={[{ key: "subject", header: t("aiSettings.quota.subject") }, { key: "amount", header: t("aiSettings.dashboard.amount"), render: (row) => row.amount.toFixed(2) }]} data={ranking} getRowKey={(row) => row.subject} /> : <EmptyState description="" title={t("aiSettings.dashboard.noData")} />}
      </section>
    </div>
  );
}
