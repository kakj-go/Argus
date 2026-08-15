/** Platform-side aggregate used by the SaaS OpenSandbox usage dashboard. */
export interface SandboxUsagePoint {
  date: string;
  sessions: number;
  sessionMinutes: number;
  cpuMinutes: number;
  artifactStorageMb: number;
}
