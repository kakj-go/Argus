import type { ISODateString } from "./common";

export interface ModelCompatibilityResult {
  openAICompatible: boolean;
  streaming: boolean;
  toolCalling: boolean;
  structuredOutput: boolean;
  testedAt: ISODateString;
  diagnostics: string[];
}

export interface AIModel {
  id: string;
  enterpriseId: string;
  name: string;
  baseUrl: string;
  modelId: string;
  credentialRef: string;
  inputPricePerMillionTokens: number;
  outputPricePerMillionTokens: number;
  compatibility: ModelCompatibilityResult;
  healthStatus: "healthy" | "degraded" | "unreachable";
  enabled: boolean;
  revision: number;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface TestAndCreateAIModelInput {
  name: string;
  baseUrl: string;
  apiKey: string;
  modelId: string;
  inputPricePerMillionTokens: number;
  outputPricePerMillionTokens: number;
}

export interface TestAndCreateAIModelResult {
  created: boolean;
  model?: AIModel;
  compatibility: ModelCompatibilityResult;
}

export interface UpdateAIModelInput {
  name?: string;
  baseUrl?: string;
  apiKey?: string;
  modelId?: string;
  inputPricePerMillionTokens?: number;
  outputPricePerMillionTokens?: number;
  enabled?: boolean;
}

export interface ModelQuota {
  id: string;
  enterpriseId: string;
  modelId: string;
  subjectType: "department" | "user";
  subjectId: string;
  monthlyAmount: number;
  updatedAt: ISODateString;
}

export interface ModelAvailability {
  modelId: string;
  available: boolean;
  reason?: "disabled" | "unhealthy" | "compatibility_failed" | "department_quota_exhausted" | "user_quota_exhausted";
  departmentRemaining?: number;
  userRemaining?: number;
}

export interface ModelUsagePoint {
  date: string;
  modelId: string;
  departmentId: string;
  userId: string;
  inputTokens: number;
  outputTokens: number;
  requestCount: number;
  successCount: number;
  errorCount: number;
  toolCallingFailures: number;
  structuredOutputFailures: number;
  avgLatencyMs: number;
  inputPricePerMillionSnapshot: number;
  outputPricePerMillionSnapshot: number;
  amount: number;
}

export interface ModelUsageSummary {
  from: string;
  to: string;
  modelId?: string;
  totalInputTokens: number;
  totalOutputTokens: number;
  totalRequests: number;
  successRate: number;
  totalAmount: number;
  avgLatencyMs: number;
  errorCount: number;
  toolCallingFailures: number;
  structuredOutputFailures: number;
  points: ModelUsagePoint[];
}

export interface UsageRange {
  from?: string;
  to?: string;
  modelId?: string;
}

export function calculateModelAmount(
  inputTokens: number,
  outputTokens: number,
  inputPricePerMillionSnapshot: number,
  outputPricePerMillionSnapshot: number,
): number {
  return (
    (inputTokens / 1_000_000) * inputPricePerMillionSnapshot +
    (outputTokens / 1_000_000) * outputPricePerMillionSnapshot
  );
}
