import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type Project } from "@argus/api-client";
import { Badge, Button, DataTable, EmptyState, Field, FormDrawer, Input, Spinner } from "@argus/ui";
import { useOrgProjects } from "./org-users-tab";
import { formatDateTime } from "./shared";

export function OrgProjectsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const projects = useOrgProjects();
  const [editing, setEditing] = useState<Project | null | undefined>(undefined);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["org", "projects"] });
  const save = useMutation({
    mutationFn: (input: { id?: string; name: string; description?: string }) =>
      input.id ? api.org.updateProject(input.id, input) : api.org.createProject(input),
    onSuccess: () => { setEditing(undefined); void invalidate(); },
  });
  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">{t("settings.org.tabs.projects")}</h2>
        <Button onClick={() => setEditing(null)} size="sm" variant="primary">{t("settings.org.projectsTab.create")}</Button>
      </div>
      {projects.isPending ? (
        <Spinner />
      ) : (projects.data ?? []).length === 0 ? (
        <EmptyState description="" title={t("settings.org.projectsTab.empty")} />
      ) : (
        <DataTable<Project & Record<string, unknown>>
          columns={[
            { key: "name", header: t("settings.common.name"), render: (row) => <span>{row.name} {row.default && <Badge>{t("settings.org.projectsTab.default")}</Badge>}</span> },
            { key: "description", header: t("settings.common.description") },
            { key: "createdAt", header: t("settings.common.createdAt"), render: (row) => formatDateTime(row.createdAt) },
            {
              key: "actions", header: t("settings.common.actions"), render: (row) => (
                <Button onClick={() => setEditing(row)} size="sm" variant="ghost">{t("settings.common.edit")}</Button>
              ),
            },
          ]}
          data={(projects.data ?? []) as Array<Project & Record<string, unknown>>}
          getRowKey={(row) => row.id}
        />
      )}
      {editing !== undefined && <ProjectDrawer loading={save.isPending} onClose={() => setEditing(undefined)} onSubmit={(input) => save.mutate({ ...input, id: editing?.id })} project={editing} />}
    </div>
  );
}

function ProjectDrawer({ project, loading, onClose, onSubmit }: {
  project: Project | null;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: { name: string; description?: string }) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(project?.name ?? "");
  const [description, setDescription] = useState(project?.description ?? "");
  return (
    <FormDrawer loading={loading} onOpenChange={(open) => !open && onClose()} onSubmit={() => onSubmit({ name, description: description || undefined })} open title={project ? t("settings.org.projectsTab.editTitle") : t("settings.org.projectsTab.create")}>
      <div className="argus-settings-form">
        <Field label={t("settings.common.name")}><Input onChange={(event) => setName(event.target.value)} required value={name} /></Field>
        <Field label={t("settings.common.description")}><Input onChange={(event) => setDescription(event.target.value)} value={description} /></Field>
      </div>
    </FormDrawer>
  );
}
