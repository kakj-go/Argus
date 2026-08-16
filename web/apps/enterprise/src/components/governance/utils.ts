import { useEffect, useState } from "react";
import type { PendingActionStatus, RiskLevel } from "@argus/api-client";
import type { TaskStatus, TaskStepStatus } from "@argus/api-client/provisional";

/** Badge tone per task status. */
export function taskStatusTone(
  status: TaskStatus,
): "neutral" | "accent" | "success" | "warning" | "danger" | "info" {
  switch (status) {
    case "succeeded":
      return "success";
    case "failed":
    case "timed_out":
      return "danger";
    case "cancelled":
      return "neutral";
    case "running":
      return "info";
    case "waiting_approval":
    case "waiting_input":
      return "warning";
    default:
      return "accent";
  }
}

/** Badge tone per pending action status. */
export function pendingStatusTone(
  status: PendingActionStatus,
): "neutral" | "accent" | "success" | "warning" | "danger" | "info" {
  switch (status) {
    case "succeeded":
      return "success";
    case "failed":
    case "rejected":
    case "expired":
      return "danger";
    case "cancelled":
      return "neutral";
    case "awaiting_approval":
      return "warning";
    case "awaiting_confirmation":
      return "accent";
    case "executing":
    case "ready":
      return "info";
    default:
      return "neutral";
  }
}

/** Risk levels ordered most-severe first, for grouping. */
export const RISK_ORDER: RiskLevel[] = [
  "critical",
  "dangerous",
  "write",
  "read",
];

export function riskTone(
  risk: RiskLevel,
): "neutral" | "info" | "warning" | "danger" {
  switch (risk) {
    case "critical":
    case "dangerous":
      return "danger";
    case "write":
      return "warning";
    default:
      return "info";
  }
}

/** Statuses where the action is still open for user input. */
export const OPEN_PENDING_STATUSES: PendingActionStatus[] = [
  "awaiting_confirmation",
  "awaiting_approval",
  "ready",
];

/** Terminal-ish statuses shown as read-only results. */
export const DONE_PENDING_STATUSES: PendingActionStatus[] = [
  "executing",
  "succeeded",
  "failed",
  "cancelled",
  "expired",
  "rejected",
];

export function isTaskActive(status: TaskStatus): boolean {
  return status === "running" || status === "pending";
}

/** Map a step status onto the Timeline item status vocabulary. */
export function stepTimelineStatus(
  status: TaskStepStatus,
): "done" | "current" | "pending" | "danger" {
  switch (status) {
    case "done":
      return "done";
    case "running":
      return "current";
    case "failed":
      return "danger";
    default:
      return "pending";
  }
}

/** Locale-aware date-time formatting (HH:mm:ss for today, with date otherwise). */
export function formatDateTime(
  iso: string | undefined,
  locale: string,
): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  const now = new Date();
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();
  return new Intl.DateTimeFormat(locale, {
    ...(sameDay ? {} : { month: "2-digit", day: "2-digit" }),
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

export function formatDateTimeFull(
  iso: string | undefined,
  locale: string,
): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

/** Human duration between two ISO timestamps (defaults end to now). */
export function formatDuration(
  startIso: string | undefined,
  endIso: string | undefined,
): string {
  if (!startIso) return "—";
  const start = Date.parse(startIso);
  const end = endIso ? Date.parse(endIso) : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return "—";
  const seconds = Math.round((end - start) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

/** mm:ss countdown remaining until an ISO deadline. */
export function formatCountdown(expiresAt: string, now: number): string {
  const remaining = Date.parse(expiresAt) - now;
  const total = Math.max(0, Math.ceil(remaining / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

export function isExpired(expiresAt: string, now: number): boolean {
  return Date.parse(expiresAt) <= now;
}

export function isToday(iso: string): boolean {
  const date = new Date(iso);
  const now = new Date();
  return (
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()
  );
}

/** Ticking clock for countdowns / live durations. */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}
