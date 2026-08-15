import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type EnterpriseSandboxQuota } from "@argus/api-client";
import {
  Alert,
  Button,
  Field,
  Input,
  Spinner,
  useUiText,
} from "@argus/ui";

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

  const [draft, setDraft] = useState<EnterpriseSandboxQuota | null>(null);
  useEffect(() => {
    if (quota.data) setDraft({ ...quota.data });
  }, [quota.data]);

  const save = useMutation({
    mutationFn: (input: EnterpriseSandboxQuota) =>
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
  if (quota.isError || !draft) {
    return (
      <Alert
        description={text(
          "该企业尚未配置 Sandbox 配额，种子数据外的企业需先由平台初始化。",
          "No sandbox quota configured for this enterprise yet.",
        )}
        title={t("enterprises.quota.title")}
        tone="warning"
      />
    );
  }

  const numberField = (
    label: string,
    key: keyof Omit<EnterpriseSandboxQuota, "enterpriseId" | "allowedProfiles">,
  ) => (
    <Field label={label}>
      <Input
        min={0}
        onChange={(event) =>
          setDraft({ ...draft, [key]: Number(event.target.value) || 0 })
        }
        type="number"
        value={String(draft[key])}
      />
    </Field>
  );

  const toggleProfile = (profileId: string, checked: boolean) => {
    const next = checked
      ? [...draft.allowedProfiles, profileId]
      : draft.allowedProfiles.filter((id) => id !== profileId);
    setDraft({ ...draft, allowedProfiles: next });
  };

  return (
    <div className="quota-editor">
      <Field label={t("enterprises.quota.allowedProfiles")}>
        <div className="quota-editor__profiles">
          {(profiles.data ?? []).map((profile) => (
            <label className="quota-editor__profile" key={profile.id}>
              <input
                checked={draft.allowedProfiles.includes(profile.id)}
                onChange={(event) =>
                  toggleProfile(profile.id, event.target.checked)
                }
                type="checkbox"
              />
              <span>{profile.name}</span>
            </label>
          ))}
        </div>
      </Field>
      <div className="form-grid">
        {numberField(
          t("enterprises.quota.maxConcurrentSessions"),
          "maxConcurrentSessions",
        )}
        {numberField(
          t("enterprises.quota.maxDailySessionMinutes"),
          "maxDailySessionMinutes",
        )}
        {numberField(
          t("enterprises.quota.maxDailyCpuMinutes"),
          "maxDailyCpuMinutes",
        )}
        {numberField(
          t("enterprises.quota.maxArtifactStorageMb"),
          "maxArtifactStorageMb",
        )}
        {numberField(
          t("enterprises.quota.artifactRetentionDays"),
          "artifactRetentionDays",
        )}
      </div>
      <Button
        loading={save.isPending}
        onClick={() => save.mutate(draft)}
        variant="primary"
      >
        {t("common.save")}
      </Button>
    </div>
  );
}
