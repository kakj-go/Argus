import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  History,
  Link2,
  LockKeyhole,
} from "lucide-react";
import { formatApiError, useApi } from "@argus/api-client";
import type {
  CardDemo,
  CardValidationRun,
  CardRuntimeValidationReport,
  CardVersion,
  InteractiveCard,
  SlotBinding,
  ToolSchemaCatalogEntry,
} from "@argus/api-client/contracts";
import { SandboxCard } from "@argus/card-host";
import {
  Badge,
  Alert,
  Button,
  EmptyState,
  Field,
  FormDrawer,
  PageShell,
  RowAction,
  Select,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  useTheme,
} from "@argus/ui";
import { cardOrigin } from "../lib/card-contract";
import { usePermission } from "../lib/permissions";
import "../styles/ai-settings.css";

const VALIDATION_CASES = [
  { scenario: "default", locale: "zh-CN", color_scheme: "light" },
  { scenario: "empty", locale: "en-US", color_scheme: "dark" },
  { scenario: "error", locale: "zh-CN", color_scheme: "dark" },
  { scenario: "large", locale: "en-US", color_scheme: "light" },
  { scenario: "light", locale: "zh-CN", color_scheme: "light" },
  { scenario: "dark", locale: "en-US", color_scheme: "dark" },
  { scenario: "zh-CN", locale: "zh-CN", color_scheme: "dark" },
  { scenario: "en-US", locale: "en-US", color_scheme: "light" },
] as const;

export function SettingsInteractiveCardsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const canPublish = usePermission("interactive_card.publish");
  const [tab, setTab] = useState<"enterprise" | "system">("enterprise");
  const [selected, setSelected] = useState<InteractiveCard | null>(null);
  const cards = useQuery({
    queryKey: ["interactive-cards"],
    queryFn: () => api.interactiveCards.list(),
  });
  const filtered = (cards.data ?? []).filter((card) => card.source === tab);

  return (
    <PageShell
      description={t("aiSettings.cards.description")}
      title={t("shell.nav.settingsInteractiveCards")}
    >
      <Tabs onValueChange={(value) => setTab(value as typeof tab)} value={tab}>
        <TabsList>
          <TabsTrigger value="enterprise">
            {t("aiSettings.cards.custom")}
          </TabsTrigger>
          <TabsTrigger value="system">
            {t("aiSettings.cards.system")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value={tab}>
          <CardRows cards={filtered} onOpen={setSelected} />
        </TabsContent>
      </Tabs>
      {selected && (
        <CardDetailDrawer
          canPublish={canPublish && selected.source === "enterprise"}
          card={selected}
          onClose={() => setSelected(null)}
          onUpdated={setSelected}
        />
      )}
    </PageShell>
  );
}

function CardRows({
  cards,
  onOpen,
}: {
  cards: InteractiveCard[];
  onOpen: (card: InteractiveCard) => void;
}) {
  const { t } = useTranslation();
  if (!cards.length) {
    return <EmptyState description="" title={t("aiSettings.cards.noCards")} />;
  }
  return (
    <div className="argus-ic-list">
      {cards.map((card) => (
        <article className="argus-ic-row" key={card.id}>
          <div className="argus-ic-row__meta">
            <div>
              <b>{card.name}</b>
              <Badge>r{card.active_revision ?? card.latest_revision}</Badge>
            </div>
            <p>{card.description}</p>
            <span>
              <StatusBadge tone={availabilityTone(card.availability)}>
                {card.availability}
              </StatusBadge>{" "}
              · {card.slug} · {t(`aiSettings.cards.${card.lifecycle}`)}
            </span>
          </div>
          <div className="argus-ic-row__actions">
            {card.source === "system" && (
              <LockKeyhole
                aria-label={t("aiSettings.cards.readonly")}
                size={15}
              />
            )}
            <RowAction onClick={() => onOpen(card)}>
              {t("aiSettings.cards.detail")}
            </RowAction>
          </div>
        </article>
      ))}
    </div>
  );
}

function CardDetailDrawer({
  card,
  canPublish,
  onClose,
  onUpdated,
}: {
  card: InteractiveCard;
  canPublish: boolean;
  onClose: () => void;
  onUpdated: (card: InteractiveCard) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const versions = useQuery({
    queryKey: ["interactive-card-versions", card.id],
    queryFn: () => api.interactiveCards.listVersions(card.id),
  });
  const [revision, setRevision] = useState(
    card.active_revision ?? card.latest_revision,
  );
  const [bindingSlot, setBindingSlot] = useState<string | null>(null);
  const [validation, setValidation] = useState<CardValidationRun | null>(null);
  const version = useQuery({
    queryKey: ["interactive-card-version", card.id, revision],
    queryFn: () => api.interactiveCards.getVersion(card.id, revision),
  });
  const refresh = async () => {
    const updated = await api.interactiveCards.get(card.id);
    onUpdated(updated);
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["interactive-cards"] }),
      queryClient.invalidateQueries({
        queryKey: ["interactive-card-versions", card.id],
      }),
    ]);
  };
  const changeState = useMutation({
    mutationFn: (action: "activate" | "disable" | "rollback" | "deprecate") =>
      api.interactiveCards.changeState(card.id, action, {
        expected_version: card.version,
        revision,
      }),
    onSuccess: (updated) => {
      onUpdated(updated);
      void refresh();
    },
  });
  const startValidation = useMutation({
    mutationFn: () =>
      api.interactiveCards.startValidation(card.id, {
        revision,
        runtime_version: "argus-card-runtime/v1",
      }),
    onSuccess: setValidation,
  });
  const selectedIsActive = card.enabled && card.active_revision === revision;
  const selectedCanActivate =
    version.data?.status === "validated" || version.data?.status === "retired";
  const selectedIsRollback =
    card.enabled &&
    card.active_revision !== undefined &&
    revision < card.active_revision;

  return (
    <>
      <FormDrawer
        footer={
          canPublish ? (
            <>
              <Button
                loading={startValidation.isPending}
                onClick={() => startValidation.mutate()}
                variant="secondary"
              >
                {t("aiSettings.cards.validate")}
              </Button>
              {selectedIsActive ? (
                <Button
                  loading={changeState.isPending}
                  onClick={() => changeState.mutate("disable")}
                  variant="secondary"
                >
                  {t("aiSettings.cards.disable")}
                </Button>
              ) : (
                <Button
                  disabled={!selectedCanActivate}
                  loading={changeState.isPending}
                  onClick={() =>
                    changeState.mutate(
                      selectedIsRollback ? "rollback" : "activate",
                    )
                  }
                  variant="primary"
                >
                  {card.enabled
                    ? t(
                        selectedIsRollback
                          ? "aiSettings.cards.rollbackRevision"
                          : "aiSettings.cards.activateRevision",
                      )
                    : t("aiSettings.cards.enable")}
                </Button>
              )}
            </>
          ) : (
            <Badge>
              <LockKeyhole size={12} />
              {t("aiSettings.cards.readonly")}
            </Badge>
          )
        }
        onOpenChange={(open) => !open && onClose()}
        open
        title={card.name}
        width={820}
      >
        <div className="argus-ic-detail">
          <VersionPicker
            activeRevision={card.active_revision}
            onChange={setRevision}
            revision={revision}
            versions={versions.data ?? []}
          />
          {version.data && <VersionPreview version={version.data} />}
          {version.data && (
            <section>
              <h3>Slots</h3>
              <div className="argus-ic-slots">
                {version.data.manifest.slots.map((slot) => (
                  <button
                    disabled={!canPublish}
                    key={slot.name}
                    onClick={() => setBindingSlot(slot.name)}
                    type="button"
                  >
                    <Link2 size={13} />
                    <span>{slot.name}</span>
                    <small>
                      {slot.kind} · {String(slot.value_type)}
                    </small>
                  </button>
                ))}
              </div>
            </section>
          )}
          {validation && version.data && (
            <ValidationRunner
              onComplete={() => {
                setValidation(null);
                void queryClient.invalidateQueries({
                  queryKey: ["interactive-card-version", card.id, revision],
                });
              }}
              run={validation}
              version={version.data}
            />
          )}
        </div>
      </FormDrawer>
      {bindingSlot && version.data && (
        <BindingDrawer
          card={card}
          onClose={() => setBindingSlot(null)}
          onSaved={(created) => {
            setBindingSlot(null);
            setRevision(created.revision);
            void refresh();
          }}
          slotName={bindingSlot}
          version={version.data}
        />
      )}
    </>
  );
}

function VersionPicker({
  activeRevision,
  revision,
  versions,
  onChange,
}: {
  activeRevision?: number;
  revision: number;
  versions: Array<{ revision: number; status: string }>;
  onChange: (revision: number) => void;
}) {
  return (
    <div className="argus-ic-version-picker">
      <History size={16} />
      <Select
        onValueChange={(value) => onChange(Number(value))}
        options={versions.map((version) => ({
          value: String(version.revision),
          label: `r${version.revision} · ${version.status}${version.revision === activeRevision ? " · active" : ""}`,
        }))}
        value={String(revision)}
      />
    </div>
  );
}

function VersionPreview({ version }: { version: CardVersion }) {
  const { i18n, t } = useTranslation();
  const { resolvedTheme } = useTheme();
  const [expanded, setExpanded] = useState(false);
  const locale = i18n.language === "en-US" ? "en-US" : "zh-CN";
  const demo = version.demos.find((item) => item.scenario === "default");
  return (
    <div className={`argus-ic-preview ${expanded ? "is-expanded" : ""}`}>
      <SandboxCard
        card_origin={cardOrigin()}
        color_scheme={resolvedTheme}
        html={version.entrypoint_html}
        initial_data={asObject(demo?.data)}
        locale={locale}
        manifest={version.manifest}
        max_height={expanded ? 1200 : 360}
        min_height={120}
        render_plan={previewPlan(version, locale, resolvedTheme)}
        title={`Card r${version.revision}`}
      />
      <Button
        className="argus-ic-preview__expand"
        onClick={() => setExpanded((value) => !value)}
        size="sm"
        variant="ghost"
      >
        {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        {expanded
          ? t("aiSettings.cards.collapse")
          : t("aiSettings.cards.expand")}
      </Button>
    </div>
  );
}

function ValidationRunner({
  run,
  version,
  onComplete,
}: {
  run: CardValidationRun;
  version: CardVersion;
  onComplete: () => void;
}) {
  const api = useApi();
  const [index, setIndex] = useState(0);
  const [reports, setReports] = useState<CardRuntimeValidationReport[]>([]);
  const submitted = useRef(false);
  const validationCase = VALIDATION_CASES[index];
  const demo = version.demos.find(
    (item) => item.scenario === validationCase?.scenario,
  );
  const submit = useMutation({
    mutationFn: () =>
      api.interactiveCards.submitValidationEvidence(run.id, {
        nonce: run.nonce,
        content_hash: run.content_hash,
        runtime_version: run.runtime_version,
        scenarios: reports.map((report) => ({
          scenario: report.scenario,
          ready: report.ready,
          protocol_violations: report.protocol_violations,
          runtime_errors: report.runtime_errors,
          serious_a11y_violations: report.serious_a11y_violations,
          missing_required_slots: report.missing_required_slots,
          size_violation: report.size_violation,
        })),
      }),
    onSuccess: onComplete,
  });
  useEffect(() => {
    if (reports.length !== VALIDATION_CASES.length || submitted.current) return;
    submitted.current = true;
    submit.mutate();
  }, [reports, submit]);
  if (!validationCase) {
    return (
      <div className="argus-ic-validation">
        <CheckCircle2 size={16} />
        {submit.isPending ? "Submitting evidence" : "Validation complete"}
      </div>
    );
  }
  return (
    <div className="argus-ic-validation">
      <Badge>{validationCase.scenario}</Badge>
      <SandboxCard
        card_origin={cardOrigin()}
        color_scheme={validationCase.color_scheme}
        html={version.entrypoint_html}
        initial_data={asObject(demo?.data)}
        key={validationCase.scenario}
        locale={validationCase.locale}
        manifest={version.manifest}
        onValidationReport={(report) => {
          setReports((current) =>
            current.some((item) => item.scenario === report.scenario)
              ? current
              : [...current, report],
          );
          setIndex((value) => value + 1);
        }}
        render_plan={previewPlan(
          version,
          validationCase.locale,
          validationCase.color_scheme,
        )}
        title={`Validation ${validationCase.scenario}`}
        validation={{
          nonce: run.nonce,
          content_hash: run.content_hash,
          runtime_version: run.runtime_version,
          scenario: validationCase.scenario,
        }}
      />
    </div>
  );
}

function BindingDrawer({
  card,
  version,
  slotName,
  onClose,
  onSaved,
}: {
  card: InteractiveCard;
  version: CardVersion;
  slotName: string;
  onClose: () => void;
  onSaved: (version: CardVersion) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const catalog = useQuery({
    queryKey: ["tool-schema-catalog"],
    queryFn: () => api.interactiveCards.listToolSchemas(),
  });
  const existing = version.slot_bindings.find(
    (binding) => binding.slot_name === slotName,
  );
  const tools = useMemo(() => catalog.data?.items ?? [], [catalog.data?.items]);
  const schema = z.object({
    mode: z.enum(["strict", "preferred"]),
    toolId: z.string().min(1, t("settings.common.required")),
    path: z.string().min(1, t("settings.common.required")),
  });
  type BindingFormValues = z.infer<typeof schema>;
  const {
    control,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<BindingFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      mode: existing?.mode ?? "strict",
      toolId: existing?.tool_id ?? "",
      path: existing?.path ?? "",
    },
  });
  const toolId = watch("toolId");
  const tool = tools.find((item) => item.tool_id === toolId) ?? tools[0];
  useEffect(() => {
    if (!toolId && tools[0]) setValue("toolId", tools[0].tool_id);
  }, [setValue, toolId, tools]);
  useEffect(() => {
    const currentPath = watch("path");
    if (tool && !tool.fields.some((field) => field.path === currentPath)) {
      setValue("path", tool.fields[0]?.path ?? "");
    }
  }, [setValue, tool, watch]);
  const save = useMutation({
    mutationFn: (values: BindingFormValues) => {
      const selectedTool = tools.find((item) => item.tool_id === values.toolId);
      if (!selectedTool) throw new Error("Tool Schema required");
      const field = selectedTool.fields.find(
        (item) => item.path === values.path,
      );
      if (!field) throw new Error("Tool field required");
      const binding: SlotBinding = {
        slot_name: slotName,
        slot_kind: existing?.slot_kind ?? "data",
        mode: values.mode,
        tool_id: selectedTool.tool_id,
        output_schema_version: selectedTool.output_schema_version,
        schema_hash: selectedTool.schema_hash,
        path: field.path,
        value_type: field.value_type,
        semantic_type: field.semantic_type,
      };
      return api.interactiveCards.createConfigurationVersion(card.id, {
        base_revision: version.revision,
        expected_version: card.version,
        name: card.name,
        description: card.description,
        slot_bindings: [
          ...version.slot_bindings.filter(
            (item) => item.slot_name !== slotName,
          ),
          binding,
        ],
        demos: version.demos,
      });
    },
    onSuccess: onSaved,
  });
  return (
    <FormDrawer
      loading={save.isPending}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={handleSubmit((values) => save.mutate(values))}
      open
      submitLabel={t("aiSettings.cards.saveBinding")}
      title={`${t("aiSettings.cards.bindingTitle")} · ${slotName}`}
    >
      <div className="argus-settings-form">
        {save.isError && (
          <Alert
            description={formatApiError(
              save.error,
              t("aiSettings.cards.validationFailed"),
              (requestId) => t("common.requestReference", { requestId }),
            )}
            title={t("aiSettings.cards.validationFailed")}
            tone="danger"
          />
        )}
        <Field
          error={errors.mode?.message}
          requirement="required"
          label={t("aiSettings.cards.bindingMode")}
        >
          <Controller
            control={control}
            name="mode"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={[
                  { value: "strict", label: "strict" },
                  { value: "preferred", label: "preferred" },
                ]}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          error={errors.toolId?.message}
          requirement="required"
          label={t("aiSettings.cards.tool")}
        >
          <Controller
            control={control}
            name="toolId"
            render={({ field }) => (
              <Select
                onValueChange={(value) => {
                  field.onChange(value);
                  setValue("path", "");
                }}
                options={tools.map(toolOption)}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          error={errors.path?.message}
          requirement="required"
          label={t("aiSettings.cards.field")}
        >
          <Controller
            control={control}
            name="path"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={(tool?.fields ?? []).map((option) => ({
                  value: option.path,
                  label: `${option.path} · ${String(option.value_type)}`,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}

function previewPlan(
  version: CardVersion,
  locale: "zh-CN" | "en-US",
  colorScheme: "light" | "dark",
) {
  return {
    schema_version: "argus.render_plan/v1" as const,
    card_id: version.card_id,
    card_revision: version.revision,
    card_instance_id: `preview-${version.card_id}-${version.revision}`,
    data_bindings: [],
    query_binding_ids: {},
    action_binding_ids: {},
    locale,
    color_scheme: colorScheme,
  };
}

function asObject(value: CardDemo["data"] | undefined) {
  return (value ?? {}) as Record<string, unknown>;
}

function toolOption(tool: ToolSchemaCatalogEntry) {
  return {
    value: tool.tool_id,
    label: `${tool.tool_id} · ${tool.output_schema_version}`,
  };
}

function availabilityTone(value: InteractiveCard["availability"]) {
  if (value === "available") return "success" as const;
  if (value === "dependency_pending") return "warning" as const;
  return "neutral" as const;
}
