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
  type ConnectionTestResult,
  type SandboxBackend,
} from "@argus/api-client";
import {
  ActionGroup,
  Alert,
  Button,
  DataTable,
  Field,
  FormDrawer,
  Input,
  RowAction,
  Spinner,
  StatusBadge,
  Switch,
} from "@argus/ui";

type BackendRow = {
  id: string;
  name: string;
  endpoint: string;
  credentialRef: string;
  tlsVerify: boolean;
  enabled: boolean;
  defaultStorage: string;
  healthStatus: SandboxBackend["healthStatus"];
};

function healthTone(status: SandboxBackend["healthStatus"]) {
  if (status === "healthy") return "success" as const;
  if (status === "degraded") return "warning" as const;
  return "danger" as const;
}

type FormState = {
  id: string | null;
  name: string;
  endpoint: string;
  credentialRef: string;
  tlsVerify: boolean;
  defaultStorage: string;
};

const EMPTY_FORM: FormState = {
  id: null,
  name: "",
  endpoint: "",
  credentialRef: "",
  tlsVerify: true,
  defaultStorage: "sandbox-artifacts",
};

const backendConstraints = {
  credential: formConstraint("SandboxBackendWrite", "api_key"),
  endpoint: formConstraint("SandboxBackendWrite", "endpoint"),
  name: formConstraint("SandboxBackendWrite", "name"),
};

/** 服务连接 Tab：SandboxBackend 列表、新建/编辑、启停、内联连接测试。 */
export function BackendsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [form, setForm] = useState<FormState | null>(null);
  const [testResult, setTestResult] = useState<{
    id: string;
    result: ConnectionTestResult;
  } | null>(null);

  const backends = useQuery({
    queryKey: ["platform", "sandboxBackends"],
    queryFn: () => api.platform.sandboxBackends.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ["platform", "sandboxBackends"],
    });

  const save = useMutation({
    mutationFn: (input: FormState) =>
      input.id
        ? api.platform.sandboxBackends.update(input.id, {
            name: input.name,
            endpoint: input.endpoint,
            credentialRef: input.credentialRef,
            tlsVerify: input.tlsVerify,
            defaultStorage: input.defaultStorage,
          })
        : api.platform.sandboxBackends.create({
            name: input.name,
            endpoint: input.endpoint,
            credentialRef: input.credentialRef,
            tlsVerify: input.tlsVerify,
            defaultStorage: input.defaultStorage,
          }),
    onSuccess: () => {
      setForm(null);
      void invalidate();
    },
  });

  const toggle = useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.platform.sandboxBackends.update(input.id, { enabled: input.enabled }),
    onSuccess: () => void invalidate(),
  });

  const test = useMutation({
    mutationFn: (id: string) => api.platform.sandboxBackends.test(id),
    onSuccess: (result, id) => setTestResult({ id, result }),
  });

  const rows: BackendRow[] = (backends.data ?? []).map((item) => ({
    id: item.id,
    name: item.name,
    endpoint: item.endpoint,
    credentialRef: item.credentialRef,
    tlsVerify: item.tlsVerify,
    enabled: item.enabled,
    defaultStorage: item.defaultStorage,
    healthStatus: item.healthStatus,
  }));

  return (
    <div className="argus-platform-stack">
      <div className="argus-tab-toolbar">
        <Button onClick={() => setForm(EMPTY_FORM)} variant="primary">
          {t("sandbox.backends.add")}
        </Button>
      </div>

      {testResult && (
        <Alert
          description={`${testResult.result.checks
            .map((check) => `${check.name}: ${check.status}`)
            .join(
              " · ",
            )} — ${t("sandbox.backends.test.latency", { ms: testResult.result.latencyMs })}`}
          title={t(
            testResult.result.success
              ? "sandbox.backends.test.success"
              : "sandbox.backends.test.failure",
          )}
          tone={testResult.result.success ? "success" : "danger"}
        />
      )}

      {backends.isPending ? (
        <Spinner />
      ) : (
        <DataTable<BackendRow>
          columns={[
            { key: "name", header: t("sandbox.backends.table.name") },
            {
              key: "endpoint",
              header: t("sandbox.backends.table.endpoint"),
              render: (row) => (
                <code className="argus-mono">{row.endpoint}</code>
              ),
            },
            {
              key: "credentialRef",
              header: t("sandbox.backends.table.credential"),
              render: (row) => (
                <code className="argus-mono">{row.credentialRef}</code>
              ),
            },
            {
              key: "tlsVerify",
              header: t("sandbox.backends.table.tls"),
              render: (row) => t(row.tlsVerify ? "common.yes" : "common.no"),
            },
            {
              key: "defaultStorage",
              header: t("sandbox.backends.table.storage"),
              render: (row) => (
                <code className="argus-mono">{row.defaultStorage}</code>
              ),
            },
            {
              key: "healthStatus",
              header: t("sandbox.backends.table.health"),
              render: (row) => (
                <StatusBadge tone={healthTone(row.healthStatus)}>
                  {t(`sandbox.backends.health.${row.healthStatus}`)}
                </StatusBadge>
              ),
            },
            {
              key: "enabled",
              header: t("common.status"),
              render: (row) => (
                <Switch
                  checked={row.enabled}
                  label={t("common.status")}
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
                <ActionGroup>
                  <RowAction
                    onClick={() =>
                      setForm({
                        id: row.id,
                        name: row.name,
                        endpoint: row.endpoint,
                        credentialRef: row.credentialRef,
                        tlsVerify: row.tlsVerify,
                        defaultStorage: row.defaultStorage,
                      })
                    }
                  >
                    {t("common.edit")}
                  </RowAction>
                  <RowAction
                    loading={test.isPending && test.variables === row.id}
                    onClick={() => test.mutate(row.id)}
                  >
                    {t("sandbox.backends.test")}
                  </RowAction>
                </ActionGroup>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      {form && (
        <BackendFormDrawer
          initial={form}
          loading={save.isPending}
          onClose={() => setForm(null)}
          onSubmit={(values) => save.mutateAsync(values)}
        />
      )}
    </div>
  );
}

function BackendFormDrawer({
  initial,
  loading,
  onClose,
  onSubmit,
}: {
  initial: FormState;
  loading: boolean;
  onClose: () => void;
  onSubmit: (values: FormState) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(backendConstraints.name.minLength ?? 1, t("sandbox.form.required"))
      .max(backendConstraints.name.maxLength ?? 128),
    endpoint: z
      .string()
      .trim()
      .min(1, t("sandbox.form.required"))
      .max(backendConstraints.endpoint.maxLength ?? 2048)
      .refine((value) => {
        try {
          const url = new URL(value);
          return url.protocol === "http:" || url.protocol === "https:";
        } catch {
          return false;
        }
      }, t("sandbox.form.urlInvalid")),
    credentialRef: z
      .string()
      .trim()
      .max(backendConstraints.credential.maxLength ?? 8192)
      .refine(
        (value) => initial.id !== null || value.length > 0,
        t("sandbox.form.required"),
      ),
    tlsVerify: z.boolean(),
    defaultStorage: z.string(),
  });
  type Values = z.infer<typeof schema>;
  const {
    control,
    clearErrors,
    handleSubmit,
    register,
    setError,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: initial,
  });
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({ ...values, id: initial.id });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("sandbox.form.saveFailed"),
        fieldMap: {
          api_key: "credentialRef",
          credentialRef: "credentialRef",
          endpoint: "endpoint",
          name: "name",
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
      onOpenChange={(open) => !open && onClose()}
      onSubmit={submit}
      open
      submitLabel={t("common.save")}
      title={t(initial.id ? "sandbox.backends.edit" : "sandbox.backends.add")}
    >
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
        label={t("sandbox.backends.form.name")}
      >
        <Input
          {...register("name")}
          maxLength={backendConstraints.name.maxLength}
        />
      </Field>
      <Field
        error={errors.endpoint?.message}
        requirement="required"
        label={t("sandbox.backends.form.endpoint")}
      >
        <Input
          {...register("endpoint")}
          maxLength={backendConstraints.endpoint.maxLength}
          placeholder="https://sandbox.internal"
          type="url"
        />
      </Field>
      <Field
        error={errors.credentialRef?.message}
        requirement={initial.id ? "optional" : "required"}
        label={t("sandbox.backends.form.credentialRef")}
      >
        <Input
          {...register("credentialRef")}
          maxLength={backendConstraints.credential.maxLength}
        />
      </Field>
      <Field
        error={errors.defaultStorage?.message}
        requirement="optional"
        label={t("sandbox.backends.form.defaultStorage")}
      >
        <Input {...register("defaultStorage")} />
      </Field>
      <Controller
        control={control}
        name="tlsVerify"
        render={({ field }) => (
          <label className="argus-switch-row">
            <Switch
              checked={field.value}
              label={t("sandbox.backends.form.tlsVerify")}
              onChange={field.onChange}
            />
            <span>{t("sandbox.backends.form.tlsVerify")}</span>
          </label>
        )}
      />
    </FormDrawer>
  );
}
