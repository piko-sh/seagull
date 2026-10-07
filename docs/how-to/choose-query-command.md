---
title: Choose a query command
description: "Choose the return value your caller needs before annotating the statement."
section: How-to guides
subsection: Queries
order: 320
---

# Choose a query command

Choose the command by what the caller needs to receive. A statement that returns rows needs a row-returning command; a write without rows usually needs `exec`.

## Match the command to the result

| Caller needs | Command | Result |
|---|---|---|
| One row | `one` | A row and an error; no match returns the driver's no-rows error. |
| All matching rows | `many` | A slice and an error. |
| Success or failure of a write | `exec` | An error. |
| Affected-row count | `execrows` | A count and an error. |
| The driver's execution result | `execresult` | The result and an error. |

Put the name and command in the query header. This example reads one boat:

```sql
-- sgl.query(GetBoat, one)
SELECT id, name, berth, at_sea FROM boats WHERE id = ?;
```

Run `sgl vet` and `sgl generate`, then call the generated method:

```go
boat, err := queries.GetBoat(ctx, id)
```

Check the error before using `boat`. Handle the no-rows error as a missing boat, or use [optional single-row results](optional-one.md) when the caller should receive a presence flag instead.

## Choose a different execution pattern when needed

Use [stream](stream-rows.md) to process rows without buffering them, or [batch and copyfrom](bulk-writes.md) to send a slice of inputs. For a queued ClickHouse mutation, use [asyncexec](async-mutations.md) and account for its completion semantics.

Do not depend on `LastInsertId` across drivers. pgx returns a `CommandTag` for `execresult`; ClickHouse refuses both `execrows` and `execresult` because its driver reports no affected-row count (Q059). The [query commands reference](../reference/query-commands.md) lists each signature and emitter restriction.

The [working example](../../examples/sqlite/002_commands) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/002_commands
go test .
```
