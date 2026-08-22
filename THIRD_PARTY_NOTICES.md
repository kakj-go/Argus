# Third-Party Notices

## Prometheus PromQL Parser

Argus uses the PromQL parser and AST packages from Prometheus:

- Project: Prometheus
- Module: `github.com/prometheus/prometheus`
- Version: `v0.314.0`
- Commit: `d7598b7141418fa35be2b5ec5d0fefb634199610`
- License: Apache License 2.0

Copyright 2012-2015 The Prometheus Authors.

The complete upstream license and NOTICE files are included in the locked Go
module and are collected by the release SBOM process. The authoritative parser
lock for this repository is `deploy/query-parsers.lock.json`.

## graph-gophers/graphql-go

Argus uses `github.com/graph-gophers/graphql-go` version `v1.10.2` as the
schema-first runtime for the read-only SkyWalking-compatible trace query
surface. The module is distributed under the MIT License; its complete license
text is retained in the locked Go module and collected by the release SBOM
process.

Argus also uses the executable-document AST parser from
`github.com/graphql-go/graphql` version `v0.8.1` to apply request-specific
field-count, fragment-cycle, read-only and introspection checks before the
fixed SDL runtime executes a query. That module is also distributed under the
MIT License.
