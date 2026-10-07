---
title: Query commands
description: A query command determines the generated method’s execution pattern and
  return value. The dialect must support the SQL and the selected emitter must support
  the command.
section: Reference
subsection: Core
order: 190
---

# Query commands

The command in `sgl.query(Name, command)` determines how the generated method executes
and what it returns. The dialect must support the SQL and the emitter must support the
command. Static methods take `ctx` followed by their parameters, when present.

| Kind | Return value | Behaviour |
|---|---|---|
| `one` | `(<Name>Row, error)` | One row; a miss returns the driver's no-rows error. |
| `many` | `([]<Name>Row, error)` | All matching rows. |
| `exec` | `error` | Executes without reading result rows. |
| `asyncexec` | `error` | Executes a ClickHouse mutation that completes asynchronously. |
| `execrows` | `(int64, error)` | Affected-row count; refused on ClickHouse. |
| `execresult` | `(sql.Result, error)` or `(pgconn.CommandTag, error)` | Driver result; refused on ClickHouse. |
| `stream` | `iter.Seq2[<Name>Row, error]` | A range-over-func iterator that scans one row at a time. |
| `batch` | `error` | Executes for a slice of `<Name>Params`; see [batch execution](query-commands.md#batch-execution). |
| `copyfrom` | `(int64, error)` with pgx; `error` otherwise | Bulk insertion; see [copying rows](query-commands.md#copying-rows). |

With the preset's `WrapErrors` set, every error a method returns is labelled with the
error prefix and the method's name, such as `sgl: GetUser: ...`, and still matches the
error beneath with `errors.Is`; see
[labelled errors](generated-go.md#labelled-errors-wraperrors).

## Reading and writing rows

A `one` method returns `sql.ErrNoRows`, or `pgx.ErrNoRows` with pgx, when nothing matches.
With `optional: true`, it returns `(<Name>Row, bool, error)` instead: a miss is
`(zero, false, nil)`. See [optional single rows](../how-to/optional-one.md).

A `stream` method can be called as `for row, err := range queries.Name(ctx, params)`.
It scans without buffering the result set; breaking the loop closes the rows. Its return
type is `iter.Seq2[<Name>Row, error]` under the default preset; a preset with
`ModernIdioms` off returns `func(func(<Name>Row, error) bool)`, the same underlying type.
See [streaming rows](../how-to/stream-rows.md).

`execrows` and `execresult` are refused on both ClickHouse emitters
([Q059](diagnostics.md#q059)), whose driver provides neither affected-row counts nor
an insert result. `asyncexec` is accepted only for asynchronous ClickHouse mutations
([Q041](diagnostics.md#q041)); its generated comment explains that acceptance does not
mean completion. See [asynchronous mutations](../how-to/async-mutations.md).

## Batch execution

A `batch` method takes `items []<Name>Params` and returns only `error`. An empty slice
sends nothing. Result rows, including those from `RETURNING`, are discarded;
[Q009](diagnostics.md#q009) warns when the query returns rows.

| Emitter | Execution |
|---|---|
| pgx | Queues one statement per item in a `pgx.Batch`; the statement need not be an `INSERT`. Returns the first failing statement's error, or else the error closing the batch reports, such as a deferred constraint failing at commit. |
| database/sql | Sends chunked multi-row `INSERT`s, or one statement per item when a clause follows `VALUES`. |
| Native ClickHouse | Appends rows through `PrepareBatch` when the row contains only placeholders; otherwise sends chunked multi-row `INSERT`s. |

For database/sql and native ClickHouse, the statement must be an `INSERT` into a named
table, with exactly one `VALUES` row containing every parameter. Other shapes are
[Q060](diagnostics.md#q060). A trailing clause can reference inserted values through
the engine's own form, such as `EXCLUDED.name` or `VALUES(name)`.

The database/sql emitter sends an upsert once per item because a PostgreSQL statement
cannot change the same row twice. Multi-row statements are chunked under the engine's
bind-variable limit; ClickHouse uses typed placeholders and at most 1024 bound values
per statement. A `VALUES` row these chunks repeat that binds more variables than one
statement allows could not be sent even alone, so it is refused when the code is generated
([Q060](diagnostics.md#q060)): insert fewer columns per statement, or move values every
row shares into the SQL. Separate statements can commit separately unless the caller
supplies a transaction; with `WrapErrors`, a failed statement's error names the row it
starts at, counted from zero. See [bulk writes](../how-to/bulk-writes.md).

## Copying rows

A `copyfrom` method also takes `items []<Name>Params`. Its signature and restrictions
depend on the emitter:

| Emitter | Execution | Return value |
|---|---|---|
| pgx | PostgreSQL `COPY FROM STDIN` | `(int64, error)`, including the copied-row count |
| database/sql | Chunked multi-row `INSERT`s | `error` |
| Native ClickHouse | `PrepareBatch` | `error` |

pgx requires an `INSERT` into a named table with an explicit column list and one
`VALUES` row containing only placeholders. No clause may follow it, including
`ON CONFLICT` or `RETURNING` ([Q060](diagnostics.md#q060)). COPY writes values in binary;
types that need per-connection registration, such as `ltree`, composites and enum
arrays, are refused ([Q059](diagnostics.md#q059)). Use `batch` for those cases.

Native ClickHouse requires the same single-row INSERT shape, with only placeholders in
its `VALUES` row (Q060). database/sql uses the multi-row shape of `batch`, but keeps
multi-row chunks even when a clause follows `VALUES`. An upsert that repeats a key
within a chunk can therefore fail on PostgreSQL; use `batch` to send each item separately.
An empty slice sends nothing on any emitter.
