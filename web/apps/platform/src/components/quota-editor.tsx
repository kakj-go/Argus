import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  formConstraint,
  formatApiError,
  useApi,
  type EnterpriseSandboxQuota,
} from "@argus/api-client";
import { Alert, Button, Field, Input, Spinner, useUiText } from "@argus/ui";

const quotaConstraints = {
  concurrent: formConstraint("SandboxQuotaWrite", "max_concurrent_sessions"),
  sessionSeconds: formConstraint(
    "SandboxQuotaWrite",
    "monthly_session_seconds",
  ),
};

/**
 * 企业 Sandbox 配额编辑器（企业管理详情抽屉与 OpenSandbox 企业配额 Tab 共用）。
 * mock 中配额记录随种子数据存在；未配置的企业展示提示。
 */
export function QuotaEditor({ enterpriseId }: { enterpriseId: string }) {
  const { t } = useTranslation();
  const text = useUiText();
  const api = useApi();
  const queryClient = useQueryClient();

  const quota = useQuery({
    queryKey: ["platform", "quota", enterpriseId],
    queryFn: () => api.platform.quotas.get(enterpriseId),
    retry: false,
  });
  const profiles = useQuery({
    queryKey: ["platform", "profiles"],
    queryFn: () => api.platform.profiles.list(),
  });

  const schema = z.object({
    allowedProfiles: z.array(z.string()),
    maxConcurrentSessions: z
      .number()
      .int()
      .min(quotaConstraints.concurrent.minimum ?? 0)
      .max(quotaConstraints.concurrent.maximum ?? 10000),
    maxDailySessionMinutes: z
      .number()
      .int()
      .min((quotaConstraints.sessionSeconds.minimum ?? 0) / 60),
    maxDailyCpuMinutes: z.number().int().min(0),
    maxArtifactStorageMb: z.number().int().min(0),
    artifactRetentionDays: z.number().int().min(0),
  });
  type QuotaFormValues = z.infer<typeof schema>;
  const {
    control,
    handleSubmit,
    register,
    reset,
    formState: { errors },
  } = useForm<QuotaFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      allowedProfiles: [],
      maxConcurrentSessions: 0,
      maxDailySessionMinutes: 0,
      maxDailyCpuMinutes: 0,
      maxArtifactStorageMb: 0,
      artifactRetentionDays: 0,
    },
  });
  useEffect(() => {
    if (quota.data) reset(quota.data);
  }, [quota.data, reset]);

  const save = useMutation({
    mutationFn: (input: QuotaFormValues) =>
      api.platform.quotas.update(enterpriseId, {
        allowedProfiles: input.allowedProfiles,
        maxConcurrentSessions: input.maxConcurrentSessions,
        maxDailySessionMinutes: input.maxDailySessionMinutes,
        maxDailyCpuMinutes: input.maxDailyCpuMinutes,
        maxArtifactStorageMb: input.maxArtifactStorageMb,
        artifactRetentionDays: input.artifactRetentionDays,
      }),
    onSuccess: (updated) => {
      queryClient.setQueryData(["platform", "quota", enterpriseId], updated);
      void queryClient.invalidateQueries({ queryKey: ["platform", "quota"] });
    },
  });

  if (quota.isPending) return <Spinner />;
  if (quota.isError || !quota.data) {
    return (
      <Alert
        description={text(
          "该企业尚未配置 Sandbox 配额，种子数据外的企业需先由平台初始化。",
          "No sandbox quota configured for this enterprise yet.",
        )}
        title={t("sandbox.quotas.edit")}
        tone="warning"
      />
    );
  }

  const numberField = (
    label: string,
    key: keyof Omit<EnterpriseSandboxQuota, "enterpriseId" | "allowedProfiles">,
  ) => (
    <Field error={errors[key]?.message} requirement="required" label={label}>
      <Input
        {...register(key, { valueAsNumber: true })}
        min={0}
        type="number"
      />
    </Field>
  );

  return (
    <form
      className="argus-quota-editor"
      onSubmit={handleSubmit((values) => save.mutate(values))}
    >
      {save.isError && (
        <Alert
          description={formatApiError(
            save.error,
            t("sandbox.form.saveFailed"),
            (requestId) => t("common.requestReference", { requestId }),
          )}
          title={t("sandbox.form.saveFailed")}
          tone="danger"
        />
      )}
      <Field
        controlMode="group"
        error={errors.allowedProfiles?.message}
        requirement="optional"
        label={t("sandbox.quotas.table.allowedProfiles")}
      >
        <Controller
          control={control}
          name="allowedProfiles"
          render={({ field }) => (
            <div className="argus-quota-editor__profiles">
              {(profiles.data ?? []).map((profile) => (
                <label className="argus-quota-editor__profile" key={profile.id}>
                  <input
                    checked={field.value.includes(profile.id)}
                    onChange={(event) =>
                      field.onChange(
                        event.target.checked
                          ? [...field.value, profile.id]
                          : field.value.filter((id) => id !== profile.id),
                      )
                    }
                    type="checkbox"
                  />
                  <span>{profile.name}</span>
                </label>
              ))}
            </div>
          )}
        />
      </Field>
      <div className="argus-form-grid">
        {numberField(
          t("sandbox.quotas.table.concurrent"),
          "maxConcurrentSessions",
        )}
        {numberField(
          t("sandbox.quotas.table.dailyMinutes"),
          "maxDailySessionMinutes",
        )}
        {numberField(t("sandbox.quotas.table.dailyCpu"), "maxDailyCpuMinutes")}
        {numberField(t("sandbox.quotas.table.storage"), "maxArtifactStorageMb")}
        {numberField(
          t("sandbox.quotas.table.retention"),
          "artifactRetentionDays",
        )}
      </div>
      <Button loading={save.isPending} type="submit" variant="primary">
        {t("common.save")}
      </Button>
    </form>
  );
}
