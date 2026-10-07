---
title: Handle an optional single row
description: 'Use command one with optional: true. Check err first, then the returned
  presence boolean, and only use the row when it is present.'
section: How-to guides
subsection: Results
order: 480
---

# Handle an optional single row

Use `one, optional: true` when a missing row is an expected outcome. The method returns a row, a presence boolean and an error; check them in that order of responsibility: error first, then presence.

## Declare the expected absence

Add `optional: true` to a static `one` query. The same option handles an insert skipped on conflict:

```sql
-- sgl.query(RingGull, one, optional: true)
INSERT INTO gulls (ring, species) VALUES (?, ?)
ON CONFLICT (ring) DO NOTHING
RETURNING id, ring;

-- sgl.query(FindGull, one, optional: true)
SELECT id, ring, species FROM gulls WHERE ring = ?;
```

The [migration](../../examples/sqlite/018_optional_one/db/migrations/0001_gulls.up.sql) makes each ring unique. A miss or skipped insert returns `(zero, false, nil)` rather than the driver’s no-rows sentinel.

## Check the error before the presence flag

Run `sgl generate`, construct `store.New(database)`, and keep the row access after both checks:

```go
	for _, ring := range []string{"GB1002", "GB9999"} {
		gull, ok, err := queries.FindGull(ctx, ring)
		if err != nil {
			return fmt.Errorf("finding %s: %w", ring, err)
		}
		if !ok {
			fmt.Fprintf(output, "no gull wears ring %s\n", ring)
			continue
		}
		fmt.Fprintf(output, "ring %s is on gull %d, a %s\n", ring, gull.ID, gull.Species)
	}
```

An unexpected database failure still returns an error. Do not treat every error as an ordinary missing row.

## Check a found row and a miss

From the Seagull repository root, run the optional-row example:

```sh
cd examples/sqlite/018_optional_one
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/018_optional_one/example.go) for the exact Go API and its [test](../../examples/sqlite/018_optional_one/main_test.go) for assertions and failure handling.

The complete example produces:

```text
ringed a herring gull as GB1001, gull 1
ringed a kittiwake as GB1002, gull 2
ring GB1001 is already taken, so the black-headed gull was not ringed
ring GB1002 is on gull 2, a kittiwake
no gull wears ring GB9999
```

## Use presence for the whole row

This option applies only to a static `one` query. It does not change nullable fields within a present row and is unavailable on runtime builders.

See the [query commands reference](../reference/query-commands.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
