export interface ArgusRuntimeConfig {
  cardOrigin?: string;
  platformLoginUrl?: string;
}

let config: ArgusRuntimeConfig = {};

/**
 * Deployment-time portal configuration served by the cluster at
 * /argus-runtime.json (rendered by Helm from the install hosts). Build-time
 * VITE_* values stay as dev/mock overrides; when both are absent the caller
 * decides whether to fail closed.
 */
export async function loadRuntimeConfig(): Promise<ArgusRuntimeConfig> {
  try {
    const response = await fetch("/argus-runtime.json", { cache: "no-store" });
    if (response.ok) {
      config = (await response.json()) as ArgusRuntimeConfig;
    }
  } catch {
    // Vite dev servers and mock mode serve no runtime config file.
  }
  return config;
}

export function runtimeConfig(): ArgusRuntimeConfig {
  return config;
}
