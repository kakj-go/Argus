import type {
  AIModel as AIModelContract,
  AIModelTestResult,
  Conversation as ConversationContract,
  ConversationEvent,
  ConversationEventPage,
  ConversationPage,
  ModelQuota as ModelQuotaContract,
  ModelUsage,
  Run,
} from "../../generated/contracts";
import { ClientOperationUnavailableError } from "../../transport/errors";
import type {
  AIModel,
  ModelCompatibilityResult,
  ModelQuota,
  ModelUsageSummary,
} from "../../types";
import { page, type RealDomainContext } from "./context";

function modelCompatibility(
  value: AIModelTestResult,
): ModelCompatibilityResult {
  const passed = (name: AIModelTestResult["checks"][number]["name"]) =>
    value.checks.some(
      (check) => check.name === name && check.status === "passed",
    );
  return {
    openAICompatible: passed("basic"),
    streaming: passed("streaming"),
    toolCalling: passed("tool_calling"),
    structuredOutput: passed("structured_output"),
    testedAt: value.model?.last_tested_at ?? new Date(0).toISOString(),
    diagnostics: value.checks.flatMap((check) =>
      check.status === "failed" ? [check.error_code ?? check.name] : [],
    ),
  };
}

function modelView(value: AIModelContract): AIModel {
  return {
    id: value.id,
    enterpriseId: "",
    name: value.name,
    baseUrl: value.base_url,
    modelId: value.model_id,
    apiProtocol: value.api_protocol,
    contextWindowTokens: value.context_window_tokens,
    maxOutputTokens: value.max_output_tokens,
    credentialRef: "write-only",
    inputPricePerMillionTokens: value.input_price_per_million,
    outputPricePerMillionTokens: value.output_price_per_million,
    compatibility: {
      openAICompatible: value.health_status === "healthy",
      streaming: value.health_status === "healthy",
      toolCalling: value.capabilities?.supports_tool_calling ?? false,
      structuredOutput: value.capabilities?.supports_structured_output ?? false,
      testedAt: value.last_tested_at ?? value.updated_at,
      diagnostics: [],
    },
    healthStatus:
      value.health_status === "healthy"
        ? "healthy"
        : value.health_status === "unhealthy"
          ? "unreachable"
          : "degraded",
    enabled: value.status === "enabled",
    revision: value.revision,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
  };
}

function quotaView(value: ModelQuotaContract): ModelQuota {
  return {
    id: value.id,
    enterpriseId: "",
    modelId: value.model_id,
    subjectType: value.subject_type,
    subjectId: value.subject_id,
    monthlyAmount: value.monthly_amount,
    updatedAt: value.updated_at,
  };
}

export function installAgentDomains(context: RealDomainContext): void {
  const { client, http, sse, remember, expectedVersion, idempotencyKey } =
    context;

  client.conversations = {
    async list(query) {
      const params = new URLSearchParams();
      if (query?.page?.cursor) params.set("cursor", query.page.cursor);
      if (query?.page?.limit) params.set("limit", String(query.page.limit));
      const value = await http.request<ConversationPage>(
        `conversations${params.size ? `?${params}` : ""}`,
      );
      value.items.forEach(remember);
      return page({ items: value.items, page: value.page });
    },
    async get(id) {
      return remember(
        await http.request<ConversationContract>(`conversations/${id}`),
      );
    },
    async create(input) {
      const selectedModelId = input?.selected_model_id;
      if (!selectedModelId) {
        throw new ClientOperationUnavailableError(
          "conversations.create.selected_model_id",
        );
      }
      return remember(
        await http.request<ConversationContract>("conversations", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { title: input?.title, selected_model_id: selectedModelId },
        }),
      );
    },
    async archive(id) {
      return remember(
        await http.request<ConversationContract>(`conversations/${id}`, {
          method: "PUT",
          csrf: true,
          body: { status: "archived", expected_version: expectedVersion(id) },
        }),
      );
    },
    async listEvents(id) {
      const value = await http.request<ConversationEventPage>(
        `conversations/${id}/ledger?limit=200`,
      );
      return value.items;
    },
    async *sendMessage(id, input, streamOptions) {
      const accepted = await http.request<{
        event: ConversationEvent;
        run: { run_id: string };
      }>(`conversations/${id}/messages`, {
        method: "POST",
        csrf: true,
        headers: { "Idempotency-Key": idempotencyKey() },
        body: input,
      });
      yield* sse.stream(`conversations/${id}/events`, {
        signal: streamOptions?.signal,
        last_event_id:
          streamOptions?.last_event_id ?? String(accepted.event.sequence),
      });
    },
    async updateModel(id, modelId) {
      return remember(
        await http.request<ConversationContract>(`conversations/${id}`, {
          method: "PUT",
          csrf: true,
          body: {
            selected_model_id: modelId,
            expected_version: expectedVersion(id),
          },
        }),
      );
    },
    subscribe(id, listener) {
      let stopped = false;
      let sequence = 0;
      const poll = async () => {
        if (stopped) return;
        try {
          const value = await http.request<ConversationEventPage>(
            `conversations/${id}/ledger?cursor=${sequence}&limit=200`,
          );
          for (const event of value.items) {
            if (event.sequence <= sequence) continue;
            sequence = event.sequence;
            listener(event);
          }
        } finally {
          if (!stopped) globalThis.setTimeout(() => void poll(), 1000);
        }
      };
      void poll();
      return () => {
        stopped = true;
      };
    },
  };

  client.runs = {
    get: (runId) => http.request<Run>(`runs/${runId}`),
    cancel: async (runId) => {
      const value = await http.request<{ run: Run }>(`runs/${runId}/cancel`, {
        method: "POST",
        csrf: true,
        headers: { "Idempotency-Key": idempotencyKey() },
      });
      return value.run;
    },
    compact: async (runId) => {
      const value = await http.request<{ run: Run }>(`runs/${runId}/compact`, {
        method: "POST",
        csrf: true,
        headers: { "Idempotency-Key": idempotencyKey() },
      });
      return value.run;
    },
  };

  client.models = {
    async list() {
      const value = await http.request<{
        items: AIModelContract[];
        page: { next_cursor: string | null; has_more: boolean };
      }>("enterprise/ai-models?limit=100");
      return value.items.map((item) => modelView(remember(item)));
    },
    async get(id) {
      return modelView(
        remember(
          await http.request<AIModelContract>(`enterprise/ai-models/${id}`),
        ),
      );
    },
    async testAndCreate(input) {
      const value = await http.request<AIModelTestResult>(
        "enterprise/ai-models/test-and-create",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            name: input.name,
            base_url: input.baseUrl,
            model_id: input.modelId,
            api_protocol: input.apiProtocol,
            api_key: input.apiKey,
            context_window_tokens: input.contextWindowTokens,
            max_output_tokens: input.maxOutputTokens,
            input_price_per_million: input.inputPricePerMillionTokens,
            output_price_per_million: input.outputPricePerMillionTokens,
          },
        },
      );
      const compatibility = modelCompatibility(value);
      return {
        created: value.compatible && Boolean(value.model),
        ...(value.model ? { model: modelView(remember(value.model)) } : {}),
        compatibility,
      };
    },
    async update(id, patch) {
      return modelView(
        remember(
          await http.request<AIModelContract>(`enterprise/ai-models/${id}`, {
            method: "PUT",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
            body: {
              name: patch.name,
              base_url: patch.baseUrl,
              model_id: patch.modelId,
              api_protocol: patch.apiProtocol,
              api_key: patch.apiKey,
              context_window_tokens: patch.contextWindowTokens,
              max_output_tokens: patch.maxOutputTokens,
              input_price_per_million: patch.inputPricePerMillionTokens,
              output_price_per_million: patch.outputPricePerMillionTokens,
              status:
                patch.enabled === undefined
                  ? undefined
                  : patch.enabled
                    ? "enabled"
                    : "disabled",
              expected_version: expectedVersion(id),
            },
          }),
        ),
      );
    },
    async delete(id) {
      await client.models.update(id, { enabled: false });
    },
    async test(id) {
      return modelCompatibility(
        await http.request<AIModelTestResult>(
          `enterprise/ai-models/${id}/test`,
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
          },
        ),
      );
    },
    async listAvailability() {
      return (await client.models.list()).map((item) => ({
        modelId: item.id,
        available:
          item.enabled &&
          item.healthStatus === "healthy" &&
          item.compatibility.toolCalling &&
          item.compatibility.structuredOutput,
        ...(!item.enabled
          ? { reason: "disabled" as const }
          : item.healthStatus !== "healthy"
            ? { reason: "unhealthy" as const }
            : !item.compatibility.toolCalling ||
                !item.compatibility.structuredOutput
              ? { reason: "compatibility_failed" as const }
              : {}),
      }));
    },
    async listQuotas(modelId) {
      const values = await http.request<ModelQuotaContract[]>(
        "enterprise/model-quotas",
      );
      values.forEach(remember);
      return values
        .filter((item) => !modelId || item.model_id === modelId)
        .map(quotaView);
    },
    async setQuota(input) {
      if (input.monthlyAmount === undefined) return null;
      const current = (await client.models.listQuotas(input.modelId)).find(
        (item) =>
          item.subjectType === input.subjectType &&
          item.subjectId === input.subjectId,
      );
      const value = await http.request<ModelQuotaContract>(
        "enterprise/model-quotas",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            model_id: input.modelId,
            subject_type: input.subjectType,
            subject_id: input.subjectId,
            monthly_amount: input.monthlyAmount,
            expected_version: current ? expectedVersion(current.id) : undefined,
          },
        },
      );
      remember(value);
      return quotaView(value);
    },
    async usage(range): Promise<ModelUsageSummary> {
      const month =
        range?.from?.slice(0, 7) ?? new Date().toISOString().slice(0, 7);
      const params = new URLSearchParams({ month });
      if (range?.modelId) params.set("model_id", range.modelId);
      const values = await http.request<ModelUsage[]>(
        `enterprise/model-usage?${params}`,
      );
      return {
        from: `${month}-01`,
        to: `${month}-31`,
        modelId: range?.modelId,
        totalInputTokens: values.reduce(
          (sum, item) => sum + item.input_tokens,
          0,
        ),
        totalOutputTokens: values.reduce(
          (sum, item) => sum + item.output_tokens,
          0,
        ),
        totalRequests: values.reduce(
          (sum, item) => sum + item.request_count,
          0,
        ),
        successRate: values.length > 0 ? 1 : 0,
        totalAmount: values.reduce((sum, item) => sum + item.amount, 0),
        avgLatencyMs: 0,
        errorCount: 0,
        toolCallingFailures: 0,
        structuredOutputFailures: 0,
        points: [],
      };
    },
  };

}
