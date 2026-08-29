import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import type {
  PendingActionPublic,
  PendingActionFilter,
  RiskLevel,
} from "@argus/api-client";
import { useApi } from "@argus/api-client";
import {
  Badge,
  EmptyState,
  FilterBar,
  PageShell,
  Spinner,
  StatCard,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import "../styles/governance.css";
import { ApprovalDetail } from "../components/governance/approval-detail";
import { RemoteAccessApprovals } from "../components/governance/remote-access-approvals";
import {
  DONE_PENDING_STATUSES,
  formatCountdown,
  formatDateTime,
  isExpired,
  isToday,
  OPEN_PENDING_STATUSES,
  pendingStatusTone,
  RISK_ORDER,
  riskTone,
  useNow,
} from "../components/governance/utils";

type PrimaryTab = "operation" | "remote";
type ScopeFilter = "mine" | "created" | "done";

/** 待审批（路由 /approvals）：收件箱式列表 + 右侧详情。 */
export function ApprovalsPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";

  const [search, setSearch] = useState("");
  const [risk, setRisk] = useState("");
  const params = new URLSearchParams(window.location.search);
  const [primary, setPrimary] = useState<PrimaryTab>(
    params.get("approval") === "remote" ? "remote" : "operation",
  );
  const [scope, setScope] = useState<ScopeFilter>(
    params.get("scope") === "created" || params.get("scope") === "done"
      ? (params.get("scope") as ScopeFilter)
      : "mine",
  );
  const [selectedRef, setSelectedRef] = useState<string | null>(null);

  const filter = useMemo<PendingActionFilter>(
    () => ({
      ...(search.trim() ? { query: search.trim() } : {}),
      ...(risk ? { risk: [risk as RiskLevel] } : {}),
      ...(primary === "operation"
        ? scope === "done"
          ? { status: DONE_PENDING_STATUSES }
          : { scope }
        : {}),
    }),
    [primary, search, risk, scope],
  );

  useEffect(() => {
    const next = new URLSearchParams(window.location.search);
    next.set("approval", primary);
    next.set("scope", scope);
    window.history.replaceState(
      null,
      "",
      `${window.location.pathname}?${next.toString()}`,
    );
  }, [primary, scope]);

  const listQuery = useQuery({
    queryKey: ["approvals", "list", filter],
    queryFn: () => api.approvals.list(filter),
    enabled: primary === "operation",
  });
  const mineStatsQuery = useQuery({
    queryKey: ["approvals", "stats", "mine"],
    queryFn: () => api.approvals.list({ scope: "mine" }),
  });
  const statsQuery = useQuery({
    queryKey: ["approvals", "stats"],
    queryFn: () => api.approvals.list(),
  });

  const actions = primary === "remote" ? [] : (listQuery.data?.items ?? []);
  const allActions = statsQuery.data?.items ?? [];
  const now = useNow();

  const groups = RISK_ORDER.map((riskLevel) => ({
    riskLevel,
    items: actions.filter((action) => action.risk === riskLevel),
  })).filter((group) => group.items.length > 0);

  const awaitingMine = (mineStatsQuery.data?.items ?? []).filter(
    (action) => action.status === "awaiting_approval",
  ).length;
  const handledToday = allActions.filter(
    (action) =>
      DONE_PENDING_STATUSES.includes(action.status) &&
      isToday(action.updated_at),
  ).length;
  const highestRisk = RISK_ORDER.find((riskLevel) =>
    allActions.some(
      (action) =>
        action.risk === riskLevel &&
        OPEN_PENDING_STATUSES.includes(action.status),
    ),
  );

  return (
    <PageShell
      description={t("governance.approvals.description")}
      title={t("governance.approvals.title")}
    >
      <Tabs
        className="argus-approval-tabs"
        onValueChange={(value) => setPrimary(value as PrimaryTab)}
        value={primary}
      >
        <TabsList>
          <TabsTrigger value="operation">
            {t("governance.approvals.primary.operation")}
          </TabsTrigger>
          <TabsTrigger value="remote">
            {t("governance.approvals.primary.remote")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="remote">
          <Tabs
            className="argus-approval-scope-tabs"
            onValueChange={(value) => setScope(value as ScopeFilter)}
            value={scope}
          >
            <TabsList>
              <TabsTrigger value="mine">
                {t("governance.approvals.scope.mine")}
              </TabsTrigger>
              <TabsTrigger value="created">
                {t("governance.approvals.scope.created")}
              </TabsTrigger>
              <TabsTrigger value="done">
                {t("governance.approvals.scope.done")}
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <RemoteAccessApprovals scope={scope} />
        </TabsContent>
        <TabsContent value="operation">
          <div className="argus-gov-stats">
            <StatCard
              label={t("governance.approvals.stats.mine")}
              tone={awaitingMine > 0 ? "warning" : "neutral"}
              value={awaitingMine}
            />
            <StatCard
              label={t("governance.approvals.stats.handledToday")}
              value={handledToday}
            />
            <StatCard
              label={t("governance.approvals.stats.highestRisk")}
              tone={
                highestRisk === "critical" || highestRisk === "dangerous"
                  ? "danger"
                  : highestRisk === "write"
                    ? "warning"
                    : "neutral"
              }
              value={
                highestRisk
                  ? t(`governance.approvals.risk.${highestRisk}`)
                  : t("governance.approvals.stats.none")
              }
            />
          </div>

          <Tabs
            className="argus-approval-scope-tabs"
            onValueChange={(value) => setScope(value as ScopeFilter)}
            value={scope}
          >
            <TabsList>
              <TabsTrigger value="mine">
                {t("governance.approvals.scope.mine")}
              </TabsTrigger>
              <TabsTrigger value="created">
                {t("governance.approvals.scope.created")}
              </TabsTrigger>
              <TabsTrigger value="done">
                {t("governance.approvals.scope.done")}
              </TabsTrigger>
            </TabsList>
          </Tabs>

          <FilterBar
            filters={[
              {
                key: "risk",
                value: risk,
                allLabel: t("governance.approvals.allRisks"),
                options: RISK_ORDER.map((value) => ({
                  value,
                  label: t(`governance.approvals.risk.${value}`),
                })),
                onChange: setRisk,
              },
            ]}
            onRefresh={() => {
              void queryClient.invalidateQueries({ queryKey: ["approvals"] });
            }}
            refreshing={listQuery.isFetching}
            search={{
              value: search,
              onChange: setSearch,
              placeholder: t("governance.approvals.searchPlaceholder"),
            }}
          />

          <div className="argus-approvals-layout">
            <div className="argus-approval-inbox">
              {listQuery.isPending ? (
                <Spinner label={t("common.loading")} />
              ) : actions.length === 0 ? (
                <EmptyState
                  description={t("governance.approvals.emptyDescription")}
                  title={t("governance.approvals.emptyTitle")}
                />
              ) : (
                groups.map((group) => (
                  <section
                    className={`argus-approval-group is-${group.riskLevel}`}
                    key={group.riskLevel}
                  >
                    <header className="argus-approval-group__header">
                      <i aria-hidden className="argus-approval-group__dot" />
                      {t(`governance.approvals.risk.${group.riskLevel}`)}
                      <span className="argus-approval-group__count">
                        {group.items.length}
                      </span>
                    </header>
                    {group.items.map((action) => (
                      <ApprovalInboxItem
                        action={action}
                        key={action.action_ref}
                        locale={locale}
                        now={now}
                        onSelect={() => setSelectedRef(action.action_ref)}
                        selected={selectedRef === action.action_ref}
                      />
                    ))}
                  </section>
                ))
              )}
            </div>

            <div>
              {selectedRef ? (
                <ApprovalDetail
                  actionRef={selectedRef}
                  key={selectedRef}
                  readOnly={scope !== "mine"}
                />
              ) : (
                <EmptyState
                  description={t("governance.approvals.selectHint")}
                  title={t("governance.approvals.selectHintTitle")}
                />
              )}
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}

function ApprovalInboxItem({
  action,
  selected,
  onSelect,
  now,
  locale,
}: {
  action: PendingActionPublic;
  selected: boolean;
  onSelect: () => void;
  now: number;
  locale: string;
}) {
  const { t } = useTranslation();
  const open = OPEN_PENDING_STATUSES.includes(action.status);
  const expired = open && isExpired(action.expires_at, now);

  return (
    <button
      className={[
        "argus-approval-item",
        selected ? "is-selected" : "",
        expired ? "is-expired" : "",
        !open ? "is-closed" : "",
      ]
        .filter(Boolean)
        .join(" ")}
      onClick={onSelect}
      type="button"
    >
      <span className="argus-approval-item__top">
        <span className="argus-approval-item__title">{action.title}</span>
        <Badge tone={riskTone(action.risk)}>
          {t(`governance.approvals.risk.${action.risk}`)}
        </Badge>
        <StatusBadge tone={pendingStatusTone(action.status)}>
          {t(`governance.approvals.status.${action.status}`)}
        </StatusBadge>
      </span>
      <p className="argus-approval-item__summary">{action.summary}</p>
      <span className="argus-approval-item__meta">
        <span>{formatDateTime(action.created_at, locale)}</span>
        {open && (
          <>
            <span>·</span>
            <span
              className={`argus-approval-item__countdown${expired ? " is-expired" : ""}`}
            >
              <Clock aria-hidden size={11} />
              {expired
                ? t("governance.approvals.expired")
                : formatCountdown(action.expires_at, now)}
            </span>
          </>
        )}
      </span>
    </button>
  );
}
