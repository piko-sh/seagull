---
title: Pass a slice to a query
description: "Write an IN predicate and mark its placeholder with kind: slice."
section: How-to guides
subsection: Queries
order: 400
---

# Pass a slice to a query

Pass a variable-length list to an `IN` predicate with `kind: slice`. The emitter binds the values and supplies the appropriate empty-list test.

## Declare the list input

In a configured SQLite project, mark the placeholder as a slice:

```sql
-- sgl.query(HutsOnBeaches, many)
-- ?1 as sgl.param(beaches, kind: slice)
SELECT number, beach, colour, booked
FROM huts
WHERE beach IN (?1)
ORDER BY number;
```

Keep the placeholder as the list’s single item. For a list beside other scalar inputs, give each its own binding:

```sql
-- sgl.query(FreeHutsInColours, many)
-- ?1 as sgl.param(beach)
-- ?2 as sgl.param(colours, kind: slice)
SELECT number, colour
FROM huts
WHERE beach = ?1 AND colour IN (?2) AND booked = 0
ORDER BY number;
```

The [schema](../../examples/sqlite/010_slices/db/migrations/0001_huts.up.sql) gives the compared columns their types. This is an input-list directive, distinct from reading an array column.

## Pass populated and empty lists

Run `sgl generate` and construct `store.New(database)`. Pass the generated params struct:

```go
	free, err := queries.FreeHutsInColours(ctx, store.FreeHutsInColoursParams{
		Beach:   "Polzeath",
		Colours: []string{"blue", "yellow"},
	})
	if err != nil {
		return fmt.Errorf("finding free huts: %w", err)
	}
```

An empty slice matches no row for `IN` and excludes no row for `NOT IN`. Include that case in the caller’s tests; the example exercises an empty beach list as well as populated ones.

## Check expanded and empty lists

From the Seagull repository root, run the slice example:

```sh
cd examples/sqlite/010_slices
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/010_slices/example.go) for the exact Go API and its [test](../../examples/sqlite/010_slices/main_test.go) for assertions and failure handling.

The complete example produces:

```text
huts on [Polzeath Fistral]: [1 2 3 6]
huts on [Watergate]: [4 5]
huts on []: []
booking huts [1 3 4] booked 3
booking huts [3 5] booked 1
hut 2 at Polzeath is yellow and free
```

## Let the emitter bind the list

With database/sql, values expand into bound placeholders at call time. pgx uses an array with `ANY` or `ALL` instead. ClickHouse takes an `Array` parameter directly and needs no `kind: slice`. A list exceeding the engine’s bind-variable limit returns `ErrSglTooManyBindVariables` before reaching the database.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
