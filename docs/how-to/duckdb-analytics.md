---
title: Write analytical DuckDB queries
description: Keep analytical projections, windows and supported file-reading functions
  in SQL and inspect the inferred result. Test file paths and extension availability
  in the execution environment.
section: How-to guides
subsection: Dialects
order: 680
---

# Write analytical DuckDB queries

Keep ranking and file-backed analysis in DuckDB SQL, then call the generated methods from Go. Use the first query for the largest landing per harbour; use the second when the input comes from a Parquet file.

## Write the analytical projection

In a configured DuckDB project, select the row after computing its window rank:

```sql
-- sgl.query(BiggestLandingPerHarbour, many)
SELECT harbour, boat, species, kg, landed_on
FROM landings
QUALIFY row_number() OVER (PARTITION BY harbour ORDER BY kg DESC) = 1
ORDER BY harbour;
```

For a file-backed relation whose columns the analyser cannot discover, cast the selected values to the types the caller needs:

```sql
-- sgl.query(AuctionPrices, many)
-- $1 as sgl.param(price_file, type: varchar)
SELECT CAST(species AS VARCHAR) AS species, CAST(pence_per_kg AS INTEGER) AS pence_per_kg
FROM read_parquet($1)
ORDER BY species;
```

The [migration](../../examples/duckdb/038_duckdb_analytics/db/migrations/0001_landings.up.sql) creates `landings`. Keep the Parquet path as a bound parameter so the caller can select its input file.

## Call with a readable input file

Run `sgl generate`, link DuckDB’s database/sql driver into your application, and construct `store.New(database)`. Read the ranked result:

```go
	biggest, err := queries.BiggestLandingPerHarbour(ctx)
	if err != nil {
		return fmt.Errorf("finding the biggest landings: %w", err)
	}
	fmt.Fprintln(output, "biggest landing per harbour (QUALIFY):")
	for _, landing := range biggest {
		fmt.Fprintf(output, "  %-8s %-10s %4dkg of %s on %s\n", landing.Harbour, landing.Boat, landing.Kg, landing.Species, landing.LandedOn.Format(time.DateOnly))
	}
```

For `AuctionPrices`, pass the path to an existing Parquet file as `priceList`:

```go
	prices, err := queries.AuctionPrices(ctx, priceList)
	if err != nil {
		return fmt.Errorf("reading the price list: %w", err)
	}
	fmt.Fprintln(output, "auction prices (read_parquet):")
	for _, price := range prices {
		fmt.Fprintf(output, "  %-8s %dp a kg\n", *price.Species, *price.PencePerKg)
	}
```

The complete example creates its price-list file before calling the query. Generation does not open that file or verify its columns.

## Check ranking and file scanning

From the Seagull repository root, run the analytical example:

```sh
cd examples/duckdb/038_duckdb_analytics
go test .
```

The test opens DuckDB in process, so it needs no server, but the DuckDB driver needs cgo and a C toolchain. See [running examples](run-examples.md).

The complete example produces:

```text
biggest landing per harbour (QUALIFY):
  Brixham  Driftwood   220kg of mackerel on 2026-05-01
  Newlyn   Kittiwake   300kg of mackerel on 2026-05-02
kg by species (FILTER):
  Brixham  cod 150, hake 40, mackerel 220
  Newlyn   cod 180, hake 80, mackerel 300
kg by species (PIVOT):
  Brixham  cod 150, hake 40, mackerel 220
  Newlyn   cod 180, hake 80, mackerel 300
tonnes per boat (macro):
  Driftwood  0.220 t
  Gull Wing  0.190 t
  Kittiwake  0.420 t
  Salty Chip 0.140 t
Newlyn's catch (table macro):
  cod      180kg
  hake     80kg
  mackerel 300kg
auction prices (read_parquet):
  cod      900p a kg
  hake     650p a kg
  mackerel 350p a kg
value per boat (landings joined to the Parquet file):
  Driftwood  £770.00
  Gull Wing  £1610.00
  Kittiwake  £2130.00
  Salty Chip £1060.00
```

## Verify external data at execution time

DuckDB runs in process, but its Go driver requires cgo and a C toolchain. Confirm file access and required extensions in the application’s execution environment. A cast tells Seagull the intended result type; the server still validates the value when the query runs.

See the [dialects/duckdb reference](../reference/dialects/duckdb.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
