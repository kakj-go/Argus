import type {
  SandboxBackend as SandboxBackendContract,
  SandboxImage as SandboxImageContract,
  SandboxProfile as SandboxProfileContract,
  SandboxQuota,
  SandboxSession,
  SandboxUsage,
} from "../../generated/contracts";
import { ClientOperationUnavailableError } from "../../transport/errors";
import type {
  EnterpriseSandboxQuota,
  SandboxBackend,
  SandboxImage,
  SandboxProfile,
  SandboxSessionMeta,
} from "../../types";
import type { RealDomainContext } from "./context";

function backendView(value: SandboxBackendContract): SandboxBackend {
  return {
    id: value.id,
    name: value.name,
    endpoint: value.endpoint,
    credentialRef: "write-only",
    tlsVerify: true,
    enabled: value.status === "enabled",
    defaultStorage: "",
    healthStatus:
      value.health_status === "healthy"
        ? "healthy"
        : value.health_status === "unhealthy"
          ? "unreachable"
          : "degraded",
    createdAt: value.created_at,
  };
}

function imageView(value: SandboxImageContract): SandboxImage {
  return {
    id: value.id,
    name: value.name,
    reference: value.image_ref,
    digest: value.digest,
    languages: [],
    scanStatus: "passed",
    signatureStatus: "verified",
    enabled: value.status === "enabled",
    createdAt: value.created_at,
  };
}

function profileView(value: SandboxProfileContract): SandboxProfile {
  return {
    id: value.id,
    name: value.name,
    description: value.task_kinds.join(", "),
    imageId: value.image_id,
    resources: {
      cpu: value.cpu_millis / 1000,
      memoryMb: value.memory_mib,
      diskMb: 0,
      pids: 0,
    },
    timeouts: {
      commandSeconds: value.timeout_seconds,
      idleSeconds: value.timeout_seconds,
      lifetimeSeconds: value.timeout_seconds,
    },
    network: {
      mode: value.network_mode === "none" ? "deny_all" : "allow_list",
      allowedDomains: [],
    },
    capabilities: {
      fileUpload: false,
      artifactDownload: false,
      secretInjection: false,
      gpu: false,
    },
    builtin: false,
    enabled: value.status === "enabled",
    createdAt: value.created_at,
  };
}

function quotaView(value: SandboxQuota): EnterpriseSandboxQuota {
  return {
    enterpriseId: value.enterprise_id,
    allowedProfiles: [],
    maxConcurrentSessions: value.max_concurrent_sessions,
    maxDailySessionMinutes: Math.floor(value.monthly_session_seconds / 60),
    maxDailyCpuMinutes: 0,
    maxArtifactStorageMb: 0,
    artifactRetentionDays: 0,
  };
}

function sessionView(value: SandboxSession): SandboxSessionMeta {
  const status: SandboxSessionMeta["status"] =
    value.status === "creating"
      ? "starting"
      : value.status === "unknown"
        ? "idle"
        : value.status;
  return {
    id: value.id,
    enterpriseId: value.enterprise_id,
    userId: "service",
    profileId: value.profile_id,
    status,
    startedAt: value.created_at,
    lastActivityAt: value.updated_at,
    ...(value.status === "terminated"
      ? { terminatedAt: value.updated_at }
      : {}),
  };
}

export function installSandboxDomains(context: RealDomainContext): void {
  const { client, http, versions, remember, idempotencyKey } = context;
  const backends = new Map<string, SandboxBackendContract>();
  const images = new Map<string, SandboxImageContract>();
  const profiles = new Map<string, SandboxProfileContract>();

  client.platform.sandboxBackends = {
    async list() {
      const values = await http.request<SandboxBackendContract[]>(
        "platform/sandbox/backends",
      );
      return values.map((value) => {
        backends.set(value.id, value);
        remember(value);
        return backendView(value);
      });
    },
    async create(input) {
      const value = await http.request<SandboxBackendContract>(
        "platform/sandbox/backends",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            name: input.name,
            endpoint: input.endpoint,
            api_key: input.credentialRef,
            status: "enabled",
            expected_version: 0,
          },
        },
      );
      backends.set(value.id, value);
      remember(value);
      return backendView(value);
    },
    async update(id, patch) {
      let current = backends.get(id);
      if (!current) {
        await client.platform.sandboxBackends.list();
        current = backends.get(id);
      }
      if (!current)
        throw new ClientOperationUnavailableError("sandbox.backend");
      const value = await http.request<SandboxBackendContract>(
        `platform/sandbox/backends/${id}`,
        {
          method: "PUT",
          csrf: true,
          body: {
            name: patch.name ?? current.name,
            endpoint: patch.endpoint ?? current.endpoint,
            ...(patch.credentialRef && patch.credentialRef !== "write-only"
              ? { api_key: patch.credentialRef }
              : {}),
            status:
              patch.enabled === undefined
                ? current.status
                : patch.enabled
                  ? "enabled"
                  : "disabled",
            expected_version: current.version,
          },
        },
      );
      backends.set(value.id, value);
      remember(value);
      return backendView(value);
    },
    async test(id) {
      const started = performance.now();
      const value = await http.request<SandboxBackendContract>(
        `platform/sandbox/backends/${id}/test`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      );
      backends.set(value.id, value);
      remember(value);
      const success = value.health_status === "healthy";
      return {
        success,
        latencyMs: Math.max(0, Math.round(performance.now() - started)),
        checks: [
          {
            name: "lifecycle_api",
            status: success ? ("passed" as const) : ("failed" as const),
            detail: value.endpoint,
          },
        ],
      };
    },
  };

  client.platform.images = {
    async list() {
      const values = await http.request<SandboxImageContract[]>(
        "platform/sandbox/images",
      );
      return values.map((value) => {
        images.set(value.id, value);
        remember(value);
        return imageView(value);
      });
    },
    async create(input) {
      if (backends.size === 0) await client.platform.sandboxBackends.list();
      const backend = [...backends.values()].find(
        (item) => item.status === "enabled",
      );
      if (!backend) {
        throw new ClientOperationUnavailableError("sandbox.image.backend");
      }
      const value = await http.request<SandboxImageContract>(
        "platform/sandbox/images",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            backend_id: backend.id,
            name: input.name,
            image_ref: input.reference,
            digest: input.digest,
            status: "disabled",
            expected_version: 0,
          },
        },
      );
      images.set(value.id, value);
      remember(value);
      return imageView(value);
    },
    async setEnabled(id, enabled) {
      let current = images.get(id);
      if (!current) {
        await client.platform.images.list();
        current = images.get(id);
      }
      if (!current) throw new ClientOperationUnavailableError("sandbox.image");
      const value = await http.request<SandboxImageContract>(
        `platform/sandbox/images/${id}`,
        {
          method: "PUT",
          csrf: true,
          body: {
            backend_id: current.backend_id,
            name: current.name,
            image_ref: current.image_ref,
            digest: current.digest,
            status: enabled ? "enabled" : "disabled",
            expected_version: current.version,
          },
        },
      );
      images.set(value.id, value);
      remember(value);
      return imageView(value);
    },
  };

  client.platform.profiles = {
    async list() {
      const values = await http.request<SandboxProfileContract[]>(
        "platform/sandbox/profiles",
      );
      return values.map((value) => {
        profiles.set(value.id, value);
        remember(value);
        return profileView(value);
      });
    },
    async create(input) {
      if (images.size === 0) await client.platform.images.list();
      const image = images.get(input.imageId);
      if (!image) {
        throw new ClientOperationUnavailableError("sandbox.profile.image");
      }
      const value = await http.request<SandboxProfileContract>(
        "platform/sandbox/profiles",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            name: input.name,
            backend_id: image.backend_id,
            image_id: input.imageId,
            task_kinds: ["attachment_processing"],
            cpu_millis: Math.round(input.resources.cpu * 1000),
            memory_mib: input.resources.memoryMb,
            timeout_seconds: input.timeouts.lifetimeSeconds,
            network_mode:
              input.network.mode === "deny_all" ? "none" : "restricted",
            status: "enabled",
            expected_version: 0,
          },
        },
      );
      profiles.set(value.id, value);
      remember(value);
      return profileView(value);
    },
    async update(id, patch) {
      let current = profiles.get(id);
      if (!current) {
        await client.platform.profiles.list();
        current = profiles.get(id);
      }
      if (!current)
        throw new ClientOperationUnavailableError("sandbox.profile");
      const imageId = patch.imageId ?? current.image_id;
      if (images.size === 0) await client.platform.images.list();
      const image = images.get(imageId);
      if (!image) {
        throw new ClientOperationUnavailableError("sandbox.profile.image");
      }
      const value = await http.request<SandboxProfileContract>(
        `platform/sandbox/profiles/${id}`,
        {
          method: "PUT",
          csrf: true,
          body: {
            name: patch.name ?? current.name,
            backend_id: image.backend_id,
            image_id: imageId,
            task_kinds: current.task_kinds,
            cpu_millis:
              patch.resources?.cpu === undefined
                ? current.cpu_millis
                : Math.round(patch.resources.cpu * 1000),
            memory_mib: patch.resources?.memoryMb ?? current.memory_mib,
            timeout_seconds:
              patch.timeouts?.lifetimeSeconds ?? current.timeout_seconds,
            network_mode: patch.network
              ? patch.network.mode === "deny_all"
                ? "none"
                : "restricted"
              : current.network_mode,
            status:
              patch.enabled === undefined
                ? current.status
                : patch.enabled
                  ? "enabled"
                  : "disabled",
            expected_version: current.version,
          },
        },
      );
      profiles.set(value.id, value);
      remember(value);
      return profileView(value);
    },
  };

  client.platform.quotas = {
    async get(enterpriseId) {
      const value = await http.request<SandboxQuota>(
        `platform/sandbox/enterprise-quotas/${enterpriseId}`,
      );
      versions.set(`sandbox-quota:${enterpriseId}`, value.version);
      return quotaView(value);
    },
    async update(enterpriseId, patch) {
      const current = await client.platform.quotas.get(enterpriseId);
      const value = await http.request<SandboxQuota>(
        `platform/sandbox/enterprise-quotas/${enterpriseId}`,
        {
          method: "PUT",
          csrf: true,
          body: {
            max_concurrent_sessions:
              patch.maxConcurrentSessions ?? current.maxConcurrentSessions,
            monthly_session_seconds:
              (patch.maxDailySessionMinutes ?? current.maxDailySessionMinutes) *
              60,
            expected_version:
              versions.get(`sandbox-quota:${enterpriseId}`) ?? 1,
          },
        },
      );
      versions.set(`sandbox-quota:${enterpriseId}`, value.version);
      return quotaView(value);
    },
  };

  client.platform.sessions = {
    async list(filter) {
      const values = await http.request<SandboxSession[]>(
        "platform/sandbox/sessions",
      );
      return values
        .filter(
          (value) =>
            (!filter?.enterpriseId ||
              value.enterprise_id === filter.enterpriseId) &&
            (!filter?.status?.length ||
              filter.status.includes(sessionView(value).status)),
        )
        .map(sessionView);
    },
    async terminate(id) {
      return sessionView(
        await http.request<SandboxSession>(
          `platform/sandbox/sessions/${id}/terminate`,
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
          },
        ),
      );
    },
  };
  client.platform.usage = {
    list: () => http.request<SandboxUsage[]>("platform/sandbox/usage"),
  };
}
