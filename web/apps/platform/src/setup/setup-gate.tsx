import { useEffect, useMemo, useState, type ReactNode } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { FormProvider, useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import {
  ApiError,
  apiErrorRequestId,
  passwordPolicyRuleFromError,
  useApi,
} from "@argus/api-client";
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
import { usePlatformAuthStore } from "@argus/auth";
import { StepReview } from "./step-review";
import { StepSystem } from "./step-system";
import { StepToken } from "./step-token";
import {
  createInitialDraft,
  createSetupSchemas,
  toSubmission,
  type SetupDraft,
} from "./validation";

/**
 * 一次性系统初始化向导（docs/07 §2-4）。
 * 仅平台 `uninitialized` 状态可进入；提交为单事务，成功后向导永久关闭。
 */
export function PlatformSetupGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const statusQuery = useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.setup.status(),
    retry: 1,
    refetchOnWindowFocus: false,
  });

  const [step, setStep] = useState(0);
  const schemas = useMemo(() => createSetupSchemas(t), [t]);
  const form = useForm<SetupDraft>({
    resolver: zodResolver(schemas.setup),
    defaultValues: createInitialDraft(),
    mode: "onTouched",
  });
  const draft = useWatch({ control: form.control }) as SetupDraft;

  const submitMutation = useMutation({
    mutationFn: (input: SetupDraft) => api.setup.submit(toSubmission(input)),
    onSuccess: (_result, input) => {
      window.history.replaceState(null, "", "/login");
      queryClient.setQueryData(["setup", "status"], {
        state: "initialized",
        platformName: input.platformName.trim(),
      });
    },
  });

  const submitErrorDescription = (() => {
    if (!(submitMutation.error instanceof ApiError)) {
      return t("setup.review.genericError");
    }
    let description: string;
    if (submitMutation.error.code === "SETUP_TOKEN_INVALID") {
      description = t("setup.review.tokenInvalid");
    } else {
      const passwordRule = passwordPolicyRuleFromError(submitMutation.error);
      description = passwordRule
        ? t(`setup.system.password.${passwordRule}`)
        : t("setup.review.genericError");
    }
    const requestId = apiErrorRequestId(submitMutation.error);
    return requestId
      ? `${description} ${t("setup.review.requestReference", { requestId })}`
      : description;
  })();

  const canNext =
    step === 0
      ? schemas.token.safeParse(draft).success
      : step === 1
        ? schemas.system.safeParse(draft).success
        : !submitMutation.isPending;

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
      id: "review",
      title: t("setup.steps.review.title"),
      description: t("setup.steps.review.description"),
    },
  ];

  if (statusQuery.data?.state === "initialized") {
    return <InitializedPlatformEntry>{children}</InitializedPlatformEntry>;
  }

  let content: ReactNode;
  if (statusQuery.isPending) {
    content = (
      <Card className="argus-setup-card">
        <CardContent>
          <div className="argus-setup-loading">
            <LoaderCircle aria-hidden className="argus-spin" size={20} />
            <span>{t("setup.loading")}</span>
          </div>
        </CardContent>
      </Card>
    );
  } else if (statusQuery.isError) {
    content = (
      <Card className="argus-setup-card">
        <CardContent>
          <div className="argus-setup-fields">
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
  } else if (statusQuery.data.state !== "uninitialized") {
    content = (
      <Card className="argus-setup-card">
        <CardContent>
          <div className="argus-setup-loading">
            <LoaderCircle aria-hidden className="argus-spin" size={20} />
            <span>{t("setup.initializing")}</span>
          </div>
        </CardContent>
      </Card>
    );
  } else {
    content = (
      <Card className="argus-setup-card">
        <CardContent>
          <FormProvider {...form}>
            <Wizard
              canNext={canNext}
              current={step}
              onBack={() => setStep((prev) => Math.max(0, prev - 1))}
              onNext={() =>
                setStep((prev) => Math.min(steps.length - 1, prev + 1))
              }
              onSubmit={form.handleSubmit((values) =>
                submitMutation.mutate(values),
              )}
              steps={steps}
              submitLabel={t("setup.review.submit")}
              submitting={submitMutation.isPending}
            >
              {step === 0 && <StepToken />}
              {step === 1 && <StepSystem />}
              {step === 2 && (
                <div className="argus-setup-fields">
                  <StepReview draft={draft} />
                  {submitMutation.isError && (
                    <Alert
                      description={submitErrorDescription}
                      title={t("setup.review.errorTitle")}
                      tone="danger"
                    />
                  )}
                </div>
              )}
            </Wizard>
          </FormProvider>
        </CardContent>
      </Card>
    );
  }

  return (
    <main className="argus-setup-shell">
      <div className="argus-setup-appearance">
        <AppearanceControls />
      </div>
      <header className="argus-setup-header">
        <div className="argus-setup-brand">◉</div>
        <Badge tone="accent">{t("setup.badge")}</Badge>
        <h1>{t("setup.title")}</h1>
        <p>{t("setup.subtitle")}</p>
      </header>
      {content}
    </main>
  );
}

function InitializedPlatformEntry({ children }: { children: ReactNode }) {
  const session = usePlatformAuthStore.getState().session;
  const shouldOpenLogin = !session && window.location.pathname !== "/login";
  const [ready, setReady] = useState(!shouldOpenLogin);

  useEffect(() => {
    if (!shouldOpenLogin) return;
    window.history.replaceState(null, "", "/login");
    setReady(true);
  }, [shouldOpenLogin]);

  if (!ready) {
    return (
      <main className="argus-auth-state">
        <LoaderCircle aria-hidden className="argus-spin" size={20} />
      </main>
    );
  }
  return <>{children}</>;
}
