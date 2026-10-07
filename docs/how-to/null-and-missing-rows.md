---
title: Handle NULL and missing rows
description: "Read nullable result fields as pointers and check them before dereferencing."
section: How-to guides
subsection: Results
order: 340
---

# Handle NULL and missing rows

Distinguish a NULL value from a query that returns no row. A nullable result field is a pointer; a missing `one` result is the driver’s no-rows error unless you declare it optional.

## Read nullable columns and optional joined rows

The [migration](../../examples/sqlite/004_nullability/db/migrations/0001_gulls.up.sql) allows a gull’s nickname to be NULL. A left join also makes the sighting columns nullable when a gull has no sighting:

```sql
-- sgl.query(ListSightings, many)
SELECT gulls.ring, gulls.nickname, sightings.place
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
ORDER BY gulls.id, sightings.seen_on;
```

```sql
-- sgl.query(SummariseGulls, many)
SELECT
    gulls.ring,
    COALESCE(gulls.nickname, 'ring ' || gulls.ring) AS known_as,
    COUNT(sightings.id) AS times_seen,
    MAX(sightings.seen_on) AS last_seen
FROM gulls
LEFT JOIN sightings ON sightings.gull_id = gulls.id
GROUP BY gulls.id
ORDER BY gulls.id;
```

`COALESCE` supplies a non-null display name; `COUNT` supplies zero, while the last-sighting aggregate remains nullable.

## Check a field before dereferencing it

Run `sgl generate` and construct `store.New(database)`. Read the rows and format nullable fields:

```go
	rows, err := queries.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("listing sightings: %w", err)
	}
	fmt.Fprintln(output, "ring    nickname  place")
	for _, row := range rows {
		fmt.Fprintf(output, "%-7s %-9s %s\n", row.Ring, orNull(row.Nickname), orNull(row.Place))
	}
```

The caller’s helper handles nil explicitly:

```go
func orNull(value *string) string {
	if value == nil {
		return "NULL"
	}
	return *value
}
```

For a missing whole row, either handle `sql.ErrNoRows`/`pgx.ErrNoRows`, or [declare an optional single-row query](optional-one.md). These are different from a row whose fields contain NULL.

## Check missing values and aggregate results

From the Seagull repository root, run the nullability example:

```sh
cd examples/sqlite/004_nullability
go test .
```

The example test provides its local inputs. See its [caller](../../examples/sqlite/004_nullability/example.go) for the exact Go API and its [test](../../examples/sqlite/004_nullability/main_test.go) for assertions and failure handling.

The complete example produces:

```text
ring    nickname  place
GB1001  Steven    the pier
GB1001  Steven    the chip shop
GB1002  NULL      the harbour wall
GB1003  Gwen      NULL

ring    known as     times seen  last seen
GB1001  Steven       2           2026-05-03
GB1002  ring GB1002  1           2026-05-02
GB1003  Gwen         0           NULL
```

## Keep value absence separate from row absence

Use `nullable: false` only when the query can never return NULL for that field. A NOT NULL declaration on the underlying table is insufficient on the nullable side of an outer join. For optional related objects, [embed the joined result](embed-results.md) and check the nested pointer.

See the [type mapping reference](../reference/type-mapping.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
