import { useMutation, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  presentApiFormError,
  useApi,
  type Department,
} from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  Field,
  FormDrawer,
  Input,
  StatusBadge,
} from "@argus/ui";
import { useOrgDepartments, useOrgUsers } from "./org-users-tab";

type DepartmentRow = {
  id: string;
  name: string;
  description: string;
  is_default: boolean;
  member_count: number;
  status: Department["status"];
};

export function OrgDepartmentsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const departments = useOrgDepartments();
  const users = useOrgUsers();
  const [editing, setEditing] = useState<Department | null | undefined>(
    undefined,
  );
  const [statusTarget, setStatusTarget] = useState<DepartmentRow | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });
  const save = useMutation({
    mutationFn: (input: { id?: string; name: string; description?: string }) =>
      input.id
        ? api.org.updateDepartment(input.id, input)
        : api.org.createDepartment(input),
    onSuccess: () => {
      setEditing(undefined);
      void invalidate();
    },
  });
  const toggleStatus = useMutation({
    mutationFn: async (department: DepartmentRow) => {
      if (department.status === "active") {
        await api.org.deleteDepartment(department.id);
      } else {
        await api.org.updateDepartment(department.id, { status: "active" });
      }
    },
    onSuccess: () => {
      setStatusTarget(null);
      void invalidate();
    },
  });
  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.departments")}
        </h2>
        <Button onClick={() => setEditing(null)} size="sm" variant="primary">
          {t("settings.org.departments.create")}
        </Button>
      </div>
      <DataTable<DepartmentRow>
        columns={[
          {
            key: "name",
            header: t("settings.common.name"),
            render: (row) => (
              <span>
                {row.name}{" "}
                {row.is_default && (
                  <Badge>{t("settings.org.departments.default")}</Badge>
                )}
              </span>
            ),
          },
          { key: "description", header: t("settings.common.description") },
          { key: "member_count", header: t("settings.common.members") },
          {
            key: "status",
            header: t("settings.common.status"),
            render: (row) => (
              <StatusBadge
                tone={row.status === "active" ? "success" : "neutral"}
              >
                {t(`settings.common.${row.status}`)}
              </StatusBadge>
            ),
          },
          {
            key: "actions",
            header: t("settings.common.actions"),
            render: (row) => (
              <span className="argus-settings-inline-actions">
                <Button
                  onClick={() =>
                    setEditing(
                      departments.data?.find(
                        (department) => department.id === row.id,
                      ) ?? null,
                    )
                  }
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.common.edit")}
                </Button>
                <Button
                  disabled={
                    row.status === "active" &&
                    (row.is_default || row.member_count > 0)
                  }
                  onClick={() => setStatusTarget(row)}
                  size="sm"
                  title={
                    row.status === "active" && row.is_default
                      ? t("settings.org.departments.defaultLocked")
                      : row.status === "active" && row.member_count > 0
                        ? t("settings.org.departments.membersLocked")
                        : undefined
                  }
                  variant="ghost"
                >
                  {row.status === "active"
                    ? t("settings.org.departments.disable")
                    : t("settings.org.departments.enable")}
                </Button>
              </span>
            ),
          },
        ]}
        data={(departments.data ?? []).map((department) => ({
          id: department.id,
          name: department.name,
          description: department.description ?? "",
          is_default: department.is_default,
          status: department.status,
          member_count: (users.data ?? []).filter(
            (user) => user.department_id === department.id,
          ).length,
        }))}
        getRowKey={(row) => row.id}
      />
      {editing !== undefined && (
        <DepartmentDrawer
          department={editing}
          loading={save.isPending}
          onClose={() => setEditing(undefined)}
          onSubmit={(input) => save.mutateAsync({ ...input, id: editing?.id })}
        />
      )}
      <ConfirmDialog
        danger={statusTarget?.status === "active"}
        description={
          statusTarget?.status === "active"
            ? t("settings.org.departments.disableDescription")
            : t("settings.org.departments.enableDescription")
        }
        loading={toggleStatus.isPending}
        onConfirm={() => statusTarget && toggleStatus.mutate(statusTarget)}
        onOpenChange={(open) => !open && setStatusTarget(null)}
        open={statusTarget !== null}
        title={
          statusTarget?.status === "active"
            ? t("settings.org.departments.disableTitle")
            : t("settings.org.departments.enableTitle")
        }
      />
    </div>
  );
}

function DepartmentDrawer({
  department,
  loading,
  onClose,
  onSubmit,
}: {
  department: Department | null;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: { name: string; description?: string }) => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const departmentSchema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t("settings.common.required")),
        description: z.string().trim(),
      }),
    [t],
  );
  type DepartmentForm = z.infer<typeof departmentSchema>;
  const {
    clearErrors,
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<DepartmentForm>({
    resolver: zodResolver(departmentSchema),
    defaultValues: {
      name: department?.name ?? "",
      description: department?.description ?? "",
    },
  });
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        name: values.name,
        description: values.description || undefined,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: { description: "description", name: "name" },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          setError(field, { message, type: "server" }, { shouldFocus: true }),
        setFormError: (message) =>
          setError("root", { message, type: "server" }),
      });
    }
  });
  return (
    <FormDrawer
      loading={loading}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={submit}
      open
      title={
        department
          ? t("settings.org.departments.editTitle")
          : t("settings.org.departments.create")
      }
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          requirement="required"
          error={errors.name?.message}
          label={t("settings.common.name")}
        >
          <Input {...register("name")} required />
        </Field>
        <Field requirement="optional" label={t("settings.common.description")}>
          <Input {...register("description")} />
        </Field>
      </div>
    </FormDrawer>
  );
}
