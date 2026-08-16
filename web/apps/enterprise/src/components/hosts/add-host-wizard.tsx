import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  useApi,
  type BastionScope,
  type ConnectionTestResult,
  type CreateHostInput,
  type Environment,
  type PendingActionPublic,
} from "@argus/api-client";
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
import { ARGUS_EGRESS_IP, isPublicAddress, parseTags } from "./host-utils";
import { PendingActionConfirm } from "./pending-action-confirm";

type Mode = "via_bastion" | "direct";
type Protocol = "ssh" | "winrm";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

/** 向导内的连接测试为纯前端模拟（待创建的主机尚无 testConnection 目标）。 */
function simulateTest(input: {
  mode: Mode;
  address: string;
  scopeName?: string;
}): Promise<ConnectionTestResult> {
  return new Promise((resolve) => {
    window.setTimeout(() => {
      const routeDetail =
        input.mode === "via_bastion"
          ? (input.scopeName ?? "bastion")
          : "direct_executor";
      resolve({
        success: true,
        latencyMs: 120 + Math.round(Math.random() * 120),
        checks: [
          { name: "dns_resolve", status: "passed", detail: input.address },
          { name: "network_route", status: "passed", detail: routeDetail },
          { name: "host_key", status: "passed" },
          { name: "authentication", status: "passed" },
        ],
      });
    }, 900);
  });
}

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

  const [step, setStep] = useState(0);
  const [mode, setMode] = useState<Mode>("via_bastion");
  const [scopeId, setScopeId] = useState("");
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [port, setPort] = useState("22");
  const [protocol, setProtocol] = useState<Protocol>("ssh");
  const [platform, setPlatform] = useState<"linux" | "windows">("linux");
  const [account, setAccount] = useState("");
  const [secretId, setSecretId] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [tagsText, setTagsText] = useState("");
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(
    null,
  );
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const secretsQuery = useQuery({
    queryKey: ["secrets"],
    queryFn: () => api.secrets.list(),
    enabled: open,
  });

  const credentialSecrets = useMemo(() => {
    const items = secretsQuery.data?.items ?? [];
    const types =
      protocol === "winrm"
        ? ["winrm_password"]
        : ["ssh_password", "ssh_private_key"];
    return items.filter((secret) => types.includes(secret.type));
  }, [secretsQuery.data, protocol]);

  const reset = () => {
    setStep(0);
    setMode("via_bastion");
    setScopeId("");
    setName("");
    setAddress("");
    setPort("22");
    setProtocol("ssh");
    setPlatform("linux");
    setAccount("");
    setSecretId("");
    setEnvironment("production");
    setTagsText("");
    setTestResult(null);
    setPendingAction(null);
  };

  const close = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const addressValid =
    address.trim().length > 0 &&
    (mode === "via_bastion" || isPublicAddress(address));
  const step1Valid = mode === "direct" || scopeId.length > 0;
  const step2Valid = name.trim().length > 0 && addressValid && Number(port) > 0;

  const runTest = async () => {
    setTesting(true);
    setTestResult(null);
    const scopeName = scopes.find((scope) => scope.id === scopeId)?.name;
    const result = await simulateTest({
      mode,
      address: address.trim(),
      scopeName,
    });
    setTestResult(result);
    setTesting(false);
  };

  const submit = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      const input: CreateHostInput = {
        name: name.trim(),
        address: address.trim(),
        port: Number(port),
        platform,
        connectionMode:
          mode === "via_bastion"
            ? "via_bastion"
            : protocol === "winrm"
              ? "direct_winrm"
              : "direct_ssh",
        bastionScopeId: mode === "via_bastion" ? scopeId : undefined,
        credentialRef: secretId || undefined,
        environment,
        labels: parseTags(tagsText),
      };
      setPendingAction(await api.hosts.previewCreate(input));
    } finally {
      setSubmitting(false);
    }
  };

  const selectedScope = scopes.find((scope) => scope.id === scopeId);

  return (
    <FormDrawer
      footer={<></>}
      onOpenChange={close}
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
                : Boolean(testResult?.success)
          }
          current={step}
          onBack={() => setStep((value) => Math.max(0, value - 1))}
          onNext={() => setStep((value) => value + 1)}
          onSubmit={() => void submit()}
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
          submitting={submitting}
        >
          {step === 0 && (
            <div className="argus-choice-list">
              <button
                className={`argus-choice ${mode === "via_bastion" ? "is-selected" : ""}`}
                onClick={() => setMode("via_bastion")}
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
                      onClick={() => setScopeId(scope.id)}
                      type="button"
                    >
                      <span className="argus-choice__text">
                        <b>{scope.name}</b>
                        <small>
                          {t(`hosts.env.${scope.environment}`)} ·{" "}
                          {t("hosts.scope.members", {
                            count: scope.memberHostIds.length,
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
                onClick={() => setMode("direct")}
                type="button"
              >
                <span className="argus-choice__text">
                  <b>{t("hosts.wizard.direct")}</b>
                  <small>{t("hosts.wizard.directDesc")}</small>
                </span>
              </button>
              {mode === "direct" && (
                <p className="argus-inline-note">
                  {t("hosts.wizard.egressNote", { ip: ARGUS_EGRESS_IP })}
                </p>
              )}
            </div>
          )}

          {step === 1 && (
            <>
              <Field label={t("hosts.wizard.name")}>
                <Input
                  onChange={(event) => setName(event.target.value)}
                  placeholder={t("hosts.wizard.namePlaceholder")}
                  value={name}
                />
              </Field>
              <Field
                error={
                  address.trim() && !addressValid
                    ? t("hosts.wizard.publicAddressInvalid")
                    : undefined
                }
                label={t("hosts.wizard.address")}
              >
                <Input
                  onChange={(event) => setAddress(event.target.value)}
                  placeholder={t("hosts.wizard.addressPlaceholder")}
                  value={address}
                />
              </Field>
              <div className="argus-form-row">
                <Field label={t("hosts.wizard.protocol")}>
                  <Select
                    onValueChange={(value) => {
                      const next = value as Protocol;
                      setProtocol(next);
                      setPort(next === "winrm" ? "5986" : "22");
                      setPlatform(next === "winrm" ? "windows" : "linux");
                      setSecretId("");
                    }}
                    options={[
                      { value: "ssh", label: "SSH" },
                      { value: "winrm", label: "WinRM" },
                    ]}
                    value={protocol}
                  />
                </Field>
                <Field label={t("hosts.wizard.port")}>
                  <Input
                    inputMode="numeric"
                    onChange={(event) => setPort(event.target.value)}
                    value={port}
                  />
                </Field>
              </div>
              <div className="argus-form-row">
                <Field label={t("hosts.wizard.platform")}>
                  <Select
                    onValueChange={(value) =>
                      setPlatform(value as "linux" | "windows")
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
                <Field label={t("hosts.wizard.environment")}>
                  <Select
                    onValueChange={(value) =>
                      setEnvironment(value as Environment)
                    }
                    options={ENVIRONMENTS.map((env) => ({
                      value: env,
                      label: t(`hosts.env.${env}`),
                    }))}
                    value={environment}
                  />
                </Field>
              </div>
              <Field label={t("hosts.wizard.account")}>
                <Input
                  onChange={(event) => setAccount(event.target.value)}
                  placeholder={t("hosts.wizard.accountPlaceholder")}
                  value={account}
                />
              </Field>
              <Field label={t("hosts.wizard.secret")}>
                <Select
                  onValueChange={setSecretId}
                  options={[
                    { value: "", label: t("hosts.wizard.secretNone") },
                    ...credentialSecrets.map((secret) => ({
                      value: secret.id,
                      label: `${secret.name}（${secret.type}）`,
                    })),
                  ]}
                  value={secretId}
                />
                {credentialSecrets.length === 0 && (
                  <span className="argus-field__hint">
                    {t("hosts.wizard.secretEmpty")} ·{" "}
                    <Link to="/settings/secrets">
                      {t("hosts.wizard.secretCreate")}
                    </Link>
                  </span>
                )}
              </Field>
              <Field
                hint={t("hosts.wizard.tagsHint")}
                label={t("hosts.wizard.labels")}
              >
                <Textarea
                  onChange={(event) => setTagsText(event.target.value)}
                  rows={3}
                  value={tagsText}
                />
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
                      ms: testResult.latencyMs,
                    })}
                    title={
                      testResult.success
                        ? t("hosts.wizard.testPassed")
                        : t("hosts.wizard.testFailed")
                    }
                    tone={testResult.success ? "success" : "danger"}
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
                  onClick={() => void runTest()}
                  variant="secondary"
                >
                  {testing
                    ? t("hosts.wizard.testing")
                    : t("hosts.wizard.runTest")}
                </Button>
                {!testResult?.success && (
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
