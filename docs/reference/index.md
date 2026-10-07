---
title: Reference
description: Look up syntax, defaults, contracts, support and failure behaviour.
section: Reference
order: 0
---

# Reference

Look up syntax, defaults, contracts, support and failure behaviour.

## Core

- [Configuration](configuration.md)
- [CLI](cli.md)
- [Directives](directives.md)
- [Type mapping](type-mapping.md)
- [Compatibility](compatibility.md)
- [Known limitations](limitations.md)
- [Generator API](generator-api.md)
- [Query commands](query-commands.md)
- [Runtime query builder](runtime-builder.md)
- [Generated Go API](generated-go.md)
- [Output writing API](output-writing.md)
- [Presets](presets.md)
- [Errors and observability](errors-observability.md)
- [Migration and seed files](migration-files.md)
- [Migration runtime](migration-runtime.md)
- [Dialect and driver matrix](driver-matrix.md)

## Dialects

- [Dialect reference](dialects/index.md)
- [SQLite](dialects/sqlite.md)
- [PostgreSQL](dialects/postgres.md)
- [CockroachDB](dialects/cockroachdb.md)
- [TimescaleDB](dialects/timescaledb.md)
- [MySQL](dialects/mysql.md)
- [MariaDB](dialects/mariadb.md)
- [DuckDB](dialects/duckdb.md)
- [ClickHouse](dialects/clickhouse.md)

## Embedding

- [Playground and WASM](playground-wasm.md)
- [SQL model](sql-model.md)

## Extensions

The Go API is the root package, `seagull.sh/seagull`, and the `sdk` kit, with one area per
extension point. Each area declares the contracts a program can implement as ports and
holds its built-in implementations as provider packages named `<area>_<role>_<impl>`,
such as `dialect_engine_postgres` or `migrate_backend_sqlite`. The dialect area has no
port: its built-in engines sit behind an opaque handle.

- [Dialect API](dialect-api.md): `sdk/dialect`
- [Emit API](emit-api.md): `sdk/emit`
- [Migrate API](migrate-api.md): `sdk/migrate`, with the lexical rules a backend splits
  its files under
- [Schema API](schema-api.md): `sdk/schema`
- [Observe API](observe-api.md): `sdk/observe`

## Orientation

- [Glossary](glossary.md)

## Diagnostics

- [Diagnostic codes](diagnostics.md): generated directly from the engine’s registry.
