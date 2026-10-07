---
title: Route reads to a replica
description: "With database/sql generation, construct Queries using NewWithReplica(writer, reader)."
section: How-to guides
subsection: Connections
order: 540
---

# Route reads to a replica

With database/sql, give generated queries a writer and a reader to send suitable reads to a replica. Review which workflows can tolerate replica lag before routing their reads away from the primary.

## Supply the writer and reader

Open the application connections, then pass them to the generated constructor:

```go
queries := store.NewWithReplica(writer, reader)
```

The [working caller](../../examples/sqlite/024_read_replicas/example.go) wraps both connections to show where each statement runs. `NewWithReplica` comes from the database/sql emitter: `go.driver: database/sql` in `sgl.yaml`, or `emit_driver_databasesql` from `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` when generating from Go, where it is the default. The pgx and native ClickHouse emitters, `emit_driver_pgx` and `emit_driver_clickhouse`, do not generate it.

Read-only queries use the reader. Writes, writing CTEs, row-locking SELECTs and calls to functions that may modify data use the writer. An unknown function also sends the query to the writer and produces Q005. A function counts as modifying data when any statement of its body writes, or when a procedural body runs dynamic SQL with `EXECUTE` or starts a statement such as `CALL`, `COPY` or `NOTIFY`; mark one that only reads with `sgl.migration(readonly: true)` above its `CREATE FUNCTION`. See [reader and writer routing](../reference/directives.md#reader-and-writer-routing) for the complete analysis rules.

## Keep consistency-sensitive reads on the writer

A committed write does not guarantee that a replica has caught up. If a read must observe the primary's state, use writer-bound queries or set `readonly: false` on that query:

```sql
-- sgl.query(CountSightings, one, readonly: false)
SELECT COUNT(*) AS total FROM sightings;
```

Regenerate after changing the directive. `CountSightings` now uses the writer even though its SQL is a SELECT. `readonly: true` overrides the analysis in the other direction, so use it only after reviewing the statement's effects and the replica's permissions.

## Keep a transaction on its connection

Use the transaction-bound `Queries` for every operation in a transaction. `WithTx` binds both sides to that transaction; replica routing does not split a transaction across connections. Follow the [transaction guide](transactions.md) when a read and write must share a transaction.

The [replica explanation](../explanation/replicas.md) covers the conservative routing choice and its consistency limits. Test with the actual deployment roles; a local writable connection does not exercise a replica's restrictions.

The [working example](../../examples/sqlite/024_read_replicas) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/024_read_replicas
go test .
```
