import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type ConnectionTestResult,
  type SandboxBackend,
} from "@argus/api-client";
import {
  Alert,
  Button,
  DataTable,
  Field,
  FormDrawer,
  Input,
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
    queryClient.invalidateQueries({ queryKey: ["platform", "sandboxBackends"] });

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
    <div className="platform-stack">
      <div className="tab-toolbar">
        <Button onClick={() => setForm(EMPTY_FORM)} variant="primary">
          {t("sandbox.backends.add")}
        </Button>
      </div>

      {testResult && (
        <Alert
          description={`${testResult.result.checks
            .map((check) => `${check.name}: ${check.status}`)
            .join(" · ")} — ${t("sandbox.backends.test.latency", { ms: testResult.result.latencyMs })}`}
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
              render: (row) => <code className="mono">{row.endpoint}</code>,
            },
            {
              key: "credentialRef",
              header: t("sandbox.backends.table.credential"),
              render: (row) => <code className="mono">{row.credentialRef}</code>,
            },
            {
              key: "tlsVerify",
              header: t("sandbox.backends.table.tls"),
              render: (row) =>
                t(row.tlsVerify ? "common.yes" : "common.no"),
            },
            {
              key: "defaultStorage",
              header: t("sandbox.backends.table.storage"),
              render: (row) => <code className="mono">{row.defaultStorage}</code>,
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
                <div className="row-actions">
                  <Button
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
                    size="sm"
                    variant="ghost"
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    loading={test.isPending && test.variables === row.id}
                    onClick={() => test.mutate(row.id)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("sandbox.backends.test")}
                  </Button>
                </div>
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
        title={t(
          form?.id ? "sandbox.backends.edit" : "sandbox.backends.add",
        )}
      >
        {form && (
          <>
            <Field label={t("sandbox.backends.form.name")}>
              <Input
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
                required
                value={form.name}
              />
            </Field>
            <Field label={t("sandbox.backends.form.endpoint")}>
              <Input
                onChange={(event) =>
                  setForm({ ...form, endpoint: event.target.value })
                }
                placeholder="https://sandbox.internal"
                required
                value={form.endpoint}
              />
            </Field>
            <Field label={t("sandbox.backends.form.credentialRef")}>
              <Input
                onChange={(event) =>
                  setForm({ ...form, credentialRef: event.target.value })
                }
                required
                value={form.credentialRef}
              />
            </Field>
            <Field label={t("sandbox.backends.form.defaultStorage")}>
              <Input
                onChange={(event) =>
                  setForm({ ...form, defaultStorage: event.target.value })
                }
                value={form.defaultStorage}
              />
            </Field>
            <label className="switch-row">
              <Switch
                checked={form.tlsVerify}
                label={t("sandbox.backends.form.tlsVerify")}
                onChange={(checked) =>
                  setForm({ ...form, tlsVerify: checked })
                }
              />
              <span>{t("sandbox.backends.form.tlsVerify")}</span>
            </label>
          </>
        )}
      </FormDrawer>
    </div>
  );
}
