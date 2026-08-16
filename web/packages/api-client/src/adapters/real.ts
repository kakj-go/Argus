import type { ArgusApiClient } from "../client";
import { ClientOperationUnavailableError } from "../transport/errors";
import { HttpTransport, type HttpTransportOptions } from "../transport/http";
import { SseTransport } from "../transport/sse";
import { WebSocketTransport } from "../transport/websocket";

function unavailable(operation: string): Promise<never> {
  return Promise.reject(new ClientOperationUnavailableError(operation));
}

function unavailableSync(operation: string): never {
  throw new ClientOperationUnavailableError(operation);
}

function createUnavailableClient(): ArgusApiClient {
  return {
    auth: {
      login: () => unavailable("auth.login"),
      logout: () => unavailable("auth.logout"),
      me: () => unavailable("auth.me"),
    },
    conversations: {
      list: () => unavailable("conversations.list"),
      get: () => unavailable("conversations.get"),
      create: () => unavailable("conversations.create"),
      archive: () => unavailable("conversations.archive"),
      listEvents: () => unavailable("conversations.listEvents"),
      sendMessage: () => unavailableSync("conversations.sendMessage"),
      updateModel: () => unavailable("conversations.updateModel"),
      subscribe: () => unavailableSync("conversations.subscribe"),
    },
    hosts: {
      list: () => unavailable("hosts.list"),
      get: () => unavailable("hosts.get"),
      previewCreate: () => unavailable("hosts.previewCreate"),
      update: () => unavailable("hosts.update"),
      delete: () => unavailable("hosts.delete"),
      testConnection: () => unavailable("hosts.testConnection"),
      getCollector: () => unavailable("hosts.getCollector"),
      previewCollectorInstall: () => unavailable("hosts.previewCollectorInstall"),
      listSessions: () => unavailable("hosts.listSessions"),
      createSession: () => unavailable("hosts.createSession"),
      getSession: () => unavailable("hosts.getSession"),
      terminateSession: () => unavailable("hosts.terminateSession"),
    },
    connectors: {
      list: () => unavailable("connectors.list"),
      get: () => unavailable("connectors.get"),
      listBastionScopes: () => unavailable("connectors.listBastionScopes"),
      getBastionScope: () => unavailable("connectors.getBastionScope"),
      createBastionScope: () => unavailable("connectors.createBastionScope"),
      updateBastionScope: () => unavailable("connectors.updateBastionScope"),
      regenerateEnrollmentToken: () => unavailable("connectors.regenerateEnrollmentToken"),
      createUninstallCommand: () => unavailable("connectors.createUninstallCommand"),
      deleteBastionScope: () => unavailable("connectors.deleteBastionScope"),
      rotateCertificate: () => unavailable("connectors.rotateCertificate"),
    },
    kubernetes: {
      listClusters: () => unavailable("kubernetes.listClusters"),
      getCluster: () => unavailable("kubernetes.getCluster"),
      previewCreateCluster: () => unavailable("kubernetes.previewCreateCluster"),
      updateCluster: () => unavailable("kubernetes.updateCluster"),
      deleteCluster: () => unavailable("kubernetes.deleteCluster"),
      testClusterConnection: () => unavailable("kubernetes.testClusterConnection"),
      listWorkloads: () => unavailable("kubernetes.listWorkloads"),
      listNodeBindings: () => unavailable("kubernetes.listNodeBindings"),
      verifyNodeBinding: () => unavailable("kubernetes.verifyNodeBinding"),
      listCollectionClaims: () => unavailable("kubernetes.listCollectionClaims"),
      getCollector: () => unavailable("kubernetes.getCollector"),
      previewCollectorInstall: () => unavailable("kubernetes.previewCollectorInstall"),
    },
    tasks: {
      list: () => unavailable("tasks.list"),
      get: () => unavailable("tasks.get"),
      cancel: () => unavailable("tasks.cancel"),
      subscribe: () => unavailableSync("tasks.subscribe"),
      subscribeTask: () => unavailableSync("tasks.subscribeTask"),
    },
    approvals: {
      list: () => unavailable("approvals.list"),
      get: () => unavailable("approvals.get"),
      preview: () => unavailable("approvals.preview"),
      confirm: () => unavailable("approvals.confirm"),
      cancel: () => unavailable("approvals.cancel"),
      approve: () => unavailable("approvals.approve"),
      reject: () => unavailable("approvals.reject"),
    },
    models: {
      list: () => unavailable("models.list"),
      get: () => unavailable("models.get"),
      testAndCreate: () => unavailable("models.testAndCreate"),
      update: () => unavailable("models.update"),
      delete: () => unavailable("models.delete"),
      test: () => unavailable("models.test"),
      listAvailability: () => unavailable("models.listAvailability"),
      listQuotas: () => unavailable("models.listQuotas"),
      setQuota: () => unavailable("models.setQuota"),
      usage: () => unavailable("models.usage"),
    },
    interactiveCards: {
      list: () => unavailable("interactiveCards.list"),
      get: () => unavailable("interactiveCards.get"),
      create: () => unavailable("interactiveCards.create"),
      update: () => unavailable("interactiveCards.update"),
      delete: () => unavailable("interactiveCards.delete"),
      updateBindings: () => unavailable("interactiveCards.updateBindings"),
      validate: () => unavailable("interactiveCards.validate"),
      renderDemo: () => unavailable("interactiveCards.renderDemo"),
      enable: () => unavailable("interactiveCards.enable"),
      disable: () => unavailable("interactiveCards.disable"),
      deprecate: () => unavailable("interactiveCards.deprecate"),
      listToolSchemas: () => unavailable("interactiveCards.listToolSchemas"),
    },
    org: {
      listUsers: () => unavailable("org.listUsers"),
      getEnterpriseUser: () => unavailable("org.getEnterpriseUser"),
      inviteUser: () => unavailable("org.inviteUser"),
      updateUser: () => unavailable("org.updateUser"),
      updateEnterpriseUser: () => unavailable("org.updateEnterpriseUser"),
      listDepartments: () => unavailable("org.listDepartments"),
      createDepartment: () => unavailable("org.createDepartment"),
      updateDepartment: () => unavailable("org.updateDepartment"),
      deleteDepartment: () => unavailable("org.deleteDepartment"),
      listRoles: () => unavailable("org.listRoles"),
      createRole: () => unavailable("org.createRole"),
      updateRole: () => unavailable("org.updateRole"),
      deleteRole: () => unavailable("org.deleteRole"),
      listRoleBindings: () => unavailable("org.listRoleBindings"),
      createRoleBinding: () => unavailable("org.createRoleBinding"),
      updateRoleBinding: () => unavailable("org.updateRoleBinding"),
      deleteRoleBinding: () => unavailable("org.deleteRoleBinding"),
      listDataScopes: () => unavailable("org.listDataScopes"),
      saveDataScope: () => unavailable("org.saveDataScope"),
      deleteDataScope: () => unavailable("org.deleteDataScope"),
      listApprovalPolicies: () => unavailable("org.listApprovalPolicies"),
      saveApprovalPolicy: () => unavailable("org.saveApprovalPolicy"),
      listServiceAccounts: () => unavailable("org.listServiceAccounts"),
      createServiceAccount: () => unavailable("org.createServiceAccount"),
      updateServiceAccount: () => unavailable("org.updateServiceAccount"),
      listApiKeys: () => unavailable("org.listApiKeys"),
      createApiKey: () => unavailable("org.createApiKey"),
      revokeApiKey: () => unavailable("org.revokeApiKey"),
    },
    secrets: {
      list: () => unavailable("secrets.list"),
      get: () => unavailable("secrets.get"),
      create: () => unavailable("secrets.create"),
      update: () => unavailable("secrets.update"),
      delete: () => unavailable("secrets.delete"),
    },
    audit: { list: () => unavailable("audit.list") },
    platform: {
      enterprises: {
        list: () => unavailable("platform.enterprises.list"),
        get: () => unavailable("platform.enterprises.get"),
        create: () => unavailable("platform.enterprises.create"),
        update: () => unavailable("platform.enterprises.update"),
        suspend: () => unavailable("platform.enterprises.suspend"),
        activate: () => unavailable("platform.enterprises.activate"),
        disable: () => unavailable("platform.enterprises.disable"),
      },
      admins: {
        list: () => unavailable("platform.admins.list"),
        create: () => unavailable("platform.admins.create"),
        resendInvite: () => unavailable("platform.admins.resendInvite"),
        resetAuth: () => unavailable("platform.admins.resetAuth"),
        disable: () => unavailable("platform.admins.disable"),
      },
      sandboxBackends: {
        list: () => unavailable("platform.sandboxBackends.list"),
        create: () => unavailable("platform.sandboxBackends.create"),
        update: () => unavailable("platform.sandboxBackends.update"),
        test: () => unavailable("platform.sandboxBackends.test"),
      },
      images: {
        list: () => unavailable("platform.images.list"),
        create: () => unavailable("platform.images.create"),
        setEnabled: () => unavailable("platform.images.setEnabled"),
      },
      profiles: {
        list: () => unavailable("platform.profiles.list"),
        create: () => unavailable("platform.profiles.create"),
        update: () => unavailable("platform.profiles.update"),
      },
      quotas: {
        get: () => unavailable("platform.quotas.get"),
        update: () => unavailable("platform.quotas.update"),
      },
      sessions: {
        list: () => unavailable("platform.sessions.list"),
        terminate: () => unavailable("platform.sessions.terminate"),
      },
      audit: { list: () => unavailable("platform.audit.list") },
    },
    setup: {
      status: () => unavailable("setup.status"),
      submit: () => unavailable("setup.submit"),
    },
  };
}

export interface RealAdapter {
  client: ArgusApiClient;
  http: HttpTransport;
  sse: SseTransport;
  websocket: WebSocketTransport;
}

export function createRealAdapter(options: HttpTransportOptions): RealAdapter {
  const http = new HttpTransport(options);
  return {
    client: createUnavailableClient(),
    http,
    sse: new SseTransport(http),
    websocket: new WebSocketTransport(),
  };
}
