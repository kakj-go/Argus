import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  formConstraint,
  presentApiFormError,
  useApi,
  type SandboxImage,
} from "@argus/api-client";
import {
  Alert,
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

const imageConstraints = {
  digest: formConstraint("SandboxImageWrite", "digest"),
  name: formConstraint("SandboxImageWrite", "name"),
  reference: formConstraint("SandboxImageWrite", "image_ref"),
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
  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(imageConstraints.name.minLength ?? 1, t("sandbox.form.required"))
      .max(imageConstraints.name.maxLength ?? 128),
    reference: z
      .string()
      .trim()
      .min(
        imageConstraints.reference.minLength ?? 1,
        t("sandbox.form.required"),
      )
      .max(imageConstraints.reference.maxLength ?? 1024),
    digest: z
      .string()
      .trim()
      .regex(/^sha256:[a-f0-9]{64}$/i, t("sandbox.form.digestInvalid")),
    languages: z.string().trim().min(1, t("sandbox.form.required")),
  });
  type ImageFormValues = z.infer<typeof schema>;
  const {
    clearErrors,
    handleSubmit,
    register,
    reset,
    setError,
    formState: { errors },
  } = useForm<ImageFormValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", reference: "", digest: "", languages: "" },
  });

  const images = useQuery({
    queryKey: ["platform", "images"],
    queryFn: () => api.platform.images.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "images"] });

  const create = useMutation({
    mutationFn: (values: ImageFormValues) =>
      api.platform.images.create({
        name: values.name,
        reference: values.reference,
        digest: values.digest,
        languages: values.languages
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
      reset();
      void invalidate();
    },
  });

  const setEnabled = useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.platform.images.setEnabled(input.id, input.enabled),
    onSuccess: () => void invalidate(),
  });
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await create.mutateAsync(values);
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("sandbox.form.saveFailed"),
        fieldMap: {
          digest: "digest",
          image_ref: "reference",
          name: "name",
        },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          setError(field, { message, type: "server" }, { shouldFocus: true }),
        setFormError: (message) =>
          setError("root", { message, type: "server" }),
      });
    }
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
        onOpenChange={(open) => {
          setCreateOpen(open);
          if (!open) reset();
        }}
        onSubmit={submit}
        open={createOpen}
        submitLabel={t("common.create")}
        title={t("sandbox.images.add")}
      >
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("sandbox.form.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          error={errors.name?.message}
          requirement="required"
          label={t("sandbox.images.form.name")}
        >
          <Input
            {...register("name")}
            maxLength={imageConstraints.name.maxLength}
          />
        </Field>
        <Field
          error={errors.reference?.message}
          requirement="required"
          label={t("sandbox.images.form.reference")}
        >
          <Input
            {...register("reference")}
            maxLength={imageConstraints.reference.maxLength}
            placeholder="registry.argus.local/sandbox/…"
          />
        </Field>
        <Field
          error={errors.digest?.message}
          requirement="required"
          label={t("sandbox.images.form.digest")}
        >
          <Input {...register("digest")} placeholder="sha256:…" />
        </Field>
        <Field
          error={errors.languages?.message}
          requirement="required"
          hint={t("sandbox.images.form.languagesHint")}
          label={t("sandbox.images.form.languages")}
        >
          <Input {...register("languages")} placeholder="python:3.13" />
        </Field>
      </FormDrawer>
    </div>
  );
}
