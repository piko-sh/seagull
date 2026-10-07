---
title: Add bounded pagination
description: "Place parameters in LIMIT and OFFSET and name them with sgl.param."
section: How-to guides
subsection: Queries
order: 430
---

# Add bounded pagination

Give a query LIMIT and OFFSET parameters when callers need pages of results. A default size handles an omitted input, and a maximum prevents an oversized page request.

## Set a page size and deterministic order

Name the pagination parameters with `sgl.param`, and put `default` and `max` on the LIMIT parameter. Use an ordering that distinguishes rows so page boundaries do not depend on ties:

```sql
-- sgl.query(ListFinds, many)
-- ?1 as sgl.param(page_size, default: 3, max: 5)
-- ?2 as sgl.param(page_offset)
SELECT id, creature, pool, size_mm
FROM finds
ORDER BY id
LIMIT ?1 OFFSET ?2;
```

Run `sgl vet` and `sgl generate`. The generated Params struct now carries `PageSize` and `PageOffset`. A zero size uses 3 in this example; a size above 5 is capped at 5. Validate application inputs such as negative offsets before calling the method.

## Fetch the requested page

Pass the size and offset to the generated method. The example combines them with an optional creature filter:

```go
		crabs, err := queries.SearchFinds(ctx, store.SearchFindsParams{
			Creature:   new("crab"),
			PageSize:   2,
			PageOffset: offset,
		})
		if err != nil {
			return fmt.Errorf("searching crabs from %d: %w", offset, err)
		}
```

The returned slice contains that page, and the error reports a failed query or scan. Increment the offset by the page size to fetch the next page. A deterministic order removes ambiguity from tied values, but rows inserted or removed between calls can still shift an offset-based page.

For caller-selected ordering, use the [sorting guide](sorting-pagination.md). For a fluent builder, use [runtime queries](runtime-queries.md); its zero `Limit` omits the limit rather than applying this directive's default.

## Check the input boundaries

Test omitted, ordinary and oversized page sizes, and verify that an offset moves to the expected rows. The [directive reference](../reference/directives.md) defines LIMIT defaults and caps.

The [working example](../../examples/sqlite/013_pagination) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/013_pagination
go test .
```
