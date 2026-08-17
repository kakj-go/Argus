import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
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
import {
  ARGUS_EGRESS_ADDRESSES,
  isPublicAddress,
  parseLabels,
} from "./host-utils";
import { PendingActionConfirm } from "./pending-action-confirm";

type Mode = "via_bastion" | "direct";
type Protocol = "ssh" | "winrm";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

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
  const [mode, setMode] = useState<Mode>("via_bastion");
  const [scopeId, setScopeId] = useState("");
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [port, setPort] = useState("22");
  const [protocol, setProtocol] = useState<Protocol>("ssh");
  const [platform, setPlatform] = useState<"linux" | "windows">("linux");
  const [account, setAccount] = useState("");
  const [credentialId, setCredentialId] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [labelsText, setLabelsText] = useState("");
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ConnectionTest | null>(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [submitting, setSubmitting] = useState(false);

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
    setMode("via_bastion");
    setScopeId("");
    setName("");
    setAddress("");
    setPort("22");
    setProtocol("ssh");
    setPlatform("linux");
    setAccount("");
    setCredentialId("");
    setEnvironment("production");
    setLabelsText("");
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
  const step2Valid =
    name.trim().length > 0 &&
    addressValid &&
    Number(port) > 0 &&
    account.trim().length > 0 &&
    credentialId.length > 0;

  const runTest = async () => {
    setTesting(true);
    setTestResult(null);
    const connectionMode =
      mode === "via_bastion"
        ? "via_bastion"
        : protocol === "winrm"
          ? "direct_winrm"
          : "direct_ssh";
    let result = await api.hosts.createConnectionTest({
      address: address.trim(),
      port: Number(port),
      platform,
      connection_mode: connectionMode,
      bastion_scope_id: mode === "via_bastion" ? scopeId : undefined,
      credential_id: credentialId,
      username: account.trim(),
    });
    for (let attempt = 0; attempt < 60; attempt += 1) {
      if (!["queued", "running"].includes(result.status)) break;
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      result = await api.hosts.getConnectionTest(result.id);
    }
    setTestResult(result);
    setTesting(false);
  };

  const submit = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      if (!testResult || testResult.status !== "succeeded") return;
      const input: HostPreviewCreate = {
        name: name.trim(),
        address: address.trim(),
        port: Number(port),
        platform,
        connection_mode:
          mode === "via_bastion"
            ? "via_bastion"
            : protocol === "winrm"
              ? "direct_winrm"
              : "direct_ssh",
        bastion_scope_id: mode === "via_bastion" ? scopeId : undefined,
        credential_id: credentialId,
        username: account.trim(),
        environment,
        labels: parseLabels(labelsText),
        connection_test_id: testResult.id,
      };
      setPendingAction(await api.hosts.previewCreateResource(input));
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
                : testResult?.status === "succeeded"
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
                  {t("hosts.wizard.egressNote", { ip: egressDisplay })}
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
                      setCredentialId("");
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
                  ariaLabel={t("hosts.wizard.secret")}
                  onValueChange={setCredentialId}
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
                  onClick={() => void runTest()}
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
