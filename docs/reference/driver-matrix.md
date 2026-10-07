---
title: Dialect and driver matrix
description: The dialect analyses SQL. The emitter chooses the generated Go driver
  API. The driver linked into an application or custom CLI opens the actual database.
section: Reference
subsection: Core
order: 270
---

# Dialect and driver matrix

`go.driver` chooses the emitter and the driver API generated code calls. The application's
Go module must supply that driver. A CLI's linked runtime drivers determine which
connections it can open for migrations and introspection.

| | `database/sql` | `pgx` | `clickhouse` (native) |
|---|---|---|---|
| Dialects | Every dialect | PostgreSQL family | ClickHouse |
| `execresult` | `sql.Result`; refused on ClickHouse | `pgconn.CommandTag` | Refused |
| `batch` | Multi-row INSERT, or one statement per item for a trailing clause | One statement per item in `pgx.Batch` | Native batch or multi-row INSERT |
| `copyfrom` | Multi-row INSERT; returns `error` | COPY; returns `(int64, error)` | Native batch; returns `error` |
| `kind: slice` | Expands an IN list; ClickHouse uses an Array parameter | Binds an array to `= ANY($1)` | Uses an Array parameter |
| `prepared.go` | With `go.prepared: true`, except ClickHouse | Unavailable | Unavailable |
| Runtime builder | Supported | Supported | Supported |
| `NewWithReplica` | Supported | Unavailable | Unavailable |
| `WithTx`, `RunInTx` | Supported | Supported | Unavailable |

The [query-command reference](query-commands.md) defines the required INSERT shapes and
COPY type restrictions. Both ClickHouse emitters refuse `execrows` and `execresult`
([Q059](diagnostics.md#q059)). `asyncexec` is accepted only on ClickHouse.

## Connection settings

MySQL and MariaDB's `github.com/go-sql-driver/mysql` driver needs `parseTime=true` in the
DSN, for example `app:secret@tcp(localhost:3306)/shop?parseTime=true`. Generated code scans
`DATE`, `DATETIME` and `TIMESTAMP` into `time.Time`; without that setting the driver
returns text and the read fails. The generated package comment records this requirement.

## Prepared-statement defaults

`go.prepared` defaults to `false` in `sgl.yaml`. Setting it with pgx, native ClickHouse or
database/sql on ClickHouse is a configuration error; ClickHouse's database/sql driver
prepares only INSERT statements.

The library's `emit_driver_databasesql.New()` writes `prepared.go` by default for
compatibility with existing embedded generators. Pass
`emit_driver_databasesql.WithPrepared(false)` to disable it.
See [emitter selection](generator-api.md#emitters).
