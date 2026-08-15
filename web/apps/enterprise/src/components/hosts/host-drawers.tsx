import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type BastionScope,
  type ConnectorEnrollmentToken,
  type ConnectorStatus,
  type ConnectorUninstallCommand,
  type ConnectorUninstallResult,
  type Environment,
  type Host,
  type UpdateHostInput,
} from "@argus/api-client";
import {
  Alert,
  Button,
  CodeBlock,
  Dialog,
  Field,
  FormDrawer,
  Input,
  Select,
  Textarea,
} from "@argus/ui";
import { formatDateTime, parseTags, tagsToText } from "./host-utils";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

function BastionFields({
  name,
  environment,
  tagsText,
  onNameChange,
  onEnvironmentChange,
  onTagsTextChange,
  autoFocus = false,
}: {
  name: string;
  environment: Environment;
  tagsText: string;
  onNameChange: (value: string) => void;
  onEnvironmentChange: (value: Environment) => void;
  onTagsTextChange: (value: string) => void;
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
        hint={t("hosts.bastionForm.tagsHint")}
        label={t("hosts.bastionForm.tags")}
      >
        <Textarea
          onChange={(event) => onTagsTextChange(event.target.value)}
          rows={3}
          value={tagsText}
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
  const [tagsText, setTagsText] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const close = (next: boolean) => {
    if (!next) {
      setName("");
      setEnvironment("production");
      setTagsText("");
    }
    onOpenChange(next);
  };

  const submit = async () => {
    if (submitting || !name.trim()) return;
    setSubmitting(true);
    try {
      await api.connectors.createBastionScope({
        name: name.trim(),
        environment,
        tags: parseTags(tagsText),
      });
      onCreated();
      close(false);
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
      <BastionFields
        autoFocus
        environment={environment}
        name={name}
        onEnvironmentChange={setEnvironment}
        onNameChange={setName}
        onTagsTextChange={setTagsText}
        tagsText={tagsText}
      />
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
  connectorStatus?: ConnectorStatus;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [name, setName] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [tagsText, setTagsText] = useState("");
  const [token, setToken] = useState<ConnectorEnrollmentToken | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [generationError, setGenerationError] = useState("");
  const canInstall =
    scope?.status === "pending" ||
    scope?.status === "uninstalled" ||
    connectorStatus === "offline";

  useEffect(() => {
    if (!scope) return;
    setName(scope.name);
    setEnvironment(scope.environment);
    setTagsText(tagsToText(scope.tags));
    setToken(scope.registrationToken ?? null);
    setGenerationError("");
  }, [scope]);

  const submit = async () => {
    if (!scope || submitting || !name.trim()) return;
    setSubmitting(true);
    try {
      await api.connectors.updateBastionScope(scope.id, {
        name: name.trim(),
        environment,
        tags: parseTags(tagsText),
      });
      onSaved();
      onOpenChange(false);
    } finally {
      setSubmitting(false);
    }
  };

  const generateCommand = async () => {
    if (!scope || generating) return;
    setGenerating(true);
    setGenerationError("");
    try {
      const nextToken = await api.connectors.regenerateEnrollmentToken(
        scope.id,
      );
      setToken(nextToken);
      onSaved();
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
      <BastionFields
        environment={environment}
        name={name}
        onEnvironmentChange={setEnvironment}
        onNameChange={setName}
        onTagsTextChange={setTagsText}
        tagsText={tagsText}
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
              {token
                ? t("hosts.bastionForm.commandRegenerate")
                : t("hosts.bastionForm.commandGenerate")}
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
        {token && token.status === "active" && (
          <div className="argus-bastion-command__code">
            <CodeBlock code={token.installCommand} language="bash" />
            <span>
              {t("hosts.scope.tokenExpires", {
                time: formatDateTime(token.expiresAt),
              })}
            </span>
          </div>
        )}
      </section>
    </FormDrawer>
  );
}

/** 生成需在当前堡垒机上执行的一次性卸载命令。 */
export function BastionUninstallDialog({
  scope,
  onOpenChange,
  onChanged,
  onSimulate,
}: {
  scope: BastionScope | null;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
  onSimulate?: (scopeId: string, commandId: string) => ConnectorUninstallResult;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [command, setCommand] = useState<ConnectorUninstallCommand | null>(
    null,
  );
  const [result, setResult] = useState<ConnectorUninstallResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!scope) {
      setCommand(null);
      setResult(null);
      setError("");
      return;
    }
    let cancelled = false;
    setLoading(true);
    setCommand(null);
    setResult(null);
    setError("");
    void api.connectors
      .createUninstallCommand(scope.id)
      .then((nextCommand) => {
        if (!cancelled) setCommand(nextCommand);
      })
      .catch((nextError: unknown) => {
        if (cancelled) return;
        setError(
          nextError instanceof Error
            ? nextError.message
            : t("hosts.bastionUninstall.commandFailed"),
        );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, scope, t]);

  const simulateUninstall = () => {
    if (!scope || !command || !onSimulate) return;
    const nextResult = onSimulate(scope.id, command.id);
    setResult(nextResult);
    if (nextResult.success) onChanged();
  };

  return (
    <Dialog
      description={t("hosts.bastionUninstall.description")}
      footer={
        <Button onClick={() => onOpenChange(false)} variant="secondary">
          {t("hosts.bastionUninstall.close")}
        </Button>
      }
      onOpenChange={onOpenChange}
      open={scope !== null}
      title={t("hosts.bastionUninstall.title", { name: scope?.name ?? "" })}
    >
      {loading && <p className="argus-muted">{t("common.loading")}</p>}
      <Alert
        description={t("hosts.bastionUninstall.warning")}
        title={t("hosts.bastionUninstall.warningTitle")}
        tone="warning"
      />
      {error && (
        <Alert
          description={error}
          title={t("hosts.bastionUninstall.commandFailed")}
          tone="danger"
        />
      )}
      {command && command.status === "active" && (
        <div className="argus-bastion-command__code">
          <CodeBlock code={command.uninstallCommand} language="bash" />
          <span>
            {t("hosts.scope.tokenExpires", {
              time: formatDateTime(command.expiresAt),
            })}
          </span>
        </div>
      )}
      {onSimulate && command && !result?.success && (
        <Button onClick={simulateUninstall} variant="ghost">
          {t("hosts.bastionUninstall.simulate")}（{t("hosts.scope.demoOnly")}）
        </Button>
      )}
      {result && (
        <Alert
          description={result.message}
          title={
            result.success
              ? t("hosts.bastionUninstall.completed")
              : t("hosts.bastionUninstall.commandFailed")
          }
          tone={result.success ? "success" : "danger"}
        />
      )}
    </Dialog>
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
  const [environment, setEnvironment] = useState<Environment>("production");
  const [tagsText, setTagsText] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // host 变化时同步表单初值（drawer 打开期间 host 固定）。
  if (host && host.id !== loadedFor) {
    setLoadedFor(host.id);
    setName(host.name);
    setAddress(host.address);
    setPort(String(host.port));
    setEnvironment(host.environment);
    setTagsText(tagsToText(host.tags));
  }

  const submit = async () => {
    if (!host || submitting || !name.trim()) return;
    setSubmitting(true);
    try {
      const patch: UpdateHostInput = {
        name: name.trim(),
        address: address.trim(),
        port: Number(port) || host.port,
        environment,
        tags: parseTags(tagsText),
      };
      await api.hosts.update(host.id, patch);
      onSaved();
      onOpenChange(false);
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
      <Field label={t("hosts.wizard.name")}>
        <Input onChange={(event) => setName(event.target.value)} value={name} />
      </Field>
      <Field label={t("hosts.wizard.address")}>
        <Input
          onChange={(event) => setAddress(event.target.value)}
          value={address}
        />
      </Field>
      <div className="argus-form-row">
        <Field label={t("hosts.wizard.port")}>
          <Input
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
      <Field hint={t("hosts.wizard.tagsHint")} label={t("hosts.wizard.tags")}>
        <Textarea
          onChange={(event) => setTagsText(event.target.value)}
          rows={3}
          value={tagsText}
        />
      </Field>
    </FormDrawer>
  );
}
