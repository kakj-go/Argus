# Argus contract catalog

M0 freezes shared contracts without implementing domain services or migrating
the handwritten frontend API client.

| Authority | Scope | Generated consumers |
| --- | --- | --- |
| `openapi/argus.yaml` | Browser and external REST/SSE DTOs | Domain-split Go OpenAPI models, `@argus/api-client/contracts` |
| `schemas/` | Runtime-validated labels, actions, cards, agent context, streams, telemetry queries | OpenAPI references and contract validators |
| `proto/` | Internal RPC, Connector stream, trusted telemetry identity | Go protobuf/gRPC packages |
| `contracts/` | Error/state registries and semantic rules not expressible in Schema | Contract tests and later domain implementations |

Public JSON uses `snake_case`. Server-only action records are split into the
PendingAction lifecycle record, immutable plan record, and one-time Token
Record. These records, Worker records, and model projection records are never
referenced by the TypeScript generation entry point.
