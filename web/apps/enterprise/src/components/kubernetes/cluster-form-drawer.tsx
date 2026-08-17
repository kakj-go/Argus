import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type ConfirmActionResult,
  type Environment,
  type KubernetesCluster,
  type PendingActionPublic,
} from "@argus/api-client";
import type {
  ConnectionTest,
  KubernetesPreviewCreate,
  KubernetesPreviewUpdate,
} from "@argus/api-client/contracts";
import {
  Alert,
  Button,
  CodeBlock,
  Field,
  FormDrawer,
  Input,
  Select,
  Textarea,
} from "@argus/ui";
import { PendingActionCard } from "./pending-action-card";
import { labelsToText, parseLabels } from "../hosts/host-utils";

type ClusterConnectionMode = KubernetesCluster["connection_mode"];

const CONNECTION_MODES: ClusterConnectionMode[] = [
  "via_bastion",
  "direct",
  "in_cluster",
];
const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

export type ClusterFormState =
  { mode: "create" } | { mode: "edit"; cluster: KubernetesCluster };

/**
 * 添加/编辑集群抽屉。kubeconfig 先写入 Secret/Credential，再执行连接测试和两阶段确认。
 */
export function ClusterFormDrawer({
  state,
  onClose,
}: {
  state: ClusterFormState | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const editing = state?.mode === "edit" ? state.cluster : null;

  const [name, setName] = useState("");
  const [apiServer, setApiServer] = useState("");
  const [connectionMode, setConnectionMode] =
    useState<ClusterConnectionMode>("via_bastion");
  const [bastionScopeId, setBastionScopeId] = useState("");
  const [environment, setEnvironment] = useState<Environment>("production");
  const [kubeconfig, setKubeconfig] = useState("");
  const [labelsText, setLabelsText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [enrollment, setEnrollment] =
    useState<ConfirmActionResult["enrollment"]>();

  const scopesQuery = useQuery({
    queryKey: ["connectors", "bastionScopes"],
    queryFn: () => api.connectors.listBastionScopes(),
    enabled: state !== null && connectionMode === "via_bastion",
  });

  const resetAndClose = () => {
    setName("");
    setApiServer("");
    setConnectionMode("via_bastion");
    setBastionScopeId("");
    setEnvironment("production");
    setKubeconfig("");
    setLabelsText("");
    setError(null);
    setPendingAction(null);
    setEnrollment(undefined);
    onClose();
  };

  const invalidateClusters = () =>
    queryClient.invalidateQueries({ queryKey: ["kubernetes"] });

  const submit = useMutation({
    mutationFn: async () => {
      let credentialId = editing?.credential_id || undefined;
      let connectionTest: ConnectionTest | undefined;
      if (connectionMode !== "in_cluster" && kubeconfig.trim()) {
        const secret = await api.secrets.create({
          name: `kubeconfig-${name.trim()}`,
          type: "kubeconfig",
          value: kubeconfig,
        });
        const credential = await api.secrets.createCredential({
          name: `kubernetes-${name.trim()}`,
          protocol: "kubernetes",
          secret_id: secret.id,
        });
        credentialId = credential.id;
      }
      if (connectionMode !== "in_cluster") {
        if (!credentialId) throw new Error("Kubernetes credential is required");
        connectionTest = await api.kubernetes.createConnectionTest({
          api_server: apiServer.trim(),
          connection_mode: connectionMode,
          bastion_scope_id:
            connectionMode === "via_bastion" ? bastionScopeId : undefined,
          credential_id: credentialId,
        });
        for (let attempt = 0; attempt < 60; attempt += 1) {
          if (!["queued", "running"].includes(connectionTest.status)) break;
          await new Promise((resolve) => window.setTimeout(resolve, 500));
          connectionTest = await api.kubernetes.getConnectionTest(
            connectionTest.id,
          );
        }
        if (connectionTest.status !== "succeeded") {
          throw new Error(
            connectionTest.error_code ?? "Connection test failed",
          );
        }
      }
      if (editing) {
        const input: KubernetesPreviewUpdate = {
          name: name.trim(),
          environment,
          labels: parseLabels(labelsText),
          credential_id: credentialId,
          connection_test_id: connectionTest?.id,
          expected_version: editing.resource_version ?? 1,
        };
        return api.kubernetes.previewUpdateResource(editing.id, input);
      }
      const input: KubernetesPreviewCreate = {
        name: name.trim(),
        api_server: apiServer.trim(),
        connection_mode: connectionMode,
        bastion_scope_id:
          connectionMode === "via_bastion" ? bastionScopeId : undefined,
        credential_id: credentialId,
        environment,
        labels: parseLabels(labelsText),
        connection_test_id: connectionTest?.id,
      };
      return api.kubernetes.previewCreateResource(input);
    },
    onSuccess: (action) => {
      setPendingAction(action);
    },
    onError: () => setError(t("kubernetes.loadFailed")),
  });

  const handleSubmit = () => {
    setError(null);
    const missing =
      !name.trim() ||
      !apiServer.trim() ||
      (connectionMode !== "in_cluster" && !editing && !kubeconfig.trim()) ||
      (connectionMode === "via_bastion" && !bastionScopeId);
    if (missing) {
      setError(t("kubernetes.form.required"));
      return;
    }
    submit.mutate();
  };

  // 打开编辑时回填字段（state 切换时重置）
  const [filledFor, setFilledFor] = useState<KubernetesCluster | null>(null);
  if (editing && editing !== filledFor) {
    setFilledFor(editing);
    setName(editing.name);
    setApiServer(editing.api_server);
    setConnectionMode(editing.connection_mode);
    setBastionScopeId(editing.bastion_scope_id ?? "");
    setEnvironment(editing.environment);
    setKubeconfig("");
    setLabelsText(labelsToText(editing.labels));
    setPendingAction(null);
    setEnrollment(undefined);
    setError(null);
  }

  return (
    <FormDrawer
      footer={pendingAction ? <span /> : undefined}
      loading={submit.isPending}
      onOpenChange={(open) => {
        if (!open) resetAndClose();
      }}
      onSubmit={handleSubmit}
      open={state !== null}
      submitLabel={
        editing ? t("kubernetes.form.save") : t("kubernetes.form.submit")
      }
      title={
        editing
          ? t("kubernetes.form.editTitle")
          : t("kubernetes.form.createTitle")
      }
      width={520}
    >
      {enrollment ? (
        <div className="argus-k8s-stack">
          <Alert
            description={t("kubernetes.form.enrollmentWarning")}
            title={t("kubernetes.form.enrollmentTitle")}
            tone="warning"
          />
          <CodeBlock code={enrollment.install_command} language="bash" />
          <p className="argus-muted">
            {t("kubernetes.form.enrollmentExpires", {
              time: new Date(enrollment.expires_at).toLocaleString(),
            })}
          </p>
          <div className="argus-form-actions">
            <Button onClick={resetAndClose} variant="primary">
              {t("kubernetes.form.enrollmentClose")}
            </Button>
          </div>
        </div>
      ) : pendingAction ? (
        <PendingActionCard
          action={pendingAction}
          onSettled={(confirmed, result) => {
            void invalidateClusters();
            if (confirmed && result?.enrollment) {
              setPendingAction(null);
              setEnrollment(result.enrollment);
              return;
            }
            resetAndClose();
          }}
        />
      ) : (
        <div className="argus-k8s-stack">
          {error && (
            <p className="argus-field__hint is-error" role="alert">
              {error}
            </p>
          )}
          <Field label={t("kubernetes.form.name")}>
            <Input
              autoFocus
              onChange={(event) => setName(event.target.value)}
              placeholder={t("kubernetes.form.namePlaceholder")}
              value={name}
            />
          </Field>
          <Field label={t("kubernetes.form.api_server")}>
            <Input
              disabled={editing !== null}
              onChange={(event) => setApiServer(event.target.value)}
              placeholder={t("kubernetes.form.api_serverPlaceholder")}
              value={apiServer}
            />
          </Field>
          <Field label={t("kubernetes.form.connection_mode")}>
            <Select
              disabled={editing !== null}
              onValueChange={(value) =>
                setConnectionMode(value as ClusterConnectionMode)
              }
              options={CONNECTION_MODES.map((mode) => ({
                value: mode,
                label: t(`kubernetes.mode.${mode}`),
              }))}
              value={connectionMode}
            />
          </Field>
          {connectionMode === "via_bastion" && (
            <Field label={t("kubernetes.form.bastionScope")}>
              <Select
                disabled={editing !== null}
                onValueChange={setBastionScopeId}
                options={[
                  { value: "", label: "—" },
                  ...(scopesQuery.data?.items ?? []).map((scope) => ({
                    value: scope.id,
                    label: scope.name,
                  })),
                ]}
                value={bastionScopeId}
              />
            </Field>
          )}
          <Field label={t("kubernetes.form.environment")}>
            <Select
              onValueChange={(value) => setEnvironment(value as Environment)}
              options={ENVIRONMENTS.map((item) => ({
                value: item,
                label: t(`kubernetes.environment.${item}`),
              }))}
              value={environment}
            />
          </Field>
          <Field
            hint={t("kubernetes.form.labelsHint")}
            label={t("kubernetes.form.labels")}
          >
            <Textarea
              onChange={(event) => setLabelsText(event.target.value)}
              rows={3}
              value={labelsText}
            />
          </Field>
          <Field
            hint={
              editing
                ? t("kubernetes.form.kubeconfigRotateHint")
                : t("kubernetes.form.kubeconfigHint")
            }
            label={t("kubernetes.form.kubeconfig")}
          >
            <Textarea
              onChange={(event) => setKubeconfig(event.target.value)}
              placeholder={t("kubernetes.form.kubeconfigPlaceholder")}
              rows={8}
              value={kubeconfig}
            />
          </Field>
        </div>
      )}
    </FormDrawer>
  );
}
