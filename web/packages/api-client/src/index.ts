export * from "./types";
export type { ArgusApiClient } from "./client";
export { createMockApiClient } from "./mock";
export type { MockApiClient, MockOptions } from "./mock";
export { createRealAdapter } from "./adapters/real";
export type { RealAdapter } from "./adapters/real";
export { createConfiguredApiClient } from "./factory";
export type { ApiClientFactoryOptions } from "./factory";
export { HttpTransport } from "./transport/http";
export type { HttpRequestOptions, HttpTransportOptions } from "./transport/http";
export { SseTransport, decodeSse, parseSseBlock } from "./transport/sse";
export type { SseFrame, SseStreamOptions } from "./transport/sse";
export { WebSocketTransport } from "./transport/websocket";
export type {
  WebSocketCloseState,
  WebSocketTransportOptions,
} from "./transport/websocket";
export {
  ApiError,
  ClientConfigurationError,
  ClientOperationUnavailableError,
  StreamTerminatedError,
} from "./transport/errors";
export { ApiProvider, useApi } from "./react";
export type { ApiProviderProps } from "./react";
