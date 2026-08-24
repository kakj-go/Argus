import { describe, expect, it } from "vitest";
import {
  apiErrorField,
  formatApiError,
  presentApiFormError,
} from "./error-presentation";
import { ApiError } from "./transport/errors";

describe("API error presentation", () => {
  it("uses only the public server message and request reference", () => {
    const error = new ApiError(
      {
        code: "INVALID_ARGUMENT",
        message_key: "errors.common.invalid_argument",
        message: "The submitted value is invalid.",
        request_id: "request-public-error",
        retryable: false,
      },
      400,
    );
    expect(
      formatApiError(
        error,
        "Fallback",
        (requestId) => `Request ID: ${requestId}`,
      ),
    ).toBe("The submitted value is invalid. Request ID: request-public-error");
  });

  it("does not expose arbitrary Error messages", () => {
    expect(
      formatApiError(
        new Error("postgres://secret@internal/database"),
        "Operation failed",
        (requestId) => requestId,
      ),
    ).toBe("Operation failed");
  });

  it("returns only a safe field identifier", () => {
    const error = new ApiError(
      {
        code: "INVALID_ARGUMENT",
        message_key: "errors.common.invalid_argument",
        params: { field: "super_admin.email", rule: "format" },
        request_id: "request-field-error",
        retryable: false,
      },
      400,
    );
    expect(apiErrorField(error)).toBe("super_admin.email");
    error.params!.field = "password\ninternal stack";
    expect(apiErrorField(error)).toBeUndefined();
  });

  it("maps only allowlisted API fields to form fields", () => {
    const error = new ApiError(
      {
        code: "INVALID_ARGUMENT",
        message_key: "errors.common.invalid_argument",
        message: "The reason is too short.",
        params: { field: "reason", rule: "minLength", min_length: 8 },
        request_id: "request-form-field",
        retryable: false,
      },
      400,
    );
    const fieldErrors: Array<[string, string]> = [];
    const formErrors: string[] = [];

    expect(
      presentApiFormError(error, {
        fallback: "Save failed",
        fieldMap: { reason: "breakGlassReason" },
        requestReference: (requestId) => `Request ID: ${requestId}`,
        setFieldError: (field, message) => fieldErrors.push([field, message]),
        setFormError: (message) => formErrors.push(message),
      }),
    ).toBe("field");
    expect(fieldErrors).toEqual([
      [
        "breakGlassReason",
        "The reason is too short. Request ID: request-form-field",
      ],
    ]);
    expect(formErrors).toEqual([]);
  });

  it("uses the form summary for unknown or unsafe fields", () => {
    const error = new ApiError(
      {
        code: "INVALID_ARGUMENT",
        message_key: "errors.common.invalid_argument",
        params: { field: "internal_secret" },
        request_id: "request-form-summary",
        retryable: false,
      },
      400,
    );
    const fieldErrors: string[] = [];
    const formErrors: string[] = [];

    expect(
      presentApiFormError(error, {
        fallback: "Save failed",
        fieldMap: { public_name: "name" },
        requestReference: (requestId) => `Request ID: ${requestId}`,
        setFieldError: (field) => fieldErrors.push(field),
        setFormError: (message) => formErrors.push(message),
      }),
    ).toBe("form");
    expect(fieldErrors).toEqual([]);
    expect(formErrors).toEqual([
      "Save failed Request ID: request-form-summary",
    ]);
  });
});
