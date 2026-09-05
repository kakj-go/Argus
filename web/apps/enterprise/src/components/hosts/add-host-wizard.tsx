import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useReducer, useRef, useState } from "react";
import { useForm, type UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";

import {
  formConstraint,
  presentApiFormError,
  useApi,
  type ActionOneTimeResult,
  type BastionScope,
  type ConfirmActionResult,
  type ConnectionTest,
  type Environment,
  type HostPreviewCreate,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  Button,
  ConfirmDialog,
  Dialog,
  Field,
  Input,
  ModeGrid,
  ScenarioCard,
  Select,
  Textarea,
  TopologyDiagram,
  WizardProgress,
} from "@argus/ui";

import { formatDateTime } from "../settings/shared";
import { ARGUS_EGRESS_ADDRESSES, parseLabels } from "./host-utils";
import { InstallInstructionPanel } from "./install-instruction-panel";
import {
  cleanHostModeSpecificDraft,
  hostModeSwitchLosesFields,
  onboardingWizardReducer,
  onboardingWizardStep,
  type HostModeSpecificDraft,
  type HostOnboardingMode,
  type OnboardingWizardState,
} from "./onboarding-wizard-state";
import { PendingActionConfirm } from "./pending-action-confirm";

const HOST_FORM_ID = "argus-host-onboarding-form";
const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];
const initialState: OnboardingWizardState<HostOnboardingMode> = {
  phase: "select_mode",
  mode: "direct_both",
};
const constraints = {
  address: formConstraint("HostPreviewCreate", "address"),
  name: formConstraint("HostPreviewCreate", "name"),
  port: formConstraint("HostPreviewCreate", "port"),
  username: formConstraint("HostPreviewCreate", "username"),
};

type HostWizardForm = HostModeSpecificDraft & {
  name: string;
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
  scopes: BastionScope[];
  onCreated: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [wizard, dispatch] = useReducer(
    onboardingWizardReducer<HostOnboardingMode>,
    initialState,
  );
  const [pendingMode, setPendingMode] = useState<HostOnboardingMode | null>(
    null,
  );
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [connectionTest, setConnectionTest] = useState<ConnectionTest | null>(
    null,
  );
  const [oneTimeResult, setOneTimeResult] =
    useState<ActionOneTimeResult | null>(null);
  const [testing, setTesting] = useState(false);
  const createdNotified = useRef(false);

  const isSelf = wizard.mode === "self_enrolled";
  const isMember =
    wizard.mode === "bastion_member" || wizard.mode === "bastion_tunnel_member";

  const schema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t("hosts.wizard.required"))
            .max(constraints.name.maxLength ?? 128),
          environment: z.enum(["development", "staging", "production"]),
          labelsText: z.string(),
          address: z.string(),
          port: z.string(),
          protocol: z.enum(["ssh", "winrm"]),
          platform: z.enum(["linux", "windows"]),
          architecture: z.enum(["amd64", "arm64"]),
          account: z.string(),
          credentialId: z.string(),
          scopeId: z.string(),
        })
        .superRefine((value, context) => {
          if (isSelf) return;
          const issue = (message: string, field: keyof HostWizardForm) =>
            context.addIssue({ code: "custom", message, path: [field] });
          if (
            value.address.trim().length <
              (constraints.address.minLength ?? 1) ||
            value.address.trim().length > (constraints.address.maxLength ?? 512)
          ) {
            issue(t("hosts.wizard.required"), "address");
          }
          const port = Number(value.port);
          if (
            !Number.isInteger(port) ||
            port < (constraints.port.minimum ?? 1) ||
            port > (constraints.port.maximum ?? 65535)
          ) {
            issue(t("hosts.wizard.portInvalid"), "port");
          }
          if (
            value.account.trim().length <
              (constraints.username.minLength ?? 1) ||
            value.account.trim().length >
              (constraints.username.maxLength ?? 256)
          ) {
            issue(t("hosts.wizard.required"), "account");
          }
          if (!value.credentialId)
            issue(t("hosts.wizard.required"), "credentialId");
          if (isMember && !value.scopeId)
            issue(t("hosts.wizard.scopeRequired"), "scopeId");
        }),
    [isMember, isSelf, t],
  );
  const form = useForm<HostWizardForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      environment: "production",
      labelsText: "",
      address: "",
      port: "22",
      protocol: "ssh",
      platform: "linux",
      architecture: "amd64",
      account: "",
      credentialId: "",
      scopeId: "",
    },
  });
  const values = form.watch();
  const credentialsQuery = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
    enabled: open && wizard.phase !== "select_mode" && !isSelf,
  });
  const credentials = useMemo(
    () =>
      (credentialsQuery.data ?? []).filter(
        (credential) => credential.protocol === values.protocol,
      ),
    [credentialsQuery.data, values.protocol],
  );

  const reset = () => {
    form.reset();
    dispatch({ type: "reset", mode: "direct_both" });
    setPendingMode(null);
    setPendingAction(null);
    setConnectionTest(null);
    setOneTimeResult(null);
    setTesting(false);
    createdNotified.current = false;
  };
  const close = (next: boolean) => {
    if (!next) {
      if (pendingAction) {
        void api.approvals.cancel(pendingAction.action_ref).catch(() => {
          // The server may already have expired or committed the preview.
        });
      }
      reset();
    }
    onOpenChange(next);
  };
  const notifyCreated = () => {
    if (createdNotified.current) return;
    createdNotified.current = true;
    onCreated();
  };

  const specificDraft = (): HostModeSpecificDraft => ({
    address: values.address,
    port: values.port,
    protocol: values.protocol,
    platform: values.platform,
    architecture: values.architecture,
    account: values.account,
    credentialId: values.credentialId,
    scopeId: values.scopeId,
  });
  const applyMode = (mode: HostOnboardingMode) => {
    const cleaned = cleanHostModeSpecificDraft(
      specificDraft(),
      wizard.mode,
      mode,
    );
    for (const [field, value] of Object.entries(cleaned) as Array<
      [keyof HostModeSpecificDraft, string]
    >) {
      form.setValue(field, value as never);
    }
    setConnectionTest(null);
    setPendingAction(null);
    dispatch({ type: "select_mode", mode });
  };
  const requestMode = (mode: HostOnboardingMode) => {
    if (mode === wizard.mode) return;
    if (hostModeSwitchLosesFields(specificDraft(), wizard.mode, mode)) {
      setPendingMode(mode);
      return;
    }
    applyMode(mode);
  };

  const connectionMode = (input: HostWizardForm) => {
    if (isSelf) return "self_enrolled" as const;
    if (isMember) return "via_bastion" as const;
    return input.protocol === "winrm"
      ? ("direct_winrm" as const)
      : ("direct_ssh" as const);
  };

  const runConnectionTest = async (input: HostWizardForm) => {
    let result = await api.hosts.createConnectionTest({
      address: input.address,
      port: Number(input.port),
      platform: input.platform,
      connection_mode: connectionMode(input) as
        "via_bastion" | "direct_ssh" | "direct_winrm",
      bastion_scope_id: isMember ? input.scopeId : undefined,
      credential_id: input.credentialId,
      username: input.account,
    });
    for (
      let attempt = 0;
      attempt < 60 && ["queued", "running"].includes(result.status);
      attempt += 1
    ) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      result = await api.hosts.getConnectionTest(result.id);
    }
    setConnectionTest(result);
    return result;
  };

  const preparePreview = async (input: HostWizardForm) => {
    if (testing) return;
    form.clearErrors();
    setTesting(true);
    try {
      const test = isSelf ? null : await runConnectionTest(input);
      if (test && test.status !== "succeeded") {
        form.setError("root", {
          type: "server",
          message: t("hosts.wizard.testFailed"),
        });
        return;
      }
      const request: HostPreviewCreate = {
        name: input.name,
        platform: isSelf ? "linux" : input.platform,
        connection_mode: connectionMode(input),
        environment: input.environment,
        labels: parseLabels(input.labelsText),
        ...(isSelf
          ? { architecture: input.architecture }
          : {
              address: input.address,
              port: Number(input.port),
              credential_id: input.credentialId,
              username: input.account,
              bastion_scope_id: isMember ? input.scopeId : undefined,
              connection_test_id: test!.id,
            }),
      };
      setPendingAction(await api.hosts.previewCreateResource(request));
      dispatch({
        type: "next",
        terminal: isSelf ? "confirm_command" : "verify",
      });
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
    } finally {
      setTesting(false);
    }
  };

  const committed = (result: ConfirmActionResult) => {
    setPendingAction(null);
    if (result.one_time_result) {
      setOneTimeResult(result.one_time_result);
      dispatch({ type: "commit_command" });
    } else {
      dispatch({ type: "commit_complete" });
    }
    notifyCreated();
  };
  const previewBack = async () => {
    if (pendingAction) {
      try {
        await api.approvals.cancel(pendingAction.action_ref);
      } catch {
        // Expired previews are safe to discard.
      }
    }
    setPendingAction(null);
    setConnectionTest(null);
    dispatch({ type: "back" });
  };

  const steps = [
    {
      id: "mode",
      title: t("hosts.wizard.step1"),
      description: t("hosts.wizard.step1Desc"),
    },
    {
      id: "details",
      title: t("hosts.wizard.step2"),
      description: t("hosts.wizard.step2Desc"),
    },
    {
      id: "verify",
      title: isSelf
        ? t("hosts.wizard.generateCommand")
        : t("hosts.wizard.step3"),
      description: t("hosts.wizard.step3Desc"),
    },
  ];

  return (
    <>
      <Dialog
        className="argus-dialog--wizard"
        description={t("hosts.wizard.dialogDesc")}
        footer={
          <HostFooter
            back={() => dispatch({ type: "back" })}
            busy={testing}
            close={close}
            next={() =>
              dispatch({
                type: "next",
                terminal: isSelf ? "confirm_command" : "verify",
              })
            }
            phase={wizard.phase}
            previewBack={() => void previewBack()}
            t={t}
            valid={Boolean(values.name.trim())}
          />
        }
        onOpenChange={close}
        open={open}
        title={t("hosts.wizard.title")}
        width={1080}
      >
        <WizardProgress
          current={onboardingWizardStep(wizard.phase)}
          steps={steps}
        />
        <div
          className="argus-wizard-dialog__content"
          data-mode={wizard.mode}
          data-phase={wizard.phase}
          data-testid="host-onboarding-flow"
        >
          {wizard.phase === "select_mode" && (
            <HostModeStep mode={wizard.mode} onSelect={requestMode} t={t} />
          )}
          {wizard.phase === "details" && (
            <HostDetails
              credentials={credentials}
              form={form}
              isMember={isMember}
              isSelf={isSelf}
              mode={wizard.mode}
              onChangeMode={() => dispatch({ type: "change_mode" })}
              onValid={preparePreview}
              scopes={scopes}
              t={t}
              values={values}
            />
          )}
          {(wizard.phase === "verify" || wizard.phase === "confirm_command") &&
            pendingAction && (
              <div className="argus-dialog__flow">
                {connectionTest && (
                  <Alert
                    description={
                      connectionTest.status === "succeeded"
                        ? t("hosts.wizard.testPassed")
                        : t("hosts.wizard.testFailed")
                    }
                    title={t("hosts.wizard.connectionTestSummary")}
                    tone={
                      connectionTest.status === "succeeded"
                        ? "success"
                        : "danger"
                    }
                  />
                )}
                <PendingActionConfirm
                  action={pendingAction}
                  claimOneTimeResult={isSelf}
                  onCancel={() => {
                    setPendingAction(null);
                    dispatch({ type: "back" });
                  }}
                  onDone={committed}
                />
              </div>
            )}
          {wizard.phase === "command_result" && oneTimeResult && (
            <div className="argus-dialog__flow argus-detail-section">
              <Alert
                description={t("hosts.wizard.commandOnce")}
                title={t("hosts.wizard.commandTitle")}
                tone="success"
              />
              <InstallInstructionPanel result={oneTimeResult} />
              <p className="argus-muted">
                {t("hosts.wizard.commandExpires", {
                  time: formatDateTime(oneTimeResult.expires_at),
                })}
              </p>
            </div>
          )}
          {wizard.phase === "completed" && (
            <Alert
              description={t("hosts.wizard.completedDesc")}
              title={t("hosts.wizard.completed")}
              tone="success"
            />
          )}
        </div>
      </Dialog>
      <ConfirmDialog
        description={t("hosts.wizard.modeSwitchWarning")}
        onConfirm={() => {
          if (pendingMode) applyMode(pendingMode);
          setPendingMode(null);
        }}
        onOpenChange={(next) => !next && setPendingMode(null)}
        open={pendingMode !== null}
        title={t("hosts.wizard.modeSwitchTitle")}
      />
    </>
  );
}

type Translate = (key: string, options?: Record<string, unknown>) => string;

function HostDetails({
  credentials,
  form,
  isMember,
  isSelf,
  mode,
  onChangeMode,
  onValid,
  scopes,
  t,
  values,
}: {
  credentials: Array<{ id: string; name: string }>;
  form: UseFormReturn<HostWizardForm>;
  isMember: boolean;
  isSelf: boolean;
  mode: HostOnboardingMode;
  onChangeMode: () => void;
  onValid: (values: HostWizardForm) => Promise<void>;
  scopes: BastionScope[];
  t: Translate;
  values: HostWizardForm;
}) {
  const errors = form.formState.errors;
  return (
    <form
      className="argus-wizard-details"
      id={HOST_FORM_ID}
      onSubmit={form.handleSubmit(onValid)}
    >
      <div className="argus-selected-mode">
        <div>
          <span>{t("hosts.wizard.selectedMode")}</span>
          <strong>{t(`hosts.scenario.titleOf.${mode}`)}</strong>
        </div>
        <Button onClick={onChangeMode} type="button" variant="ghost">
          {t("hosts.wizard.changeMode")}
        </Button>
      </div>
      {errors.root?.message && (
        <Alert
          description={errors.root.message}
          title={t("hosts.wizard.previewFailed")}
          tone="danger"
        />
      )}
      <div className="argus-scenario-wizard__form">
        <Field
          error={errors.name?.message}
          label={t("hosts.wizard.name")}
          requirement="required"
        >
          <Input
            autoFocus
            placeholder={t("hosts.wizard.namePlaceholder")}
            {...form.register("name")}
          />
        </Field>
        <Field label={t("hosts.wizard.environment")} requirement="required">
          <Select
            onValueChange={(value) =>
              form.setValue("environment", value as Environment, {
                shouldValidate: true,
              })
            }
            options={ENVIRONMENTS.map((value) => ({
              value,
              label: t(`hosts.env.${value}`),
            }))}
            value={values.environment}
          />
        </Field>
        {isMember && (
          <Field
            className="argus-field--wide"
            error={errors.scopeId?.message}
            label={t("hosts.wizard.scope")}
            requirement="required"
          >
            <Select
              onValueChange={(value) =>
                form.setValue("scopeId", value, { shouldValidate: true })
              }
              options={scopes.map((scope) => ({
                value: scope.id,
                label: scope.name,
              }))}
              placeholder={t("hosts.wizard.selectScope")}
              value={values.scopeId}
            />
          </Field>
        )}
        {isSelf ? (
          <>
            <Field label={t("hosts.wizard.platform")} requirement="none">
              <Input disabled value={t("hosts.wizard.platformLinux")} />
            </Field>
            <Field
              label={t("hosts.wizard.architecture")}
              requirement="required"
            >
              <Select
                onValueChange={(value) =>
                  form.setValue(
                    "architecture",
                    value as HostWizardForm["architecture"],
                    { shouldValidate: true },
                  )
                }
                options={[
                  { value: "amd64", label: "amd64" },
                  { value: "arm64", label: "arm64" },
                ]}
                value={values.architecture}
              />
            </Field>
            <Alert
              description={t("hosts.wizard.selfEnrolledPrereq")}
              title={t("hosts.wizard.selfEnrolledPrereqTitle")}
              tone="info"
            />
          </>
        ) : (
          <>
            <Field
              error={errors.address?.message}
              label={t("hosts.wizard.address")}
              requirement="required"
            >
              <Input
                placeholder={t("hosts.wizard.addressPlaceholder")}
                {...form.register("address")}
              />
            </Field>
            <Field
              error={errors.port?.message}
              label={t("hosts.wizard.port")}
              requirement="required"
            >
              <Input inputMode="numeric" {...form.register("port")} />
            </Field>
            <Field label={t("hosts.wizard.protocol")} requirement="required">
              <Select
                onValueChange={(value) => {
                  const protocol = value as HostWizardForm["protocol"];
                  form.setValue("protocol", protocol, { shouldValidate: true });
                  form.setValue("port", protocol === "winrm" ? "5986" : "22");
                  form.setValue(
                    "platform",
                    protocol === "winrm" ? "windows" : "linux",
                  );
                  form.setValue("credentialId", "");
                }}
                options={[
                  { value: "ssh", label: "SSH" },
                  { value: "winrm", label: "WinRM" },
                ]}
                value={values.protocol}
              />
            </Field>
            <Field label={t("hosts.wizard.platform")} requirement="required">
              <Select
                disabled={values.protocol === "winrm"}
                onValueChange={(value) =>
                  form.setValue(
                    "platform",
                    value as HostWizardForm["platform"],
                    { shouldValidate: true },
                  )
                }
                options={[
                  { value: "linux", label: t("hosts.wizard.platformLinux") },
                  {
                    value: "windows",
                    label: t("hosts.wizard.platformWindows"),
                  },
                ]}
                value={values.platform}
              />
            </Field>
            <Field
              error={errors.account?.message}
              label={t("hosts.wizard.account")}
              requirement="required"
            >
              <Input
                placeholder={t("hosts.wizard.accountPlaceholder")}
                {...form.register("account")}
              />
            </Field>
            <Field
              error={errors.credentialId?.message}
              label={t("hosts.wizard.secret")}
              requirement="required"
            >
              <Select
                onValueChange={(value) =>
                  form.setValue("credentialId", value, {
                    shouldValidate: true,
                  })
                }
                options={credentials.map((credential) => ({
                  value: credential.id,
                  label: credential.name,
                }))}
                placeholder={t("hosts.wizard.secretNone")}
                value={values.credentialId}
              />
            </Field>
          </>
        )}
        <Field
          className="argus-field--wide"
          label={t("hosts.wizard.labels")}
          requirement="optional"
        >
          <Textarea {...form.register("labelsText")} rows={3} />
        </Field>
      </div>
    </form>
  );
}

function HostModeStep({
  mode,
  onSelect,
  t,
}: {
  mode: HostOnboardingMode;
  onSelect: (mode: HostOnboardingMode) => void;
  t: Translate;
}) {
  const modes: HostOnboardingMode[] = [
    "direct_both",
    "direct_in",
    "self_enrolled",
    "bastion_member",
    "bastion_tunnel_member",
  ];
  return (
    <div className="argus-mode-selection">
      <div className="argus-mode-selection__cards">
        {modes.map((candidate) => (
          <ScenarioCard
            description={t(`hosts.scenario.summaryOf.${candidate}`)}
            diagram={<HostTopology mode={candidate} t={t} />}
            key={candidate}
            onSelect={() => onSelect(candidate)}
            refLabel={hostModeReference(candidate, t)}
            selected={mode === candidate}
            statusLabel={t("hosts.scenario.statusSupported")}
            title={t(`hosts.scenario.titleOf.${candidate}`)}
          />
        ))}
      </div>
      <div className="argus-mode-selection__detail">
        <h3>{t(`hosts.scenario.titleOf.${mode}`)}</h3>
        <p>{t(`hosts.scenario.summaryOf.${mode}`)}</p>
        <ModeGrid items={hostModeGrid(mode, t)} />
        {(mode === "direct_both" || mode === "direct_in") && (
          <Alert
            description={t("hosts.wizard.egressNote", {
              ip:
                ARGUS_EGRESS_ADDRESSES.join(", ") ||
                t("hosts.wizard.egressNotConfigured"),
            })}
            title={t("hosts.wizard.networkPrerequisite")}
            tone="info"
          />
        )}
      </div>
    </div>
  );
}

function HostTopology({ mode, t }: { mode: HostOnboardingMode; t: Translate }) {
  if (mode === "bastion_member" || mode === "bastion_tunnel_member") {
    return (
      <TopologyDiagram
        label={t(`hosts.scenario.titleOf.${mode}`)}
        layout="member"
        links={[
          {
            mode: "ok",
            direction: "down",
            label: t("hosts.topology.sshManage"),
            slot: "left",
          },
          {
            mode: mode === "bastion_member" ? "ok" : "blocked",
            direction: mode === "bastion_member" ? "up" : "none",
            label:
              mode === "bastion_member"
                ? t("hosts.topology.otlpPush")
                : t("hosts.topology.noDirect"),
            slot: "right",
          },
          {
            mode: mode === "bastion_member" ? "ok" : "tunnel",
            direction: "right",
            label:
              mode === "bastion_member"
                ? t("hosts.topology.egress")
                : t("hosts.topology.tunnelOtlp"),
            slot: "h",
          },
        ]}
        nodes={[
          { label: t("hosts.topology.bastion"), kind: "bastion" },
          { label: t("hosts.topology.member") },
          { label: t("hosts.topology.argus"), kind: "argus" },
        ]}
      />
    );
  }
  const links =
    mode === "self_enrolled"
      ? [
          {
            mode: "blocked" as const,
            direction: "none" as const,
            label: t("hosts.topology.noDirect"),
            slot: "top" as const,
          },
          {
            mode: "ok" as const,
            direction: "right" as const,
            label: t("hosts.topology.selfPush"),
            slot: "mid" as const,
          },
        ]
      : mode === "direct_both"
        ? [
            {
              mode: "ok" as const,
              direction: "left" as const,
              label: t("hosts.topology.sshOk"),
              slot: "top" as const,
            },
            {
              mode: "ok" as const,
              direction: "right" as const,
              label: t("hosts.topology.pushOk"),
              slot: "mid" as const,
            },
          ]
        : [
            {
              mode: "ok" as const,
              direction: "left" as const,
              label: t("hosts.topology.sshOk"),
              slot: "top" as const,
            },
            {
              mode: "blocked" as const,
              direction: "none" as const,
              label: t("hosts.topology.noEgress"),
              slot: "mid" as const,
            },
            {
              mode: "tunnel" as const,
              direction: "right" as const,
              label: t("hosts.topology.tunnelBack"),
              slot: "bottom" as const,
            },
          ];
  return (
    <TopologyDiagram
      label={t(`hosts.scenario.titleOf.${mode}`)}
      layout="pair"
      links={links}
      nodes={[
        { label: t("hosts.topology.host") },
        { label: t("hosts.topology.argus"), kind: "argus" },
      ]}
    />
  );
}

function hostModeReference(mode: HostOnboardingMode, t: Translate) {
  if (mode === "direct_both") return t("hosts.scenario.refCase1");
  if (mode === "direct_in") return t("hosts.scenario.refCase2");
  if (mode === "self_enrolled") return t("hosts.scenario.refCase5");
  if (mode === "bastion_member") return t("hosts.scenario.refStandard");
  return t("hosts.scenario.refCase3");
}

function hostModeGrid(mode: HostOnboardingMode, t: Translate) {
  const self = mode === "self_enrolled";
  const member = mode === "bastion_member" || mode === "bastion_tunnel_member";
  return [
    {
      label: t("hosts.wizard.modeGrid.mode"),
      value: self
        ? t("hosts.wizard.modeGrid.modeSelf")
        : member
          ? t("hosts.wizard.modeGrid.modeBastion")
          : t("hosts.wizard.modeGrid.modeDirect"),
      tone: "info" as const,
    },
    {
      label: t("hosts.wizard.modeGrid.install"),
      value: self
        ? t("hosts.wizard.modeGrid.installSelf")
        : member
          ? t("hosts.wizard.modeGrid.installConnector")
          : t("hosts.wizard.modeGrid.installExecutor"),
    },
    {
      label: t("hosts.wizard.modeGrid.telemetry"),
      value:
        mode === "direct_in"
          ? t("hosts.wizard.modeGrid.telemetryExecutorTunnel")
          : mode === "bastion_tunnel_member"
            ? t("hosts.wizard.modeGrid.telemetryBastionTunnel")
            : member
              ? t("hosts.wizard.modeGrid.telemetryGateway")
              : t("hosts.wizard.modeGrid.telemetryDirect"),
    },
    {
      label: t("hosts.wizard.modeGrid.terminal"),
      value: self
        ? t("hosts.wizard.modeGrid.terminalNo")
        : t("hosts.wizard.modeGrid.terminalYes"),
    },
    {
      label: t("hosts.wizard.modeGrid.liveness"),
      value: self
        ? t("hosts.wizard.modeGrid.livenessSeen")
        : t("hosts.wizard.modeGrid.livenessProbe"),
    },
    {
      label: t("hosts.wizard.modeGrid.egress"),
      value:
        mode === "direct_both"
          ? t("hosts.wizard.modeGrid.egressBoth")
          : mode === "direct_in"
            ? t("hosts.wizard.modeGrid.egressInOnly")
            : self
              ? t("hosts.wizard.modeGrid.egressSelf")
              : t("hosts.wizard.modeGrid.egressBastion"),
      tone: "warn" as const,
    },
  ];
}

function HostFooter({
  phase,
  valid,
  busy,
  t,
  close,
  back,
  next,
  previewBack,
}: {
  phase: OnboardingWizardState<HostOnboardingMode>["phase"];
  valid: boolean;
  busy: boolean;
  t: Translate;
  close: (next: boolean) => void;
  back: () => void;
  next: () => void;
  previewBack: () => void;
}) {
  return (
    <>
      <span className="argus-dialog__footer-hint">
        {t("hosts.wizard.footerHint")}
      </span>
      {phase === "select_mode" && (
        <Button onClick={() => close(false)} variant="secondary">
          {t("common.cancel")}
        </Button>
      )}
      {phase === "select_mode" && (
        <Button onClick={next} variant="primary">
          {t("hosts.wizard.next")}
        </Button>
      )}
      {phase === "details" && (
        <Button disabled={busy} onClick={back} variant="secondary">
          {t("hosts.wizard.back")}
        </Button>
      )}
      {phase === "details" && (
        <Button
          disabled={!valid}
          form={HOST_FORM_ID}
          loading={busy}
          type="submit"
          variant="primary"
        >
          {t("hosts.wizard.next")}
        </Button>
      )}
      {(phase === "verify" || phase === "confirm_command") && (
        <Button onClick={previewBack} variant="secondary">
          {t("hosts.wizard.back")}
        </Button>
      )}
      {(phase === "command_result" || phase === "completed") && (
        <Button onClick={() => close(false)} variant="primary">
          {phase === "command_result"
            ? t("hosts.wizard.commandSaved")
            : t("common.close")}
        </Button>
      )}
    </>
  );
}
