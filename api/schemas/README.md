# Runtime JSON Schemas

These JSON Schema 2020-12 files are authoritative for documents validated at
runtime. OpenAPI references these schemas when the same document crosses the
HTTP boundary. Private schemas remain server-only validation authorities and
must never be referenced by the browser generation entry point.

All public fields use `snake_case`. A breaking change requires a new schema
version; v1 may only receive backward-compatible additions.

`common/public-json.schema.json` is the shared recursive definition for
dynamic public payloads. It rejects private action tokens, credentials,
secrets, commit metadata, and remote-access tickets at any nesting depth.

PendingAction storage has three independent authorities:

- `action/pending-action-private.schema.json` owns identity and lifecycle state
  and stores only `plan_record_id` and `token_record_id` references.
- `action/pending-action-plan.schema.json` owns the immutable, hashed execution
  plan and resolved private parameters.
- `action/pending-action-token.schema.json` owns encrypted one-time Token
  material and its consumption lifecycle.

None of these three server-only records are part of browser TypeScript
generation.
