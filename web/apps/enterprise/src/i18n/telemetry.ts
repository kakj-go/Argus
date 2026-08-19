export const telemetryZh = {
  telemetry: {
    collector: "Collector",
    metrics: "指标",
    logs: "日志",
    traces: "链路",
    lastHour: "最近 1 小时，查询结果按当前 DataScope 与字段策略裁剪。",
    partial: "结果已裁剪",
    partialDescription: "部分资源或结果因授权范围、预算或数据延迟未返回。",
    queryFailed: "遥测查询不可用",
    queryFailedDescription: "请检查 Collector、数据链路和查询权限后重试。",
    empty: "当前时间范围内没有数据",
    metricsSummary: "当前资源最近一小时的指标时间序列",
  },
};

export const telemetryEn = {
  telemetry: {
    collector: "Collector",
    metrics: "Metrics",
    logs: "Logs",
    traces: "Traces",
    lastHour: "Last hour. Results are filtered by the current DataScope and field policy.",
    partial: "Partial result",
    partialDescription: "Some resources or rows were omitted by authorization, budget, or data lag.",
    queryFailed: "Telemetry query unavailable",
    queryFailedDescription: "Check the Collector, telemetry pipeline, and query permission, then retry.",
    empty: "No data in this time range",
    metricsSummary: "Metric time series for this resource during the last hour",
  },
};
