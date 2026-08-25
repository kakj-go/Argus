import { ApiError } from "./transport/errors";

export type ApiErrorTranslationParams = Record<
  string,
  string | number | boolean
>;

export type ApiErrorTranslator = (
  code: string,
  messageKey: string,
  params?: ApiErrorTranslationParams,
) => string | undefined;

let apiErrorTranslator: ApiErrorTranslator | undefined;

export function setApiErrorTranslator(translator: ApiErrorTranslator | undefined) {
  apiErrorTranslator = translator;
}

export interface ApiErrorPresentation {
  code: string;
  messageKey: string;
  params?: Record<string, string | number | boolean>;
  publicMessage?: string;
  requestId?: string;
  retryable: boolean;
  status: number;
}

export interface ApiFormErrorOptions<TField extends string> {
  fallback: string;
  fieldMap: Partial<Record<string, TField>>;
  requestReference: (requestId: string) => string;
  setFieldError: (field: TField, message: string) => void;
  setFormError: (message: string) => void;
}

export function apiErrorPresentation(
  error: unknown,
): ApiErrorPresentation | null {
  if (!(error instanceof ApiError)) return null;
  return {
    code: error.code,
    messageKey: error.message_key,
    params: error.params,
    publicMessage: error.public_message,
    requestId: error.request_id === "unknown" ? undefined : error.request_id,
    retryable: error.retryable,
    status: error.status,
  };
}

export function formatApiError(
  error: unknown,
  fallback: string,
  requestReference: (requestId: string) => string,
): string {
  const presentation = apiErrorPresentation(error);
  if (!presentation) return fallback;
  const message =
    presentation.publicMessage ??
    apiErrorTranslator?.(
      presentation.code,
      presentation.messageKey,
      presentation.params,
    ) ??
    fallback;
  return presentation.requestId
    ? `${message} ${requestReference(presentation.requestId)}`
    : message;
}

export function formatErrorCode(
  code: string | undefined,
  fallback = "Operation failed",
): string {
  if (!code) return fallback;
  return apiErrorTranslator?.(code, `errors.codes.${code}`) ?? fallback;
}

export function apiErrorField(error: unknown): string | undefined {
  const field = apiErrorPresentation(error)?.params?.field;
  return typeof field === "string" && /^[A-Za-z0-9_.-]+$/.test(field)
    ? field
    : undefined;
}

export function presentApiFormError<TField extends string>(
  error: unknown,
  options: ApiFormErrorOptions<TField>,
): "field" | "form" {
  const message = formatApiError(
    error,
    options.fallback,
    options.requestReference,
  );
  const apiField = apiErrorField(error);
  const formField = apiField ? options.fieldMap[apiField] : undefined;
  if (formField) {
    options.setFieldError(formField, message);
    return "field";
  }
  options.setFormError(message);
  return "form";
}
