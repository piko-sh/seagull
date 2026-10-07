---
title: Use ClickHouse MergeTree tables
description: Choose clickhouse, declare an engine and ordering in CREATE TABLE, and
  generate for database/sql or the native clickhouse emitter. Use the matching connection
  API in the caller.
section: How-to guides
subsection: Dialects
order: 640
---

# Use ClickHouse MergeTree tables

Use the `clickhouse` dialect to keep MergeTree declarations and typed named parameters in SQL. The example below reads the latest berth assigned to each boat through the database/sql emitter.

## Declare the versioned table and queries

In `sgl.yaml`, select `dialect: clickhouse` and `go.driver: database/sql` for this caller. Add a versioned table to your migration:

```sql
CREATE TABLE berths (
    boat    String,
    berth   String,
    version UInt32
) ENGINE = ReplacingMergeTree(version) ORDER BY boat;
```

Write the insert and the current-berth query in `db/queries/harbour.sql`:

```sql
-- sgl.query(MoveBoat, exec)
INSERT INTO berths (boat, berth, version) VALUES ({boat:String}, {berth:String}, {version:UInt32});
```

```sql
-- sgl.query(CurrentBerths, many)
SELECT boat, berth, version FROM berths FINAL ORDER BY boat;
```

## Write versions and read the current berth

Generate the package with `sgl generate` and create `queries := store.New(database)`. Give later assignments a larger version. The [caller](../../examples/clickhouse/034_clickhouse_mergetree/example.go) writes several assignments and reads the current ones:

```go
	moves := []store.MoveBoatParams{
		{Boat: "Kittiwake", Berth: "north quay", Version: 1},
		{Boat: "Salty Chip", Berth: "slipway", Version: 1},
		{Boat: "Kittiwake", Berth: "fish market", Version: 2},
		{Boat: "Gull Wing", Berth: "north quay", Version: 1},
		{Boat: "Kittiwake", Berth: "outer harbour", Version: 3},
	}
	for _, move := range moves {
		if err := queries.MoveBoat(ctx, move); err != nil {
			return fmt.Errorf("moving %s: %w", move.Boat, err)
		}
	}
	berths, err := queries.CurrentBerths(ctx)
	if err != nil {
		return fmt.Errorf("reading berths: %w", err)
	}
	fmt.Fprintln(output, "berths, latest version of each boat:")
```

Use the native `clickhouse` emitter when your application calls clickhouse-go’s native connection API; select the emitter before generating and adapt composite fields to its types.

## Check the MergeTree queries

From the Seagull repository root, run the MergeTree example:

```sh
cd examples/clickhouse/034_clickhouse_mergetree
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
berths, latest version of each boat:
  Gull Wing  north quay    v1
  Kittiwake  outer harbour v3
  Salty Chip slipway       v1
daily catch, summed by day and species:
  2026-08-01 crab       30 kg
  2026-08-01 mackerel  200 kg
  2026-08-02 crab       25 kg
  2026-08-02 mackerel   95 kg
  2026-08-02 pollock    45 kg
species stats, merged from both days' states:
  crab       55 kg in all, heaviest landing 30 kg
  mackerel  295 kg in all, heaviest landing 120 kg
  pollock    45 kg in all, heaviest landing 45 kg
mackerel landings of 90 kg or more:
  #1 Kittiwake  120 kg
  #4 Kittiwake  95 kg
```

## Read the latest version explicitly

ReplacingMergeTree merges versions in the background. `FINAL` makes this query resolve them when it reads; inserting a newer version alone does not make every ordinary read return only that version.

Seagull reads engine clauses to model the table; it does not change ClickHouse’s merge or transaction semantics. Migration locking does not wrap schema changes in a transaction.

See the [dialects/clickhouse reference](../reference/dialects/clickhouse.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
