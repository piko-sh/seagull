---
title: Return inserted or updated rows
description: Add RETURNING to a supported write and choose one or many according to
  the intended result. Return the needed columns directly rather than issuing a separate
  read.
section: How-to guides
subsection: Writes
order: 500
---

# Return inserted or updated rows

Return a write’s values in the same statement with `RETURNING`. Choose `one` when the write produces one expected row and `many` when it can change several rows.

## Declare the write’s result shape

Add an insert that returns the generated identifier, a default and a computed value:

```sql
-- sgl.query(PlaceOrder, one)
INSERT INTO orders (item, quantity) VALUES (?, ?)
RETURNING id, status, quantity * 250 AS price_pence;
```

For a write affecting several rows, declare `many`:

```sql
-- sgl.query(ServeWaitingOrders, many)
UPDATE orders SET status = 'served' WHERE status = 'waiting'
RETURNING id, item, quantity;
```

The [migration](../../examples/sqlite/020_returning/db/migrations/0001_orders.up.sql) supplies the status default. Seagull types the `RETURNING` projection as it types a select list.

## Read and order the returned rows

Run `sgl generate` and construct `store.New(database)`. A multi-row write returns a slice; sort it if the caller needs a defined order:

```go
	served, err := queries.ServeWaitingOrders(ctx)
	if err != nil {
		return fmt.Errorf("serving orders: %w", err)
	}
	slices.SortFunc(served, func(a, b store.ServeWaitingOrdersRow) int { return cmp.Compare(a.ID, b.ID) })
	for _, order := range served {
		fmt.Fprintf(output, "served    #%d %-12s x%d\n", order.ID, order.Item, order.Quantity)
	}
```

A `one` write that matches no row returns the no-rows error. Use [optional single-row results](optional-one.md) when that is an expected outcome, including an insert skipped on conflict.

## Check defaults and values after the write

From the Seagull repository root, run the RETURNING example:

```sh
cd examples/sqlite/020_returning
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/020_returning/example.go) for the exact Go API and its [test](../../examples/sqlite/020_returning/main_test.go) for assertions and failure handling.

The complete example produces:

```text
placed    #1 chips        waiting, 500p
placed    #2 battered cod waiting, 250p
placed    #3 mushy peas   waiting, 250p
cancelled #2 battered cod x1
served    #1 chips        x2
served    #3 mushy peas   x1
cannot cancel #1: it is no longer waiting
```

## Check RETURNING support and order

PostgreSQL-family dialects, SQLite and DuckDB support `RETURNING`; MariaDB supports it for INSERT and DELETE, while MySQL and ClickHouse do not. A returned slice has no promised order unless the engine’s statement provides one.

See the [query commands reference](../reference/query-commands.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
