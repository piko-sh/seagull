---
title: Use PostgreSQL-specific queries
description: Select postgres and either database/sql or pgx. Keep PostgreSQL constructs
  such as LATERAL, arrays and window expressions in SQL, then inspect the generated
  result and parameter types.
section: How-to guides
subsection: Dialects
order: 610
---

# Use PostgreSQL-specific queries

Use PostgreSQL’s `LATERAL` join to select the largest few related rows for each parent. Keep the ranking in SQL and read the typed result through database/sql or pgx.

The steps below use `sgl` and `sgl.yaml`. A program that generates or migrates from Go imports the PostgreSQL providers of the `sdk` kit instead:

| Role | Package | Constructor |
|---|---|---|
| Dialect | `seagull.sh/seagull/sdk/dialect/dialect_engine_postgres` | `dialect_engine_postgres.New()` |
| Emitter | `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` or `seagull.sh/seagull/sdk/emit/emit_driver_pgx` | `emit_driver_databasesql.New()`, the default, or `emit_driver_pgx.New()` |
| Migrations | `seagull.sh/seagull/sdk/migrate/migrate_backend_postgres` | `migrate_backend_postgres.New()`, or `NewPgBouncer()` behind a transaction-mode pooler |
| Live schema | `seagull.sh/seagull/sdk/schema/schema_source_postgres` | `schema_source_postgres.New(database, engine)`, for `seagull.WithSchemaSource` |

## Select the related rows with LATERAL

In a configured `postgres` project, choose `go.driver: database/sql` for this caller or `pgx` for a native pgx caller. The [migration](../../examples/postgres/031_postgres_power_queries/db/migrations/0001_harbour.up.sql) creates boats and their landings. Add this query:

```sql
-- sgl.query(BiggestLandings, many)
SELECT boats.name, biggest.species, biggest.kilos
FROM boats
LEFT JOIN LATERAL (
    SELECT species, kilos
    FROM landings
    WHERE landings.boat_id = boats.id
    ORDER BY kilos DESC
    LIMIT 2
) AS biggest ON true
ORDER BY boats.name, biggest.kilos DESC;
```

The inner query can refer to the current boat. Its `LIMIT 2` applies per boat; the left join keeps boats with no landings.

## Handle a boat with no matching landing

Run `sgl generate` and construct `store.New(database)`. The related landing fields are pointers because the join can find no row:

```go
	biggest, err := queries.BiggestLandings(ctx)
	if err != nil {
		return fmt.Errorf("finding the biggest landings: %w", err)
	}
	fmt.Fprintln(output, "two biggest landings per boat (LEFT JOIN LATERAL):")
	for _, landing := range biggest {
		if landing.Species == nil || landing.Kilos == nil {
			fmt.Fprintf(output, "  %-9s nothing landed\n", landing.Name)
			continue
		}
		fmt.Fprintf(output, "  %-9s %-8s %3dkg\n", landing.Name, *landing.Species, *landing.Kilos)
	}
```

For other PostgreSQL tasks, the [complete example](../../examples/postgres/031_postgres_power_queries) also covers `DISTINCT ON`, full-text search, JSON table functions and a data-modifying CTE. [SQL functions](sql-functions.md) and [arrays](arrays.md) have separate guides.

## Check the per-boat limit and absent rows

From the Seagull repository root, run the PostgreSQL example:

```sh
cd examples/postgres/031_postgres_power_queries
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
latest landing per boat (DISTINCT ON):
  Kittiwake mackerel 2026-07-04
  Puffin    mackerel 2026-07-03
two biggest landings per boat (LEFT JOIN LATERAL):
  Gannet    nothing landed
  Kittiwake mackerel 120kg
  Kittiwake mackerel  80kg
  Puffin    pollock   60kg
  Puffin    mackerel  25kg
search "mackerel" (full-text):
  #1 rank 0.0760  a big shoal of mackerel off the point
  #5 rank 0.0760  a few mackerel by the harbour wall
  #4 rank 0.0608  feathers out, gulls everywhere
search "crab or pollock" (full-text):
  #2 rank 0.0304  pots hauled in a heavy swell
  #3 rank 0.0304  lines over the wrecks
gear (jsonb_each):
  Kittiwake nets  2
  Kittiwake pots  40
  Puffin    lines 12
archived before 2026-07-03 (DELETE and INSERT in one statement):
  #1 mackerel 2026-07-01
  #2 crab     2026-07-02
  #3 pollock  2026-07-01
```

## Choose the exact database dialect

Test the SQL against the PostgreSQL version you deploy. CockroachDB and TimescaleDB share a parser foundation but have their own capabilities; choose their exact dialect rather than assuming all PostgreSQL forms transfer unchanged.

See the [dialects/postgres reference](../reference/dialects/postgres.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
