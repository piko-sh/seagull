---
title: Build queries at runtime
description: "Mark the SELECT dynamic: runtime, then call the generated builder with Where, OrderBy, Limit or Offset and finish with All, One or Count."
section: How-to guides
subsection: Queries
order: 440
---

# Build queries at runtime

Use a runtime builder when callers need to combine filters, ordering and pagination without writing SQL fragments. The query fixes the selected columns; the builder binds values and accepts only supported columns and operators.

## Select the columns callers may filter

Mark a SELECT `dynamic: runtime`, and include the columns your application needs to filter or sort. Keep any condition that must always apply in the SQL. This query excludes claimed items regardless of the caller's filters:

```sql
-- sgl.query(SearchUnclaimed, many, dynamic: runtime)
SELECT id, item, colour, found_at, days_held
FROM lost_property
WHERE claimed = 0
```

Run `sgl vet` and `sgl generate` after changing the query. The method now returns a builder instead of a slice.

## Compose and run a search

Create `Queries` from your connection, then chain the required operations. In the example, this selects at most two unclaimed items held for 30 days or more:

```go
	longest, err := queries.SearchUnclaimed().
		Where("days_held", ">=", 30).
		OrderBy("days_held", "DESC").
		Limit(2).
		All(ctx)
	if err != nil {
		return fmt.Errorf("finding long-held items: %w", err)
	}
```

`All` runs the search and returns its rows. Use `One` for the first matching row, or `Count` to count matches when the query can be rewritten for counting. Q024 warns when `Count` is unavailable. `SearchUnclaimed()` takes no context: each of `All`, `One` and `Count` takes the context the query runs with, so a builder can be composed before a request's context exists.

The builder joins added filters to the SQL's own `WHERE` inside parentheses, so an added filter cannot bypass the base condition. Each call modifies the same builder; start a new one for an independent search.

## Handle invalid input before returning results

Check the terminal operation's error. An unsupported column, operator or direction is recorded by the builder and returned when `All`, `One` or `Count` runs; the first invalid input wins, and `One` and `Count` then return the zero row or 0 with it. A column expression holding a backslash or a NUL is always refused, so a key cannot escape its quotes. Values stay bound parameters.

`Limit` and `Offset` are available only when the query writes no pagination of its own. Do not use a `kind: slice` SQL parameter in a runtime query (Q018); pass a slice value to an `IN` filter instead. The [runtime builder reference](../reference/runtime-builder.md) lists the available methods, operators and errors.

The [working example](../../examples/sqlite/014_runtime_builder) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/014_runtime_builder
go test .
```
