# OpenAPI contracts

`argus.yaml` is the OpenAPI 3.1 entry point for browser and external HTTP
contracts. Public JSON fields use `snake_case`, and the first version is served
under `/api/v1`.

The source is split by domain under `components/`. Runtime documents such as
label selectors, pending actions, cards, agent events, and telemetry queries
are owned by JSON Schema under `api/schemas` and referenced from OpenAPI.

`generation/` contains reference-only manifests that partition the unified
authority into common, identity, authorization, labels, action, card, agent,
stream, and telemetry generated packages. They do not redefine DTO fields.

Generated Go and TypeScript files are derived artifacts. Do not edit them or
use them as the source of business rules.

Dynamic public JSON is recursively validated by JSON Schema. The TypeScript
generator intentionally maps it to `unknown`, so callers must narrow data only
after the corresponding runtime schema has accepted it.
