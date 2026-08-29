import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApi } from "@argus/api-client";
import type { DataAuthorizationResourceType, DataAuthorizationSubjectType } from "@argus/api-client";
import { ConfirmDialog, FormDrawer, ResourceAuthorizationDualList, Spinner } from "@argus/ui";

async function listAllAuthorization(
  api: ReturnType<typeof useApi>,
  subjectType: DataAuthorizationSubjectType,
  subjectId: string,
  resourceType: DataAuthorizationResourceType,
) {
  const items: import("@argus/api-client").DataAuthorizationPage["items"] = [];
  let cursor: string | undefined;
  let first: import("@argus/api-client").DataAuthorizationPage | undefined;
  do {
    const page = await api.org.listDataAuthorization(subjectType, subjectId, resourceType, cursor, 200);
    first ??= page;
    items.push(...page.items);
    cursor = page.page?.has_more ? page.page.next_cursor ?? undefined : undefined;
  } while (cursor);
  return {
    ...(first ?? { authorization_version: 1, affected_member_count: 0, page: { next_cursor: null, has_more: false } }),
    items,
    page: { next_cursor: null, has_more: false },
  };
}

export function DataAuthorizationDialog({ open, subjectLabel, subjectType, subjectId, onOpenChange }: { open: boolean; subjectLabel: string; subjectType: DataAuthorizationSubjectType; subjectId: string; onOpenChange: (open: boolean) => void }) {
  const api = useApi();
  const queryClient = useQueryClient();
  const hosts = useQuery({ queryKey: ["authorization", subjectType, subjectId, "host"], enabled: open && Boolean(subjectId), queryFn: () => listAllAuthorization(api, subjectType, subjectId, "host") });
  const clusters = useQuery({ queryKey: ["authorization", subjectType, subjectId, "kubernetes_cluster"], enabled: open && Boolean(subjectId), queryFn: () => listAllAuthorization(api, subjectType, subjectId, "kubernetes_cluster") });
  const [value, setValue] = useState<{ host: string[]; kubernetes_cluster: string[] }>({ host: [], kubernetes_cluster: [] });
  const [baseline, setBaseline] = useState<Record<DataAuthorizationResourceType, string[]>>({ host: [], kubernetes_cluster: [] });
  const [confirmOpen, setConfirmOpen] = useState(false);
  useEffect(() => {
    if (!hosts.data || !clusters.data) return;
    const hostSelected = hosts.data.items.filter((item) => item.direct || item.inherited).map((item) => item.resource_id);
    const clusterSelected = clusters.data.items.filter((item) => item.direct || item.inherited).map((item) => item.resource_id);
    setValue({ host: hostSelected, kubernetes_cluster: clusterSelected });
    setBaseline({
      host: hosts.data.items.filter((item) => item.direct).map((item) => item.resource_id),
      kubernetes_cluster: clusters.data.items.filter((item) => item.direct).map((item) => item.resource_id),
    });
  }, [hosts.data, clusters.data]);
  const hostItems = useMemo(() => (hosts.data?.items ?? []).map((item) => ({ id: item.resource_id, label: item.name, inherited: item.inherited, source: item.sources.join(", ") })), [hosts.data]);
  const clusterItems = useMemo(() => (clusters.data?.items ?? []).map((item) => ({ id: item.resource_id, label: item.name, inherited: item.inherited, source: item.sources.join(", ") })), [clusters.data]);
  const save = useMutation({
    mutationFn: async () => {
      let version = hosts.data?.authorization_version ?? clusters.data?.authorization_version ?? 1;
      for (const resourceType of ["host", "kubernetes_cluster"] as const) {
        const before = new Set(baseline[resourceType]);
        const after = new Set(value[resourceType]);
        const add = [...after].filter((id) => !before.has(id));
        const remove = [...before].filter((id) => !after.has(id));
        if (add.length) {
          await api.org.updateDataAuthorization(subjectType, subjectId, resourceType, add, false, version);
          version = (await listAllAuthorization(api, subjectType, subjectId, resourceType)).authorization_version;
        }
        if (remove.length) {
          await api.org.updateDataAuthorization(subjectType, subjectId, resourceType, remove, true, version);
          version = (await listAllAuthorization(api, subjectType, subjectId, resourceType)).authorization_version;
        }
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["authorization", subjectType, subjectId] });
      onOpenChange(false);
    },
  });
  const dirty = (Object.keys(value) as DataAuthorizationResourceType[]).some((resourceType) => {
    const before = new Set(baseline[resourceType]);
    return value[resourceType].some((id) => !before.has(id)) || baseline[resourceType].some((id) => !value[resourceType].includes(id));
  });
  const affectedMemberCount = hosts.data?.affected_member_count ?? clusters.data?.affected_member_count ?? 0;
  const submit = () => {
    if (subjectType === "role" && dirty) {
      setConfirmOpen(true);
      return;
    }
    save.mutate();
  };
  const loading = hosts.isPending || clusters.isPending;
  return <>
  <FormDrawer open={open} onOpenChange={onOpenChange} onSubmit={submit} loading={save.isPending} submitLabel="保存" title={"数据授权：" + subjectLabel}>
    {loading ? <Spinner /> : <ResourceAuthorizationDualList hosts={hostItems} clusters={clusterItems} value={value} onChange={setValue} />}
    {save.error ? <p className="argus-form-error" role="alert">保存失败，请刷新后重试</p> : null}
  </FormDrawer>
  <ConfirmDialog open={confirmOpen} onOpenChange={setConfirmOpen} loading={save.isPending} title="确认变更角色数据授权" description={`该角色当前影响 ${affectedMemberCount} 个成员，保存后会立即刷新其资源访问范围。`} onConfirm={() => { setConfirmOpen(false); save.mutate(); }} />
  </>;
}
