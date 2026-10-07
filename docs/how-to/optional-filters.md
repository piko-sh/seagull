---
title: Add optional filters
description: Make the parameter optional and write a predicate that explicitly disables
  the filter when the value is NULL. Pass nil to omit it and a pointer to apply it.
section: How-to guides
subsection: Queries
order: 410
---

# Add optional filters

Let a caller omit individual filters by passing nil. Declare the Go parameter optional and write SQL that explicitly disables the condition when its parameter is NULL.

## Declare each optional predicate

In a configured project, add the optional bindings and their NULL tests to the query header and predicate:

```sql
-- sgl.query(SearchTrips, many)
-- ?1 as sgl.param(destination, optional: true)
-- ?2 as sgl.param(max_price, optional: true)
-- ?3 as sgl.param(dogs_welcome, optional: true)
SELECT id, name, destination, price_pence, dogs_welcome
FROM trips
WHERE (?1 IS NULL OR destination = ?1)
  AND (?2 IS NULL OR price_pence <= ?2)
  AND (?3 IS NULL OR dogs_welcome = ?3)
ORDER BY price_pence, id;
```

The pointer controls whether a value is present; the `IS NULL OR` expression controls whether the SQL filter applies. Keep both parts when adapting this pattern to another column.

## Pass nil or a pointer to the desired value

Run `sgl generate` and construct `store.New(database)`. An empty `SearchTripsParams` applies no filters; a non-nil field applies that filter. The [caller](../../examples/sqlite/011_optional_filters/example.go) exercises each choice:

```go
	searches := []struct {
		label      string
		parameters store.SearchTripsParams
	}{
		{label: "no filters", parameters: store.SearchTripsParams{}},
		{label: "to Gull Rock", parameters: store.SearchTripsParams{Destination: new("Gull Rock")}},
		{label: "£15 or less", parameters: store.SearchTripsParams{MaxPrice: new(int64(1500))}},
		{label: "to Gull Rock with a dog", parameters: store.SearchTripsParams{Destination: new("Gull Rock"), DogsWelcome: new(true)}},
	}
```

Pass the chosen params to `queries.SearchTrips(ctx, params)` and handle its error before reading the returned rows.

## Check omitted and combined filters

From the Seagull repository root, run the optional-filter example:

```sh
cd examples/sqlite/011_optional_filters
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/011_optional_filters/example.go) for the exact Go API and its [test](../../examples/sqlite/011_optional_filters/main_test.go) for assertions and failure handling.

The complete example produces:

```text
no filters:              Sunset sail, Seal spotting, Mackerel fishing, Puffin cruise
to Gull Rock:            Seal spotting, Mackerel fishing
£15 or less:             Sunset sail, Seal spotting
to Gull Rock with a dog: Seal spotting
```

## Keep the SQL NULL test explicit

On PostgreSQL, cast a parameter first encountered in `IS NULL`, for example `$1::text IS NULL`, so the server can type it. On ClickHouse use a nullable placeholder type, such as `{destination:Nullable(String)}`. `optional: true` shapes the Go input but never removes or rewrites the SQL predicate, and cannot be combined with `default:`.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
