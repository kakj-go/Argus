import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type SandboxImage } from "@argus/api-client";
import {
  Badge,
  Button,
  DataTable,
  Field,
  FormDrawer,
  Input,
  Spinner,
  StatusBadge,
  Switch,
} from "@argus/ui";

type ImageRow = {
  id: string;
  name: string;
  reference: string;
  digest: string;
  languages: string;
  scanStatus: SandboxImage["scanStatus"];
  signatureStatus: SandboxImage["signatureStatus"];
  enabled: boolean;
};

function scanTone(status: SandboxImage["scanStatus"]) {
  if (status === "passed") return "success" as const;
  if (status === "pending") return "warning" as const;
  return "danger" as const;
}

function signatureTone(status: SandboxImage["signatureStatus"]) {
  if (status === "verified") return "success" as const;
  if (status === "unsigned") return "warning" as const;
  return "danger" as const;
}

/** 镜像 Tab：登记镜像（digest 固定）+ 启停 Switch。 */
export function ImagesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [reference, setReference] = useState("");
  const [digest, setDigest] = useState("");
  const [languages, setLanguages] = useState("");

  const images = useQuery({
    queryKey: ["platform", "images"],
    queryFn: () => api.platform.images.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "images"] });

  const create = useMutation({
    mutationFn: () =>
      api.platform.images.create({
        name: name.trim(),
        reference: reference.trim(),
        digest: digest.trim(),
        languages: languages
          .split(",")
          .map((entry) => entry.trim())
          .filter(Boolean)
          .map((entry) => {
            const [lang, version] = entry.split(":");
            return {
              name: (lang ?? "").trim(),
              version: (version ?? "").trim(),
            };
          }),
      }),
    onSuccess: () => {
      setCreateOpen(false);
      setName("");
      setReference("");
      setDigest("");
      setLanguages("");
      void invalidate();
    },
  });

  const setEnabled = useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.platform.images.setEnabled(input.id, input.enabled),
    onSuccess: () => void invalidate(),
  });

  const rows: ImageRow[] = (images.data ?? []).map((item) => ({
    id: item.id,
    name: item.name,
    reference: item.reference,
    digest: item.digest,
    languages: item.languages
      .map((lang) => `${lang.name} ${lang.version}`)
      .join(", "),
    scanStatus: item.scanStatus,
    signatureStatus: item.signatureStatus,
    enabled: item.enabled,
  }));

  return (
    <div className="argus-platform-stack">
      <div className="argus-tab-toolbar">
        <Button onClick={() => setCreateOpen(true)} variant="primary">
          {t("sandbox.images.add")}
        </Button>
      </div>

      {images.isPending ? (
        <Spinner />
      ) : (
        <DataTable<ImageRow>
          columns={[
            { key: "name", header: t("sandbox.images.table.name") },
            {
              key: "reference",
              header: t("sandbox.images.table.reference"),
              render: (row) => (
                <code className="argus-mono">{row.reference}</code>
              ),
            },
            {
              key: "digest",
              header: t("sandbox.images.table.digest"),
              render: (row) => <code className="argus-mono">{row.digest}</code>,
            },
            {
              key: "languages",
              header: t("sandbox.images.table.languages"),
              render: (row) => <Badge>{row.languages}</Badge>,
            },
            {
              key: "scanStatus",
              header: t("sandbox.images.table.scan"),
              render: (row) => (
                <StatusBadge tone={scanTone(row.scanStatus)}>
                  {t(`sandbox.images.scan.${row.scanStatus}`)}
                </StatusBadge>
              ),
            },
            {
              key: "signatureStatus",
              header: t("sandbox.images.table.signature"),
              render: (row) => (
                <StatusBadge tone={signatureTone(row.signatureStatus)}>
                  {t(`sandbox.images.signature.${row.signatureStatus}`)}
                </StatusBadge>
              ),
            },
            {
              key: "enabled",
              header: t("sandbox.images.table.enabled"),
              render: (row) => (
                <Switch
                  checked={row.enabled}
                  label={t("sandbox.images.table.enabled")}
                  onChange={(checked) =>
                    setEnabled.mutate({ id: row.id, enabled: checked })
                  }
                />
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <FormDrawer
        loading={create.isPending}
        onOpenChange={setCreateOpen}
        onSubmit={() => create.mutate()}
        open={createOpen}
        submitLabel={t("common.create")}
        title={t("sandbox.images.add")}
      >
        <Field label={t("sandbox.images.form.name")}>
          <Input
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </Field>
        <Field label={t("sandbox.images.form.reference")}>
          <Input
            onChange={(event) => setReference(event.target.value)}
            placeholder="registry.argus.local/sandbox/…"
            required
            value={reference}
          />
        </Field>
        <Field label={t("sandbox.images.form.digest")}>
          <Input
            onChange={(event) => setDigest(event.target.value)}
            placeholder="sha256:…"
            required
            value={digest}
          />
        </Field>
        <Field
          hint={t("sandbox.images.form.languagesHint")}
          label={t("sandbox.images.form.languages")}
        >
          <Input
            onChange={(event) => setLanguages(event.target.value)}
            placeholder="python:3.13"
            required
            value={languages}
          />
        </Field>
      </FormDrawer>
    </div>
  );
}
