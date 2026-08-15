import { createContext, useContext, type ReactNode } from "react";
import type { ArgusApiClient } from "./client";

const ApiContext = createContext<ArgusApiClient | null>(null);

export interface ApiProviderProps {
  client: ArgusApiClient;
  children: ReactNode;
}

/** Injects the API client (real or mock) into the component tree. */
export function ApiProvider({ client, children }: ApiProviderProps) {
  return <ApiContext.Provider value={client}>{children}</ApiContext.Provider>;
}

/** Returns the API client provided by the nearest ApiProvider. */
export function useApi(): ArgusApiClient {
  const client = useContext(ApiContext);
  if (!client) {
    throw new Error("useApi must be used within an ApiProvider");
  }
  return client;
}
