---
title: Override SQL-to-Go types
description: "Choose the narrowest override that expresses the requirement: a query column, a catalogue column, a built-in SQL type or a preset type slot."
section: How-to guides
subsection: Types
order: 390
---

# Override SQL-to-Go types

Choose the scope of a Go type override: one query output, one schema column, or every occurrence of an SQL type. This example represents money as an application type without changing the integer stored in SQLite.

## Choose where the override applies

To use `menu.Pence` for a column’s model field, direct results and parameters, put the qualified-column directive in its migration:

```sql
-- sgl.column(menu_items.price_pence, go_type: "seagull.sh/seagull/examples/sqlite/009_overrides/internal/menu.Pence")
CREATE TABLE menu_items (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    section     TEXT    NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('available', 'sold_out')),
    price_pence INTEGER NOT NULL,
    nutrition   TEXT    NOT NULL
);
```

For a derived result used only by one query, put the override in that query’s header:

```sql
-- sgl.query(CheapestPerSection, many)
-- sgl.column(cheapest, go_type: "seagull.sh/seagull/examples/sqlite/009_overrides/internal/menu.Pence", nullable: false)
SELECT section, MIN(price_pence) AS cheapest
FROM menu_items
GROUP BY section
ORDER BY section;
```

Replace the example’s import path with your own type’s package. Assert `nullable: false` only when every returned group has a non-null minimum. For a project-wide override, use `go.types.overrides`; the example’s [configuration](../../examples/sqlite/009_overrides/sgl.yaml) shows a column override.

## Generate and read the application type

Run `sgl generate` and inspect the changed field before adapting the caller. The [money type](../../examples/sqlite/009_overrides/internal/menu/menu.go) supplies the application’s formatting methods; the caller uses the generated result directly:

```go
	cheapest, err := queries.CheapestPerSection(ctx)
	if err != nil {
		return fmt.Errorf("finding the cheapest items: %w", err)
	}
	for _, section := range cheapest {
		fmt.Fprintf(output, "cheapest %-6s from %s\n", section.Section, section.Cheapest)
	}
	return nil
```

Keep the driver contract in view: a defined integer type can use the normal integer path, while a different representation may need `Scan` and `Value`. Test both binding and reading, including NULL if the field permits it.

## Check binding and scanning

From the Seagull repository root, run the override example:

```sh
cd examples/sqlite/009_overrides
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/009_overrides/example.go) for the exact Go API and its [test](../../examples/sqlite/009_overrides/main_test.go) for assertions and failure handling.

The complete example produces:

```text
Cod and chips       £9.50  1050 kcal
Haddock and chips  sold out
Chips               £3.50   560 kcal
Mushy peas          £1.75   120 kcal
cheapest fish   from £9.50
cheapest sides  from £1.75
```

## Keep overrides within their scope

A query-level directive overrides migration and configured mappings for that output. Direct passthrough columns preserve a catalogue override; computations, CTEs, derived tables and casts to another SQL type can lose it. A cast to the column’s own type keeps it; one with other modifiers, such as `code::varchar(4)` of a `varchar(8)` column, is a cast to another type. Configuration expects value types without a leading pointer; nullability supplies the pointer when needed.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
