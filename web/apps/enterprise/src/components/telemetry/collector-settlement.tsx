import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import type { CollectorInstance } from "@argus/api-client";
import { Button, StatusBadge } from "@argus/ui";
import { collectorTone } from "../hosts/host-utils";

type CollectorStatus = CollectorInstance["status"];

/** 收敛进行中的状态;到达其余状态即视为终态。 */
const TRANSITIONAL_STATUSES = ["pending_install", "installing", "uninstalling"];

/** 与执行器操作超时(collectorOperationTimeout 2m)对齐的等待上限。 */
const SETTLEMENT_TIMEOUT_MS = 150_000;

export type CollectorSettlementSnapshot = {
  status: CollectorStatus;
  last_operation_error_code?: string;
} | null;

/**
 * 确认提交后的 Collector 收敛面板:轮询状态直到终态,
 * 成功自动关闭;失败/超时停留并展示原因。
 */
export function CollectorSettlementPanel({
  poll,
  onSettled,
  onClose,
}: {
  poll: () => Promise<CollectorSettlementSnapshot>;
  /** 终态回调:converged/uninstalled 视为成功。 */
  onSettled: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const startedAt = useRef(Date.now());
  const settledRef = useRef(false);

  const settlementQuery = useQuery({
    queryKey: ["collector-settlement"],
    queryFn: poll,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status && !TRANSITIONAL_STATUSES.includes(status)) return false;
      return 1500;
    },
    retry: false,
  });

  const snapshot = settlementQuery.data;
  const timedOut = Date.now() - startedAt.current > SETTLEMENT_TIMEOUT_MS;
  const terminal = snapshot?.status && !TRANSITIONAL_STATUSES.includes(snapshot.status);
  const failed =
    Boolean(terminal && snapshot?.status !== "converged" && snapshot?.status !== "uninstalled") ||
    (timedOut && !terminal);

  useEffect(() => {
    if (!terminal || settledRef.current) return;
    settledRef.current = true;
    if (snapshot?.status === "converged" || snapshot?.status === "uninstalled") {
      onSettled();
    }
  }, [terminal, snapshot?.status, onSettled]);

  return (
    <div className="argus-detail-section">
      <h3 className="argus-detail-section__title">
        {t("common.collectorSettlement.title")}
      </h3>
      <p className="argus-muted">
        {t("common.collectorSettlement.description")}
      </p>
      <div className="argus-form-actions">
        <span>
          {t("common.collectorSettlement.status")}{" "}
          {snapshot ? (
            <StatusBadge
              pulse={!terminal}
              tone={collectorTone(snapshot.status)}
            >
              {t(`hosts.collectorStatus.${snapshot.status}`)}
            </StatusBadge>
          ) : (
            "—"
          )}
        </span>
      </div>
      {failed && (
        <div className="argus-form-actions">
          <span className="argus-detail-section__title">
            {snapshot?.last_operation_error_code
              ? t("common.collectorSettlement.failedWith", {
                  code: snapshot.last_operation_error_code,
                })
              : t("common.collectorSettlement.failed")}
          </span>
          {timedOut && !terminal && (
            <p className="argus-muted">
              {t("common.collectorSettlement.timeout")}
            </p>
          )}
        </div>
      )}
      <div className="argus-form-actions">
        <Button onClick={onClose} variant="secondary">
          {t("common.collectorSettlement.close")}
        </Button>
      </div>
    </div>
  );
}
