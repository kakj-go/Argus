import type {
  CardBindingInvokeResult,
  CardPresentation,
  CardValidationRun,
  CardVersion,
  CardVersionSummary,
  InteractiveCard,
  ToolSchemaCatalog,
} from "../../generated/contracts";
import type { RealDomainContext } from "./context";

export function installCardDomains(ctx: RealDomainContext): void {
  const { client, http, remember, idempotencyKey } = ctx;
  client.interactiveCards = {
    async list() {
      const value = await http.request<{ items: InteractiveCard[] }>(
        "enterprise/interactive-cards",
      );
      value.items.forEach(remember);
      return value.items;
    },
    async get(id) {
      return remember(
        await http.request<InteractiveCard>(
          `enterprise/interactive-cards/${id}`,
        ),
      );
    },
    async listVersions(id) {
      const value = await http.request<{ items: CardVersionSummary[] }>(
        `enterprise/interactive-cards/${id}/versions`,
      );
      return value.items;
    },
    getVersion: (id, revision) =>
      http.request<CardVersion>(
        `enterprise/interactive-cards/${id}/versions/${revision}`,
      ),
    createConfigurationVersion: (id, input) =>
      http.request<CardVersion>(
        `enterprise/interactive-cards/${id}/versions`,
        mutation(input, idempotencyKey()),
      ),
    startValidation: (id, input) =>
      http.request<CardValidationRun>(
        `enterprise/interactive-cards/${id}/validation-runs`,
        mutation(input, idempotencyKey()),
      ),
    submitValidationEvidence: (runId, input) =>
      http.request<CardValidationRun>(
        `enterprise/card-validation-runs/${runId}/evidence`,
        mutation(input, idempotencyKey()),
      ),
    async changeState(id, action, input) {
      return remember(
        await http.request<InteractiveCard>(
          `enterprise/interactive-cards/${id}/${action}`,
          mutation(input, idempotencyKey()),
        ),
      );
    },
    listToolSchemas: () =>
      http.request<ToolSchemaCatalog>("enterprise/tool-schema-catalog"),
    createPresentation: (cardInstanceId, input) =>
      http.request<CardPresentation>(
        `enterprise/card-instances/${cardInstanceId}/presentations`,
        mutation(input, idempotencyKey()),
      ),
    invokeQueryBinding: (bindingId) =>
      http.request<CardBindingInvokeResult>(
        `enterprise/card-query-bindings/${bindingId}/invoke`,
        mutation(undefined, idempotencyKey()),
      ),
    invokeActionBinding: (bindingId) =>
      http.request<CardBindingInvokeResult>(
        `enterprise/card-action-bindings/${bindingId}/invoke`,
        mutation(undefined, idempotencyKey()),
      ),
  };
}

function mutation(body: unknown, key: string) {
  return {
    method: "POST",
    body,
    csrf: true,
    headers: { "Idempotency-Key": key },
  } as const;
}
