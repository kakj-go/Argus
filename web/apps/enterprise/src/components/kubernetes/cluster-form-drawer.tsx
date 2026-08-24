import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  apiErrorField,
  formConstraint,
  formatApiError,
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
const clusterConstraints = {
  name: formConstraint("KubernetesPreviewCreate", "name"),
  apiServer: formConstraint("KubernetesPreviewCreate", "api_server"),
  kubeconfig: formConstraint("SecretCreate", "value"),
};

type ClusterFormValues = {
  name: string;
  apiServer: string;
  connectionMode: ClusterConnectionMode;
  bastionScopeId: string;
  environment: Environment;
  kubeconfig: string;
  labelsText: string;
};

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
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [enrollment, setEnrollment] =
    useState<ConfirmActionResult["one_time_result"]>();
  const schema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t("kubernetes.form.required"))
            .max(clusterConstraints.name.maxLength ?? 128),
          apiServer: z
            .string()
            .trim()
            .min(1, t("kubernetes.form.required"))
            .max(clusterConstraints.apiServer.maxLength ?? 2048)
            .refine((value) => {
              try {
                const url = new URL(value);
                return url.protocol === "http:" || url.protocol === "https:";
              } catch {
                return false;
              }
            }, t("kubernetes.form.required")),
          connectionMode: z.enum(["via_bastion", "direct", "in_cluster"]),
          bastionScopeId: z.string(),
          environment: z.enum(["development", "staging", "production"]),
          kubeconfig: z
            .string()
            .max(clusterConstraints.kubeconfig.maxLength ?? 1048576),
          labelsText: z.string(),
        })
        .superRefine((values, context) => {
          if (values.connectionMode === "via_bastion" && !values.bastionScopeId) {
            context.addIssue({
              code: "custom",
              message: t("kubernetes.form.required"),
              path: ["bastionScopeId"],
            });
          }
          if (
            values.connectionMode !== "in_cluster" &&
            !editing &&
            !values.kubeconfig.trim()
          ) {
            context.addIssue({
              code: "custom",
              message: t("kubernetes.form.required"),
              path: ["kubeconfig"],
            });
          }
        }),
    [editing, t],
  );
  const {
    control,
    register,
    reset,
    setError,
    handleSubmit,
    watch,
    formState: { errors },
  } = useForm<ClusterFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      apiServer: "",
      connectionMode: "via_bastion",
      bastionScopeId: "",
      environment: "production",
      kubeconfig: "",
      labelsText: "",
    },
  });
  const connectionMode = watch("connectionMode");

  const scopesQuery = useQuery({
    queryKey: ["connectors", "bastionScopes"],
    queryFn: () => api.connectors.listBastionScopes(),
    enabled: state !== null && connectionMode === "via_bastion",
  });

  const resetAndClose = () => {
    reset();
    setPendingAction(null);
    setEnrollment(undefined);
    onClose();
  };

  const invalidateClusters = () =>
    queryClient.invalidateQueries({ queryKey: ["kubernetes"] });

  const submit = useMutation({
    mutationFn: async (values: ClusterFormValues) => {
      let credentialId = editing?.credential_id || undefined;
      let connectionTest: ConnectionTest | undefined;
      if (values.connectionMode !== "in_cluster" && values.kubeconfig.trim()) {
        const secret = await api.secrets.create({
          name: `kubeconfig-${values.name}`,
          type: "kubeconfig",
          value: values.kubeconfig,
        });
        const credential = await api.secrets.createCredential({
          name: `kubernetes-${values.name}`,
          protocol: "kubernetes",
          secret_id: secret.id,
        });
        credentialId = credential.id;
      }
      if (values.connectionMode !== "in_cluster") {
        if (!credentialId) throw new Error("Kubernetes credential is required");
        connectionTest = await api.kubernetes.createConnectionTest({
          api_server: values.apiServer,
          connection_mode: values.connectionMode,
          bastion_scope_id:
            values.connectionMode === "via_bastion"
              ? values.bastionScopeId
              : undefined,
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
          name: values.name,
          environment: values.environment,
          labels: parseLabels(values.labelsText),
          credential_id: credentialId,
          connection_test_id: connectionTest?.id,
          expected_version: editing.resource_version ?? 1,
        };
        return api.kubernetes.previewUpdateResource(editing.id, input);
      }
      const input: KubernetesPreviewCreate = {
        name: values.name,
        api_server: values.apiServer,
        connection_mode: values.connectionMode,
        bastion_scope_id:
          values.connectionMode === "via_bastion"
            ? values.bastionScopeId
            : undefined,
        credential_id: credentialId,
        environment: values.environment,
        labels: parseLabels(values.labelsText),
        connection_test_id: connectionTest?.id,
      };
      return api.kubernetes.previewCreateResource(input);
    },
    onSuccess: (action) => {
      setPendingAction(action);
    },
    onError: (error) => {
      const field = apiErrorField(error);
      const formField =
        field === "name"
          ? "name"
          : field === "api_server"
            ? "apiServer"
            : field === "bastion_scope_id"
              ? "bastionScopeId"
              : undefined;
      const message = formatApiError(
        error,
        t("kubernetes.loadFailed"),
        (requestId) => t("common.requestReference", { requestId }),
      );
      if (formField) {
        setError(formField, { message, type: "server" }, { shouldFocus: true });
      } else {
        setError("root", { message, type: "server" });
      }
    },
  });
  useEffect(() => {
    if (!state) return;
    reset({
      name: editing?.name ?? "",
      apiServer: editing?.api_server ?? "",
      connectionMode: editing?.connection_mode ?? "via_bastion",
      bastionScopeId: editing?.bastion_scope_id ?? "",
      environment: editing?.environment ?? "production",
      kubeconfig: "",
      labelsText: labelsToText(editing?.labels ?? {}),
    });
    setPendingAction(null);
    setEnrollment(undefined);
  }, [editing, reset, state]);

  return (
    <FormDrawer
      footer={pendingAction ? <span /> : undefined}
      loading={submit.isPending}
      onOpenChange={(open) => {
        if (!open) resetAndClose();
      }}
      onSubmit={handleSubmit((values) => submit.mutate(values))}
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
          <CodeBlock
            code={enrollment.enrollment.install_command ?? ""}
            language="bash"
          />
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
          claimOneTimeResult={connectionMode === "in_cluster" && !editing}
          onSettled={(confirmed, result) => {
            void invalidateClusters();
            if (confirmed && result?.one_time_result) {
              setPendingAction(null);
              setEnrollment(result.one_time_result);
              return;
            }
            resetAndClose();
          }}
        />
      ) : (
        <div className="argus-k8s-stack">
          {errors.root?.message && (
            <Alert
              description={errors.root.message}
              title={t("kubernetes.loadFailed")}
              tone="danger"
            />
          )}
          <Field requirement="required" error={errors.name?.message} label={t("kubernetes.form.name")}>
            <Input
              {...register("name")}
              autoFocus
              maxLength={clusterConstraints.name.maxLength}
              placeholder={t("kubernetes.form.namePlaceholder")}
            />
          </Field>
          <Field requirement="required" error={errors.apiServer?.message} label={t("kubernetes.form.apiServer")}>
            <Input
              {...register("apiServer")}
              disabled={editing !== null}
              maxLength={clusterConstraints.apiServer.maxLength}
              placeholder={t("kubernetes.form.apiServerPlaceholder")}
            />
          </Field>
          <Field
            requirement="required"
            label={t("kubernetes.form.connectionMode")}
          >
            <Controller control={control} name="connectionMode" render={({ field }) => (
              <Select
                disabled={editing !== null}
                onValueChange={field.onChange}
                options={CONNECTION_MODES.map((mode) => ({ value: mode, label: t(`kubernetes.mode.${mode}`) }))}
                value={field.value}
              />
            )} />
          </Field>
          {connectionMode === "via_bastion" && (
            <Field
              requirement="required"
              error={errors.bastionScopeId?.message}
              label={t("kubernetes.form.bastionScope")}
            >
              <Controller control={control} name="bastionScopeId" render={({ field }) => (
                <Select
                  disabled={editing !== null}
                  onValueChange={field.onChange}
                  options={[{ value: "", label: "—" }, ...(scopesQuery.data?.items ?? []).map((scope) => ({ value: scope.id, label: scope.name }))]}
                  value={field.value}
                />
              )} />
            </Field>
          )}
          <Field
            requirement="required"
            label={t("kubernetes.form.environment")}
          >
            <Controller control={control} name="environment" render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={ENVIRONMENTS.map((item) => ({ value: item, label: t(`kubernetes.environment.${item}`) }))}
                value={field.value}
              />
            )} />
          </Field>
          <Field
            requirement="optional"
            hint={t("kubernetes.form.labelsHint")}
            label={t("kubernetes.form.labels")}
          >
            <Textarea
              {...register("labelsText")}
              rows={3}
            />
          </Field>
          <Field
            requirement={
              connectionMode !== "in_cluster" && !editing
                ? "required"
                : "optional"
            }
            error={errors.kubeconfig?.message}
            hint={
              editing
                ? t("kubernetes.form.kubeconfigRotateHint")
                : t("kubernetes.form.kubeconfigHint")
            }
            label={t("kubernetes.form.kubeconfig")}
          >
            <Textarea
              {...register("kubeconfig")}
              maxLength={clusterConstraints.kubeconfig.maxLength}
              placeholder={t("kubernetes.form.kubeconfigPlaceholder")}
              rows={8}
            />
          </Field>
        </div>
      )}
    </FormDrawer>
  );
}
