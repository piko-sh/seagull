---
title: Read ClickHouse arrays, maps and tuples
description: Describe complex values with native ClickHouse types, then inspect the
  driver-specific Go scan types. Keep typed named parameters aligned with the declared
  SQL values.
section: How-to guides
subsection: Dialects
order: 650
---

# Read ClickHouse arrays, maps and tuples

Read ClickHouse arrays, maps and tuples without serialising them to text. This example uses the database/sql emitter; the native emitter has a different representation for named tuples.

## Declare the composite columns

Add the map, array and named tuple to your migration:

```sql
CREATE TABLE kiosks (
    id       UInt64,
    name     String,
    town     LowCardinality(String),
    till     IPv4,
    prices   Map(String, UInt32),
    tags     Array(String),
    location Tuple(lat Float64, lon Float64)
) ENGINE = MergeTree() ORDER BY id;
```

Read one kiosk, or turn its array into rows with `ARRAY JOIN`:

```sql
-- sgl.query(GetKiosk, one)
SELECT id, name, town, till, prices, tags, location FROM kiosks WHERE id = {id:UInt64};
```

```sql
-- sgl.query(KiosksByTag, many)
SELECT tag, name
FROM kiosks
ARRAY JOIN tags AS tag
ORDER BY tag, name;
```

## Read the generated composite fields

Run `sgl generate` with `go.driver: database/sql`, then use `store.New(database)`. The generated row exposes `Prices` as a typed map, `Tags` as a slice and `Location` as `map[string]any`:

```go
	first, err := queries.GetKiosk(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting kiosk 1: %w", err)
	}
	fmt.Fprintf(output, "town      %-24T %s\n", first.Town, first.Town)
	fmt.Fprintf(output, "tags      %-24T %v\n", first.Tags, first.Tags)
	fmt.Fprintf(output, "prices    %-24T %v\n", first.Prices, first.Prices)
	fmt.Fprintf(output, "location  %-24T %v\n", first.Location, first.Location)
	fmt.Fprintf(output, "till      %-24T %v\n", first.Till, first.Till)
```

When you select the native `clickhouse` emitter, named tuple fields become an anonymous struct instead. Use the [native tuple reference](../reference/dialects/clickhouse.md#type-wrappers) when adapting the caller.

## Check the composite values

From the Seagull repository root, run the composite-type example:

```sh
cd examples/clickhouse/035_clickhouse_arrays_maps
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
town      string                   Padstow
tags      []string                 [chips takeaway]
prices    map[string]uint32        map[chips:350 mushy peas:150]
location  map[string]interface {}  map[lat:50.5413 lon:-4.9389]
till      net.IP                   10.0.0.11
kiosks by tag, one row per tag:
  cafe       Gull's Rest
  chips      Chips Ahoy
  ice cream  The Ice Cream Hut
  seating    Gull's Rest
  takeaway   Chips Ahoy
  takeaway   The Ice Cream Hut
kiosks in Padstow:
  #1 Chips Ahoy
  #2 The Ice Cream Hut
```

## Match the caller to the emitter

Keep the emitter choice consistent with the caller. A database/sql tuple map is keyed by the SQL field names; a native named tuple uses generated Go fields. Nullable scalars and nullable array elements need values that can represent NULL. Cast a result or add a type override only when the selected driver can read that representation.

See the [dialects/clickhouse reference](../reference/dialects/clickhouse.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
