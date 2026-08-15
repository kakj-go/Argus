import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type Department } from "@argus/api-client";
import { Badge, Button, ConfirmDialog, DataTable, Field, FormDrawer, Input } from "@argus/ui";
import { useOrgDepartments, useOrgUsers } from "./org-users-tab";

export function OrgDepartmentsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const departments = useOrgDepartments();
  const users = useOrgUsers();
  const [editing, setEditing] = useState<Department | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<Department | null>(null);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org"] });
  const save = useMutation({
    mutationFn: (input: { id?: string; name: string; description?: string }) =>
      input.id ? api.org.updateDepartment(input.id, input) : api.org.createDepartment(input),
    onSuccess: () => { setEditing(undefined); void invalidate(); },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.org.deleteDepartment(id),
    onSuccess: () => { setDeleting(null); void invalidate(); },
  });
  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">{t("settings.org.tabs.departments")}</h2>
        <Button onClick={() => setEditing(null)} size="sm" variant="primary">{t("settings.org.departments.create")}</Button>
      </div>
      <DataTable<Department & Record<string, unknown>>
        columns={[
          { key: "name", header: t("settings.common.name"), render: (row) => <span>{row.name} {row.default && <Badge>{t("settings.org.departments.default")}</Badge>}</span> },
          { key: "description", header: t("settings.common.description") },
          { key: "members", header: t("settings.common.members"), render: (row) => String((users.data ?? []).filter((user) => user.departmentId === row.id).length) },
          {
            key: "actions", header: t("settings.common.actions"), render: (row) => (
              <span className="argus-settings-inline-actions">
                <Button onClick={() => setEditing(row)} size="sm" variant="ghost">{t("settings.common.edit")}</Button>
                <Button disabled={row.default} onClick={() => setDeleting(row)} size="sm" variant="ghost">{t("settings.common.delete")}</Button>
              </span>
            ),
          },
        ]}
        data={(departments.data ?? []) as Array<Department & Record<string, unknown>>}
        getRowKey={(row) => row.id}
      />
      {editing !== undefined && <DepartmentDrawer department={editing} loading={save.isPending} onClose={() => setEditing(undefined)} onSubmit={(input) => save.mutate({ ...input, id: editing?.id })} />}
      <ConfirmDialog danger loading={remove.isPending} onConfirm={() => deleting && remove.mutate(deleting.id)} onOpenChange={(open) => !open && setDeleting(null)} open={Boolean(deleting)} title={t("settings.org.departments.deleteTitle")} />
    </div>
  );
}

function DepartmentDrawer({ department, loading, onClose, onSubmit }: {
  department: Department | null;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: { name: string; description?: string }) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(department?.name ?? "");
  const [description, setDescription] = useState(department?.description ?? "");
  return (
    <FormDrawer loading={loading} onOpenChange={(open) => !open && onClose()} onSubmit={() => onSubmit({ name, description: description || undefined })} open title={department ? t("settings.org.departments.editTitle") : t("settings.org.departments.create")}>
      <div className="argus-settings-form">
        <Field label={t("settings.common.name")}><Input onChange={(event) => setName(event.target.value)} required value={name} /></Field>
        <Field label={t("settings.common.description")}><Input onChange={(event) => setDescription(event.target.value)} value={description} /></Field>
      </div>
    </FormDrawer>
  );
}
