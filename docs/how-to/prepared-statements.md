---
title: Use prepared statements
description: Enable go.prepared, regenerate, call store.Prepare and construct Queries
  from the returned PreparedDBTX. Close the prepared wrapper when its owner shuts
  down.
section: How-to guides
subsection: Connections
order: 550
---

# Use prepared statements

With database/sql, generate and initialise a prepared-statement wrapper when the application should reuse its static statements. The wrapper's owner closes it when those statements are no longer needed.

## Enable prepared-file generation

Set `prepared: true` inside the database's `go` configuration, then run `sgl generate`:

```yaml
go:
  package: store
  out: internal/store
  prepared: true
```

This emits `prepared.go`; it does not prepare a statement at generation time. The CLI defaults this setting to false. A program that generates from Go uses the database/sql emitter, `emit_driver_databasesql` from `seagull.sh/seagull/sdk/emit/emit_driver_databasesql`, which is also the default when `seagull.New` is given no emitter; it writes the file by default unless built with `emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false))`.

## Prepare and use the wrapper

Call `store.Prepare` with the application's database and construct `Queries` from the returned value:

```go
	prepared, err := store.Prepare(ctx, database)
	if err != nil {
		return fmt.Errorf("preparing queries: %w", err)
	}
	defer func() { _ = prepared.Close() }()
```

```go
	queries := store.New(prepared)
```

A preparation error stops setup before the wrapper is used. `Prepare` has already closed the statements it prepared before the failing one, and joins any error closing them to the one it returns, so there is no wrapper to close. Static methods then use their prepared statements. Queries whose SQL changes at runtime, including batches and runtime builders, go directly to the underlying connection.

## Keep preparation inside transactions

Use `WithTx` or `RunInTx` on this `Queries` value. The wrapper binds its statements to the transaction; `RunInTx` needs the underlying `*sql.DB`:

```go
	err = queries.RunInTx(ctx, prepared.UnderlyingDB(), func(inTransaction *store.Queries) error {
		return loadTideTimes(ctx, inTransaction)
	})
	if err != nil {
		return fmt.Errorf("loading tide times: %w", err)
	}
```

The callback uses the transaction-bound value as in the [transaction guide](transactions.md). Closing the prepared wrapper releases the statements; it does not transfer ownership of the application's database.

`go.prepared` is available only for database/sql on engines whose driver can prepare the generated statements. It is a configuration error with pgx or native ClickHouse, and with ClickHouse through database/sql. See [prepared.go](../reference/generated-go.md#preparedgo) for exclusions and signatures.

The [working example](../../examples/sqlite/025_prepared_statements) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/025_prepared_statements
go test .
```
