---
title: Validate sorting and pagination
description: Declare sortable columns in sgl.sortable and accept only the generated
  choices at your application boundary. Add LIMIT and OFFSET parameters, with default
  and max on LIMIT when appropriate.
section: How-to guides
subsection: Queries
order: 420
---

# Validate sorting and pagination

Use `sgl.sortable` when callers should choose a sort column from a fixed set. Accept the generated choices at the application boundary instead of concatenating request text into SQL.

## Declare the allowed sort columns

List the columns callers may use in the query's sortable directive:

```sql
-- sgl.query(ListEntries, many)
-- sgl.sortable(order_by, [boat, crew, finish_seconds])
SELECT id, boat, crew, finish_seconds FROM entries
```

Run `sgl vet` and `sgl generate`. The method receives a Params struct with an ordering choice and direction, and the generated package supplies constants such as `ListEntriesOrderByCrew` and `OrderDesc`.

## Map request values to generated choices

Translate your API's accepted values to those constants, then call the method. To list the largest crews first:

```go
rows, err := queries.ListEntries(ctx, store.ListEntriesParams{
    OrderBy: store.ListEntriesOrderByCrew,
    OrderByDirection: store.OrderDesc,
})
```

Check `err` before using `rows`. The [tested caller](../../examples/sqlite/012_sortable/example.go) exercises these constants and shows each resulting order.

Validate unknown request values yourself if the application should reject them. The sortable helper omits an invalid column without returning an error; an invalid direction is dropped while the column remains. A [runtime builder](runtime-queries.md) reports invalid ordering input instead.

## Add pagination without losing a stable order

Add named LIMIT and OFFSET parameters as the [pagination guide](pagination.md) shows. Choose a deterministic ordering for paged results; a size cap does not resolve ties.

The sortable choice replaces the query's top-level ORDER BY. Do not put a SQL parameter inside that original ordering, since it would lose its bind when replaced (Q057). See the [directive reference](../reference/directives.md) for the complete sortable contract.

The [working example](../../examples/sqlite/012_sortable) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/012_sortable
go test .
```
