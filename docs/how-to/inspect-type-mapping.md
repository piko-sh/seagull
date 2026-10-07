---
title: Inspect SQL-to-Go types
description: Generate a representative query and inspect its row type alongside the
  table model. Select the emitter before choosing overrides because it may supply
  driver-specific scan types.
section: How-to guides
subsection: Types
order: 350
---

# Inspect SQL-to-Go types

Inspect a representative query’s generated row alongside its table model before adding type overrides. This SQLite example covers integers, floating-point values, dates, binary data and a nullable column.

## Select the values the caller will read

Choose the emitter in `sgl.yaml` first; this example uses database/sql. The [migration](../../examples/sqlite/005_type_mapping/db/migrations/0001_landings.up.sql) declares each column. Put a write and matching read in your query file:

```sql
-- sgl.query(RecordLanding, one)
INSERT INTO landings (species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- sgl.query(GetLanding, one)
SELECT id, species, boxes, fish, weight_g, length_cm, on_ice, landed_at, tag, notes
FROM landings
WHERE id = ?;
```

Run `sgl generate` and compare `internal/store/models.go` with the query’s `GetLandingRow`. The query result can be more nullable than the table when its SQL introduces missing values.

## Inspect values through the real driver

Call the generated method and inspect the Go values, rather than assuming an SQL spelling determines the runtime representation:

```go
	landing, err := queries.GetLanding(ctx, recorded.ID)
	if err != nil {
		return fmt.Errorf("getting landing %d: %w", recorded.ID, err)
	}
	fmt.Fprintf(output, "id         %-10T %d\n", landing.ID, landing.ID)
	fmt.Fprintf(output, "species    %-10T %s\n", landing.Species, landing.Species)
	fmt.Fprintf(output, "boxes      %-10T %d\n", landing.Boxes, landing.Boxes)
	fmt.Fprintf(output, "fish       %-10T %d\n", landing.Fish, landing.Fish)
	fmt.Fprintf(output, "weight_g   %-10T %d\n", landing.WeightG, landing.WeightG)
	fmt.Fprintf(output, "length_cm  %-10T %.1f\n", landing.LengthCm, landing.LengthCm)
	fmt.Fprintf(output, "on_ice     %-10T %t\n", landing.OnIce, landing.OnIce)
	fmt.Fprintf(output, "landed_at  %-10T %s\n", landing.LandedAt, landing.LandedAt.Format(time.RFC3339))
	// %T prints []byte as []uint8, since byte is an alias for uint8.
	fmt.Fprintf(output, "tag        %-10T % x\n", landing.Tag, landing.Tag)
	fmt.Fprintf(output, "notes      %-10T %s\n", landing.Notes, *landing.Notes)
```

The `%T` output below shows the actual types for this driver. If a mapping does not fit the application, [add an override](override-types.md) and repeat the read with representative values.

## Check the reported Go types

From the Seagull repository root, run the type-mapping example:

```sh
cd examples/sqlite/005_type_mapping
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/005_type_mapping/example.go) for the exact Go API and its [test](../../examples/sqlite/005_type_mapping/main_test.go) for assertions and failure handling.

The complete example produces:

```text
id         int64      1
species    string     mackerel
boxes      int64      12
fish       int64      480
weight_g   int64      163200
length_cm  float64    31.5
on_ice     bool       true
landed_at  time.Time  2026-05-01T05:30:00Z
tag        []uint8    04 a2 19 f3
notes      *string    landed at Newlyn
```

## Check declared names and cast behaviour

SQLite stores integers in up to eight bytes, so all its integer columns default to `int64`. Driver decoding also depends on exact declared type names: plain `DATE`, `DATETIME` and `TIMESTAMP` can read as `time.Time`, while a modified name such as `TIMESTAMP(3)` reads as text. A cast follows SQLite’s affinity rules. See the reference before narrowing a type or changing its representation.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
