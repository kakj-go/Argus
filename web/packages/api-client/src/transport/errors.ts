import type {
  ApiError as ApiErrorBody,
  PasswordChangeChallenge,
  MfaChallenge,
} from "../generated/contracts";

export class ApiError extends Error {
  readonly code: string;
  readonly message_key: string;
  readonly params?: Record<string, string | number | boolean>;
  readonly request_id: string;
  readonly trace_id?: string;
  readonly retryable: boolean;
  readonly status: number;

  constructor(body: ApiErrorBody, status: number) {
    super(body.message ?? body.message_key);
    this.name = "ApiError";
    this.code = body.code;
    this.message_key = body.message_key;
    this.params = body.params;
    this.request_id = body.request_id;
    this.trace_id = body.trace_id;
    this.retryable = body.retryable;
    this.status = status;
  }
}

export class ClientConfigurationError extends Error {
  readonly code = "CLIENT_CONFIGURATION_ERROR";

  constructor(message: string) {
    super(message);
    this.name = "ClientConfigurationError";
  }
}

export class ClientOperationUnavailableError extends Error {
  readonly code = "CLIENT_OPERATION_UNAVAILABLE";

  constructor(operation: string) {
    super(`Operation is not available in the real adapter: ${operation}`);
    this.name = "ClientOperationUnavailableError";
  }
}

export class PasswordChangeRequiredError extends Error {
  readonly code = "PASSWORD_CHANGE_REQUIRED";

  constructor(readonly challenge: PasswordChangeChallenge) {
    super("A password change is required before the session can be created");
    this.name = "PasswordChangeRequiredError";
  }
}

export class MfaRequiredError extends Error {
  readonly code = "MFA_REQUIRED";

  constructor(readonly challenge: MfaChallenge) {
    super("MFA proof is required before the session can be created");
    this.name = "MfaRequiredError";
  }
}

export class StreamTerminatedError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "StreamTerminatedError";
  }
}
