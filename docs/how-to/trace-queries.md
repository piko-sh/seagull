---
title: Name and trace generated queries
description: Enable go.query_names and wrap the generated DBTX interface to attach
  the lookup result to a query span or log entry. Keep parameter values out of logs
  unless the application explicitly permits them.
section: How-to guides
subsection: Connections
order: 560
---

# Name and trace generated queries

Wrap the generated `DBTX` to attach a query name to a log entry or span. The application wrapper owns query timing and tracing; generator observers measure generation, not application database calls.

## Generate the query-name lookup

Enable the lookup inside the database's `go` configuration and regenerate:

```yaml
go:
  package: store
  out: internal/store
  query_names:
    enabled: true
```

The generated `QueryName(sql)` lookup recognises the SQL passed by generated methods, including runtime-shaped queries and batch statements.

## Trace calls through a DBTX wrapper

Implement the generated interface on a wrapper that records the name and forwards each operation to its inner connection. The database/sql example forwards an execution like this:

```go
func (t tracer) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	t.trace(query)
	return t.inner.ExecContext(ctx, query, arguments...)
}
```

Its `trace` method obtains the name with `store.QueryName(query)`. Forward `QueryContext` and `QueryRowContext` in the same way, then construct `Queries` from the wrapper:

```go
	queries := store.New(tracer{inner: database, output: output, scope: "db"})
```

Generated calls now pass through the tracer. Keep parameter values out of logs unless the application permits them.

## Preserve tracing in a transaction

With database/sql, add `WrapDBTX(inner any) any` so `WithTx` and `RunInTx` can wrap their `*sql.Tx`:

```go
func (t tracer) WrapDBTX(inner any) any {
	connection, ok := inner.(store.DBTX)
	if !ok {
		return inner
	}
	return tracer{inner: connection, output: t.output, scope: "tx"}
}
```

The transaction-bound queries now pass through the wrapper too. Without this method, those calls use the bare transaction and bypass the trace. With pgx, wrap the transaction yourself before passing it to `WithTx`.

See [wrapping a transaction](../reference/generated-go.md#wrapping-a-transaction-wrapdbtx) for the hook contract and the [complete tracer](../../examples/sqlite/026_tracing_queries/example.go) for its fields and forwarding methods.

The [working example](../../examples/sqlite/026_tracing_queries) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/026_tracing_queries
go test .
```
