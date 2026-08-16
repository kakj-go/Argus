import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type CreateSandboxProfileInput,
  type SandboxProfile,
} from "@argus/api-client";
import {
  Badge,
  Button,
  DataTable,
  Field,
  FormDrawer,
  Input,
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

  const numberField = (
    label: string,
    key:
      | "cpu"
      | "memoryMb"
      | "diskMb"
      | "pids"
      | "commandSeconds"
      | "idleSeconds"
      | "lifetimeSeconds",
  ) =>
    form && (
      <Field label={label}>
        <Input
          min={0}
          onChange={(event) =>
            setForm({ ...form, [key]: Number(event.target.value) || 0 })
          }
          type="number"
          value={String(form[key])}
        />
      </Field>
    );

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
                <Button
                  onClick={() => {
                    const profile = findProfile(row.id);
                    if (profile) setForm(formFromProfile(profile));
                  }}
                  size="sm"
                  variant="ghost"
                >
                  {t("common.edit")}
                </Button>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <FormDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          if (!open) setForm(null);
        }}
        onSubmit={() => form && save.mutate(form)}
        open={form !== null}
        submitLabel={t("common.save")}
        title={t(form?.id ? "sandbox.profiles.edit" : "sandbox.profiles.add")}
        width={560}
      >
        {form && (
          <div className="argus-drawer-stack">
            <Field label={t("sandbox.profiles.form.name")}>
              <Input
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
                required
                value={form.name}
              />
            </Field>
            <Field label={t("sandbox.profiles.form.description")}>
              <Textarea
                onChange={(event) =>
                  setForm({ ...form, description: event.target.value })
                }
                rows={2}
                value={form.description}
              />
            </Field>
            <Field label={t("sandbox.profiles.form.image")}>
              <Select
                onValueChange={(value) => setForm({ ...form, imageId: value })}
                options={(images.data ?? []).map((image) => ({
                  value: image.id,
                  label: image.name,
                }))}
                value={form.imageId}
              />
            </Field>

            <section className="argus-drawer-section">
              <h3>{t("sandbox.profiles.form.resources")}</h3>
              <div className="argus-form-grid">
                {numberField(t("sandbox.profiles.form.cpu"), "cpu")}
                {numberField(t("sandbox.profiles.form.memoryMb"), "memoryMb")}
                {numberField(t("sandbox.profiles.form.diskMb"), "diskMb")}
                {numberField(t("sandbox.profiles.form.pids"), "pids")}
              </div>
            </section>

            <section className="argus-drawer-section">
              <h3>{t("sandbox.profiles.form.timeouts")}</h3>
              <div className="argus-form-grid">
                {numberField(
                  t("sandbox.profiles.form.commandSeconds"),
                  "commandSeconds",
                )}
                {numberField(
                  t("sandbox.profiles.form.idleSeconds"),
                  "idleSeconds",
                )}
                {numberField(
                  t("sandbox.profiles.form.lifetimeSeconds"),
                  "lifetimeSeconds",
                )}
              </div>
            </section>

            <section className="argus-drawer-section">
              <h3>{t("sandbox.profiles.form.network")}</h3>
              <Field label={t("sandbox.profiles.form.networkMode")}>
                <Select
                  onValueChange={(value) =>
                    setForm({
                      ...form,
                      networkMode: value as "deny_all" | "allow_list",
                    })
                  }
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
                  value={form.networkMode}
                />
              </Field>
              {form.networkMode === "allow_list" && (
                <Field
                  hint={t("sandbox.profiles.form.allowedDomainsHint")}
                  label={t("sandbox.profiles.form.allowedDomains")}
                >
                  <Input
                    onChange={(event) =>
                      setForm({ ...form, allowedDomains: event.target.value })
                    }
                    value={form.allowedDomains}
                  />
                </Field>
              )}
            </section>

            <section className="argus-drawer-section">
              <h3>{t("sandbox.profiles.form.capabilities")}</h3>
              {(
                [
                  "fileUpload",
                  "artifactDownload",
                  "secretInjection",
                  "gpu",
                ] as const
              ).map((cap) => (
                <label className="argus-switch-row" key={cap}>
                  <Switch
                    checked={form[cap]}
                    label={t(`sandbox.profiles.caps.${cap}`)}
                    onChange={(checked) => setForm({ ...form, [cap]: checked })}
                  />
                  <span>{t(`sandbox.profiles.caps.${cap}`)}</span>
                </label>
              ))}
            </section>
          </div>
        )}
      </FormDrawer>
    </div>
  );
}
