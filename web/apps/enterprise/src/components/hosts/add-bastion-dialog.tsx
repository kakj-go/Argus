import { zodResolver } from "@hookform/resolvers/zod";
import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";

import {
  formConstraint,
  formatApiError,
  formatErrorCode,
  presentApiFormError,
  useApi,
  type ActionOneTimeResult,
  type ConfirmActionResult,
  type ConnectionTest,
  type ConnectorInstallOperation,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Alert,
  Badge,
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
import { parseLabels } from "./host-utils";
import { InstallInstructionPanel } from "./install-instruction-panel";
import {
  onboardingWizardReducer,
  onboardingWizardStep,
  type OnboardingWizardState,
} from "./onboarding-wizard-state";
import { PendingActionConfirm } from "./pending-action-confirm";
import {
  connectorControlTunnelStatusLabel,
  connectorInstallEventStatusLabel,
  connectorInstallStageLabel,
  connectorInstallStatusLabel,
} from "./connector-install-presentation";

const BASTION_FORM_ID = "argus-add-bastion-form";
const ENVIRONMENTS = ["development", "staging", "production"] as const;
type InstallMode = "command" | "direct_install" | "direct_install_tunnel";

type BastionFormValues = {
  name: string;
  environment: (typeof ENVIRONMENTS)[number];
  labelsText: string;
  address: string;
  port: string;
  username: string;
  credentialId: string;
};

const initialState: OnboardingWizardState<InstallMode> = {
  phase: "select_mode",
  mode: "command",
};

export function AddBastionDialog({
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
  const [wizard, dispatch] = useReducer(
    onboardingWizardReducer<InstallMode>,
    initialState,
  );
  const [pendingMode, setPendingMode] = useState<InstallMode | null>(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [connectionTest, setConnectionTest] = useState<ConnectionTest | null>(
    null,
  );
  const [operation, setOperation] = useState<ConnectorInstallOperation | null>(
    null,
  );
  const [oneTimeResult, setOneTimeResult] =
    useState<ActionOneTimeResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const createdNotified = useRef(false);
  const constraints = { name: formConstraint("BastionPreviewCreate", "name") };

  const credentialsQuery = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
    enabled:
      open && wizard.phase !== "select_mode" && wizard.mode !== "command",
  });
  const credentials = useMemo(
    () =>
      (credentialsQuery.data ?? []).filter(
        (credential) => credential.protocol === "ssh",
      ),
    [credentialsQuery.data],
  );

  const schema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t("hosts.wizard.required"))
            .max(constraints.name.maxLength ?? 128),
          environment: z.enum(ENVIRONMENTS),
          labelsText: z.string(),
          address: z.string(),
          port: z.string(),
          username: z.string(),
          credentialId: z.string(),
        })
        .superRefine((value, context) => {
          if (wizard.mode === "command") return;
          const issue = (message: string, field: keyof BastionFormValues) =>
            context.addIssue({ code: "custom", message, path: [field] });
          if (!value.address.trim())
            issue(t("hosts.wizard.required"), "address");
          const port = Number(value.port);
          if (!Number.isInteger(port) || port < 1 || port > 65535) {
            issue(t("hosts.wizard.portInvalid"), "port");
          }
          if (!value.username.trim())
            issue(t("hosts.wizard.required"), "username");
          if (!value.credentialId)
            issue(t("hosts.wizard.required"), "credentialId");
        }),
    [constraints.name.maxLength, t, wizard.mode],
  );
  const form = useForm<BastionFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      environment: "production",
      labelsText: "",
      address: "",
      port: "22",
      username: "",
      credentialId: "",
    },
  });
  const values = form.watch();

  const reset = () => {
    form.reset();
    dispatch({ type: "reset", mode: "command" });
    setPendingMode(null);
    setPendingAction(null);
    setConnectionTest(null);
    setOperation(null);
    setOneTimeResult(null);
    setTesting(false);
    setSubmitting(false);
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
  const notifyCreated = useCallback(() => {
    if (createdNotified.current) return;
    createdNotified.current = true;
    onCreated();
  }, [onCreated]);

  useEffect(() => {
    const terminalOperationStatuses = [
      "succeeded",
      "failed",
      "expired",
      "result_unknown",
    ];
    if (
      wizard.phase !== "installing" ||
      !operation ||
      terminalOperationStatuses.includes(operation.status)
    )
      return;
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const next = await api.connectors.getInstallOperation(operation.id);
        if (cancelled) return;
        setOperation(next);
        if (next.status === "succeeded") {
          dispatch({ type: "operation_complete" });
          notifyCreated();
        }
      } catch {
        // Preserve the durable last-known timeline and retry on the next render.
      }
    }, 800);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [api.connectors, notifyCreated, operation, wizard.phase]);

  const hasDirectFields = Boolean(
    values.address.trim() ||
    values.username.trim() ||
    values.credentialId ||
    values.port !== "22",
  );
  const applyMode = (mode: InstallMode) => {
    const changingFamily = (wizard.mode === "command") !== (mode === "command");
    if (changingFamily) {
      form.setValue("address", "");
      form.setValue("port", "22");
      form.setValue("username", "");
      form.setValue("credentialId", "");
    }
    setConnectionTest(null);
    setPendingAction(null);
    dispatch({ type: "select_mode", mode });
  };
  const requestMode = (mode: InstallMode) => {
    if (mode === wizard.mode) return;
    if (wizard.mode !== "command" && mode === "command" && hasDirectFields) {
      setPendingMode(mode);
      return;
    }
    applyMode(mode);
  };

  const createConnectionTest = async (input: BastionFormValues) => {
    let result = await api.hosts.createConnectionTest({
      address: input.address,
      port: Number(input.port),
      platform: "linux",
      connection_mode: "direct_ssh",
      credential_id: input.credentialId,
      username: input.username,
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

  const preparePreview = form.handleSubmit(async (input) => {
    if (submitting || testing) return;
    form.clearErrors();
    setSubmitting(true);
    let connectionTestId: string | undefined;
    try {
      if (wizard.mode !== "command") {
        setTesting(true);
        const test = await createConnectionTest(input);
        if (test.status !== "succeeded") {
          form.setError("root", {
            type: "server",
            message: formatErrorCode(
              test.error_code,
              t("hosts.bastionForm.testFailed"),
            ),
          });
          return;
        }
        connectionTestId = test.id;
      }
      const action = await api.connectors.previewCreateBastionScope({
        name: input.name,
        environment: input.environment,
        labels: parseLabels(input.labelsText),
        install_mode: wizard.mode,
        ...(wizard.mode === "command"
          ? {}
          : {
              address: input.address,
              port: Number(input.port),
              username: input.username,
              credential_id: input.credentialId,
              connection_test_id: connectionTestId,
            }),
      });
      setPendingAction(action);
      dispatch({
        type: "next",
        terminal: wizard.mode === "command" ? "confirm_command" : "verify",
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("hosts.bastionForm.commandGenerateFailed"),
        fieldMap: {
          name: "name",
          address: "address",
          port: "port",
          username: "username",
          credential_id: "credentialId",
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
      setSubmitting(false);
    }
  });

  const committed = (result: ConfirmActionResult) => {
    setPendingAction(null);
    if (result.one_time_result) {
      setOneTimeResult(result.one_time_result);
      dispatch({ type: "commit_command" });
      notifyCreated();
      return;
    }
    const operationId = result.execution?.operation_ref?.id;
    if (operationId) {
      void api.connectors.getInstallOperation(operationId).then((next) => {
        setOperation(next);
        dispatch({ type: "commit_operation" });
        notifyCreated();
      });
      return;
    }
    dispatch({ type: "commit_complete" });
    notifyCreated();
  };

  const previewBack = async () => {
    if (pendingAction) {
      try {
        await api.approvals.cancel(pendingAction.action_ref);
      } catch {
        // An expired preview can be discarded locally.
      }
    }
    setPendingAction(null);
    setConnectionTest(null);
    dispatch({ type: "back" });
  };

  const retryOperation = async () => {
    if (!operation) return;
    try {
      setPendingAction(
        await api.connectors.previewRetryInstallOperation(operation.id),
      );
    } catch (error) {
      form.setError("root", {
        type: "server",
        message: formatApiError(
          error,
          t("hosts.bastionForm.commandGenerateFailed"),
          (requestId) => t("common.requestReference", { requestId }),
        ),
      });
    }
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
      title:
        wizard.mode === "command"
          ? t("hosts.bastionForm.generateCommand")
          : t("hosts.bastionForm.installAndEnroll"),
      description: t("hosts.wizard.step3Desc"),
    },
  ];
  const footer = renderFooter({
    phase: wizard.phase,
    mode: wizard.mode,
    formValid: Boolean(values.name.trim()),
    busy: submitting || testing,
    t,
    close,
    back: () => dispatch({ type: "back" }),
    next: () =>
      dispatch({
        type: "next",
        terminal: wizard.mode === "command" ? "confirm_command" : "verify",
      }),
    previewBack: () => void previewBack(),
  });

  return (
    <>
      <Dialog
        className="argus-dialog--wizard"
        description={t("hosts.bastionForm.dialogDesc")}
        footer={footer}
        onOpenChange={close}
        open={open}
        title={t("hosts.bastionForm.title")}
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
          data-testid="bastion-onboarding-flow"
        >
          {wizard.phase === "select_mode" && (
            <BastionModeStep mode={wizard.mode} onSelect={requestMode} t={t} />
          )}
          {wizard.phase === "details" && (
            <form
              className="argus-wizard-details"
              id={BASTION_FORM_ID}
              onSubmit={preparePreview}
            >
              <SelectedMode
                mode={wizard.mode}
                onChange={() => dispatch({ type: "change_mode" })}
                t={t}
              />
              {form.formState.errors.root?.message && (
                <Alert
                  description={form.formState.errors.root.message}
                  title={t("hosts.bastionForm.testFailed")}
                  tone="danger"
                />
              )}
              <div className="argus-scenario-wizard__form">
                <Field
                  error={form.formState.errors.name?.message}
                  label={t("hosts.bastionForm.name")}
                  requirement="required"
                >
                  <Input
                    autoFocus
                    placeholder={t("hosts.bastionForm.namePlaceholder")}
                    {...form.register("name")}
                  />
                </Field>
                <Field
                  label={t("hosts.bastionForm.environment")}
                  requirement="required"
                >
                  <Select
                    onValueChange={(value) =>
                      form.setValue(
                        "environment",
                        value as BastionFormValues["environment"],
                        { shouldValidate: true },
                      )
                    }
                    options={ENVIRONMENTS.map((value) => ({
                      value,
                      label: t(`hosts.env.${value}`),
                    }))}
                    value={values.environment}
                  />
                </Field>
                {wizard.mode !== "command" && (
                  <>
                    <Field
                      error={form.formState.errors.address?.message}
                      label={t("hosts.wizard.address")}
                      requirement="required"
                    >
                      <Input
                        placeholder={t("hosts.wizard.addressPlaceholder")}
                        {...form.register("address")}
                      />
                    </Field>
                    <Field
                      error={form.formState.errors.port?.message}
                      label={t("hosts.wizard.port")}
                      requirement="required"
                    >
                      <Input inputMode="numeric" {...form.register("port")} />
                    </Field>
                    <Field
                      error={form.formState.errors.username?.message}
                      label={t("hosts.wizard.account")}
                      requirement="required"
                    >
                      <Input
                        placeholder={t("hosts.wizard.accountPlaceholder")}
                        {...form.register("username")}
                      />
                    </Field>
                    <Field
                      error={form.formState.errors.credentialId?.message}
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
                  label={t("hosts.bastionForm.labels")}
                  requirement="optional"
                >
                  <Textarea {...form.register("labelsText")} rows={3} />
                </Field>
              </div>
            </form>
          )}
          {(wizard.phase === "verify" || wizard.phase === "confirm_command") &&
            pendingAction && (
              <div className="argus-dialog__flow">
                {connectionTest && (
                  <ConnectionTestSummary result={connectionTest} t={t} />
                )}
                <PendingActionConfirm
                  action={pendingAction}
                  claimOneTimeResult={wizard.mode === "command"}
                  onCancel={() => {
                    setPendingAction(null);
                    dispatch({ type: "back" });
                  }}
                  onDone={committed}
                />
              </div>
            )}
          {wizard.phase === "command_result" && oneTimeResult && (
            <CommandResult result={oneTimeResult} t={t} />
          )}
          {wizard.phase === "installing" && operation && (
            <OperationProgress
              onRetest={() => {
                setPendingAction(null);
                setOperation(null);
                dispatch({ type: "return_details" });
              }}
              onRetry={() => void retryOperation()}
              onRetryCancel={() => setPendingAction(null)}
              onRetryDone={committed}
              operation={operation}
              pendingAction={pendingAction}
              t={t}
            />
          )}
          {wizard.phase === "completed" && (
            <Alert
              description={t("hosts.bastionForm.installCompletedDesc")}
              title={t("hosts.bastionForm.installCompleted")}
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

function BastionModeStep({
  mode,
  onSelect,
  t,
}: {
  mode: InstallMode;
  onSelect: (mode: InstallMode) => void;
  t: Translate;
}) {
  const cards: Array<{
    mode: InstallMode;
    title: string;
    description: string;
    ref: string;
  }> = [
    {
      mode: "command",
      title: t("hosts.bastionForm.standardTitle"),
      description: t("hosts.bastionForm.standardDesc"),
      ref: t("hosts.bastionForm.commandRef"),
    },
    {
      mode: "direct_install",
      title: t("hosts.bastionForm.directTitle"),
      description: t("hosts.bastionForm.directDesc"),
      ref: t("hosts.bastionForm.directRef"),
    },
    {
      mode: "direct_install_tunnel",
      title: t("hosts.bastionForm.tunnelTitle"),
      description: t("hosts.bastionForm.tunnelDesc"),
      ref: t("hosts.bastionForm.tunnelRef"),
    },
  ];
  return (
    <div className="argus-mode-selection">
      <div className="argus-mode-selection__cards">
        {cards.map((card) => (
          <ScenarioCard
            description={card.description}
            diagram={<BastionTopology mode={card.mode} t={t} />}
            key={card.mode}
            onSelect={() => onSelect(card.mode)}
            refLabel={card.ref}
            selected={mode === card.mode}
            statusLabel={t("hosts.scenario.statusSupported")}
            title={card.title}
          />
        ))}
      </div>
      <div className="argus-mode-selection__detail">
        <h3>{t(`hosts.bastionForm.titleOf.${mode}`)}</h3>
        <p>{t(`hosts.bastionForm.summaryOf.${mode}`)}</p>
        <ModeGrid items={bastionModeGrid(mode, t)} />
      </div>
    </div>
  );
}

function BastionTopology({ mode, t }: { mode: InstallMode; t: Translate }) {
  const links =
    mode === "command"
      ? [
          {
            mode: "ok" as const,
            direction: "right" as const,
            label: t("hosts.topology.egress"),
            slot: "mid" as const,
          },
        ]
      : mode === "direct_install"
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
              label: t("hosts.topology.egress"),
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
      label={t(`hosts.bastionForm.titleOf.${mode}`)}
      layout="pair"
      links={links}
      nodes={[
        { label: t("hosts.topology.bastion"), kind: "bastion" },
        { label: t("hosts.topology.argus"), kind: "argus" },
      ]}
    />
  );
}

function bastionModeGrid(mode: InstallMode, t: Translate) {
  return [
    {
      label: t("hosts.bastionGrid.access"),
      value: t(`hosts.bastionForm.titleOf.${mode}`),
      tone: "info" as const,
    },
    {
      label: t("hosts.bastionGrid.prereq"),
      value:
        mode === "direct_install_tunnel"
          ? t("hosts.bastionForm.prereqTunnel")
          : t("hosts.bastionForm.prereq"),
    },
    {
      label: t("hosts.bastionGrid.members"),
      value: t("hosts.bastionGrid.membersValue"),
    },
    {
      label: t("hosts.bastionGrid.telemetry"),
      value: t("hosts.bastionGrid.telemetryValue"),
    },
    {
      label: t("hosts.bastionGrid.terminal"),
      value: t("hosts.bastionGrid.terminalValue"),
    },
    {
      label: t("hosts.bastionGrid.egress"),
      value:
        mode === "direct_install_tunnel"
          ? t("hosts.topology.noEgress")
          : t("hosts.bastionGrid.egressValue"),
      tone: "warn" as const,
    },
  ];
}

function SelectedMode({
  mode,
  onChange,
  t,
}: {
  mode: InstallMode;
  onChange: () => void;
  t: Translate;
}) {
  return (
    <div className="argus-selected-mode">
      <div>
        <span>{t("hosts.wizard.selectedMode")}</span>
        <strong>{t(`hosts.bastionForm.titleOf.${mode}`)}</strong>
      </div>
      <Button onClick={onChange} type="button" variant="ghost">
        {t("hosts.wizard.changeMode")}
      </Button>
    </div>
  );
}

function ConnectionTestSummary({
  result,
  t,
}: {
  result: ConnectionTest;
  t: Translate;
}) {
  return (
    <Alert
      description={
        result.status === "succeeded"
          ? t("hosts.wizard.testPassed")
          : formatErrorCode(result.error_code, t("hosts.wizard.testFailed"))
      }
      title={t("hosts.wizard.connectionTestSummary")}
      tone={result.status === "succeeded" ? "success" : "danger"}
    />
  );
}

function CommandResult({
  result,
  t,
}: {
  result: ActionOneTimeResult;
  t: Translate;
}) {
  return (
    <div className="argus-dialog__flow argus-detail-section">
      <Alert
        description={t("hosts.bastionForm.commandWarning")}
        title={t("hosts.bastionForm.commandWarningTitle")}
        tone="warning"
      />
      <InstallInstructionPanel result={result} />
      <p className="argus-muted">
        {t("hosts.wizard.commandExpires", {
          time: formatDateTime(result.expires_at),
        })}
      </p>
    </div>
  );
}

function OperationProgress({
  operation,
  pendingAction,
  onRetry,
  onRetryCancel,
  onRetryDone,
  onRetest,
  t,
}: {
  operation: ConnectorInstallOperation;
  pendingAction: PendingActionPublic | null;
  onRetry: () => void;
  onRetryCancel: () => void;
  onRetryDone: (result: ConfirmActionResult) => void;
  onRetest: () => void;
  t: Translate;
}) {
  const failed = ["failed", "expired", "result_unknown"].includes(
    operation.status,
  );
  return (
    <div className="argus-operation-progress">
      <div className="argus-operation-progress__head">
        <div>
          <h3>{t("hosts.bastionForm.installProgress")}</h3>
          <p>{t("hosts.bastionForm.installProgressHint")}</p>
        </div>
        <Badge
          tone={
            failed
              ? "danger"
              : operation.status === "succeeded"
                ? "success"
                : "info"
          }
        >
          {connectorInstallStatusLabel(t, operation.status)}
        </Badge>
      </div>
      <ol className="argus-operation-timeline">
        {operation.events.map((event) => (
          <li className={`is-${event.status}`} key={event.id}>
            <strong>{connectorInstallStageLabel(t, event.stage)}</strong>
            <span>
              {event.error_code
                ? formatErrorCode(
                    event.error_code,
                    t("hosts.pendingActions.failed"),
                  )
                : connectorInstallEventStatusLabel(t, event.status)}
            </span>
          </li>
        ))}
      </ol>
      {operation.install_mode === "direct_install_tunnel" && (
        <Alert
          description={connectorControlTunnelStatusLabel(
            t,
            operation.control_tunnel_status,
          )}
          title={t("hosts.bastionForm.controlTunnelStatus")}
          tone={
            operation.control_tunnel_status === "established"
              ? "success"
              : "info"
          }
        />
      )}
      {failed && !pendingAction && (
        <div className="argus-form-actions">
          <Button onClick={onRetry} variant="primary">
            {t("hosts.bastionForm.retryInstall")}
          </Button>
          <Button onClick={onRetest} variant="secondary">
            {t("hosts.bastionForm.returnToTest")}
          </Button>
        </div>
      )}
      {pendingAction && (
        <PendingActionConfirm
          action={pendingAction}
          onCancel={onRetryCancel}
          onDone={onRetryDone}
        />
      )}
    </div>
  );
}

function renderFooter({
  phase,
  mode,
  formValid,
  busy,
  t,
  close,
  back,
  next,
  previewBack,
}: {
  phase: OnboardingWizardState<InstallMode>["phase"];
  mode: InstallMode;
  formValid: boolean;
  busy: boolean;
  t: Translate;
  close: (next: boolean) => void;
  back: () => void;
  next: () => void;
  previewBack: () => void;
}) {
  const terminal =
    phase === "command_result" ||
    phase === "completed" ||
    phase === "installing";
  return (
    <>
      <span className="argus-dialog__footer-hint">
        {phase === "installing"
          ? t("hosts.bastionForm.backgroundInstallHint")
          : mode === "command"
            ? t("hosts.bastionForm.footerCommandHint")
            : t("hosts.bastionForm.footerInstallHint")}
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
          disabled={!formValid}
          form={BASTION_FORM_ID}
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
      {terminal && (
        <Button onClick={() => close(false)} variant="primary">
          {phase === "command_result"
            ? t("hosts.wizard.commandSaved")
            : t("common.close")}
        </Button>
      )}
    </>
  );
}
