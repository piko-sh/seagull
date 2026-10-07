---
title: Declare SQL functions in migrations
description: Define functions in migrations before queries that use them. Seagull
  can analyse supported SQL function bodies and use declared attributes when deciding
  result types and read-only behaviour.
section: How-to guides
subsection: Queries
order: 600
---

# Declare SQL functions in migrations

Define a SQL function in a migration so the analyser can use its signature when typing calls. This PostgreSQL example combines a scalar price function with a table-valued haul function.

## Declare the signature and body in a migration

Add the functions after the [tables they read](../../examples/postgres/030_functions_in_migrations/db/migrations/0001_catches.up.sql):

```sql
CREATE FUNCTION price_pence(weight_grams INTEGER, pence_per_kilo INTEGER) RETURNS INTEGER
LANGUAGE sql IMMUTABLE STRICT
AS $$ SELECT weight_grams * pence_per_kilo / 1000 $$;

CREATE FUNCTION boat_haul(boat_name TEXT)
RETURNS TABLE (species TEXT, weight_grams INTEGER)
LANGUAGE sql STABLE
AS $$ SELECT species, weight_grams FROM catches WHERE boat = boat_name $$;
```

`STRICT` gives the scalar function NULL-on-NULL behaviour. `RETURNS TABLE` gives the second function named result columns. Put a representative call in a query file:

```sql
-- sgl.query(BoatHaul, many)
SELECT haul.species, haul.weight_grams, price_pence(haul.weight_grams, prices.pence_per_kilo) AS value_pence
FROM boat_haul(:boat) AS haul
JOIN prices ON prices.species = haul.species
ORDER BY haul.species;
```

## Generate and call the typed function result

Run `sgl generate`, apply the migrations to PostgreSQL, and construct `store.New(database)`. The generated method accepts the function’s argument and returns the selected columns:

```go
	haul, err := queries.BoatHaul(ctx, "Kittiwake")
	if err != nil {
		return fmt.Errorf("reading the Kittiwake's haul: %w", err)
	}
	fmt.Fprintf(output, "BoatHaul     on the %s: the Kittiwake's haul\n", last)
	for _, row := range haul {
		fmt.Fprintf(output, "  %-9s %6sg %5sp\n", orNull(row.Species), orNull(row.WeightGrams), orNull(row.ValuePence))
	}
	return nil
```

When a declaration or body leaves behaviour unclear, add a migration `readonly:` assertion only if you know the function’s data access. For functions implemented outside migrations, [register custom metadata](custom-functions.md).

## Check the functions on PostgreSQL

From the Seagull repository root, run the function example:

```sh
cd examples/postgres/030_functions_in_migrations
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
LandCatch    on the writer: catch 1, 12000g of mackerel from the Kittiwake
LandCatch    on the writer: catch 2, 3500g of crab from the Kittiwake
LandCatch    on the writer: catch 3, 8000g of pollock from the Puffin
SpeciesPrice on the reader: crab fetches 1200p a kilo
SpeciesPrice on the reader: halibut has no price
BoatHaul     on the reader: the Kittiwake's haul
  crab        3500g  4200p
  mackerel   12000g  5400p
```

## Declare data access accurately

Data access controls replica routing, independently of volatility. A function that writes uses the writer; a VOLATILE function that only reads can use the reader. Nullability follows the declaration and inferred body where supported. Keep procedural bodies intact and let the dialect’s lexical rules split the migration.

When the declaration says nothing about data access, Seagull reads the body. Every statement of a SQL body counts, not only the last: a body that deletes, inserts or updates before its final `SELECT` writes, and so does one that reads a writing function in `FROM`, such as `SELECT * FROM write_audit()`. A procedural body writes when it holds `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `MERGE`, `CREATE`, `DROP` or `ALTER`, runs dynamic SQL with `EXECUTE`, or starts a statement with `CALL`, `COPY`, `REFRESH MATERIALIZED VIEW`, `LOCK`, `NOTIFY`, `GRANT`, `REVOKE`, `VACUUM`, `REINDEX`, `CLUSTER`, `IMPORT FOREIGN SCHEMA`, `COMMENT ON` or `SECURITY LABEL`; a column named `comment`, `lock`, `call` or `copy` does not count. If such a function only reads, declare it `STABLE` or `IMMUTABLE`, or put `-- sgl.migration(readonly: true)` directly above its `CREATE FUNCTION`; the directive applies to that statement alone, not to other overloads of the same name.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
