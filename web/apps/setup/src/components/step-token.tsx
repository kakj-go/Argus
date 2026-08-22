import { useEffect, useState } from "react";
import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Check, Copy } from "lucide-react";
import { Button, Field, Input } from "@argus/ui";
import type { SetupDraft } from "../lib/validation";

type ManagedTokenState = "loading" | "available" | "manual";

/** 第 1 步：验证 Setup Token。 */
export function StepToken() {
  const { t } = useTranslation();
  const {
    register,
    setValue,
    watch,
    formState: { errors },
  } = useFormContext<SetupDraft>();
  const [managedState, setManagedState] = useState<ManagedTokenState>("loading");
  const [copied, setCopied] = useState(false);
  const setupToken = watch("setupToken");

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/__argus/setup-token", {
      cache: "no-store",
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (response.status === 404) return null;
        if (!response.ok) throw new Error(`setup token handoff failed: ${response.status}`);
        const token = (await response.text()).trim();
        if (token.length < 8) throw new Error("setup token handoff returned an invalid token");
        return token;
      })
      .then((token) => {
        if (token === null) {
          setManagedState("manual");
          return;
        }
        setValue("setupToken", token, { shouldValidate: true });
        setManagedState("available");
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return;
        setManagedState("manual");
      });
    return () => controller.abort();
  }, [setValue]);

  const copyToken = async () => {
    await navigator.clipboard.writeText(setupToken);
    setCopied(true);
  };

  const hint =
    managedState === "available"
      ? t("setup.token.managedHint")
      : managedState === "loading"
        ? t("setup.token.loading")
        : t("setup.token.hint");

  return (
    <div className="argus-setup-fields">
      <Field
        error={errors.setupToken?.message}
        hint={hint}
        label={t("setup.token.label")}
      >
        <div className="argus-setup-token-control">
          <Input
            {...register("setupToken")}
            autoFocus={managedState === "manual"}
            disabled={managedState === "loading"}
            placeholder={
              managedState === "loading"
                ? t("setup.token.loading")
                : t("setup.token.placeholder")
            }
            readOnly={managedState === "available"}
            type="password"
          />
          {managedState === "available" && (
            <Button
              aria-label={copied ? t("setup.token.copied") : t("setup.token.copy")}
              onClick={() => void copyToken()}
              size="icon"
              title={copied ? t("setup.token.copied") : t("setup.token.copy")}
              type="button"
              variant="secondary"
            >
              {copied ? <Check size={16} /> : <Copy size={16} />}
            </Button>
          )}
        </div>
      </Field>
    </div>
  );
}
