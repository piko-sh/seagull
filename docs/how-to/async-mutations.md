---
title: Run asynchronous ClickHouse mutations
description: "Use asyncexec for a supported ALTER UPDATE or ALTER DELETE mutation."
section: How-to guides
subsection: Dialects
order: 660
---

# Run asynchronous ClickHouse mutations

Use `asyncexec` for ClickHouse `ALTER TABLE UPDATE` or `DELETE`. A successful return means the server accepted the mutation; your workflow must decide whether it also needs to wait for completion.

## Declare the queued mutation

In a [configured ClickHouse project](clickhouse.md), put the mutation in a query file and declare `asyncexec`:

```sql
-- sgl.query(RenewLicence, asyncexec)
ALTER TABLE licences
UPDATE expires = addYears(expires, 1), renewals = renewals + 1
WHERE id = {id:UInt64};
```

```sql
-- sgl.query(PurgeExpired, asyncexec)
ALTER TABLE licences
DELETE WHERE expires < {today:Date}
SETTINGS mutations_sync = 2;
```

`PurgeExpired` requests synchronous completion through the server setting; `RenewLicence` leaves waiting to the connection or caller. The [migration](../../examples/clickhouse/036_clickhouse_async_mutations/db/migrations/0001_licences.up.sql) creates the licences table.

## Call the mutation and choose when to read

Generate the package with `sgl generate`, then call the methods through `store.New(database)`. The [example caller](../../examples/clickhouse/036_clickhouse_async_mutations/example.go) renews one licence and purges expired ones:

```go
	if err := queries.RenewLicence(ctx, 1); err != nil {
		return fmt.Errorf("renewing licence 1: %w", err)
	}
	if err := queries.PurgeExpired(ctx, date(2026, time.June, 1)); err != nil {
		return fmt.Errorf("purging expired licences: %w", err)
	}
```

If the next operation depends on the changed rows, wait through ClickHouse’s mutation facilities or configure `mutations_sync` before reading them. A nil method error alone does not establish that the mutation finished.

## Check the completed mutations

From the Seagull repository root, run the mutation example:

```sh
cd examples/clickhouse/036_clickhouse_async_mutations
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
issued:
  #1 Kittiwake  expires 2026-03-31, renewals: 0
  #2 Salty Chip expires 2026-04-30, renewals: 0
  #3 Gull Wing  expires 2026-09-30, renewals: 0
  #4 Driftwood  expires 2026-12-31, renewals: 0
after renewing #1, purging those expired by 1 June and cancelling #4:
  #1 Kittiwake  expires 2027-03-31, renewals: 1
  #3 Gull Wing  expires 2026-09-30, renewals: 0
```

## Decide whether acceptance is enough

The example’s connection sets `mutations_sync=2`, and `PurgeExpired` writes the same setting in SQL, so its final listing observes completed mutations. Declaring a queued mutation `exec` still works but produces hint [Q040](../reference/diagnostics.md#q040). A lightweight `DELETE FROM`, such as the example’s `CancelLicence`, uses plain `exec`.

See the [dialects/clickhouse reference](../reference/dialects/clickhouse.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
