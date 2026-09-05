import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useRef, useState } from "react";
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
  type BastionConnectorReplacementPreview,
  type BastionScope,
  type ActionOneTimeResult,
  type ConnectorInstallOperation,
  type Environment,
  type Host,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  Button,
  Field,
  FormDrawer,
  Input,
  Select,
  Textarea,
} from "@argus/ui";
import { labelsToText, parseLabels } from "./host-utils";
import { PendingActionConfirm } from "./pending-action-confirm";
import { InstallInstructionPanel } from "./install-instruction-panel";
import {
  connectorInstallEventStatusLabel,
  connectorInstallStageLabel,
} from "./connector-install-presentation";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];
type EditableHostConnectionMode = Exclude<
  Host["connection_mode"],
  "connector_local" | "self_enrolled"
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
      <Field
        requirement="required"
        error={errors?.name}
        label={t("hosts.bastionForm.name")}
      >
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

/** Edit Bastion metadata and expose active-Connector replacement as maintenance. */
export function EditBastionDrawer({
  scope,
  onOpenChange,
  onSaved,
}: {
  scope: BastionScope | null;
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
  const [actionKind, setActionKind] = useState<"update" | "replace" | null>(
    null,
  );
  const [installInstructions, setInstallInstructions] =
    useState<ActionOneTimeResult | null>(null);
  const [replacementAddress, setReplacementAddress] = useState("");
  const [replacementPort, setReplacementPort] = useState("22");
  const [replacementUsername, setReplacementUsername] = useState("");
  const [replacementCredentialID, setReplacementCredentialID] = useState("");
  const [replacementOperation, setReplacementOperation] =
    useState<ConnectorInstallOperation | null>(null);
  const initializedScopeID = useRef<string | null>(null);
  const submitIntent = useRef<"update" | "replace">("update");
  const canReplace = Boolean(scope?.active_connector_id);
  const directReplacement = Boolean(
    scope && scope.onboarding_mode !== "command",
  );
  const replacementCredentials = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
    enabled: Boolean(scope && directReplacement),
    select: (items) => items.filter((item) => item.protocol === "ssh"),
  });
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
    if (!scope) {
      initializedScopeID.current = null;
      return;
    }
    // A list refetch may replace the Scope object while the drawer is open.
    // Preserve a claimed command and durable operation until this Scope closes.
    if (initializedScopeID.current === scope.id) return;
    initializedScopeID.current = scope.id;
    reset({
      name: scope.name,
      environment: scope.environment,
      labelsText: labelsToText(scope.labels),
    });
    setGenerationError("");
    setPendingAction(null);
    setActionKind(null);
    setInstallInstructions(null);
    setReplacementAddress("");
    setReplacementPort("22");
    setReplacementUsername("");
    setReplacementCredentialID("");
    setReplacementOperation(null);
  }, [reset, scope]);

  useEffect(() => {
    if (
      !replacementOperation ||
      ["succeeded", "failed", "expired", "cancelled"].includes(
        replacementOperation.status,
      )
    ) {
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const next = await api.connectors.getInstallOperation(
          replacementOperation.id,
        );
        if (!cancelled) {
          setReplacementOperation(next);
          if (next.status === "succeeded") {
            onSaved();
            onOpenChange(false);
          }
        }
      } catch {
        // The server owns this timeline; keep the last durable projection and retry.
      }
    }, 800);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [api.connectors, onOpenChange, onSaved, replacementOperation]);

  const submit = async (values: BastionFormValues) => {
    if (!scope || submitting) return;
    setSubmitting(true);
    try {
      setActionKind("update");
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
      const replacement: BastionConnectorReplacementPreview = {
        expected_version: scope.resource_version ?? 1,
      };
      if (directReplacement) {
        const port = Number(replacementPort);
        if (
          !replacementAddress.trim() ||
          !Number.isInteger(port) ||
          port < 1 ||
          port > 65535 ||
          !replacementUsername.trim() ||
          !replacementCredentialID
        ) {
          setGenerationError(
            t("hosts.bastionForm.replacementConnectionRequired"),
          );
          return;
        }
        let test = await api.hosts.createConnectionTest({
          address: replacementAddress.trim(),
          port,
          platform: "linux",
          connection_mode: "direct_ssh",
          credential_id: replacementCredentialID,
          username: replacementUsername.trim(),
        });
        for (
          let attempt = 0;
          attempt < 60 && ["queued", "running"].includes(test.status);
          attempt += 1
        ) {
          await new Promise((resolve) => window.setTimeout(resolve, 500));
          test = await api.hosts.getConnectionTest(test.id);
        }
        if (test.status !== "succeeded") {
          setGenerationError(
            formatErrorCode(test.error_code, t("hosts.bastionForm.testFailed")),
          );
          return;
        }
        Object.assign(replacement, {
          address: replacementAddress.trim(),
          port,
          username: replacementUsername.trim(),
          credential_id: replacementCredentialID,
          connection_test_id: test.id,
        });
      }
      setActionKind("replace");
      setPendingAction(
        await api.connectors.previewConnectorReplacement(scope.id, replacement),
      );
    } catch (error) {
      setActionKind(null);
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

  const submitDrawer = handleSubmit(
    (values) => {
      const intent = submitIntent.current;
      submitIntent.current = "update";
      if (intent === "replace") {
        void generateCommand();
        return;
      }
      void submit(values);
    },
    () => {
      submitIntent.current = "update";
    },
  );

  return (
    <FormDrawer
      description={t("hosts.bastionForm.editDescription")}
      loading={submitting}
      onOpenChange={onOpenChange}
      onSubmit={submitDrawer}
      open={scope !== null}
      submitLabel={t("hosts.bastionForm.save")}
      title={t("hosts.bastionForm.editTitle", { name: scope?.name ?? "" })}
      width={560}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          claimOneTimeResult={
            actionKind === "replace" && scope?.onboarding_mode === "command"
          }
          onCancel={() => {
            setPendingAction(null);
            setActionKind(null);
          }}
          onDone={(result) => {
            setPendingAction(null);
            if (actionKind === "replace" && result.one_time_result) {
              setInstallInstructions(result.one_time_result);
              onSaved();
            } else if (
              actionKind === "replace" &&
              result.execution?.operation_ref?.id
            ) {
              void api.connectors
                .getInstallOperation(result.execution.operation_ref.id)
                .then(setReplacementOperation);
              setActionKind(null);
            } else {
              setActionKind(null);
              onSaved();
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

          {canReplace && (
            <section className="argus-bastion-command">
              <div className="argus-bastion-command__head">
                <div>
                  <h3>{t("hosts.bastionForm.replacementTitle")}</h3>
                  <p>
                    {scope?.onboarding_mode === "command"
                      ? t("hosts.bastionForm.replacementCommandDescription")
                      : t("hosts.bastionForm.replacementOperationDescription")}
                  </p>
                </div>
                <Button
                  loading={generating}
                  onClick={() => {
                    submitIntent.current = "replace";
                  }}
                  type="submit"
                  variant="danger"
                >
                  {t("hosts.bastionForm.replaceConnector")}
                </Button>
              </div>
              {directReplacement && !replacementOperation && (
                <div className="argus-form-grid">
                  <Field
                    requirement="required"
                    label={t("hosts.wizard.address")}
                  >
                    <Input
                      onChange={(event) =>
                        setReplacementAddress(event.target.value)
                      }
                      placeholder={t("hosts.wizard.addressPlaceholder")}
                      value={replacementAddress}
                    />
                  </Field>
                  <Field requirement="required" label={t("hosts.wizard.port")}>
                    <Input
                      inputMode="numeric"
                      onChange={(event) =>
                        setReplacementPort(event.target.value)
                      }
                      value={replacementPort}
                    />
                  </Field>
                  <Field
                    requirement="required"
                    label={t("hosts.wizard.account")}
                  >
                    <Input
                      onChange={(event) =>
                        setReplacementUsername(event.target.value)
                      }
                      placeholder={t("hosts.wizard.accountPlaceholder")}
                      value={replacementUsername}
                    />
                  </Field>
                  <Field
                    requirement="required"
                    label={t("hosts.wizard.secret")}
                  >
                    <Select
                      onValueChange={setReplacementCredentialID}
                      options={(replacementCredentials.data ?? []).map(
                        (credential) => ({
                          value: credential.id,
                          label: credential.name,
                        }),
                      )}
                      placeholder={t("hosts.wizard.secretEmpty")}
                      value={replacementCredentialID}
                    />
                  </Field>
                </div>
              )}
              <Alert
                description={t("hosts.bastionForm.replacementWarning")}
                title={t("hosts.bastionForm.replacementWarningTitle")}
                tone="warning"
              />
              {generationError && (
                <Alert
                  description={generationError}
                  title={t("hosts.bastionForm.commandGenerateFailed")}
                  tone="danger"
                />
              )}
              {installInstructions && (
                <div className="argus-bastion-command__code">
                  <InstallInstructionPanel result={installInstructions} />
                </div>
              )}
              {replacementOperation && (
                <div className="argus-stack">
                  <Alert
                    description={t("hosts.bastionForm.installProgressHint")}
                    title={`${t("hosts.bastionForm.installProgress")} · ${connectorInstallStageLabel(t, replacementOperation.stage)}`}
                    tone={
                      replacementOperation.status === "failed" ||
                      replacementOperation.status === "expired"
                        ? "danger"
                        : "info"
                    }
                  />
                  <ol className="argus-operation-timeline">
                    {replacementOperation.events.map((event) => (
                      <li key={event.id}>
                        <strong>
                          {connectorInstallStageLabel(t, event.stage)}
                        </strong>
                        <span>
                          {connectorInstallEventStatusLabel(t, event.status)}
                        </span>
                      </li>
                    ))}
                  </ol>
                </div>
              )}
            </section>
          )}
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
        host.connection_mode === "connector_local" ||
        host.connection_mode === "self_enrolled"
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
            message: formatErrorCode(
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
          <Field
            requirement="required"
            error={errors.name?.message}
            label={t("hosts.wizard.name")}
          >
            <Input
              {...register("name")}
              maxLength={hostConstraints.name.maxLength}
            />
          </Field>
          <Field
            requirement="required"
            error={errors.address?.message}
            label={t("hosts.wizard.address")}
          >
            <Input
              {...register("address")}
              disabled={host?.connection_mode === "connector_local"}
              maxLength={hostConstraints.address.maxLength}
            />
          </Field>
          <div className="argus-form-row">
            <Field
              requirement="required"
              error={errors.port?.message}
              label={t("hosts.wizard.port")}
            >
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
            <Textarea {...register("labelsText")} rows={3} />
          </Field>
        </>
      )}
    </FormDrawer>
  );
}
