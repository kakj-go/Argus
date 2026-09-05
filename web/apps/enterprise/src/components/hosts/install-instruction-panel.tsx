import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { ActionOneTimeResult } from "@argus/api-client";
import { Alert, CodeBlock, Tabs, TabsList, TabsTrigger } from "@argus/ui";

export function InstallInstructionPanel({
  result,
}: {
  result: ActionOneTimeResult;
}) {
  const { t } = useTranslation();
  // The OpenAPI contract requires arrays, but keep the render boundary safe
  // against a stale server payload so one malformed response cannot unmount
  // the whole hosts page.
  const instructionSets = Array.isArray(result.instruction_sets)
    ? result.instruction_sets
    : [];
  const [requestedScope, setRequestedScope] = useState(
    instructionSets[0]?.scope ?? "linux-system",
  );
  const instruction =
    instructionSets.find((item) => item.scope === requestedScope) ??
    instructionSets[0];

  if (!instruction) {
    return (
      <Alert
        description={t("hosts.wizard.installInstructions.missingDescription")}
        title={t("hosts.wizard.installInstructions.missing")}
        tone="danger"
      />
    );
  }

  return (
    <div className="argus-dialog__flow argus-detail-section">
      {instructionSets.length > 1 && (
        <Tabs
          onValueChange={(value) =>
            setRequestedScope(value as typeof requestedScope)
          }
          value={instruction.scope}
        >
          <TabsList>
            {instructionSets.map((item) => (
              <TabsTrigger key={item.scope} value={item.scope}>
                {t(`hosts.wizard.installInstructions.scope.${item.scope}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}

      <Alert
        description={t(
          instruction.download_tls_mode === "insecure-first-fetch"
            ? "hosts.wizard.installInstructions.insecureFirstFetch"
            : "hosts.wizard.installInstructions.downloadRisk",
        )}
        title={t(
          instruction.download_tls_mode === "insecure-first-fetch"
            ? "hosts.wizard.installInstructions.insecureFirstFetchTitle"
            : "hosts.wizard.installInstructions.downloadRiskTitle",
        )}
        tone="warning"
      />
      <CodeBlock code={instruction.command} language="bash" />
      <p className="argus-muted">
        {t("hosts.wizard.installInstructions.bundle", {
          epoch: instruction.trust_bundle_epoch,
          sha: instruction.trust_bundle_sha256,
        })}
      </p>
      <p className="argus-muted">
        {t("hosts.wizard.installInstructions.installer", {
          sha: instruction.installer_sha256,
        })}
      </p>
      {(Array.isArray(instruction.capability_warnings)
        ? instruction.capability_warnings
        : []
      ).map((warning) => (
        <Alert
          description={warning}
          key={warning}
          title={t("hosts.wizard.installInstructions.capabilityWarning")}
          tone="warning"
        />
      ))}
    </div>
  );
}
