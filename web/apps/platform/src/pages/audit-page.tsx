import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  auditPresentationKey,
  humanizeAuditCode,
  useApi,
} from "@argus/api-client";
import type { AuditResult, PlatformAuditEvent } from "@argus/api-client";
import {
  Button,
  DataTable,
  EmptyState,
  FilterBar,
  FormDrawer,
  KeyValueGrid,
  PageShell,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

type AuditRow = {
  id: string;
  createdAt: string;
  actorName: string;
  actionLabel: string;
  resource: string;
  result: AuditResult;
  summary: string;
};

function resultTone(result: AuditResult) {
  if (result === "success") return "success" as const;
  if (result === "denied") return "warning" as const;
  return "danger" as const;
}

/**
 * 平台审计：只有 enterpriseId = null 的平台域事件（mock 已保证），
 * FilterBar（动作/资源类型/结果/搜索）+ 行详情抽屉。
 */
export function AuditPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();

  const [action, setAction] = useState("");
  const [resourceType, setResourceType] = useState("");
  const [result, setResult] = useState("");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<PlatformAuditEvent | null>(null);
  const labelFor = useCallback(
    (kind: "actions" | "resourceTypes" | "actorTypes", code: string) =>
      t(auditPresentationKey("audit", kind, code), {
        defaultValue: humanizeAuditCode(code),
      }),
    [t],
  );
  const actorLabel = (item: PlatformAuditEvent) =>
    item.actorName !== item.actorUserId
      ? item.actorUsername && item.actorUsername !== item.actorName
        ? `${item.actorName} (${item.actorUsername})`
        : item.actorName
      : labelFor("actorTypes", item.actorType);
  const actionLabel = (item: PlatformAuditEvent) =>
    labelFor("actions", item.action);
  const resourceLabel = (item: PlatformAuditEvent) => {
    if (!item.resourceType) return "—";
    const typeLabel = labelFor("resourceTypes", item.resourceType);
    return item.resourceName
      ? `${typeLabel} · ${item.resourceName}`
      : typeLabel;
  };
  const summaryLabel = (item: PlatformAuditEvent) =>
    item.resourceName
      ? t("audit.summaryWithResource", {
          action: actionLabel(item),
          resource: item.resourceName,
        })
      : actionLabel(item);

  const events = useQuery({
    queryKey: ["platform", "audit", { search }],
    queryFn: () => api.platform.audit.list({ query: search || undefined }),
  });

  const actionOptions = useMemo(() => {
    const set = new Set<string>();
    for (const item of events.data?.items ?? []) set.add(item.action);
    return [...set]
      .sort()
      .map((value) => ({ value, label: labelFor("actions", value) }));
  }, [events.data, labelFor]);

  const resourceTypeOptions = useMemo(() => {
    const set = new Set<string>();
    for (const item of events.data?.items ?? []) {
      if (item.resourceType) set.add(item.resourceType);
    }
    return [...set]
      .sort()
      .map((value) => ({ value, label: labelFor("resourceTypes", value) }));
  }, [events.data, labelFor]);

  // mock 的平台审计只支持 action/query 服务端过滤，其余在客户端过滤。
  const filtered = useMemo(() => {
    let items = events.data?.items ?? [];
    if (action) items = items.filter((item) => item.action === action);
    if (resourceType) {
      items = items.filter((item) => item.resourceType === resourceType);
    }
    if (result) {
      items = items.filter((item) => item.result === (result as AuditResult));
    }
    return items;
  }, [events.data, action, resourceType, result]);

  const rows: AuditRow[] = filtered.map((item) => ({
    id: item.id,
    createdAt: item.createdAt,
    actorName: actorLabel(item),
    actionLabel: actionLabel(item),
    resource: resourceLabel(item),
    result: item.result,
    summary: summaryLabel(item),
  }));

  const openDetail = (row: AuditRow) =>
    setSelected(filtered.find((item) => item.id === row.id) ?? null);

  return (
    <PageShell description={t("audit.description")} title={t("audit.title")}>
      <div className="argus-platform-stack">
        <FilterBar
          filters={[
            {
              key: "action",
              value: action,
              allLabel: t("audit.filters.allActions"),
              options: actionOptions,
              onChange: setAction,
            },
            {
              key: "resourceType",
              value: resourceType,
              allLabel: t("audit.filters.allResources"),
              options: resourceTypeOptions,
              onChange: setResourceType,
            },
            {
              key: "result",
              value: result,
              allLabel: t("audit.filters.allResults"),
              options: (["success", "failure", "denied"] as const).map(
                (value) => ({ value, label: t(`audit.results.${value}`) }),
              ),
              onChange: setResult,
            },
          ]}
          onRefresh={() => void events.refetch()}
          refreshing={events.isFetching}
          search={{
            value: search,
            onChange: setSearch,
            placeholder: t("audit.filters.searchPlaceholder"),
          }}
        />

        {events.isPending ? (
          <Spinner />
        ) : rows.length === 0 ? (
          <EmptyState description="" title={t("audit.empty")} />
        ) : (
          <div className="argus-platform-audit-table">
            <DataTable<AuditRow>
              columns={[
                {
                  key: "createdAt",
                  header: t("audit.table.time"),
                  render: (row) => formatDateTime(row.createdAt, i18n.language),
                },
                { key: "actorName", header: t("audit.table.actor") },
                {
                  key: "actionLabel",
                  header: t("audit.table.action"),
                },
                { key: "resource", header: t("audit.table.resource") },
                {
                  key: "result",
                  header: t("audit.table.result"),
                  render: (row) => (
                    <StatusBadge tone={resultTone(row.result)}>
                      {t(`audit.results.${row.result}`)}
                    </StatusBadge>
                  ),
                },
                {
                  key: "id",
                  header: t("common.actions"),
                  render: (row) => (
                    <Button
                      onClick={() => openDetail(row)}
                      size="sm"
                      variant="ghost"
                    >
                      {t("common.detail")}
                    </Button>
                  ),
                },
              ]}
              data={rows}
              getRowKey={(row) => row.id}
            />
          </div>
        )}
      </div>

      <FormDrawer
        footer={
          <Button onClick={() => setSelected(null)} variant="secondary">
            {t("common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
        open={selected !== null}
        title={t("audit.detail.title")}
      >
        {selected && (
          <KeyValueGrid
            columns={1}
            items={[
              {
                label: t("audit.detail.id"),
                value: <code className="argus-mono">{selected.id}</code>,
              },
              {
                label: t("audit.detail.actor"),
                value: actorLabel(selected),
              },
              {
                label: t("audit.detail.actorType"),
                value: labelFor("actorTypes", selected.actorType),
              },
              {
                label: t("audit.detail.actorId"),
                value: (
                  <code className="argus-mono">{selected.actorUserId}</code>
                ),
              },
              {
                label: t("audit.detail.action"),
                value: actionLabel(selected),
              },
              {
                label: t("audit.detail.actionKey"),
                value: <code className="argus-mono">{selected.action}</code>,
              },
              { label: t("audit.detail.origin"), value: selected.origin },
              {
                label: t("audit.detail.resourceType"),
                value: selected.resourceType
                  ? labelFor("resourceTypes", selected.resourceType)
                  : "—",
              },
              {
                label: t("audit.detail.resourceName"),
                value: selected.resourceName ?? "—",
              },
              {
                label: t("audit.detail.resourceId"),
                value: selected.resourceId ?? "—",
              },
              {
                label: t("audit.detail.result"),
                value: (
                  <StatusBadge tone={resultTone(selected.result)}>
                    {t(`audit.results.${selected.result}`)}
                  </StatusBadge>
                ),
              },
              {
                label: t("audit.detail.summary"),
                value: summaryLabel(selected),
              },
              {
                label: t("audit.detail.createdAt"),
                value: formatDateTime(selected.createdAt, i18n.language),
              },
            ]}
          />
        )}
      </FormDrawer>
    </PageShell>
  );
}
