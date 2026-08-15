import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type ClusterConnectionMode,
  type Environment,
  type K8sCluster,
  type PendingAction,
} from "@argus/api-client";
import { Field, FormDrawer, Input, Select, Textarea } from "@argus/ui";
import { PendingActionCard } from "./pending-action-card";

const CONNECTION_MODES: ClusterConnectionMode[] = [
  "via_bastion",
  "direct",
  "in_cluster",
];
const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];

export type ClusterFormState =
  | { mode: "create" }
  | { mode: "edit"; cluster: K8sCluster };

/**
 * 添加/编辑集群抽屉。kubeconfig 先写入 Secret 再引用其 id（credentialRef），
 * 值不回显；新建走 previewCreateCluster 两阶段确认，编辑直接 updateCluster。
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
  const [error, setError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);

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
    setError(null);
    setPendingAction(null);
    onClose();
  };

  const invalidateClusters = () =>
    queryClient.invalidateQueries({ queryKey: ["kubernetes"] });

  const submit = useMutation({
    mutationFn: async () => {
      if (editing) {
        const patch: { name?: string; environment?: Environment; credentialRef?: string } = {
          name: name.trim(),
          environment,
        };
        if (kubeconfig.trim()) {
          const secret = await api.secrets.create({
            name: `kubeconfig-${name.trim()}`,
            type: "kubeconfig",
            value: kubeconfig,
          });
          patch.credentialRef = secret.id;
        }
        await api.kubernetes.updateCluster(editing.id, patch);
        return null;
      }
      const secret = await api.secrets.create({
        name: `kubeconfig-${name.trim()}`,
        type: "kubeconfig",
        value: kubeconfig,
      });
      return api.kubernetes.previewCreateCluster({
        name: name.trim(),
        apiServer: apiServer.trim(),
        connectionMode,
        bastionScopeId:
          connectionMode === "via_bastion" && bastionScopeId
            ? bastionScopeId
            : undefined,
        credentialRef: secret.id,
        environment,
      });
    },
    onSuccess: (action) => {
      if (action) {
        setPendingAction(action);
      } else {
        void invalidateClusters();
        resetAndClose();
      }
    },
    onError: () => setError(t("kubernetes.loadFailed")),
  });

  const handleSubmit = () => {
    setError(null);
    const missing = editing
      ? !name.trim()
      : !name.trim() || !apiServer.trim() || !kubeconfig.trim();
    if (missing) {
      setError(t("kubernetes.form.required"));
      return;
    }
    submit.mutate();
  };

  // 打开编辑时回填字段（state 切换时重置）
  const [filledFor, setFilledFor] = useState<K8sCluster | null>(null);
  if (editing && editing !== filledFor) {
    setFilledFor(editing);
    setName(editing.name);
    setApiServer(editing.apiServer);
    setConnectionMode(editing.connectionMode);
    setBastionScopeId(editing.bastionScopeId ?? "");
    setEnvironment(editing.environment);
    setKubeconfig("");
    setPendingAction(null);
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
      {pendingAction ? (
        <PendingActionCard
          action={pendingAction}
          onSettled={() => {
            void invalidateClusters();
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
          <Field label={t("kubernetes.form.apiServer")}>
            <Input
              disabled={editing !== null}
              onChange={(event) => setApiServer(event.target.value)}
              placeholder={t("kubernetes.form.apiServerPlaceholder")}
              value={apiServer}
            />
          </Field>
          <Field label={t("kubernetes.form.connectionMode")}>
            <Select
              disabled={editing !== null}
              onValueChange={(value) => setConnectionMode(value as ClusterConnectionMode)}
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
                  ...(scopesQuery.data ?? []).map((scope) => ({
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
