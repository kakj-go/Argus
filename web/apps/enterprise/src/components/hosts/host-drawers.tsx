import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import {
  apiErrorField,
  formConstraint,
  formatApiError,
  formatErrorCode,
  useApi,
  type BastionScope,
  type ConfirmActionResult,
  type Connector,
  type Environment,
  type Host,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  Button,
  CodeBlock,
  Field,
  FormDrawer,
  Input,
  Select,
  Textarea,
} from "@argus/ui";
import { formatDateTime, labelsToText, parseLabels } from "./host-utils";
import { PendingActionConfirm } from "./pending-action-confirm";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];
type EditableHostConnectionMode = Exclude<
  Host["connection_mode"],
  "connector_local"
>;
const HOST_CONNECTION_MODES: EditableHostConnectionMode[] = [
  "via_bastion",
  "direct_ssh",
  "direct_winrm",
];
const bastionConstraints = {
  name: formConstraint("BastionPreviewCreate", "name"),
};
const hostConstraints = {
  name: formConstraint("HostPreviewUpdate", "name"),
  address: formConstraint("HostPreviewUpdate", "address"),
  port: formConstraint("HostPreviewUpdate", "port"),
};

type BastionFormValues = {
  name: string;
  environment: Environment;
  labelsText: string;
};

type HostEditFormValues = {
  name: string;
  address: string;
  port: string;
  connectionMode: EditableHostConnectionMode;
  bastionScopeId: string;
  managedAccountId: string;
  environment: Environment;
  labelsText: string;
};

function BastionFields({
  name,
  environment,
  labelsText,
  onNameChange,
  onEnvironmentChange,
  onLabelsTextChange,
  errors,
  autoFocus = false,
}: {
  name: string;
  environment: Environment;
  labelsText: string;
  onNameChange: (value: string) => void;
  onEnvironmentChange: (value: Environment) => void;
  onLabelsTextChange: (value: string) => void;
  errors?: { name?: string };
  autoFocus?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <>
      <Field requirement="required" error={errors?.name} label={t("hosts.bastionForm.name")}>
        <Input
          autoFocus={autoFocus}
          maxLength={bastionConstraints.name.maxLength}
          onChange={(event) => onNameChange(event.target.value)}
          placeholder={t("hosts.bastionForm.namePlaceholder")}
          value={name}
        />
      </Field>
      <Field requirement="required" label={t("hosts.bastionForm.environment")}>
        <Select
          onValueChange={(value) => onEnvironmentChange(value as Environment)}
          options={ENVIRONMENTS.map((env) => ({
            value: env,
            label: t(`hosts.env.${env}`),
          }))}
          value={environment}
        />
      </Field>
      <Field
        requirement="optional"
        hint={t("hosts.bastionForm.labelsHint")}
        label={t("hosts.bastionForm.labels")}
      >
        <Textarea
          onChange={(event) => onLabelsTextChange(event.target.value)}
          rows={3}
          value={labelsText}
        />
      </Field>
    </>
  );
}

/** 添加堡垒机：创建 pending Bastion Scope + 一次性注册令牌。 */
export function AddBastionDrawer({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [submitting, setSubmitting] = useState(false);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [enrollment, setEnrollment] =
    useState<ConfirmActionResult["one_time_result"]>();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("hosts.bastionForm.name"))
          .max(bastionConstraints.name.maxLength ?? 128),
        environment: z.enum(["development", "staging", "production"]),
        labelsText: z.string(),
      }),
    [t],
  );
  const {
    reset,
    setError,
    setValue,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<BastionFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      environment: "production",
      labelsText: "",
    },
  });
  const name = watch("name");
  const environment = watch("environment");
  const labelsText = watch("labelsText");

  const close = (next: boolean) => {
    if (!next) {
      reset();
      setPendingAction(null);
      setEnrollment(undefined);
    }
    onOpenChange(next);
  };

  const submit = async (values: BastionFormValues) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      setPendingAction(
        await api.connectors.previewCreateBastionScope({
          name: values.name,
          environment: values.environment,
          labels: parseLabels(values.labelsText),
        }),
      );
    } catch (error) {
      const message = formatApiError(
        error,
        t("hosts.bastionForm.commandGenerateFailed"),
        (requestId) => t("common.requestReference", { requestId }),
      );
      if (apiErrorField(error) === "name") {
        setError("name", { message, type: "server" }, { shouldFocus: true });
      } else {
        setError("root", { message, type: "server" });
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDrawer
      description={t("hosts.bastionForm.description")}
      loading={submitting}
      onOpenChange={close}
      onSubmit={handleSubmit((values) => void submit(values))}
      open={open}
      submitLabel={t("hosts.bastionForm.submit")}
      title={t("hosts.bastionForm.title")}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult
          onCancel={() => setPendingAction(null)}
          onDone={(result) => {
            setPendingAction(null);
            setEnrollment(result.one_time_result);
            onCreated();
          }}
        />
      ) : enrollment ? (
        <div className="argus-bastion-command__code">
          <Alert
            description={t("hosts.bastionForm.commandWarning")}
            title={t("hosts.bastionForm.commandWarningTitle")}
            tone="warning"
          />
          <CodeBlock
            code={enrollment.enrollment.install_command ?? ""}
            language="bash"
          />
          <span>{formatDateTime(enrollment.expires_at)}</span>
        </div>
      ) : (
        <>
          {errors.root?.message && (
            <Alert
              description={errors.root.message}
              title={t("hosts.bastionForm.title")}
              tone="danger"
            />
          )}
          <BastionFields
            autoFocus
            environment={environment}
            errors={{ name: errors.name?.message }}
            name={name}
            onEnvironmentChange={(value) =>
              setValue("environment", value, { shouldValidate: true })
            }
            onNameChange={(value) =>
              setValue("name", value, { shouldValidate: true })
            }
            onLabelsTextChange={(value) => setValue("labelsText", value)}
            labelsText={labelsText}
          />
        </>
      )}
    </FormDrawer>
  );
}

/** 编辑稳定的 Bastion Scope，并按需生成一次性的安装/更新命令。 */
export function EditBastionDrawer({
  scope,
  connectorStatus,
  onOpenChange,
  onSaved,
}: {
  scope: BastionScope | null;
  connectorStatus?: Connector["status"];
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [submitting, setSubmitting] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [generationError, setGenerationError] = useState("");
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [installCommand, setInstallCommand] = useState<string | null>(null);
  const canInstall =
    scope?.status === "pending" ||
    scope?.status === "uninstalled" ||
    connectorStatus === "offline";
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("hosts.bastionForm.name"))
          .max(bastionConstraints.name.maxLength ?? 128),
        environment: z.enum(["development", "staging", "production"]),
        labelsText: z.string(),
      }),
    [t],
  );
  const {
    reset,
    setError,
    setValue,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<BastionFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      environment: "production",
      labelsText: "",
    },
  });
  const name = watch("name");
  const environment = watch("environment");
  const labelsText = watch("labelsText");

  useEffect(() => {
    if (!scope) return;
    reset({
      name: scope.name,
      environment: scope.environment,
      labelsText: labelsToText(scope.labels),
    });
    setGenerationError("");
    setPendingAction(null);
    setInstallCommand(null);
  }, [reset, scope]);

  const submit = async (values: BastionFormValues) => {
    if (!scope || submitting) return;
    setSubmitting(true);
    try {
      setPendingAction(
        await api.connectors.previewUpdateBastionScope(scope.id, {
          name: values.name,
          environment: values.environment,
          labels: parseLabels(values.labelsText),
          expected_version: scope.resource_version ?? 1,
        }),
      );
    } catch (error) {
      const message = formatApiError(
        error,
        t("hosts.bastionForm.commandGenerateFailed"),
        (requestId) => t("common.requestReference", { requestId }),
      );
      if (apiErrorField(error) === "name") {
        setError("name", { message, type: "server" }, { shouldFocus: true });
      } else {
        setError("root", { message, type: "server" });
      }
    } finally {
      setSubmitting(false);
    }
  };

  const generateCommand = async () => {
    if (!scope || generating) return;
    setGenerating(true);
    setGenerationError("");
    try {
      setPendingAction(
        await api.connectors.previewReplaceBastionConnector(
          scope.id,
          scope.resource_version ?? 1,
        ),
      );
    } catch (error) {
      setGenerationError(
        formatApiError(
          error,
          t("hosts.bastionForm.commandGenerateFailed"),
          (requestId) => t("common.requestReference", { requestId }),
        ),
      );
    } finally {
      setGenerating(false);
    }
  };

  return (
    <FormDrawer
      description={t("hosts.bastionForm.editDescription")}
      loading={submitting}
      onOpenChange={onOpenChange}
      onSubmit={handleSubmit((values) => void submit(values))}
      open={scope !== null}
      submitLabel={t("hosts.bastionForm.save")}
      title={t("hosts.bastionForm.editTitle", { name: scope?.name ?? "" })}
      width={560}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult
          onCancel={() => setPendingAction(null)}
          onDone={(result) => {
            setPendingAction(null);
            onSaved();
            if (result.one_time_result?.enrollment.install_command) {
              setInstallCommand(
                result.one_time_result.enrollment.install_command,
              );
            } else {
              onOpenChange(false);
            }
          }}
        />
      ) : (
        <>
          {errors.root?.message && (
            <Alert
              description={errors.root.message}
              title={t("hosts.bastionForm.editTitle", {
                name: scope?.name ?? "",
              })}
              tone="danger"
            />
          )}
          <BastionFields
            environment={environment}
            errors={{ name: errors.name?.message }}
            name={name}
            onEnvironmentChange={(value) =>
              setValue("environment", value, { shouldValidate: true })
            }
            onNameChange={(value) =>
              setValue("name", value, { shouldValidate: true })
            }
            onLabelsTextChange={(value) => setValue("labelsText", value)}
            labelsText={labelsText}
          />

          <section className="argus-bastion-command">
            <div className="argus-bastion-command__head">
              <div>
                <h3>{t("hosts.bastionForm.commandTitle")}</h3>
                <p>{t("hosts.bastionForm.commandDescription")}</p>
              </div>
              {canInstall && (
                <Button
                  loading={generating}
                  onClick={() => void generateCommand()}
                  variant="secondary"
                >
                  {t("hosts.bastionForm.commandGenerate")}
                </Button>
              )}
            </div>
            {canInstall ? (
              <Alert
                description={t("hosts.bastionForm.commandWarning")}
                title={t("hosts.bastionForm.commandWarningTitle")}
                tone="warning"
              />
            ) : (
              <Alert
                description={t("hosts.bastionForm.commandUnavailable")}
                title={t("hosts.bastionForm.commandUnavailableTitle")}
                tone="info"
              />
            )}
            {generationError && (
              <Alert
                description={generationError}
                title={t("hosts.bastionForm.commandGenerateFailed")}
                tone="danger"
              />
            )}
            {installCommand ? (
              <div className="argus-bastion-command__code">
                <CodeBlock code={installCommand} language="bash" />
              </div>
            ) : null}
          </section>
        </>
      )}
    </FormDrawer>
  );
}

/** 编辑主机基本信息（UpdateHostInput 范围内的字段）。 */
export function EditHostDrawer({
  host,
  onOpenChange,
  onSaved,
}: {
  host: Host | null;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [submitting, setSubmitting] = useState(false);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const schema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t("hosts.hostForm.pathRequired"))
            .max(hostConstraints.name.maxLength ?? 128),
          address: z
            .string()
            .trim()
            .min(1, t("hosts.hostForm.pathRequired"))
            .max(hostConstraints.address.maxLength ?? 512),
          port: z.string().refine((value) => {
            const parsed = Number(value);
            return (
              Number.isInteger(parsed) &&
              parsed >= (hostConstraints.port.minimum ?? 1) &&
              parsed <= (hostConstraints.port.maximum ?? 65535)
            );
          }, t("hosts.hostForm.pathRequired")),
          connectionMode: z.enum(["via_bastion", "direct_ssh", "direct_winrm"]),
          bastionScopeId: z.string(),
          managedAccountId: z.string(),
          environment: z.enum(["development", "staging", "production"]),
          labelsText: z.string(),
        })
        .superRefine((values, context) => {
          if (
            host?.connection_mode !== "connector_local" &&
            values.connectionMode === "via_bastion" &&
            !values.bastionScopeId
          ) {
            context.addIssue({
              code: "custom",
              message: t("hosts.hostForm.pathRequired"),
              path: ["bastionScopeId"],
            });
          }
          if (!host || host.connection_mode === "connector_local") return;
          const pathChanged =
            values.address !== host.address ||
            Number(values.port) !== host.port ||
            values.connectionMode !== host.connection_mode ||
            (values.connectionMode === "via_bastion"
              ? values.bastionScopeId
              : "") !== (host.bastion_scope_id ?? "");
          if (pathChanged && !values.managedAccountId) {
            context.addIssue({
              code: "custom",
              message: t("hosts.hostForm.accountRequired"),
              path: ["managedAccountId"],
            });
          }
        }),
    [host, t],
  );
  const {
    register,
    reset,
    setError,
    setValue,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<HostEditFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      address: "",
      port: "22",
      connectionMode: "via_bastion",
      bastionScopeId: "",
      managedAccountId: "",
      environment: "production",
      labelsText: "",
    },
  });
  const address = watch("address");
  const port = watch("port");
  const connectionMode = watch("connectionMode");
  const bastionScopeId = watch("bastionScopeId");
  const managedAccountId = watch("managedAccountId");
  const environment = watch("environment");

  const scopesQuery = useQuery({
    queryKey: ["connectors", "bastionScopes"],
    queryFn: () => api.connectors.listBastionScopes(),
    enabled: host !== null && host.connection_mode !== "connector_local",
  });
  const accountsQuery = useQuery({
    queryKey: ["managedAccounts"],
    queryFn: () => api.secrets.listManagedAccounts(),
    enabled: host !== null && host.connection_mode !== "connector_local",
  });
  const managedAccounts = useMemo(
    () =>
      (accountsQuery.data ?? []).filter(
        (account) =>
          account.host_id === host?.id &&
          account.status === "active" &&
          account.allowed_protocols.includes(
            connectionMode === "direct_winrm" ? "winrm" : "ssh",
          ),
      ),
    [accountsQuery.data, connectionMode, host?.id],
  );

  useEffect(() => {
    if (!host) return;
    reset({
      name: host.name,
      address: host.address,
      port: String(host.port),
      connectionMode:
        host.connection_mode === "connector_local"
          ? "via_bastion"
          : host.connection_mode,
      bastionScopeId: host.bastion_scope_id ?? "",
      managedAccountId: "",
      environment: host.environment,
      labelsText: labelsToText(host.labels),
    });
    setPendingAction(null);
  }, [host, reset]);

  const submit = async (values: HostEditFormValues) => {
    if (!host || submitting) return;
    const nextPort = Number(values.port);
    const pathChanged =
      host.connection_mode !== "connector_local" &&
      (values.address !== host.address ||
        nextPort !== host.port ||
        values.connectionMode !== host.connection_mode ||
        (values.connectionMode === "via_bastion"
          ? values.bastionScopeId
          : "") !== (host.bastion_scope_id ?? ""));
    setSubmitting(true);
    try {
      let connectionTestId: string | undefined;
      if (pathChanged) {
        const account = managedAccounts.find(
          (entry) => entry.id === values.managedAccountId,
        );
        if (!account) {
          setError("managedAccountId", {
            message: t("hosts.hostForm.accountRequired"),
            type: "validate",
          });
          return;
        }
        let connectionTest = await api.hosts.createConnectionTest({
          address: values.address,
          port: nextPort,
          platform: host.platform,
          connection_mode: values.connectionMode,
          bastion_scope_id:
            values.connectionMode === "via_bastion"
              ? values.bastionScopeId
              : undefined,
          credential_id: account.credential_id,
          username: account.username,
        });
        for (let attempt = 0; attempt < 60; attempt += 1) {
          if (!["queued", "running"].includes(connectionTest.status)) break;
          await new Promise((resolve) => window.setTimeout(resolve, 500));
          connectionTest = await api.hosts.getConnectionTest(connectionTest.id);
        }
        if (connectionTest.status !== "succeeded") {
          setError("root", {
            message:
              formatErrorCode(
                connectionTest.error_code,
                t("hosts.hostForm.testFailed"),
              ),
            type: "server",
          });
          return;
        }
        connectionTestId = connectionTest.id;
      }
      setPendingAction(
        await api.hosts.previewUpdateResource(host.id, {
          name: values.name,
          address: pathChanged ? values.address : undefined,
          port: pathChanged ? nextPort : undefined,
          connection_mode: pathChanged ? values.connectionMode : undefined,
          bastion_scope_id:
            pathChanged && values.connectionMode === "via_bastion"
              ? values.bastionScopeId
              : undefined,
          environment: values.environment,
          labels: parseLabels(values.labelsText),
          connection_test_id: connectionTestId,
          expected_version: host.resource_version ?? 1,
        }),
      );
    } catch (caught) {
      const field = apiErrorField(caught);
      const formField =
        field === "name"
          ? "name"
          : field === "address"
            ? "address"
            : field === "port"
              ? "port"
              : field === "bastion_scope_id"
                ? "bastionScopeId"
                : undefined;
      const message = formatApiError(
        caught,
        t("hosts.hostForm.testFailed"),
        (requestId) => t("common.requestReference", { requestId }),
      );
      if (formField) {
        setError(formField, { message, type: "server" }, { shouldFocus: true });
      } else {
        setError("root", { message, type: "server" });
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDrawer
      loading={submitting}
      onOpenChange={onOpenChange}
      onSubmit={handleSubmit((values) => void submit(values))}
      open={host !== null}
      submitLabel={t("hosts.hostForm.submit")}
      title={t("hosts.hostForm.editTitle", { name: host?.name ?? "" })}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          onCancel={() => setPendingAction(null)}
          onDone={() => {
            onSaved();
            onOpenChange(false);
          }}
        />
      ) : (
        <>
          {errors.root?.message && (
            <Alert
              description={errors.root.message}
              title={t("hosts.hostForm.testFailed")}
              tone="danger"
            />
          )}
          <Field requirement="required" error={errors.name?.message} label={t("hosts.wizard.name")}>
            <Input
              {...register("name")}
              maxLength={hostConstraints.name.maxLength}
            />
          </Field>
          <Field requirement="required" error={errors.address?.message} label={t("hosts.wizard.address")}>
            <Input
              {...register("address")}
              disabled={host?.connection_mode === "connector_local"}
              maxLength={hostConstraints.address.maxLength}
            />
          </Field>
          <div className="argus-form-row">
            <Field requirement="required" error={errors.port?.message} label={t("hosts.wizard.port")}>
              <Input
                {...register("port")}
                disabled={host?.connection_mode === "connector_local"}
                inputMode="numeric"
                max={hostConstraints.port.maximum}
                min={hostConstraints.port.minimum}
              />
            </Field>
            <Field requirement="required" label={t("hosts.wizard.environment")}>
              <Select
                onValueChange={(value) =>
                  setValue("environment", value as Environment, {
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
          {host?.connection_mode !== "connector_local" ? (
            <>
              <Field
                requirement="required"
                label={t("hosts.hostForm.connectionMode")}
              >
                <Select
                  onValueChange={(value) => {
                    setValue(
                      "connectionMode",
                      value as EditableHostConnectionMode,
                      { shouldValidate: true },
                    );
                    setValue("managedAccountId", "", { shouldValidate: true });
                  }}
                  options={HOST_CONNECTION_MODES.map((mode) => ({
                    value: mode,
                    label: t(`hosts.connectionMode.${mode}`),
                  }))}
                  value={connectionMode}
                />
              </Field>
              {connectionMode === "via_bastion" ? (
                <Field
                  requirement="required"
                  error={errors.bastionScopeId?.message}
                  label={t("hosts.hostForm.bastionScope")}
                >
                  <Select
                    onValueChange={(value) =>
                      setValue("bastionScopeId", value, {
                        shouldValidate: true,
                      })
                    }
                    options={[
                      { value: "", label: "-" },
                      ...(scopesQuery.data?.items ?? [])
                        .filter((scope) => scope.status === "active")
                        .map((scope) => ({
                          value: scope.id,
                          label: scope.name,
                        })),
                    ]}
                    value={bastionScopeId}
                  />
                </Field>
              ) : null}
              <Field
                requirement={
                  host &&
                  (address.trim() !== host.address ||
                    Number(port) !== host.port ||
                    connectionMode !== host.connection_mode ||
                    (connectionMode === "via_bastion" ? bastionScopeId : "") !==
                      (host.bastion_scope_id ?? ""))
                    ? "required"
                    : "optional"
                }
                error={errors.managedAccountId?.message}
                hint={t("hosts.hostForm.accountHint")}
                label={t("hosts.hostForm.account")}
              >
                <Select
                  onValueChange={(value) =>
                    setValue("managedAccountId", value, {
                      shouldValidate: true,
                    })
                  }
                  options={[
                    { value: "", label: t("hosts.hostForm.accountSelect") },
                    ...managedAccounts.map((account) => ({
                      value: account.id,
                      label: account.username,
                    })),
                  ]}
                  value={managedAccountId}
                />
              </Field>
            </>
          ) : null}
          <Field
            requirement="optional"
            hint={t("hosts.wizard.labelsHint")}
            label={t("hosts.wizard.labels")}
          >
            <Textarea
              {...register("labelsText")}
              rows={3}
            />
          </Field>
        </>
      )}
    </FormDrawer>
  );
}
