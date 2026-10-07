---
title: Use JSON and custom Go types
description: "Select a JSON column and inspect the emitted JSON helper."
section: How-to guides
subsection: Types
order: 360
---

# Use JSON and custom Go types

Read JSON through the generated `JSON` type and decode it at the application boundary. If your application uses a named destination type, choose a JSON-column override after verifying its driver contract.

## Declare the JSON columns and query

Add required and optional document columns to a migration:

```sql
CREATE TABLE orders (
    id       INTEGER PRIMARY KEY,
    customer TEXT    NOT NULL,
    basket   JSON    NOT NULL,
    delivery JSON
);
```

Read a whole document, or extract values in SQL when the caller needs only a few fields:

```sql
-- sgl.query(GetOrder, one)
SELECT id, customer, basket, delivery FROM orders WHERE id = ?;
```

```sql
-- sgl.query(OrdersWithFish, many)
-- ?1 as sgl.param(fish, type: text)
SELECT
    id,
    customer,
    basket -> '$.extras' AS extras,
    CAST(delivery ->> '$.address' AS TEXT) AS address
FROM orders
WHERE basket ->> '$.fish' = ?1
ORDER BY id;
```

The cast on `address` fixes the extracted scalar’s type. `extras` remains JSON.

## Decode a document or choose a custom destination

Run `sgl generate` and construct `store.New(database)`. Decode the result into the application’s `basket` struct:

```go
	first, err := queries.GetOrder(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting order 1: %w", err)
	}
	var decoded basket
	if err := json.Unmarshal(first.Basket, &decoded); err != nil {
		return fmt.Errorf("decoding order 1's basket: %w", err)
	}
	fmt.Fprintf(output, "order 1 stored %s\n", first.Basket)
	fmt.Fprintf(output, "decoded: %d x %s with %v, delivery is NULL: %t\n", decoded.Portions, decoded.Fish, decoded.Extras, first.Delivery == nil)
```

The [caller](../../examples/sqlite/006_json/example.go) defines `basket` and marshals it before writes. To decode directly into a named type instead, apply a JSON-column `go_type` override as described in [changing a type](../reference/type-mapping.md#changing-a-type). A plain struct can be decoded by the generated helper; a type with `Scan` or `Value` controls its own conversion.

## Check document and NULL handling

From the Seagull repository root, run the JSON example:

```sh
cd examples/sqlite/006_json
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/006_json/example.go) for the exact Go API and its [test](../../examples/sqlite/006_json/main_test.go) for assertions and failure handling.

The complete example produces:

```text
order 1 stored {"fish":"cod","portions":2,"extras":["mushy peas","curry sauce"]}
decoded: 2 x cod with [mushy peas curry sauce], delivery is NULL: true

cod orders:
  order 1  Steven    extras ["mushy peas","curry sauce"]   collecting
  order 3  Morwenna  extras ["pickled egg"]                to 4 Harbour Row
```

## Test the document boundary

For the default generated `JSON` type, nil represents SQL NULL. SQLite stores its parameters as text, preserving the document’s bytes. Other engines and driver paths may normalise or re-encode the document.

A Go override changes the representation; it does not install an extension. Test NULL, empty documents and malformed application JSON. SQLite documents written by builds before v0.0.0-alpha.1 are `BLOB`s, which no longer equal a document bound as text; convert them once with `UPDATE documents SET data = CAST(data AS TEXT) WHERE typeof(data) = 'blob'`.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
