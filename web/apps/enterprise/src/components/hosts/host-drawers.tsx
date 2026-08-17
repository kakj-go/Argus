import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import {
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

function BastionFields({
  name,
  environment,
  labelsText,
  onNameChange,
  onEnvironmentChange,
  onLabelsTextChange,
  autoFocus = false,
}: {
  name: string;
  environment: Environment;
  labelsText: string;
  onNameChange: (value: string) => void;
  onEnvironmentChange: (value: Environment) => void;
  onLabelsTextChange: (value: string) => void;
  autoFocus?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <>
      <Field label={t("hosts.bastionForm.name")}>
        <Input
          autoFocus={autoFocus}
          onChange={(event) => onNameChange(event.target.value)}
          placeholder={t("hosts.bastionForm.namePlaceholder")}
          value={name}
        />
      </Field>
      <Field label={t("hosts.bastionForm.environment")}>
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
  const [name, setName] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [labelsText, setLabelsText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [enrollment, setEnrollment] =
    useState<ConfirmActionResult["one_time_result"]>();

  const close = (next: boolean) => {
    if (!next) {
      setName("");
      setEnvironment("production");
      setLabelsText("");
      setPendingAction(null);
      setEnrollment(undefined);
    }
    onOpenChange(next);
  };

  const submit = async () => {
    if (submitting || !name.trim()) return;
    setSubmitting(true);
    try {
      setPendingAction(
        await api.connectors.previewCreateBastionScope({
          name: name.trim(),
          environment,
          labels: parseLabels(labelsText),
        }),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDrawer
      description={t("hosts.bastionForm.description")}
      loading={submitting}
      onOpenChange={close}
      onSubmit={() => void submit()}
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
        <BastionFields
          autoFocus
          environment={environment}
          name={name}
          onEnvironmentChange={setEnvironment}
          onNameChange={setName}
          onLabelsTextChange={setLabelsText}
          labelsText={labelsText}
        />
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
  const [name, setName] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [labelsText, setLabelsText] = useState("");
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

  useEffect(() => {
    if (!scope) return;
    setName(scope.name);
    setEnvironment(scope.environment);
    setLabelsText(labelsToText(scope.labels));
    setGenerationError("");
    setPendingAction(null);
    setInstallCommand(null);
  }, [scope]);

  const submit = async () => {
    if (!scope || submitting || !name.trim()) return;
    setSubmitting(true);
    try {
      setPendingAction(
        await api.connectors.previewUpdateBastionScope(scope.id, {
          name: name.trim(),
          environment,
          labels: parseLabels(labelsText),
          expected_version: scope.resource_version ?? 1,
        }),
      );
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
        error instanceof Error
          ? error.message
          : t("hosts.bastionForm.commandGenerateFailed"),
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
      onSubmit={() => void submit()}
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
          <BastionFields
            environment={environment}
            name={name}
            onEnvironmentChange={setEnvironment}
            onNameChange={setName}
            onLabelsTextChange={setLabelsText}
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
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [port, setPort] = useState("22");
  const [connectionMode, setConnectionMode] =
    useState<EditableHostConnectionMode>("via_bastion");
  const [bastionScopeId, setBastionScopeId] = useState("");
  const [managedAccountId, setManagedAccountId] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [labelsText, setLabelsText] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);

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

  // host 变化时同步表单初值（drawer 打开期间 host 固定）。
  if (host && host.id !== loadedFor) {
    setLoadedFor(host.id);
    setName(host.name);
    setAddress(host.address);
    setPort(String(host.port));
    if (host.connection_mode !== "connector_local") {
      setConnectionMode(host.connection_mode);
    }
    setBastionScopeId(host.bastion_scope_id ?? "");
    setManagedAccountId("");
    setEnvironment(host.environment);
    setLabelsText(labelsToText(host.labels));
    setError("");
    setPendingAction(null);
  }

  const submit = async () => {
    if (
      !host ||
      host.connection_mode === "connector_local" ||
      submitting ||
      !name.trim()
    )
      return;
    const nextPort = Number(port);
    const pathChanged =
      address.trim() !== host.address ||
      nextPort !== host.port ||
      connectionMode !== host.connection_mode ||
      (connectionMode === "via_bastion" ? bastionScopeId : "") !==
        (host.bastion_scope_id ?? "");
    if (
      !address.trim() ||
      !Number.isInteger(nextPort) ||
      nextPort < 1 ||
      (connectionMode === "via_bastion" && !bastionScopeId)
    ) {
      setError(t("hosts.hostForm.pathRequired"));
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      let connectionTestId: string | undefined;
      if (pathChanged) {
        const account = managedAccounts.find(
          (entry) => entry.id === managedAccountId,
        );
        if (!account) {
          setError(t("hosts.hostForm.accountRequired"));
          return;
        }
        let connectionTest = await api.hosts.createConnectionTest({
          address: address.trim(),
          port: nextPort,
          platform: host.platform,
          connection_mode: connectionMode,
          bastion_scope_id:
            connectionMode === "via_bastion" ? bastionScopeId : undefined,
          credential_id: account.credential_id,
          username: account.username,
        });
        for (let attempt = 0; attempt < 60; attempt += 1) {
          if (!["queued", "running"].includes(connectionTest.status)) break;
          await new Promise((resolve) => window.setTimeout(resolve, 500));
          connectionTest = await api.hosts.getConnectionTest(connectionTest.id);
        }
        if (connectionTest.status !== "succeeded") {
          setError(connectionTest.error_code ?? t("hosts.hostForm.testFailed"));
          return;
        }
        connectionTestId = connectionTest.id;
      }
      setPendingAction(
        await api.hosts.previewUpdateResource(host.id, {
          name: name.trim(),
          address: pathChanged ? address.trim() : undefined,
          port: pathChanged ? nextPort : undefined,
          connection_mode: pathChanged ? connectionMode : undefined,
          bastion_scope_id:
            pathChanged && connectionMode === "via_bastion"
              ? bastionScopeId
              : undefined,
          environment,
          labels: parseLabels(labelsText),
          connection_test_id: connectionTestId,
          expected_version: host.resource_version ?? 1,
        }),
      );
    } catch (caught) {
      setError(
        caught instanceof Error
          ? caught.message
          : t("hosts.hostForm.testFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDrawer
      loading={submitting}
      onOpenChange={onOpenChange}
      onSubmit={() => void submit()}
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
          {error ? (
            <p className="argus-field__hint is-error" role="alert">
              {error}
            </p>
          ) : null}
          <Field label={t("hosts.wizard.name")}>
            <Input
              onChange={(event) => setName(event.target.value)}
              value={name}
            />
          </Field>
          <Field label={t("hosts.wizard.address")}>
            <Input
              disabled={host?.connection_mode === "connector_local"}
              onChange={(event) => setAddress(event.target.value)}
              value={address}
            />
          </Field>
          <div className="argus-form-row">
            <Field label={t("hosts.wizard.port")}>
              <Input
                disabled={host?.connection_mode === "connector_local"}
                inputMode="numeric"
                onChange={(event) => setPort(event.target.value)}
                value={port}
              />
            </Field>
            <Field label={t("hosts.wizard.environment")}>
              <Select
                onValueChange={(value) => setEnvironment(value as Environment)}
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
              <Field label={t("hosts.hostForm.connectionMode")}>
                <Select
                  onValueChange={(value) => {
                    setConnectionMode(value as EditableHostConnectionMode);
                    setManagedAccountId("");
                  }}
                  options={HOST_CONNECTION_MODES.map((mode) => ({
                    value: mode,
                    label: t(`hosts.mode.${mode}`),
                  }))}
                  value={connectionMode}
                />
              </Field>
              {connectionMode === "via_bastion" ? (
                <Field label={t("hosts.hostForm.bastionScope")}>
                  <Select
                    onValueChange={setBastionScopeId}
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
                hint={t("hosts.hostForm.accountHint")}
                label={t("hosts.hostForm.account")}
              >
                <Select
                  onValueChange={setManagedAccountId}
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
            hint={t("hosts.wizard.labelsHint")}
            label={t("hosts.wizard.labels")}
          >
            <Textarea
              onChange={(event) => setLabelsText(event.target.value)}
              rows={3}
              value={labelsText}
            />
          </Field>
        </>
      )}
    </FormDrawer>
  );
}
