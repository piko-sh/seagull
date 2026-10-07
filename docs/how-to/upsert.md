---
title: Write an upsert
description: Express the conflict behaviour using the target engine’s own syntax.
  Identify the unique key in the migration and specify which values an update should
  retain or replace.
section: How-to guides
subsection: Writes
order: 510
---

# Write an upsert

Insert a new row or update the row that already owns its unique key. Decide which values an existing row should retain, replace or accumulate before writing the conflict action.

## Declare the key and conflict action

Make the key unique in your migration:

```sql
CREATE TABLE stock (
    item       TEXT    NOT NULL PRIMARY KEY,
    quantity   INTEGER NOT NULL,
    deliveries INTEGER NOT NULL DEFAULT 0
);
```

This SQLite query adds the delivered quantity to existing stock and increments the delivery count:

```sql
-- sgl.query(ReceiveDelivery, one)
INSERT INTO stock (item, quantity, deliveries) VALUES (?, ?, 1)
ON CONFLICT (item) DO UPDATE SET
    quantity   = stock.quantity + excluded.quantity,
    deliveries = stock.deliveries + 1
RETURNING item, quantity, deliveries;
```

`excluded.quantity` is the proposed insert value; `stock.quantity` is the stored one. To leave an existing row untouched, use `DO NOTHING`, as the example’s `AddItem` query does.

## Use the result of both insert and update

Run `sgl generate` and construct `store.New(database)`. Send each delivery through the same method; its returned row holds the updated totals:

```go
	deliveries := []store.ReceiveDeliveryParams{
		{Item: "cod", Quantity: 10},
		{Item: "potatoes", Quantity: 40},
		{Item: "cod", Quantity: 5},
	}
	for _, delivery := range deliveries {
		row, err := queries.ReceiveDelivery(ctx, delivery)
		if err != nil {
			return fmt.Errorf("receiving %s: %w", delivery.Item, err)
		}
		fmt.Fprintf(output, "%-8s delivery %d: +%d, now %d in stock\n",
			row.Item, row.Deliveries, delivery.Quantity, row.Quantity)
	}

```

Test a new key and a repeated key. If the write can skip a row while declared `one`, [make its result optional](optional-one.md) or handle the no-rows error.

## Check new and repeated keys

From the Seagull repository root, run the upsert example:

```sh
cd examples/sqlite/021_upserts
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/021_upserts/example.go) for the exact Go API and its [test](../../examples/sqlite/021_upserts/main_test.go) for assertions and failure handling.

The complete example produces:

```text
cod      delivery 1: +10, now 10 in stock
potatoes delivery 1: +40, now 40 in stock
cod      delivery 2: +5, now 15 in stock
added peas with no stock yet
cod is already stocked, nothing changed
cod      15
peas      0
potatoes 40
```

## Keep conflict syntax and batching explicit

Use the engine’s own conflict syntax: MySQL’s `ON DUPLICATE KEY UPDATE` differs from PostgreSQL-style `ON CONFLICT`. An upsert declared `batch` is sent once per item, so repeated keys do not collide within one multi-row statement on PostgreSQL. Refer to inserted values through `excluded.column` or the engine’s equivalent; extra parameters after the VALUES row are refused for that batch shape ([Q060](../reference/diagnostics.md#q060)).

See the [query commands reference](../reference/query-commands.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
