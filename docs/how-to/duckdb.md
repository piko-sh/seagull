---
title: Read nested data with DuckDB
description: Generate with the duckdb dialect, inspect nested result types and link
  a DuckDB driver into the application. Use the repository’s DuckDB examples to verify
  scanning with that driver.
section: How-to guides
subsection: Dialects
order: 670
---

# Read nested data with DuckDB

Read DuckDB `STRUCT`, list, map and union columns through the database/sql emitter. Start with a configured project and link the DuckDB driver into the application that will run the generated methods.

## Declare and select the nested values

Add the structured columns to your migration:

```sql
CREATE SEQUENCE boat_ids;

CREATE TABLE boats (
    id      BIGINT                                    PRIMARY KEY DEFAULT nextval('boat_ids'),
    name    VARCHAR                                   NOT NULL UNIQUE,
    berth   STRUCT(pontoon VARCHAR, number INTEGER)   NOT NULL,
    crew    VARCHAR[]                                 NOT NULL,
    route   STRUCT(lat DOUBLE, lon DOUBLE)[]          NOT NULL,
    fees    MAP(VARCHAR, INTEGER)                     NOT NULL,
    contact UNION(phone VARCHAR, vhf_channel INTEGER)
);
```

Select whole values when the caller needs their structure, or project individual elements for a smaller result:

```sql
-- sgl.query(ListBoats, many)
SELECT id, name, berth, crew, route, fees, contact FROM boats ORDER BY id;
```

```sql
-- sgl.query(BoatSummaries, many)
SELECT
    name,
    crew[1] AS skipper,
    array_length(crew) AS crew_size,
    map_keys(fees) AS months,
    fees['August'] AS august_fee
FROM boats
ORDER BY id;
```

DuckDB can hold NULL in list elements and map values. This example’s [configuration](../../examples/duckdb/037_duckdb_nested_types/sgl.yaml) sets `go.types.non_null_elements: true` because its data has none; keep the default pointer elements if your data may contain NULL.

## Read named fields and optional elements

Run `sgl generate`, then construct `store.New(database)`. The whole-row query returns generated named structs for `berth` and each `route` item:

```go
	moored, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
```

A union reads as the generated JSON representation. Decode its value and tag in the application, as the [complete caller](../../examples/duckdb/037_duckdb_nested_types/example.go) does. Check subscript results for nil because an index or map key may be missing.

## Check nested scanning with DuckDB

From the Seagull repository root, run the nested-value example:

```sh
cd examples/duckdb/037_duckdb_nested_types
go test .
```

The test opens DuckDB in process, so it needs no server, but the DuckDB driver needs cgo and a C toolchain. See [running examples](run-examples.md).

The complete example produces:

```text
Kittiwake
  berth    store.BoatsBerth        pontoon A, berth 12
  crew     []string                [Morwenna Jago]
  route    []store.BoatsRouteItem  [50.1,-5.54 50.08,-5.55]
  fees     map[string]int32        map[August:450 July:300]
  contact  store.JSON              {"value":"07700 900123","tag":"phone"}, so by phone: 07700 900123
Salty Chip
  berth    store.BoatsBerth        pontoon B, berth 3
  crew     []string                [Gwen]
  route    []store.BoatsRouteItem  []
  fees     map[string]int32        map[July:180]
  contact  store.JSON              {"value":16,"tag":"vhf_channel"}, so by vhf_channel: 16
summaries:
  Kittiwake  skipper Morwenna, crew of 2, fees for [August July], August 450
  Salty Chip skipper Gwen, crew of 1, fees for [July], August none
```

## Link the runtime driver separately

The generator is pure Go; executing DuckDB queries requires its cgo driver. The stock `sgl` can generate these queries but cannot introspect DuckDB or open it for migrations. Use a [custom CLI](custom-cli.md) when those database operations belong in the tool.

See the [dialects/duckdb reference](../reference/dialects/duckdb.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
