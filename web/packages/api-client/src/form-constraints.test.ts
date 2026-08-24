import { describe, expect, it } from "vitest";
import {
  formConstraint,
  formValueConstraint,
} from "./generated/form-constraints";

describe("generated form constraints", () => {
  it("keeps setup email optional", () => {
    expect(formConstraint("SetupSuperAdminInput", "email")).toMatchObject({
      format: "email",
      required: false,
    });
  });

  it("keeps AI model create and update requirements distinct", () => {
    expect(formConstraint("AIModelTestCreate", "api_key").required).toBe(true);
    expect(formConstraint("AIModelUpdate", "api_key").required).toBe(false);
    expect(
      formConstraint("AIModelTestCreate", "context_window_tokens").minimum,
    ).toBe(8192);
  });

  it("generates scalar label constraints from the shared schema", () => {
    expect(formValueConstraint("UserLabelKey")).toMatchObject({
      maxLength: 63,
      minLength: 1,
      pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$",
    });
    expect(formValueConstraint("LabelValue")).toMatchObject({
      maxLength: 63,
      minLength: 1,
    });
  });
});
