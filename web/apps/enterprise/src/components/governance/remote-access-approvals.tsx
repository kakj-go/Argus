import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type AccessRequest } from "@argus/api-client";
import { Button, Card, CardContent, CardHeader, EmptyState, Field, Input, StatusBadge } from "@argus/ui";

export function RemoteAccessApprovals() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [comments, setComments] = useState<Record<string, string>>({});
  const requests = useQuery({ queryKey: ["remote-access", "approval-requests"], queryFn: () => api.remoteAccess.listRequests() });
  const decide = useMutation({
    mutationFn: ({ request, requirementId, decision }: { request: AccessRequest; requirementId: string; decision: "approve" | "reject" }) => api.remoteAccess.decideRequest(request.id, { requirement_id: requirementId, decision, comment: comments[request.id]?.trim() || undefined }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["remote-access"] }),
  });
  const pending = (requests.data?.items ?? []).filter((request) => request.status === "awaiting_approval");
  return <Card>
    <CardHeader title={t("remoteAccess.approvalsTitle")} />
    <CardContent>
      {pending.length === 0 ? <EmptyState description="" title={t("remoteAccess.noApprovals")} /> : <div className="argus-remote-approval-list">
        {pending.map((request) => <section className="argus-remote-approval" key={request.id}>
          <div><strong>{request.protocol === "ssh" ? "SSH PTY" : "WinRS PowerShell"}</strong><StatusBadge tone="warning">{request.status}</StatusBadge></div>
          <p>{request.reason}</p>
          <small>{t("remoteAccess.hostAndAccount", { host: request.host_id, account: request.managed_account_id })}</small>
          {request.requirements.map((requirement) => <div className="argus-remote-approval__requirement" key={requirement.id}>
            <span>{t("remoteAccess.policyProgress", { policy: requirement.policy_id, approved: requirement.approved_count, required: requirement.minimum_approvals })}</span>
            {requirement.require_mfa ? <StatusBadge tone="danger">{t("remoteAccess.mfaFailClosed")}</StatusBadge> : requirement.status === "pending" ? <>
              <Field label={t("remoteAccess.decisionComment")}><Input onChange={(event) => setComments((value) => ({ ...value, [request.id]: event.target.value }))} value={comments[request.id] ?? ""} /></Field>
              <div className="argus-settings-inline-actions"><Button loading={decide.isPending} onClick={() => decide.mutate({ request, requirementId: requirement.id, decision: "approve" })} size="sm" variant="primary">{t("remoteAccess.approve")}</Button><Button loading={decide.isPending} onClick={() => decide.mutate({ request, requirementId: requirement.id, decision: "reject" })} size="sm" variant="danger">{t("remoteAccess.reject")}</Button></div>
            </> : <StatusBadge tone="neutral">{requirement.status}</StatusBadge>}
          </div>)}
        </section>)}
      </div>}
    </CardContent>
  </Card>;
}
