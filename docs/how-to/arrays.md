---
title: Read arrays and lists
description: Use a native array or list column where the engine supports it. Let element
  inference select a typed Go slice, then verify an empty value and multiple elements
  against the chosen driver.
section: How-to guides
subsection: Types
order: 380
---

# Read arrays and lists

Read an array column into a Go slice, preserving the difference between NULL, an empty array and a NULL element. This guide uses PostgreSQL; start with a [configured project](../tutorials/01-first-query.md).

## Declare the array and its elements

Add the array columns to a migration. Declare `non_null_elements: true` only for a column whose elements your application keeps non-null:

```sql
CREATE TABLE voyages (
    id        BIGINT   GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    boat      TEXT     NOT NULL,
    ports     TEXT[]   NOT NULL,
    catch_kg  BIGINT[] NOT NULL,
    sightings TEXT[]
);
-- sgl.column(voyages.ports, non_null_elements: true)
```

Put the write and read in `db/queries/voyages.sql`. `Ports` becomes `[]string`; `CatchKg` and `Sightings` keep pointer elements because PostgreSQL can store NULL among them.

```sql
-- sgl.query(LogVoyage, exec)
INSERT INTO voyages (boat, ports, catch_kg, sightings) VALUES ($1, $2, $3, $4);
```

```sql
-- sgl.query(ListVoyages, many)
SELECT id, boat, ports, catch_kg, sightings FROM voyages ORDER BY id;
```

## Pass slices and read the result

Run `sgl generate`, then construct `store.New(database)` from your open connection. The [caller](../../examples/postgres/008_arrays/example.go) writes populated, NULL and empty arrays:

```go
	voyages := []store.LogVoyageParams{
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "St Ives", "Mousehole"}, CatchKg: values[int64](120, 85), Sightings: values("dolphin", "basking shark")},
		{Boat: "Salty Chip", Ports: []string{"Penzance", "Mousehole"}, CatchKg: values[int64](40), Sightings: nil},
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "Lamorna"}, CatchKg: values[int64](60, 30, 15), Sightings: []*string{}},
	}
	for _, voyage := range voyages {
		if err := queries.LogVoyage(ctx, voyage); err != nil {
			return fmt.Errorf("logging %s's voyage: %w", voyage.Boat, err)
		}
	}
```

Read the generated rows without converting array text yourself:

```go
	logged, err := queries.ListVoyages(ctx)
	if err != nil {
		return fmt.Errorf("listing voyages: %w", err)
	}
	for _, voyage := range logged {
		sightings := elements(voyage.Sightings)
		if voyage.Sightings == nil {
			sightings = "NULL"
		}
		fmt.Fprintf(output, "voyage %d  %-10s  ports %s, catch %s kg, sightings %s\n", voyage.ID, voyage.Boat, voyage.Ports, elements(voyage.CatchKg), sightings)
	}
```

## Check NULL and empty arrays

From the Seagull repository root, run the array example:

```sh
cd examples/postgres/008_arrays
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
voyage 1  Kittiwake   ports [Newlyn St Ives Mousehole], catch [120 85] kg, sightings [dolphin basking shark]
voyage 2  Salty Chip  ports [Penzance Mousehole], catch [40] kg, sightings NULL
voyage 3  Kittiwake   ports [Newlyn Lamorna], catch [60 30 15] kg, sightings []

calling at Mousehole:
  Kittiwake   from Newlyn, 3 stops
  Salty Chip  from Penzance, 2 stops

voyages per boat:
  Kittiwake   [1 3]
  Salty Chip  [2]
```

## Keep NULL, empty and populated arrays distinct

Array columns differ from `kind: slice`, which expands an input list in an `IN` predicate. On PostgreSQL a nil slice represents a NULL array; an empty non-nil slice represents an empty array. A nil element in `[]*T` represents SQL NULL.

DuckDB can read nullable list elements, but its driver cannot bind a NULL element. A parameter used as the array in `id = ANY($1)` also uses plain elements. See the reference for the full element rules.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
