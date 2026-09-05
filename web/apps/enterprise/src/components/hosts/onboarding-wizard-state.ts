export type OnboardingWizardPhase =
  | "select_mode"
  | "details"
  | "verify"
  | "confirm_command"
  | "installing"
  | "command_result"
  | "completed";

export type OnboardingWizardState<Mode extends string> = {
  phase: OnboardingWizardPhase;
  mode: Mode;
};

export type OnboardingWizardAction<Mode extends string> =
  | { type: "select_mode"; mode: Mode }
  | { type: "next"; terminal: "verify" | "confirm_command" }
  | { type: "back" }
  | { type: "change_mode" }
  | { type: "commit_command" }
  | { type: "commit_operation" }
  | { type: "commit_complete" }
  | { type: "operation_complete" }
  | { type: "return_details" }
  | { type: "reset"; mode: Mode };

/** Shared host-module state machine. Business modes and fields remain outside @argus/ui. */
export function onboardingWizardReducer<Mode extends string>(
  state: OnboardingWizardState<Mode>,
  action: OnboardingWizardAction<Mode>,
): OnboardingWizardState<Mode> {
  switch (action.type) {
    case "select_mode":
      return state.phase === "select_mode" ? { ...state, mode: action.mode } : state;
    case "next":
      if (state.phase === "select_mode") return { ...state, phase: "details" };
      if (state.phase === "details") return { ...state, phase: action.terminal };
      return state;
    case "back":
      if (state.phase === "details") return { ...state, phase: "select_mode" };
      if (state.phase === "verify" || state.phase === "confirm_command") {
        return { ...state, phase: "details" };
      }
      return state;
    case "change_mode":
      return state.phase === "details" || state.phase === "verify" || state.phase === "confirm_command"
        ? { ...state, phase: "select_mode" }
        : state;
    case "commit_command":
      return state.phase === "confirm_command" ? { ...state, phase: "command_result" } : state;
    case "commit_operation":
      return state.phase === "verify" ? { ...state, phase: "installing" } : state;
    case "commit_complete":
      return state.phase === "verify" ? { ...state, phase: "completed" } : state;
    case "operation_complete":
      return state.phase === "installing" ? { ...state, phase: "completed" } : state;
    case "return_details":
      return state.phase === "installing" ? { ...state, phase: "details" } : state;
    case "reset":
      return { phase: "select_mode", mode: action.mode };
  }
}

export function onboardingWizardStep(phase: OnboardingWizardPhase): number {
  if (phase === "select_mode") return 0;
  if (phase === "details") return 1;
  return 2;
}

export type HostOnboardingMode =
  | "direct_both"
  | "direct_in"
  | "self_enrolled"
  | "bastion_member"
  | "bastion_tunnel_member";

export type HostModeSpecificDraft = {
  address: string;
  port: string;
  protocol: "ssh" | "winrm";
  platform: "linux" | "windows";
  architecture: "amd64" | "arm64";
  account: string;
  credentialId: string;
  scopeId: string;
};

function hostModeFamily(mode: HostOnboardingMode) {
  if (mode === "self_enrolled") return "self";
  if (mode === "direct_both" || mode === "direct_in") return "direct";
  return "bastion";
}

export function hostModeSwitchLosesFields(
  draft: HostModeSpecificDraft,
  from: HostOnboardingMode,
  to: HostOnboardingMode,
) {
  if (hostModeFamily(from) === hostModeFamily(to)) return false;
  if (from === "self_enrolled") return draft.architecture !== "amd64";
  return Boolean(
    draft.address.trim() ||
      draft.account.trim() ||
      draft.credentialId ||
      draft.scopeId ||
      draft.port !== "22" ||
      draft.protocol !== "ssh" ||
      draft.platform !== "linux",
  );
}

export function cleanHostModeSpecificDraft(
  draft: HostModeSpecificDraft,
  from: HostOnboardingMode,
  to: HostOnboardingMode,
): HostModeSpecificDraft {
  if (hostModeFamily(from) === hostModeFamily(to)) return draft;
  if (to === "self_enrolled") {
    return {
      address: "",
      port: "22",
      protocol: "ssh",
      platform: "linux",
      architecture: "amd64",
      account: "",
      credentialId: "",
      scopeId: "",
    };
  }
  return {
    address: "",
    port: "22",
    protocol: "ssh",
    platform: "linux",
    architecture: "amd64",
    account: "",
    credentialId: "",
    scopeId: "",
  };
}
