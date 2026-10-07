---
title: Collect one-to-many results
description: "Select the parent and child columns, declare the embed and identify the parent key with group_by."
section: How-to guides
subsection: Results
order: 460
---

# Collect one-to-many results

Collect repeated join rows into one parent with a slice of children. Use `sgl.embed` for the two objects and `group_by` for the parent’s source key.

## Select the parent key and child columns

The [migration](../../examples/sqlite/016_one_to_many/db/migrations/0001_orders.up.sql) gives each line an order ID. Add a grouped query that keeps orders with no lines:

```sql
-- sgl.query(ListOrders, many, group_by: orders.id)
-- sgl.embed(orders)
-- sgl.embed(order_lines)
SELECT orders.id, orders.customer, order_lines.id, order_lines.dish, order_lines.quantity
FROM orders
LEFT JOIN order_lines ON order_lines.order_id = orders.id
ORDER BY orders.id, order_lines.id;
```

`group_by: orders.id` identifies the parent; it does not add SQL aggregation. Select the parent key and a non-null identifying child column, such as its primary key, to distinguish no child from nullable child values.

## Iterate the child slice

Run `sgl generate` and construct `store.New(database)`. The generated row has an `Orders` object and an `OrderLines` slice:

```go
	orders, err := queries.ListOrders(ctx)
	if err != nil {
		return fmt.Errorf("listing orders: %w", err)
	}
	for _, order := range orders {
		fmt.Fprintf(output, "order %d for %s\n", order.Orders.ID, order.Orders.Customer)
		if len(order.OrderLines) == 0 {
			fmt.Fprintln(output, "  nothing ordered yet")
		}
		for _, line := range order.OrderLines {
			fmt.Fprintf(output, "  %d x %s\n", *line.Quantity, *line.Dish)
		}
	}
```

An order with no matched line has an empty child collection, so it remains available to the caller without a separate query.

## Check parents with several and no children

From the Seagull repository root, run the grouped-join example:

```sh
cd examples/sqlite/016_one_to_many
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/016_one_to_many/example.go) for the exact Go API and its [test](../../examples/sqlite/016_one_to_many/main_test.go) for assertions and failure handling.

The complete example produces:

```text
order 1 for Steven
  2 x cod and chips
  1 x mushy peas
  3 x pickled egg
order 2 for Gwen
  1 x chips
order 3 for Jago
  nothing ordered yet
```

## Keep the grouping key and ordering explicit

Qualify `group_by` with the source table name, not the Go name chosen with `as:`. Keep `ORDER BY` explicit when parent or child order matters; this query orders by both identifiers.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
