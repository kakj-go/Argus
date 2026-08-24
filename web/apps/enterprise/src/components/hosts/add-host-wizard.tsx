import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { z } from "zod";
import {
  formConstraint,
  presentApiFormError,
  useApi,
  type BastionScope,
  type Environment,
  type PendingActionPublic,
} from "@argus/api-client";
import type {
  ConnectionTest,
  HostPreviewCreate,
} from "@argus/api-client/contracts";
import {
  Alert,
  Button,
  CheckItem,
  Field,
  FormDrawer,
  Input,
  KeyValueGrid,
  Select,
  Textarea,
  Wizard,
} from "@argus/ui";
import { ARGUS_EGRESS_ADDRESSES, parseLabels } from "./host-utils";
import { PendingActionConfirm } from "./pending-action-confirm";

type Mode = "via_bastion" | "direct";
type Protocol = "ssh" | "winrm";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];
const hostConstraints = {
  address: formConstraint("HostPreviewCreate", "address"),
  name: formConstraint("HostPreviewCreate", "name"),
  port: formConstraint("HostPreviewCreate", "port"),
  username: formConstraint("HostPreviewCreate", "username"),
};

type HostWizardForm = {
  mode: Mode;
  scopeId: string;
  name: string;
  address: string;
  port: string;
  protocol: Protocol;
  platform: "linux" | "windows";
  account: string;
  credentialId: string;
  environment: Environment;
  labelsText: string;
};

export function AddHostWizard({
  open,
  onOpenChange,
  scopes,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 已激活、可选为接入点的 Bastion Scope。 */
  scopes: BastionScope[];
  onCreated: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const egressDisplay =
    ARGUS_EGRESS_ADDRESSES.join(", ") || t("hosts.wizard.egressNotConfigured");

  const [step, setStep] = useState(0);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ConnectionTest | null>(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const submitIntent = useRef<"preview" | "test">("preview");
  const schema = useMemo(
    () =>
      z
        .object({
          mode: z.enum(["via_bastion", "direct"]),
          scopeId: z.string(),
          name: z
            .string()
            .trim()
            .min(
              hostConstraints.name.minLength ?? 1,
              t("hosts.wizard.required"),
            )
            .max(hostConstraints.name.maxLength ?? 128),
          address: z
            .string()
            .trim()
            .min(
              hostConstraints.address.minLength ?? 1,
              t("hosts.wizard.required"),
            )
            .max(hostConstraints.address.maxLength ?? 512),
          port: z.string().refine((value) => {
            const parsed = Number(value);
            return (
              Number.isInteger(parsed) &&
              parsed >= (hostConstraints.port.minimum ?? 1) &&
              parsed <= (hostConstraints.port.maximum ?? 65535)
            );
          }, t("hosts.wizard.portInvalid")),
          protocol: z.enum(["ssh", "winrm"]),
          platform: z.enum(["linux", "windows"]),
          account: z
            .string()
            .trim()
            .min(
              hostConstraints.username.minLength ?? 1,
              t("hosts.wizard.required"),
            )
            .max(hostConstraints.username.maxLength ?? 256),
          credentialId: z.string().min(1, t("hosts.wizard.required")),
          environment: z.enum(ENVIRONMENTS),
          labelsText: z.string(),
        })
        .superRefine((value, context) => {
          if (value.mode === "via_bastion" && !value.scopeId) {
            context.addIssue({
              code: "custom",
              message: t("hosts.wizard.scopeRequired"),
              path: ["scopeId"],
            });
          }
        }),
    [t],
  );
  const form = useForm<HostWizardForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      mode: "via_bastion",
      scopeId: "",
      name: "",
      address: "",
      port: "22",
      protocol: "ssh",
      platform: "linux",
      account: "",
      credentialId: "",
      environment: "production",
      labelsText: "",
    },
  });
  const values = form.watch();
  const {
    account,
    address,
    credentialId,
    environment,
    mode,
    name,
    platform,
    port,
    protocol,
    scopeId,
  } = values;

  const credentialsQuery = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
    enabled: open,
  });

  const credentials = useMemo(
    () =>
      (credentialsQuery.data ?? []).filter(
        (credential) => credential.protocol === protocol,
      ),
    [credentialsQuery.data, protocol],
  );

  const reset = () => {
    setStep(0);
    form.reset();
    setTestResult(null);
    setPendingAction(null);
  };

  const close = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const addressValid = address.trim().length > 0;
  const step1Valid = mode === "direct" || scopeId.length > 0;
  const step2Valid =
    name.trim().length > 0 &&
    addressValid &&
    Number(port) > 0 &&
    account.trim().length > 0 &&
    credentialId.length > 0;

  useEffect(() => {
    setTestResult(null);
  }, [account, address, credentialId, mode, platform, port, protocol, scopeId]);

  const runTest = async (value: HostWizardForm) => {
    setTesting(true);
    setTestResult(null);
    const connectionMode =
      value.mode === "via_bastion"
        ? "via_bastion"
        : value.protocol === "winrm"
          ? "direct_winrm"
          : "direct_ssh";
    try {
      let result = await api.hosts.createConnectionTest({
        address: value.address,
        port: Number(value.port),
        platform: value.platform,
        connection_mode: connectionMode,
        bastion_scope_id:
          value.mode === "via_bastion" ? value.scopeId : undefined,
        credential_id: value.credentialId,
        username: value.account,
      });
      for (let attempt = 0; attempt < 60; attempt += 1) {
        if (!["queued", "running"].includes(result.status)) break;
        await new Promise((resolve) => window.setTimeout(resolve, 500));
        result = await api.hosts.getConnectionTest(result.id);
      }
      setTestResult(result);
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("hosts.wizard.testFailed"),
        fieldMap: {
          address: "address",
          bastion_scope_id: "scopeId",
          credential_id: "credentialId",
          platform: "platform",
          port: "port",
          username: "account",
        },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          form.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          form.setError("root", { message, type: "server" }),
      });
    } finally {
      setTesting(false);
    }
  };

  const preview = async (value: HostWizardForm) => {
    try {
      if (!testResult || testResult.status !== "succeeded") return;
      const input: HostPreviewCreate = {
        name: value.name,
        address: value.address,
        port: Number(value.port),
        platform: value.platform,
        connection_mode:
          value.mode === "via_bastion"
            ? "via_bastion"
            : value.protocol === "winrm"
              ? "direct_winrm"
              : "direct_ssh",
        bastion_scope_id:
          value.mode === "via_bastion" ? value.scopeId : undefined,
        credential_id: value.credentialId,
        username: value.account,
        environment: value.environment,
        labels: parseLabels(value.labelsText),
        connection_test_id: testResult.id,
      };
      setPendingAction(await api.hosts.previewCreateResource(input));
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("hosts.wizard.previewFailed"),
        fieldMap: {
          address: "address",
          bastion_scope_id: "scopeId",
          credential_id: "credentialId",
          name: "name",
          platform: "platform",
          port: "port",
          username: "account",
        },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          form.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          form.setError("root", { message, type: "server" }),
      });
    }
  };
  const submit = form.handleSubmit(async (value) => {
    form.clearErrors();
    if (submitIntent.current === "test") await runTest(value);
    else await preview(value);
  });

  const selectedScope = scopes.find((scope) => scope.id === scopeId);

  return (
    <FormDrawer
      footer={<></>}
      onOpenChange={close}
      onSubmit={submit}
      open={open}
      title={t("hosts.wizard.title")}
      width={620}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          onCancel={() => setPendingAction(null)}
          onDone={onCreated}
        />
      ) : (
        <Wizard
          canNext={
            step === 0
              ? step1Valid
              : step === 1
                ? step2Valid
                : testResult?.status === "succeeded"
          }
          current={step}
          onBack={() => setStep((value) => Math.max(0, value - 1))}
          onNext={() => setStep((value) => value + 1)}
          onSubmit={() => {
            submitIntent.current = "preview";
          }}
          steps={[
            {
              id: "mode",
              title: t("hosts.wizard.step1"),
              description: t("hosts.wizard.step1Desc"),
            },
            {
              id: "info",
              title: t("hosts.wizard.step2"),
              description: t("hosts.wizard.step2Desc"),
            },
            {
              id: "test",
              title: t("hosts.wizard.step3"),
              description: t("hosts.wizard.step3Desc"),
            },
          ]}
          submitLabel={t("hosts.wizard.preview")}
          submitting={form.formState.isSubmitting && !testing}
          submitType="submit"
        >
          {form.formState.errors.root?.message && (
            <Alert
              description={form.formState.errors.root.message}
              title={t("hosts.wizard.previewFailed")}
              tone="danger"
            />
          )}
          {step === 0 && (
            <div className="argus-choice-list">
              <button
                className={`argus-choice ${mode === "via_bastion" ? "is-selected" : ""}`}
                onClick={() =>
                  form.setValue("mode", "via_bastion", {
                    shouldValidate: true,
                  })
                }
                type="button"
              >
                <span className="argus-choice__text">
                  <b>{t("hosts.wizard.viaBastion")}</b>
                  <small>{t("hosts.wizard.viaBastionDesc")}</small>
                </span>
              </button>
              {mode === "via_bastion" &&
                (scopes.length > 0 ? (
                  scopes.map((scope) => (
                    <button
                      className={`argus-choice ${scopeId === scope.id ? "is-selected" : ""}`}
                      key={scope.id}
                      onClick={() =>
                        form.setValue("scopeId", scope.id, {
                          shouldValidate: true,
                        })
                      }
                      type="button"
                    >
                      <span className="argus-choice__text">
                        <b>{scope.name}</b>
                        <small>
                          {t(`hosts.env.${scope.environment}`)} ·{" "}
                          {t("hosts.scope.members", {
                            count: scope.member_count,
                          })}
                        </small>
                      </span>
                    </button>
                  ))
                ) : (
                  <Alert
                    description={t("hosts.wizard.noActiveScope")}
                    title={t("hosts.wizard.selectScope")}
                    tone="warning"
                  />
                ))}
              <button
                className={`argus-choice ${mode === "direct" ? "is-selected" : ""}`}
                onClick={() =>
                  form.setValue("mode", "direct", { shouldValidate: true })
                }
                type="button"
              >
                <span className="argus-choice__text">
                  <b>{t("hosts.wizard.direct")}</b>
                  <small>{t("hosts.wizard.directDesc")}</small>
                </span>
              </button>
              {mode === "direct" && (
                <p className="argus-inline-note">
                  {t("hosts.wizard.egressNote", { ip: egressDisplay })}
                </p>
              )}
            </div>
          )}

          {step === 1 && (
            <>
              <Field
                requirement="required"
                error={form.formState.errors.name?.message}
                label={t("hosts.wizard.name")}
              >
                <Input
                  {...form.register("name")}
                  maxLength={hostConstraints.name.maxLength}
                  placeholder={t("hosts.wizard.namePlaceholder")}
                />
              </Field>
              <Field
                requirement="required"
                error={form.formState.errors.address?.message}
                label={t("hosts.wizard.address")}
              >
                <Input
                  {...form.register("address")}
                  maxLength={hostConstraints.address.maxLength}
                  placeholder={t("hosts.wizard.addressPlaceholder")}
                />
              </Field>
              <div className="argus-form-row">
                <Field
                  requirement="required"
                  label={t("hosts.wizard.protocol")}
                >
                  <Select
                    onValueChange={(value) => {
                      const next = value as Protocol;
                      form.setValue("protocol", next, { shouldValidate: true });
                      form.setValue("port", next === "winrm" ? "5986" : "22", {
                        shouldValidate: true,
                      });
                      form.setValue(
                        "platform",
                        next === "winrm" ? "windows" : "linux",
                        { shouldValidate: true },
                      );
                      form.setValue("credentialId", "", {
                        shouldValidate: true,
                      });
                    }}
                    options={[
                      { value: "ssh", label: "SSH" },
                      { value: "winrm", label: "WinRM" },
                    ]}
                    value={protocol}
                  />
                </Field>
                <Field
                  requirement="required"
                  error={form.formState.errors.port?.message}
                  label={t("hosts.wizard.port")}
                >
                  <Input
                    {...form.register("port")}
                    inputMode="numeric"
                    max={hostConstraints.port.maximum}
                    min={hostConstraints.port.minimum}
                  />
                </Field>
              </div>
              <div className="argus-form-row">
                <Field
                  requirement="required"
                  label={t("hosts.wizard.platform")}
                >
                  <Select
                    onValueChange={(value) =>
                      form.setValue("platform", value as "linux" | "windows", {
                        shouldValidate: true,
                      })
                    }
                    options={[
                      {
                        value: "linux",
                        label: t("hosts.wizard.platformLinux"),
                      },
                      {
                        value: "windows",
                        label: t("hosts.wizard.platformWindows"),
                      },
                    ]}
                    value={platform}
                  />
                </Field>
                <Field
                  requirement="required"
                  label={t("hosts.wizard.environment")}
                >
                  <Select
                    onValueChange={(value) =>
                      form.setValue("environment", value as Environment, {
                        shouldValidate: true,
                      })
                    }
                    options={ENVIRONMENTS.map((env) => ({
                      value: env,
                      label: t(`hosts.env.${env}`),
                    }))}
                    value={environment}
                  />
                </Field>
              </div>
              <Field
                requirement="required"
                error={form.formState.errors.account?.message}
                label={t("hosts.wizard.account")}
              >
                <Input
                  {...form.register("account")}
                  maxLength={hostConstraints.username.maxLength}
                  placeholder={t("hosts.wizard.accountPlaceholder")}
                />
              </Field>
              <Field
                requirement="required"
                error={form.formState.errors.credentialId?.message}
                label={t("hosts.wizard.secret")}
              >
                <Select
                  ariaLabel={t("hosts.wizard.secret")}
                  onValueChange={(value) =>
                    form.setValue("credentialId", value, {
                      shouldValidate: true,
                    })
                  }
                  options={[
                    { value: "", label: t("hosts.wizard.secretNone") },
                    ...credentials.map((credential) => ({
                      value: credential.id,
                      label: credential.name,
                    })),
                  ]}
                  value={credentialId}
                />
                {credentials.length === 0 && (
                  <span className="argus-field__hint">
                    {t("hosts.wizard.secretEmpty")} ·{" "}
                    <Link to="/settings/secrets">
                      {t("hosts.wizard.secretCreate")}
                    </Link>
                  </span>
                )}
              </Field>
              <Field
                requirement="optional"
                hint={t("hosts.wizard.labelsHint")}
                label={t("hosts.wizard.labels")}
              >
                <Textarea {...form.register("labelsText")} rows={3} />
              </Field>
            </>
          )}

          {step === 2 && (
            <>
              <KeyValueGrid
                columns={2}
                items={[
                  {
                    label: t("hosts.row.name"),
                    value: name.trim(),
                  },
                  {
                    label: t("hosts.row.path"),
                    value:
                      mode === "via_bastion"
                        ? t("hosts.path.viaBastion", {
                            scope: selectedScope?.name ?? scopeId,
                            address: `${address.trim()}:${port}`,
                          })
                        : t("hosts.path.direct", {
                            address: `${address.trim()}:${port}`,
                          }),
                  },
                  {
                    label: t("hosts.wizard.protocol"),
                    value: protocol.toUpperCase(),
                  },
                  {
                    label: t("hosts.wizard.account"),
                    value: account.trim() || "—",
                  },
                ]}
              />
              {testResult && (
                <div className="argus-detail-section">
                  <Alert
                    description={t("hosts.test.latency", {
                      ms: testResult.latency_ms ?? 0,
                    })}
                    title={
                      testResult.status === "succeeded"
                        ? t("hosts.wizard.testPassed")
                        : t("hosts.wizard.testFailed")
                    }
                    tone={
                      testResult.status === "succeeded" ? "success" : "danger"
                    }
                  />
                  {testResult.checks.map((check) => (
                    <CheckItem
                      checked={check.status === "passed"}
                      key={check.name}
                    >
                      <span className="argus-mono">{check.name}</span>
                      {check.detail && (
                        <span className="argus-muted"> · {check.detail}</span>
                      )}
                    </CheckItem>
                  ))}
                </div>
              )}
              <div className="argus-form-actions">
                <Button
                  loading={testing}
                  onClick={() => {
                    submitIntent.current = "test";
                  }}
                  type="submit"
                  variant="secondary"
                >
                  {testing
                    ? t("hosts.wizard.testing")
                    : t("hosts.wizard.runTest")}
                </Button>
                {testResult?.status !== "succeeded" && (
                  <span className="argus-muted">
                    {t("hosts.wizard.needTest")}
                  </span>
                )}
              </div>
            </>
          )}
        </Wizard>
      )}
    </FormDrawer>
  );
}
