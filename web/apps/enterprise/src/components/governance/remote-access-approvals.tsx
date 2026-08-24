import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  formConstraint,
  presentApiFormError,
  useApi,
  type AccessRequest,
} from "@argus/api-client";
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  Field,
  Input,
  StatusBadge,
} from "@argus/ui";

const decisionCommentConstraint = formConstraint(
  "RemoteAccessDecisionCreate",
  "comment",
);

type Requirement = AccessRequest["requirements"][number];

export function RemoteAccessApprovals() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const requests = useQuery({
    queryKey: ["remote-access", "approval-requests"],
    queryFn: () => api.remoteAccess.listRequests(),
  });
  const hosts = useQuery({
    queryKey: ["hosts", "remote-access-options"],
    queryFn: () => api.hosts.list(),
  });
  const accounts = useQuery({
    queryKey: ["managed-accounts", "remote-access-options"],
    queryFn: () => api.secrets.listManagedAccounts(),
  });
  const policies = useQuery({
    queryKey: ["remote-access", "policies"],
    queryFn: () => api.remoteAccess.listPolicies(),
  });
  const decide = useMutation({
    mutationFn: ({
      requestId,
      requirementId,
      decision,
      comment,
    }: {
      requestId: string;
      requirementId: string;
      decision: "approve" | "reject";
      comment?: string;
    }) =>
      api.remoteAccess.decideRequest(requestId, {
        requirement_id: requirementId,
        decision,
        comment,
      }),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["remote-access"] }),
  });
  const pending = (requests.data?.items ?? []).filter(
    (request) => request.status === "awaiting_approval",
  );
  const unavailable = t("remoteAccess.unavailableReference");
  const hostName = (id: string) =>
    hosts.data?.items.find((item) => item.id === id)?.name ?? unavailable;
  const accountName = (id: string) =>
    accounts.data?.find((item) => item.id === id)?.username ?? unavailable;
  const policyName = (id: string) =>
    policies.data?.items.find((item) => item.id === id)?.name ?? unavailable;

  return (
    <Card>
      <CardHeader title={t("remoteAccess.approvalsTitle")} />
      <CardContent>
        {pending.length === 0 ? (
          <EmptyState description="" title={t("remoteAccess.noApprovals")} />
        ) : (
          <div className="argus-remote-approval-list">
            {pending.map((request) => (
              <section className="argus-remote-approval" key={request.id}>
                <div>
                  <strong>
                    {request.protocol === "ssh"
                      ? "SSH PTY"
                      : "WinRS PowerShell"}
                  </strong>
                  <StatusBadge tone="warning">
                    {t(`remoteAccess.requestStatuses.${request.status}`)}
                  </StatusBadge>
                </div>
                <p>{request.reason}</p>
                <small>
                  {t("remoteAccess.hostAndAccount", {
                    host: hostName(request.host_id),
                    account: accountName(request.managed_account_id),
                  })}
                </small>
                {request.requirements.map((requirement) => (
                  <div
                    className="argus-remote-approval__requirement"
                    key={requirement.id}
                  >
                    <span>
                      {t("remoteAccess.policyProgress", {
                        policy: policyName(requirement.policy_id),
                        approved: requirement.approved_count,
                        required: requirement.minimum_approvals,
                      })}
                    </span>
                    {requirement.require_mfa ? (
                      <StatusBadge tone="danger">
                        {t("remoteAccess.mfaFailClosed")}
                      </StatusBadge>
                    ) : requirement.status === "pending" ? (
                      <RemoteAccessDecisionForm
                        loading={decide.isPending}
                        onSubmit={(value) =>
                          decide.mutateAsync({
                            ...value,
                            requestId: request.id,
                            requirementId: requirement.id,
                          })
                        }
                        requirement={requirement}
                      />
                    ) : (
                      <StatusBadge tone="neutral">
                        {t(
                          `remoteAccess.requirementStatuses.${requirement.status}`,
                        )}
                      </StatusBadge>
                    )}
                  </div>
                ))}
              </section>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function RemoteAccessDecisionForm({
  loading,
  onSubmit,
  requirement,
}: {
  loading: boolean;
  onSubmit: (value: {
    comment?: string;
    decision: "approve" | "reject";
  }) => Promise<unknown>;
  requirement: Requirement;
}) {
  const { t } = useTranslation();
  const schema = z.object({
    comment: z
      .string()
      .trim()
      .max(decisionCommentConstraint.maxLength ?? 2048),
  });
  type DecisionForm = z.infer<typeof schema>;
  const form = useForm<DecisionForm>({
    resolver: zodResolver(schema),
    defaultValues: { comment: "" },
  });
  const submit = form.handleSubmit(async (value, event) => {
    form.clearErrors();
    const submitter = (event?.nativeEvent as SubmitEvent | undefined)
      ?.submitter;
    const decision =
      submitter instanceof HTMLButtonElement && submitter.value === "reject"
        ? "reject"
        : "approve";
    try {
      await onSubmit({
        comment: value.comment || undefined,
        decision,
      });
      form.reset();
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("remoteAccess.decisionFailed"),
        fieldMap: { comment: "comment" },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          form.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          form.setError("root", { message, type: "server" }),
      });
    }
  });

  return (
    <form
      aria-label={`${t("remoteAccess.decisionComment")} ${requirement.id}`}
      onSubmit={submit}
    >
      {form.formState.errors.root?.message && (
        <Alert
          description={form.formState.errors.root.message}
          title={t("remoteAccess.decisionFailed")}
          tone="danger"
        />
      )}
      <Field
        error={form.formState.errors.comment?.message}
        requirement="optional"
        label={t("remoteAccess.decisionComment")}
      >
        <Input
          {...form.register("comment")}
          maxLength={decisionCommentConstraint.maxLength}
        />
      </Field>
      <div className="argus-settings-inline-actions">
        <Button
          loading={loading}
          size="sm"
          type="submit"
          value="approve"
          variant="primary"
        >
          {t("remoteAccess.approve")}
        </Button>
        <Button
          loading={loading}
          size="sm"
          type="submit"
          value="reject"
          variant="danger"
        >
          {t("remoteAccess.reject")}
        </Button>
      </div>
    </form>
  );
}
