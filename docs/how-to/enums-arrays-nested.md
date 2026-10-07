---
title: Work with enums, arrays and nested values
description: "Start from the dialect-native SQL type."
section: How-to guides
subsection: Types
order: 370
---

# Work with enums, arrays and nested values

Use an SQL enum when the database must restrict a value to named labels. Seagull generates a Go type, constants and a `Valid` method. For collections and structured values, follow the separate [array](arrays.md) and [DuckDB nested-value](duckdb.md) guides.

## Declare the enum before the table

Add the enum and its table to your PostgreSQL migrations:

```sql
CREATE TYPE boat_status AS ENUM ('moored', 'at_sea', 'dry_dock');

CREATE TABLE boats (
    id     BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name   TEXT        NOT NULL,
    status boat_status NOT NULL DEFAULT 'moored'
);
```

Use the enum column directly in writes and reads:

```sql
-- sgl.query(SetStatus, execrows)
UPDATE boats SET status = $1 WHERE name = $2;
```

```sql
-- sgl.query(ListBoats, many)
SELECT name, status FROM boats ORDER BY id;
```

Run `sgl generate`; `models.go` declares `BoatStatus` and constants for the labels.

## Use a generated enum constant

Construct `store.New(database)` and pass a constant rather than a free-form string:

```go
	if _, err := queries.SetStatus(ctx, store.SetStatusParams{Status: store.BoatStatusAtSea, Name: "Kittiwake"}); err != nil {
		return fmt.Errorf("sending Kittiwake to sea: %w", err)
	}
```

Validate an application-provided string before binding it. The example demonstrates that a value outside the enum labels fails validation and is refused by the server:

```go
	sunk := store.BoatStatus("sunk")
	if err := queries.AddBoat(ctx, store.AddBoatParams{Name: "Wreck", Status: sunk}); err == nil {
		return errors.New("adding a boat with status sunk: got no error, want the enum to refuse it")
	}
	fmt.Fprintf(output, "status %q: Valid() is %t, and the server refuses it, as it is not a boat_status label\n", sunk, sunk.Valid())
```

If callers need methods of their own, [override the Go type](override-types.md) with a type the driver can bind and read.

## Check valid and invalid labels

From the Seagull repository root, run the enum example:

```sh
cd examples/postgres/007_enums
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
status "sunk": Valid() is false, and the server refuses it, as it is not a boat_status label

ListBoats, where status is the generated store.BoatStatus:
  Kittiwake   at_sea    store.BoatStatus
  Salty Chip  at_sea    store.BoatStatus
  Gull Wing   dry_dock  store.BoatStatus
  Driftwood   moored    store.BoatStatus

BoatsWithStatus, where a go_type override makes status a harbour.Status:
  Kittiwake   out at sea      harbour.Status
  Salty Chip  out at sea      harbour.Status
```

## Choose the scope of an enum override

A query-header `go_type` override changes only that query’s result column; its parameters keep the generated enum type. Put the override in a migration or configure a column override to affect model fields, direct projections and parameters. Set `go.types.enum: string` to use plain strings instead of generated enum types.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
