---
title: Query through CTEs, views and functions
description: "Keep reusable schema objects in migrations and name the final query with sgl.query."
section: How-to guides
subsection: Queries
order: 490
---

# Query through CTEs, views and functions

Compose a query from intermediate projections while keeping its results typed. This guide traces a family through a recursive SQLite CTE; reusable view and SQL-function variants are linked below.

## Name the recursive projection

In a configured SQLite project, use a nullable `parent_id` to link each gull to its parent. The [migration](../../examples/sqlite/019_ctes_and_windows/db/migrations/0001_gulls.up.sql) declares that relationship. Put the recursive query in your query file:

```sql
-- sgl.query(FamilyTree, many)
WITH RECURSIVE family AS (
    SELECT id, name, parent_id, 0 AS generation
    FROM gulls
    WHERE id = ?
    UNION ALL
    SELECT gulls.id, gulls.name, gulls.parent_id, family.generation + 1
    FROM gulls
    JOIN family ON gulls.parent_id = family.id
)
SELECT family.name, family.generation, parents.name AS parent
FROM family
LEFT JOIN gulls AS parents ON parents.id = family.parent_id
ORDER BY family.generation, family.id;
```

The first arm establishes the CTE’s columns and types; the recursive arm follows each parent link. Give derived columns explicit names so callers can identify their generated fields.

## Call the projection as one query

Run `sgl generate`, then call the method through `store.New(database)`:

```go
	family, err := queries.FamilyTree(ctx, 1)
	if err != nil {
		return fmt.Errorf("tracing Steven's family: %w", err)
	}
	fmt.Fprintln(output, "Steven's family:")
	for _, member := range family {
		fmt.Fprintf(output, "  generation %d  %-8s parent %s\n", member.Generation, member.Name, orNull(member.Parent))
	}
```

For a reusable summary, define a view in a migration and select from it, as the example’s `BeachRecords` query does. For a reusable operation with arguments, [define a SQL function](sql-functions.md).

## Check the recursive rows

From the Seagull repository root, run the projection example:

```sh
cd examples/sqlite/019_ctes_and_windows
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/019_ctes_and_windows/example.go) for the exact Go API and its [test](../../examples/sqlite/019_ctes_and_windows/main_test.go) for assertions and failure handling.

The complete example produces:

```text
Steven's family:
  generation 0  Steven   parent NULL
  generation 1  Gwen     parent Steven
  generation 1  Jago     parent Steven
  generation 2  Kerensa  parent Gwen
theft league:
  Gwithian      #1 Morwenna 0 of 0
  Porthmeor     #1 Steven   3 of 4
  Porthmeor     #2 Gwen     1 of 4
  Porthmeor     #3 Kerensa  0 of 4
  Porthminster  #1 Jago     2 of 3
  Porthminster  #2 Tristan  1 of 3
beach records:
  Gwithian      gulls 1, thefts 0, last theft NULL
  Porthmeor     gulls 3, thefts 4, last theft 2026-07-04
  Porthminster  gulls 2, thefts 3, last theft 2026-07-05
```

## Choose the type of the recursive anchor

Catalogue-level Go type overrides follow direct columns through views, but not through CTEs or derived tables. Reapply a per-query `sgl.column` override when a derived output needs a custom representation. A recursive member retains its anchor’s types, so cast the anchor when it would otherwise be unknown.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
