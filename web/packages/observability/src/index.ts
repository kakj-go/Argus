export type FrontendTelemetryContext = {
  enterpriseId?: string;
  route: string;
  runId?: string;
  toolCallId?: string;
};

export function reportUiEvent(name: string, context: FrontendTelemetryContext) {
  if (
    typeof window !== "undefined" &&
    window.location.hostname === "localhost"
  ) {
    console.debug("[argus-ui]", name, context);
  }
}
