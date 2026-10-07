---
title: Name and type parameters
description: "Compare each placeholder with a typed column where possible."
section: How-to guides
subsection: Queries
order: 330
---

# Name and type parameters

Bind values through typed method arguments instead of building SQL strings. Use inferred parameter names for direct column comparisons and a `sgl.param` directive when the application needs a more descriptive name.

## Write placeholders and name their role

In a configured SQLite project, ordinary `?` placeholders take names and types from the compared columns:

```sql
-- sgl.query(FindSailings, many)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE origin = ? AND destination = ?
ORDER BY departs;
```

Name range bounds explicitly when that makes the caller clearer:

```sql
-- sgl.query(SailingsDeparting, many)
-- ?1 as sgl.param(earliest)
-- ?2 as sgl.param(latest)
SELECT id, origin, destination, departs, seats_free
FROM sailings
WHERE departs >= ?1 AND departs < ?2
ORDER BY departs;
```

A single parameter normally becomes one typed method argument. Several parameters produce a query-specific params struct. The [schema](../../examples/sqlite/003_parameters/db/migrations/0001_sailings.up.sql) declares the columns supplying these types.

## Pass the generated params struct

Run `sgl generate` and construct `store.New(database)`. Fill the inferred fields:

```go
	route, err := queries.FindSailings(ctx, store.FindSailingsParams{
		Origin:      "Penzance",
		Destination: "St Mary's",
	})
	if err != nil {
		return fmt.Errorf("finding sailings: %w", err)
	}
```

For the named range parameters, use the directive’s names:

```go
	midday, err := queries.SailingsDeparting(ctx, store.SailingsDepartingParams{
		Earliest: "10:00",
		Latest:   "15:00",
	})
	if err != nil {
		return fmt.Errorf("finding midday sailings: %w", err)
	}
```

Use a parameter’s `type:` quality or an SQL cast when the expression provides no useful type. Keep values out of the SQL string.

## Check names and bound values

From the Seagull repository root, run the parameter example:

```sh
cd examples/sqlite/003_parameters
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/003_parameters/example.go) for the exact Go API and its [test](../../examples/sqlite/003_parameters/main_test.go) for assertions and failure handling.

The complete example produces:

```text
Penzance to St Mary's:
  sailing 1 at 09:15, 40 seats free
  sailing 3 at 14:30, 4 seats free
Departing between 10:00 and 15:00:
  sailing 2 at 11:00, St Mary's to Tresco
  sailing 3 at 14:30, Penzance to St Mary's
booked 3 seats on sailing 3
sailing 3 has fewer than 3 seats free
```

## Use the dialect’s placeholder forms

Placeholder spelling follows the dialect: `$N` on PostgreSQL, `?` or `?N` on SQLite, `?` on MySQL and typed `{name:Type}` on ClickHouse. A repeated named placeholder is one logical input; generation rewrites it to the execution form the driver needs. Use directives for input shape, and SQL for the condition the database should apply.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
