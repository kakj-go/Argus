import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import type {
  PendingAction,
  PendingActionStatus,
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
} from "@argus/ui";
import "../i18n/governance";
import "../styles/governance.css";
import { ApprovalDetail } from "../components/governance/approval-detail";
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

type ScopeFilter = "all" | "mine" | "confirmation" | "done";

const SCOPE_STATUSES: Record<Exclude<ScopeFilter, "all">, PendingActionStatus[]> = {
  mine: ["awaiting_approval"],
  confirmation: ["awaiting_confirmation"],
  done: DONE_PENDING_STATUSES,
};

/** 待审批（路由 /approvals）：收件箱式列表 + 右侧详情。 */
export function ApprovalsPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";

  const [search, setSearch] = useState("");
  const [risk, setRisk] = useState("");
  const [scope, setScope] = useState<ScopeFilter>("all");
  const [selectedRef, setSelectedRef] = useState<string | null>(null);

  const filter = useMemo(
    () => ({
      ...(search.trim() ? { query: search.trim() } : {}),
      ...(risk ? { riskLevel: [risk as RiskLevel] } : {}),
      ...(scope !== "all" ? { status: SCOPE_STATUSES[scope] } : {}),
    }),
    [search, risk, scope],
  );

  const listQuery = useQuery({
    queryKey: ["approvals", "list", filter],
    queryFn: () => api.approvals.list(filter),
  });
  // 统计卡片基于全量列表派生。
  const statsQuery = useQuery({
    queryKey: ["approvals", "stats"],
    queryFn: () => api.approvals.list(),
  });

  const actions = listQuery.data?.items ?? [];
  const allActions = statsQuery.data?.items ?? [];
  const now = useNow();

  const groups = RISK_ORDER.map((riskLevel) => ({
    riskLevel,
    items: actions.filter((action) => action.riskLevel === riskLevel),
  })).filter((group) => group.items.length > 0);

  const awaitingMine = allActions.filter(
    (action) => action.status === "awaiting_approval",
  ).length;
  const handledToday = allActions.filter(
    (action) =>
      DONE_PENDING_STATUSES.includes(action.status) && isToday(action.updatedAt),
  ).length;
  const highestRisk = RISK_ORDER.find((riskLevel) =>
    allActions.some(
      (action) =>
        action.riskLevel === riskLevel &&
        OPEN_PENDING_STATUSES.includes(action.status),
    ),
  );

  return (
    <PageShell
      description={t("governance.approvals.description")}
      title={t("governance.approvals.title")}
    >
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
          {
            key: "scope",
            value: scope,
            options: (["all", "mine", "confirmation", "done"] as const).map(
              (value) => ({
                value,
                label: t(`governance.approvals.scope.${value}`),
              }),
            ),
            onChange: (value) => setScope(value as ScopeFilter),
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
                    key={action.id}
                    locale={locale}
                    now={now}
                    onSelect={() => setSelectedRef(action.actionRef)}
                    selected={selectedRef === action.actionRef}
                  />
                ))}
              </section>
            ))
          )}
        </div>

        <div>
          {selectedRef ? (
            <ApprovalDetail actionRef={selectedRef} key={selectedRef} />
          ) : (
            <EmptyState
              description={t("governance.approvals.selectHint")}
              title={t("governance.approvals.selectHintTitle")}
            />
          )}
        </div>
      </div>
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
  action: PendingAction;
  selected: boolean;
  onSelect: () => void;
  now: number;
  locale: string;
}) {
  const { t } = useTranslation();
  const open = OPEN_PENDING_STATUSES.includes(action.status);
  const expired = open && isExpired(action.expiresAt, now);

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
        <Badge tone={riskTone(action.riskLevel)}>
          {t(`governance.approvals.risk.${action.riskLevel}`)}
        </Badge>
        <StatusBadge tone={pendingStatusTone(action.status)}>
          {t(`governance.approvals.status.${action.status}`)}
        </StatusBadge>
      </span>
      <p className="argus-approval-item__summary">{action.summary}</p>
      <span className="argus-approval-item__meta">
        <span>{action.createdByName ?? action.createdBy}</span>
        <span>·</span>
        <span>
          {action.conversationId
            ? t("governance.approvals.source.chatbox")
            : t("governance.approvals.source.admin")}
        </span>
        <span>·</span>
        <span>{formatDateTime(action.createdAt, locale)}</span>
        {open && (
          <>
            <span>·</span>
            <span
              className={`argus-approval-item__countdown${expired ? " is-expired" : ""}`}
            >
              <Clock aria-hidden size={11} />
              {expired
                ? t("governance.approvals.expired")
                : formatCountdown(action.expiresAt, now)}
            </span>
          </>
        )}
      </span>
    </button>
  );
}
