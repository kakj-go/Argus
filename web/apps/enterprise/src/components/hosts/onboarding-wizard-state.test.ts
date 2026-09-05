import { describe, expect, it } from "vitest";

import {
  cleanHostModeSpecificDraft,
  hostModeSwitchLosesFields,
  onboardingWizardReducer,
  type HostModeSpecificDraft,
  type OnboardingWizardState,
} from "./onboarding-wizard-state";

describe("onboardingWizardReducer", () => {
  it("follows command and operation result routes", () => {
    let command: OnboardingWizardState<"command"> = { phase: "select_mode", mode: "command" };
    command = onboardingWizardReducer(command, { type: "next", terminal: "confirm_command" });
    command = onboardingWizardReducer(command, { type: "next", terminal: "confirm_command" });
    command = onboardingWizardReducer(command, { type: "commit_command" });
    expect(command.phase).toBe("command_result");

    let direct: OnboardingWizardState<"direct"> = { phase: "select_mode", mode: "direct" };
    direct = onboardingWizardReducer(direct, { type: "next", terminal: "verify" });
    direct = onboardingWizardReducer(direct, { type: "next", terminal: "verify" });
    direct = onboardingWizardReducer(direct, { type: "commit_operation" });
    direct = onboardingWizardReducer(direct, { type: "operation_complete" });
    expect(direct.phase).toBe("completed");
  });

  it("ignores illegal transitions", () => {
    const state: OnboardingWizardState<"command"> = { phase: "select_mode", mode: "command" };
    expect(onboardingWizardReducer(state, { type: "commit_command" })).toBe(state);
    expect(onboardingWizardReducer(state, { type: "operation_complete" })).toBe(state);
  });
});

describe("host mode field cleanup", () => {
  const draft: HostModeSpecificDraft = {
    address: "10.0.0.8",
    port: "22",
    protocol: "ssh",
    platform: "linux",
    architecture: "arm64",
    account: "ops",
    credentialId: "credential-1",
    scopeId: "scope-1",
  };

  it("preserves compatible direct mode fields", () => {
    expect(cleanHostModeSpecificDraft(draft, "direct_both", "direct_in")).toBe(draft);
    expect(hostModeSwitchLosesFields(draft, "direct_both", "direct_in")).toBe(false);
  });

  it("clears incompatible fields when switching to self enrollment", () => {
    expect(hostModeSwitchLosesFields(draft, "bastion_member", "self_enrolled")).toBe(true);
    expect(cleanHostModeSpecificDraft(draft, "bastion_member", "self_enrolled")).toMatchObject({
      address: "",
      credentialId: "",
      scopeId: "",
      architecture: "amd64",
    });
  });
});
