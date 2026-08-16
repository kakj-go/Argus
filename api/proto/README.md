# Protobuf contracts

This module owns internal RPC, Connector control streams, and trusted
telemetry identity messages. Buf performs lint, code generation, and breaking
checks.

Go and gRPC plugins are pinned in `go.mod` and executed locally through
`go tool`; normal generation does not depend on Buf Schema Registry plugin
availability or rate limits.

Connector and collector tenant identity is derived from mTLS credentials and
server-side registration. Client hello messages intentionally do not contain
an authoritative `enterprise_id`.
