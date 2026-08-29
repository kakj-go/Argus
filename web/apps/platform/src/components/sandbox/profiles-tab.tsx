import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  formConstraint,
  presentApiFormError,
  useApi,
  type CreateSandboxProfileInput,
  type SandboxImage,
  type SandboxProfile,
} from "@argus/api-client";
import {
  Badge,
  Alert,
  Button,
  DataTable,
  Field,
  FormDrawer,
  Input,
  RowAction,
  Select,
  Spinner,
  Switch,
  Textarea,
} from "@argus/ui";

type ProfileRow = {
  id: string;
  name: string;
  image: string;
  resources: string;
  timeouts: string;
  network: string;
  capabilities: string[];
  builtin: boolean;
  enabled: boolean;
};

const profileConstraints = {
  cpu: formConstraint("SandboxProfileWrite", "cpu_millis"),
  memory: formConstraint("SandboxProfileWrite", "memory_mib"),
  name: formConstraint("SandboxProfileWrite", "name"),
  timeout: formConstraint("SandboxProfileWrite", "timeout_seconds"),
};

const cpuMinimum = (profileConstraints.cpu.minimum ?? 100) / 1000;
const cpuMaximum = (profileConstraints.cpu.maximum ?? 16000) / 1000;

type FormState = {
  id: string | null;
  name: string;
  description: string;
  imageId: string;
  cpu: number;
  memoryMb: number;
  diskMb: number;
  pids: number;
  commandSeconds: number;
  idleSeconds: number;
  lifetimeSeconds: number;
  networkMode: "deny_all" | "allow_list";
  allowedDomains: string;
  fileUpload: boolean;
  artifactDownload: boolean;
  secretInjection: boolean;
  gpu: boolean;
};

function formFromProfile(profile: SandboxProfile): FormState {
  return {
    id: profile.id,
    name: profile.name,
    description: profile.description,
    imageId: profile.imageId,
    cpu: profile.resources.cpu,
    memoryMb: profile.resources.memoryMb,
    diskMb: profile.resources.diskMb,
    pids: profile.resources.pids,
    commandSeconds: profile.timeouts.commandSeconds,
    idleSeconds: profile.timeouts.idleSeconds,
    lifetimeSeconds: profile.timeouts.lifetimeSeconds,
    networkMode: profile.network.mode,
    allowedDomains: profile.network.allowedDomains.join(", "),
    fileUpload: profile.capabilities.fileUpload,
    artifactDownload: profile.capabilities.artifactDownload,
    secretInjection: profile.capabilities.secretInjection,
    gpu: profile.capabilities.gpu,
  };
}

function emptyForm(imageId: string): FormState {
  return {
    id: null,
    name: "",
    description: "",
    imageId,
    cpu: 1,
    memoryMb: 1024,
    diskMb: 2048,
    pids: 128,
    commandSeconds: 300,
    idleSeconds: 180,
    lifetimeSeconds: 900,
    networkMode: "deny_all",
    allowedDomains: "",
    fileUpload: true,
    artifactDownload: true,
    secretInjection: false,
    gpu: false,
  };
}

function toInput(form: FormState): CreateSandboxProfileInput {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    imageId: form.imageId,
    resources: {
      cpu: form.cpu,
      memoryMb: form.memoryMb,
      diskMb: form.diskMb,
      pids: form.pids,
    },
    timeouts: {
      commandSeconds: form.commandSeconds,
      idleSeconds: form.idleSeconds,
      lifetimeSeconds: form.lifetimeSeconds,
    },
    network: {
      mode: form.networkMode,
      allowedDomains: form.allowedDomains
        .split(",")
        .map((entry) => entry.trim())
        .filter(Boolean),
    },
    capabilities: {
      fileUpload: form.fileUpload,
      artifactDownload: form.artifactDownload,
      secretInjection: form.secretInjection,
      gpu: form.gpu,
    },
  };
}

/** Sandbox Profile Tab：AI 唯一可选的执行单元，平台超管 CRUD + 启停。 */
export function ProfilesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [form, setForm] = useState<FormState | null>(null);

  const profiles = useQuery({
    queryKey: ["platform", "profiles"],
    queryFn: () => api.platform.profiles.list(),
  });
  const images = useQuery({
    queryKey: ["platform", "images"],
    queryFn: () => api.platform.images.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "profiles"] });

  const save = useMutation({
    mutationFn: (input: FormState) =>
      input.id
        ? api.platform.profiles.update(input.id, toInput(input))
        : api.platform.profiles.create(toInput(input)),
    onSuccess: () => {
      setForm(null);
      void invalidate();
    },
  });

  const toggle = useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.platform.profiles.update(input.id, { enabled: input.enabled }),
    onSuccess: () => void invalidate(),
  });

  const imageName = (id: string) =>
    images.data?.find((item) => item.id === id)?.name ?? id;

  const rows: ProfileRow[] = (profiles.data ?? []).map((item) => ({
    id: item.id,
    name: item.name,
    image: imageName(item.imageId),
    resources: `${item.resources.cpu}C / ${item.resources.memoryMb}MB / ${item.resources.diskMb}MB`,
    timeouts: `${item.timeouts.commandSeconds}/${item.timeouts.idleSeconds}/${item.timeouts.lifetimeSeconds}`,
    network:
      item.network.mode === "deny_all"
        ? t("sandbox.profiles.network.deny_all")
        : `${t("sandbox.profiles.network.allow_list")}: ${item.network.allowedDomains.join(", ")}`,
    capabilities: (
      ["fileUpload", "artifactDownload", "secretInjection", "gpu"] as const
    )
      .filter((cap) => item.capabilities[cap])
      .map((cap) => t(`sandbox.profiles.caps.${cap}`)),
    builtin: item.builtin,
    enabled: item.enabled,
  }));

  const findProfile = (id: string) =>
    profiles.data?.find((item) => item.id === id);

  return (
    <div className="argus-platform-stack">
      <div className="argus-tab-toolbar">
        <Button
          onClick={() => setForm(emptyForm(images.data?.[0]?.id ?? ""))}
          variant="primary"
        >
          {t("sandbox.profiles.add")}
        </Button>
      </div>

      {profiles.isPending ? (
        <Spinner />
      ) : (
        <DataTable<ProfileRow>
          columns={[
            {
              key: "name",
              header: t("sandbox.profiles.table.name"),
              render: (row) => (
                <span className="argus-cell-with-badge">
                  <code className="argus-mono">{row.name}</code>
                  {row.builtin && (
                    <Badge>{t("sandbox.profiles.builtin")}</Badge>
                  )}
                </span>
              ),
            },
            { key: "image", header: t("sandbox.profiles.table.image") },
            {
              key: "resources",
              header: t("sandbox.profiles.table.resources"),
              render: (row) => (
                <code className="argus-mono">{row.resources}</code>
              ),
            },
            {
              key: "timeouts",
              header: t("sandbox.profiles.table.timeouts"),
              render: (row) => (
                <code className="argus-mono">{row.timeouts}</code>
              ),
            },
            { key: "network", header: t("sandbox.profiles.table.network") },
            {
              key: "capabilities",
              header: t("sandbox.profiles.table.capabilities"),
              render: (row) => (
                <span className="argus-cell-with-badge">
                  {row.capabilities.map((cap) => (
                    <Badge key={cap}>{cap}</Badge>
                  ))}
                </span>
              ),
            },
            {
              key: "enabled",
              header: t("sandbox.profiles.table.enabled"),
              render: (row) => (
                <Switch
                  checked={row.enabled}
                  label={t("sandbox.profiles.table.enabled")}
                  onChange={(checked) =>
                    toggle.mutate({ id: row.id, enabled: checked })
                  }
                />
              ),
            },
            {
              key: "id",
              header: t("common.actions"),
              render: (row) => (
                <RowAction
                  onClick={() => {
                    const profile = findProfile(row.id);
                    if (profile) setForm(formFromProfile(profile));
                  }}
                >
                  {t("common.edit")}
                </RowAction>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      {form && (
        <ProfileFormDrawer
          images={images.data ?? []}
          initial={form}
          loading={save.isPending}
          onClose={() => setForm(null)}
          onSubmit={(values) => save.mutateAsync(values)}
        />
      )}
    </div>
  );
}

const numericProfileFields = [
  "cpu",
  "memoryMb",
  "diskMb",
  "pids",
  "commandSeconds",
  "idleSeconds",
  "lifetimeSeconds",
] as const;

const capabilityFields = [
  "fileUpload",
  "artifactDownload",
  "secretInjection",
  "gpu",
] as const;

function ProfileFormDrawer({
  initial,
  images,
  loading,
  onClose,
  onSubmit,
}: {
  initial: FormState;
  images: SandboxImage[];
  loading: boolean;
  onClose: () => void;
  onSubmit: (values: FormState) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(profileConstraints.name.minLength ?? 1, t("sandbox.form.required"))
      .max(profileConstraints.name.maxLength ?? 128),
    description: z.string(),
    imageId: z.string().min(1, t("sandbox.form.required")),
    cpu: z
      .number({ error: t("sandbox.form.required") })
      .min(cpuMinimum)
      .max(cpuMaximum),
    memoryMb: z
      .number({ error: t("sandbox.form.required") })
      .min(profileConstraints.memory.minimum ?? 128)
      .max(profileConstraints.memory.maximum ?? 65536),
    diskMb: z.number({ error: t("sandbox.form.required") }).min(0),
    pids: z
      .number({ error: t("sandbox.form.required") })
      .int()
      .min(0),
    commandSeconds: z
      .number({ error: t("sandbox.form.required") })
      .min(profileConstraints.timeout.minimum ?? 10)
      .max(profileConstraints.timeout.maximum ?? 3600),
    idleSeconds: z
      .number({ error: t("sandbox.form.required") })
      .min(profileConstraints.timeout.minimum ?? 10)
      .max(profileConstraints.timeout.maximum ?? 3600),
    lifetimeSeconds: z
      .number({ error: t("sandbox.form.required") })
      .min(profileConstraints.timeout.minimum ?? 10)
      .max(profileConstraints.timeout.maximum ?? 3600),
    networkMode: z.enum(["deny_all", "allow_list"]),
    allowedDomains: z.string(),
    fileUpload: z.boolean(),
    artifactDownload: z.boolean(),
    secretInjection: z.boolean(),
    gpu: z.boolean(),
  });
  type Values = z.infer<typeof schema>;
  const {
    control,
    clearErrors,
    handleSubmit,
    register,
    setError,
    watch,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: initial,
  });
  const networkMode = watch("networkMode");
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({ ...values, id: initial.id });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("sandbox.form.saveFailed"),
        fieldMap: {
          backend_id: "imageId",
          cpu_millis: "cpu",
          image_id: "imageId",
          memory_mib: "memoryMb",
          name: "name",
          network_mode: "networkMode",
          timeout_seconds: "commandSeconds",
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
  const numberField = (key: (typeof numericProfileFields)[number]) => {
    const bounds =
      key === "cpu"
        ? { maximum: cpuMaximum, minimum: cpuMinimum, step: 0.1 }
        : key === "memoryMb"
          ? profileConstraints.memory
          : key === "commandSeconds" ||
              key === "idleSeconds" ||
              key === "lifetimeSeconds"
            ? profileConstraints.timeout
            : { minimum: 0 };
    return (
      <Field
        error={errors[key]?.message}
        requirement="required"
        label={t(`sandbox.profiles.form.${key}`)}
      >
        <Input
          {...register(key, { valueAsNumber: true })}
          max={bounds.maximum}
          min={bounds.minimum}
          step={"step" in bounds ? bounds.step : undefined}
          type="number"
        />
      </Field>
    );
  };
  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={submit}
      open
      submitLabel={t("common.save")}
      title={t(initial.id ? "sandbox.profiles.edit" : "sandbox.profiles.add")}
      width={560}
    >
      <div className="argus-drawer-stack">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("sandbox.form.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          error={errors.name?.message}
          requirement="required"
          label={t("sandbox.profiles.form.name")}
        >
          <Input
            {...register("name")}
            maxLength={profileConstraints.name.maxLength}
          />
        </Field>
        <Field
          error={errors.description?.message}
          requirement="optional"
          label={t("sandbox.profiles.form.description")}
        >
          <Textarea {...register("description")} rows={2} />
        </Field>
        <Field
          error={errors.imageId?.message}
          requirement="required"
          label={t("sandbox.profiles.form.image")}
        >
          <Controller
            control={control}
            name="imageId"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={images.map((image) => ({
                  value: image.id,
                  label: image.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>

        <section className="argus-drawer-section">
          <h3>{t("sandbox.profiles.form.resources")}</h3>
          <div className="argus-form-grid">
            {numericProfileFields.slice(0, 4).map(numberField)}
          </div>
        </section>

        <section className="argus-drawer-section">
          <h3>{t("sandbox.profiles.form.timeouts")}</h3>
          <div className="argus-form-grid">
            {numericProfileFields.slice(4).map(numberField)}
          </div>
        </section>

        <section className="argus-drawer-section">
          <h3>{t("sandbox.profiles.form.network")}</h3>
          <Field
            error={errors.networkMode?.message}
            requirement="required"
            label={t("sandbox.profiles.form.networkMode")}
          >
            <Controller
              control={control}
              name="networkMode"
              render={({ field }) => (
                <Select
                  onValueChange={field.onChange}
                  options={[
                    {
                      value: "deny_all",
                      label: t("sandbox.profiles.network.deny_all"),
                    },
                    {
                      value: "allow_list",
                      label: t("sandbox.profiles.network.allow_list"),
                    },
                  ]}
                  value={field.value}
                />
              )}
            />
          </Field>
          {networkMode === "allow_list" && (
            <Field
              error={errors.allowedDomains?.message}
              requirement="optional"
              hint={t("sandbox.profiles.form.allowedDomainsHint")}
              label={t("sandbox.profiles.form.allowedDomains")}
            >
              <Input {...register("allowedDomains")} />
            </Field>
          )}
        </section>

        <section className="argus-drawer-section">
          <h3>{t("sandbox.profiles.form.capabilities")}</h3>
          {capabilityFields.map((capability) => (
            <Controller
              control={control}
              key={capability}
              name={capability}
              render={({ field }) => (
                <label className="argus-switch-row">
                  <Switch
                    checked={field.value}
                    label={t(`sandbox.profiles.caps.${capability}`)}
                    onChange={field.onChange}
                  />
                  <span>{t(`sandbox.profiles.caps.${capability}`)}</span>
                </label>
              )}
            />
          ))}
        </section>
      </div>
    </FormDrawer>
  );
}
