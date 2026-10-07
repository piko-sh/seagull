---
title: Embed joined results
description: "Add sgl.embed for the joined table."
section: How-to guides
subsection: Results
order: 450
---

# Embed joined results

Return joined columns as a nested Go struct with `sgl.embed`. This example returns a boat’s harbour as a value and its optional skipper as a pointer.

## Embed the columns of each joined table

Use `sgl.embed` in the query header and qualify the selected columns. The [schema](../../examples/sqlite/015_embed/db/migrations/0001_fleet.up.sql) gives each boat a required harbour and an optional skipper:

```sql
-- sgl.query(GetBoat, one)
-- sgl.embed(harbours)
SELECT boats.id, boats.name, harbours.id, harbours.name, harbours.county
FROM boats
INNER JOIN harbours ON harbours.id = boats.harbour_id
WHERE boats.id = ?;
```

```sql
-- sgl.query(ListCrews, many)
-- sgl.embed(skippers)
SELECT boats.id, boats.name, skippers.id, skippers.name, skippers.licence
FROM boats
LEFT JOIN skippers ON skippers.id = boats.skipper_id
ORDER BY boats.id;
```

If the same table appears twice, add `from:` to identify each SQL alias and `as:` to give the generated fields distinct names. See the [self-join example](../reference/directives.md#header-directive-sglembed).

## Access the nested field and check for absence

Generate the package with `sgl generate` and construct `store.New(database)`. An inner-joined harbour is available as `boat.Harbours`:

```go
	boat, err := queries.GetBoat(ctx, 2)
	if err != nil {
		return fmt.Errorf("getting boat 2: %w", err)
	}
	fmt.Fprintf(output, "%s is moored at %s, %s\n", boat.Name, boat.Harbours.Name, boat.Harbours.County)
```

For the outer-joined skipper, check the nested pointer before accessing its fields:

```go
	crews, err := queries.ListCrews(ctx)
	if err != nil {
		return fmt.Errorf("listing crews: %w", err)
	}
	for _, crew := range crews {
		if crew.Skippers == nil {
			fmt.Fprintf(output, "%s has no skipper\n", crew.Name)
			continue
		}
		fmt.Fprintf(output, "%s is skippered by %s, licence %s\n", crew.Name, *crew.Skippers.Name, *crew.Skippers.Licence)
	}
```

## Check matched and unmatched joins

From the Seagull repository root, run the embedding example:

```sh
cd examples/sqlite/015_embed
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/015_embed/example.go) for the exact Go API and its [test](../../examples/sqlite/015_embed/main_test.go) for assertions and failure handling.

The complete example produces:

```text
Salty Chip is moored at Newlyn, Cornwall
Kittiwake is skippered by Morwenna Penrose, licence MCA-1182
Salty Chip has no skipper
```

## Include an identifying column

The nullable side of the join determines whether an embed is a pointer, including on a self-join. A missing outer-joined object is nil. Select a non-null identifying column, such as the joined table’s primary key, so an existing object whose other fields are NULL can still be distinguished from an absent object.

See the [directives reference](../reference/directives.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
