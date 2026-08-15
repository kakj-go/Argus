import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Field, Input, StatusBadge, Switch } from "@argus/ui";
import { isValidHttpUrl, type SetupDraft } from "../lib/validation";
import type { StepProps } from "./step-token";

type Sandbox = SetupDraft["sandbox"];

/**
 * 第 3 步：OpenSandbox 基座（可选步骤，可整体跳过）。
 * [测试连接] 当前为 mock 模拟：地址合法即视为连接成功。
 */
export function StepSandbox({
  draft,
  errors,
  onSandboxChange,
}: StepProps & {
  onSandboxChange: (patch: Partial<Sandbox>) => void;
}) {
  const { t } = useTranslation();
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<"success" | "failure" | null>(
    null,
  );
  const touch = (key: string) =>
    setTouched((prev) => (prev[key] ? prev : { ...prev, [key]: true }));
  const errorFor = (key: string) => (touched[key] ? errors[key] : undefined);

  const { sandbox } = draft;
  const endpointValid = isValidHttpUrl(sandbox.endpoint.trim());

  const runTest = async () => {
    setTesting(true);
    setTestResult(null);
    // mock 模拟一次连接探测的往返延迟。
    await new Promise((resolve) => setTimeout(resolve, 800));
    setTesting(false);
    setTestResult(endpointValid ? "success" : "failure");
  };

  return (
    <div className="setup-fields">
      <p className="setup-step-intro">{t("setup.sandbox.intro")}</p>
      <div className="setup-switch-row">
        <Switch
          checked={sandbox.enabled}
          label={t("setup.sandbox.enable")}
          onChange={(enabled) => onSandboxChange({ enabled })}
        />
        <span>{t("setup.sandbox.enable")}</span>
      </div>

      {sandbox.enabled && (
        <>
          <Field
            error={errorFor("endpoint")}
            label={t("setup.sandbox.endpoint.label")}
          >
            <Input
              onBlur={() => touch("endpoint")}
              onChange={(event) => {
                onSandboxChange({ endpoint: event.target.value });
                setTestResult(null);
              }}
              placeholder={t("setup.sandbox.endpoint.placeholder")}
              value={sandbox.endpoint}
            />
          </Field>
          <Field
            error={errorFor("credential")}
            hint={t("setup.sandbox.credential.hint")}
            label={t("setup.sandbox.credential.label")}
          >
            <Input
              onBlur={() => touch("credential")}
              onChange={(event) =>
                onSandboxChange({ credential: event.target.value })
              }
              placeholder={t("setup.sandbox.credential.placeholder")}
              type="password"
              value={sandbox.credential}
            />
          </Field>
          <Field
            error={errorFor("storage")}
            label={t("setup.sandbox.storage.label")}
          >
            <Input
              onBlur={() => touch("storage")}
              onChange={(event) =>
                onSandboxChange({ storage: event.target.value })
              }
              placeholder={t("setup.sandbox.storage.placeholder")}
              value={sandbox.storage}
            />
          </Field>
          <div className="setup-test-row">
            <Button
              disabled={!endpointValid}
              loading={testing}
              onClick={runTest}
              variant="secondary"
            >
              {t("setup.sandbox.test.button")}
            </Button>
            {testResult === "success" && (
              <StatusBadge tone="success">
                {t("setup.sandbox.test.success")}
              </StatusBadge>
            )}
            {testResult === "failure" && (
              <StatusBadge tone="danger">
                {t("setup.sandbox.test.failure")}
              </StatusBadge>
            )}
          </div>
        </>
      )}
    </div>
  );
}
