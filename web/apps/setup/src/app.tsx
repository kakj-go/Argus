import { useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useApi } from "@argus/api-client";
import {
  Alert,
  AppearanceControls,
  Badge,
  Button,
  Card,
  CardContent,
  Wizard,
  type WizardStep,
} from "@argus/ui";
import { StepReview } from "./components/step-review";
import { StepSandbox } from "./components/step-sandbox";
import { StepSystem } from "./components/step-system";
import { StepToken } from "./components/step-token";
import { InitializedTerminal, SuccessTerminal } from "./components/terminal";
import {
  createInitialDraft,
  toSubmission,
  validateSandbox,
  validateSystem,
  validateToken,
  type SetupDraft,
} from "./lib/validation";

/**
 * 一次性系统初始化向导（docs/07 §2-4）。
 * 仅平台 `uninitialized` 状态可进入；提交为单事务，成功后向导永久关闭。
 */
export default function App() {
  const { t } = useTranslation();
  const api = useApi();

  const statusQuery = useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.setup.status(),
    retry: 1,
    refetchOnWindowFocus: false,
  });

  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<SetupDraft>(createInitialDraft);
  const [succeeded, setSucceeded] = useState(false);

  const submitMutation = useMutation({
    mutationFn: () => api.setup.submit(toSubmission(draft)),
    onSuccess: () => setSucceeded(true),
  });

  const tokenError = validateToken(draft, t);
  const systemErrors = useMemo(() => validateSystem(draft, t), [draft, t]);
  const sandboxErrors = useMemo(() => validateSandbox(draft, t), [draft, t]);

  const canNext =
    step === 0
      ? tokenError === null
      : step === 1
        ? Object.keys(systemErrors).length === 0
        : step === 2
          ? Object.keys(sandboxErrors).length === 0
          : !submitMutation.isPending;

  const updateDraft = (patch: Partial<SetupDraft>) =>
    setDraft((prev) => ({ ...prev, ...patch }));
  const updateAdmin = (patch: Partial<SetupDraft["admin"]>) =>
    setDraft((prev) => ({ ...prev, admin: { ...prev.admin, ...patch } }));
  const updateSandbox = (patch: Partial<SetupDraft["sandbox"]>) =>
    setDraft((prev) => ({ ...prev, sandbox: { ...prev.sandbox, ...patch } }));

  const steps: WizardStep[] = [
    {
      id: "token",
      title: t("setup.steps.token.title"),
      description: t("setup.steps.token.description"),
    },
    {
      id: "system",
      title: t("setup.steps.system.title"),
      description: t("setup.steps.system.description"),
    },
    {
      id: "sandbox",
      title: t("setup.steps.sandbox.title"),
      description: t("setup.steps.sandbox.description"),
      optional: true,
    },
    {
      id: "review",
      title: t("setup.steps.review.title"),
      description: t("setup.steps.review.description"),
    },
  ];

  let content: ReactNode;
  if (statusQuery.isPending) {
    content = (
      <Card className="setup-card">
        <CardContent>
          <div className="setup-loading">
            <LoaderCircle aria-hidden className="argus-spin" size={20} />
            <span>{t("setup.loading")}</span>
          </div>
        </CardContent>
      </Card>
    );
  } else if (statusQuery.isError) {
    content = (
      <Card className="setup-card">
        <CardContent>
          <div className="setup-fields">
            <Alert
              description={t("setup.statusError.description")}
              title={t("setup.statusError.title")}
              tone="danger"
            />
            <Button onClick={() => statusQuery.refetch()} variant="secondary">
              {t("setup.statusError.retry")}
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  } else if (succeeded) {
    content = <SuccessTerminal />;
  } else if (statusQuery.data.state !== "uninitialized") {
    // 已初始化（或初始化中）：向导永久关闭，不再渲染任何表单。
    content = <InitializedTerminal />;
  } else {
    content = (
      <Card className="setup-card">
        <CardContent>
          <Wizard
            canNext={canNext}
            current={step}
            onBack={() => setStep((prev) => Math.max(0, prev - 1))}
            onNext={() =>
              setStep((prev) => Math.min(steps.length - 1, prev + 1))
            }
            onSkip={() => {
              updateSandbox({
                enabled: false,
                endpoint: "",
                credential: "",
                storage: "",
              });
              setStep((prev) => Math.min(steps.length - 1, prev + 1));
            }}
            onSubmit={() => submitMutation.mutate()}
            steps={steps}
            submitLabel={t("setup.review.submit")}
            submitting={submitMutation.isPending}
          >
            {step === 0 && (
              <StepToken
                draft={draft}
                errors={tokenError ? { setupToken: tokenError } : {}}
                onChange={updateDraft}
              />
            )}
            {step === 1 && (
              <StepSystem
                draft={draft}
                errors={systemErrors}
                onAdminChange={updateAdmin}
                onChange={updateDraft}
              />
            )}
            {step === 2 && (
              <StepSandbox
                draft={draft}
                errors={sandboxErrors}
                onSandboxChange={updateSandbox}
              />
            )}
            {step === 3 && (
              <div className="setup-fields">
                <StepReview draft={draft} />
                {submitMutation.isError && (
                  <Alert
                    description={submitMutation.error.message}
                    title={t("setup.review.errorTitle")}
                    tone="danger"
                  />
                )}
              </div>
            )}
          </Wizard>
        </CardContent>
      </Card>
    );
  }

  return (
    <main className="setup-shell">
      <div className="setup-appearance">
        <AppearanceControls />
      </div>
      <header className="setup-header">
        <div className="setup-brand">◉</div>
        <Badge tone="accent">{t("setup.badge")}</Badge>
        <h1>{t("setup.title")}</h1>
        <p>{t("setup.subtitle")}</p>
      </header>
      {content}
    </main>
  );
}
