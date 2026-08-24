import { useEffect, useId, useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { formConstraint, formatApiError, useApi } from "@argus/api-client";
import { Alert, Button, Dialog, Field, Input } from "@argus/ui";

const codeConstraint = formConstraint("MfaCodeRequest", "code");

export function MfaStepUpDialog({
  open,
  onOpenChange,
  onComplete,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete: () => void | Promise<void>;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const formId = useId();
  const [error, setError] = useState<string>();
  const schema = useMemo(
    () =>
      z.object({
        code: z
          .string()
          .trim()
          .min(
            codeConstraint.minLength ?? 1,
            t("remoteAccess.stepUp.codeRequired"),
          )
          .max(
            codeConstraint.maxLength ?? 128,
            t("remoteAccess.stepUp.codeInvalid"),
          ),
      }),
    [t],
  );
  type StepUpForm = z.infer<typeof schema>;
  const form = useForm<StepUpForm>({
    resolver: zodResolver(schema),
    defaultValues: { code: "" },
  });

  useEffect(() => {
    if (!open) {
      form.reset();
      setError(undefined);
    }
  }, [form, open]);

  const submit = form.handleSubmit(async ({ code }) => {
    setError(undefined);
    try {
      await api.auth.stepUp({ code });
    } catch (value) {
      setError(
        formatApiError(value, t("remoteAccess.stepUp.invalid"), (requestId) =>
          t("common.requestReference", { requestId }),
        ),
      );
      return;
    }
    onOpenChange(false);
    void onComplete();
  });

  return (
    <Dialog
      description={t("remoteAccess.stepUp.description")}
      footer={
        <>
          <Button
            disabled={form.formState.isSubmitting}
            onClick={() => onOpenChange(false)}
            type="button"
            variant="secondary"
          >
            {t("remoteAccess.stepUp.cancel")}
          </Button>
          <Button
            form={formId}
            loading={form.formState.isSubmitting}
            type="submit"
            variant="primary"
          >
            {t("remoteAccess.stepUp.submit")}
          </Button>
        </>
      }
      onOpenChange={(nextOpen) => {
        if (!form.formState.isSubmitting) onOpenChange(nextOpen);
      }}
      open={open}
      title={t("remoteAccess.stepUp.title")}
    >
      <form id={formId} onSubmit={submit}>
        {error && (
          <Alert
            description={error}
            title={t("remoteAccess.stepUp.failed")}
            tone="danger"
          />
        )}
        <Field
          error={form.formState.errors.code?.message}
          label={t("account.mfa.proof")}
          requirement="required"
        >
          <Input
            autoComplete="one-time-code"
            autoFocus
            inputMode="numeric"
            {...form.register("code")}
          />
        </Field>
      </form>
    </Dialog>
  );
}
