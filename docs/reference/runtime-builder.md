---
title: Runtime query builder
description: 'A dynamic: runtime query generates a fluent builder over its analysed
  SELECT projection. Its method set and errors are part of the generated package.'
section: Reference
subsection: Core
order: 200
---

# Runtime query builder

A `dynamic: runtime` query generates a fluent builder over its analysed SELECT projection. Its method set and errors are part of the generated package.

`dynamic: runtime` replaces a fixed query method with a builder. The caller composes
filters, ordering and pagination before running a terminal operation:

```sql
-- sgl.query(SearchPosts, many, dynamic: runtime)
SELECT id, title, category, views, published FROM posts;
```

```go
rows, err := queries.SearchPosts().
    Where("category", "=", "tech").
    Where("views", ">=", 100).
    OrderBy("published", "DESC").
    Limit(20).
    Offset(40).
    All(ctx) // also .One(ctx) and .Count(ctx)
```

## Methods

The query's method takes the query's own parameters, as a static method does, but no
context, and returns its builder, `<Name>Builder`; the context goes to the terminal that
runs the query. Each call returns the same builder, so calls chain:

| Method | What it does |
|---|---|
| `Where(column, operator, value)` | Adds a condition on a column the query selects. |
| `OrderBy(column, direction)` | Adds an ordering by a column the query selects. The direction is `ASC` or `DESC`, optionally followed by `NULLS FIRST` or `NULLS LAST`, in any case. |
| `Limit(n)`, `Offset(n)` | Set the pagination; zero leaves it out. |
| `All(ctx)` | Runs the query and returns every row: `nil` when none matches, or an empty slice with `go.empty_slices: true`. |
| `One(ctx)` | Runs the query and returns its first row, or the driver's no-rows error. With any error the row is the zero value. |
| `Count(ctx)` | Counts the query's result rows with the same conditions, omitting builder-added ordering and pagination. With any error the count is 0. |

A builder gets `Where` only when its query has a place for a condition, `OrderBy` only
when it has a place for an ordering, and `Limit` and `Offset` only when the query writes no
pagination of its own. `Count` is present only when analysis can produce a count rewrite;
Q024 warns when it is unavailable. The rewrite can retain the query's own ordering or
pagination when it shapes the counted rows, such as ordering before ClickHouse LIMIT BY
or pagination within compound branches.

With database/sql, read-only queries run on the reader of a `Queries` built with
`NewWithReplica`; row-locking reads and queries that may write use the writer.

## How the SQL is built

The builder writes what the caller adds at the places the query's analysis marked, never by
searching the SQL text. Conditions are joined with `AND`, each in parentheses, and when the
query has a `WHERE` of its own, that condition is wrapped in parentheses too, so an `OR` in
either can never widen the other. Orderings follow the query's own `ORDER BY` keys, and a limit
and an offset come before any locking or trailing clause. An offset without a limit is written
alone where the dialect accepts that, and after the dialect's unbounded limit where it needs
one (`LIMIT 18446744073709551615` on MySQL and MariaDB, `LIMIT -1` on SQLite).

The comparison operators are `=`, `<>`, `!=`, `<`, `<=`, `>`, `>=`, `LIKE`, `IN`, `NOT IN`,
`IS NULL` and `IS NOT NULL`, plus those the dialect adds, such as `ILIKE` and the `jsonb` key
operators `?`, `?|` and `?&` on PostgreSQL, each written exactly so, in upper case. `IS NULL`
and `IS NOT NULL` ignore the value. An `IN` or `NOT IN` value is a slice, a single value
counting as a list of one; as with an empty slice parameter, an empty `IN` list matches no
row and an empty `NOT IN` list excludes none. A call whose values would bind more variables than
the engine accepts returns `ErrSglTooManyBindVariables` without reaching the database.

## Errors

The builder checks every dynamic input against a closed allow-list. It never interpolates a
column name, operator, or ORDER BY direction outside its allow-list into SQL. The call that
supplied the input records an error instead of panicking, and the terminal (`All`, `One` or
`Count`) returns it; when several inputs are invalid, the first one's error is returned.
Seagull always sends bound values as driver parameters, so a value can never alter the
statement's structure.

The errors are exported from the generated package, so callers can match them with
`errors.Is`:

| Error value | Returned when | Error text |
|---|---|---|
| `ErrSglUnknownColumn` | `Where` or `OrderBy` names a column the query does not select, or a column expression of an unsupported shape or holding a backslash or a NUL. | `sgl: unknown column in runtime query filter` |
| `ErrSglUnknownOperator` | `Where` uses an operator outside the allow-list. | `sgl: unknown operator in runtime query filter` |
| `ErrSglUnknownDirection` | `OrderBy` uses a direction outside the allow-list. | `sgl: unknown sort direction in runtime query order` |

```go
rows, err := queries.SearchPosts().Where(column, "=", value).All(ctx)
if errors.Is(err, db.ErrSglUnknownColumn) {
    return fmt.Errorf("cannot filter on %q: %w", column, err)
}
```

The names follow the preset's error prefix: `Err`, then the prefix in Go casing, then the
cause. A preset whose error prefix is `acme` generates `ErrAcmeUnknownColumn`, carrying the
text `acme: unknown column in runtime query filter`. With the preset's `WrapErrors` set,
each terminal labels the error it returns, as in `sgl: SearchPosts.All: sgl: unknown
column in runtime query filter`, and `errors.Is` still matches.

## Filterable columns

The column allow-list is the query's SELECT projection. A column becomes filterable and orderable only when it
appears in the SELECT list, whether directly, via `*`, or through a CTE that projects it. A
column that exists on a FROM or JOIN table but stays absent from the projection falls outside
the allow-list. The builder rejects `.Where("password_hash", "LIKE", "a%")` or
`.OrderBy("password_hash", "ASC")` on a query that does not select `password_hash`. To filter
or order by a column, select it. Computed expressions and aggregates, which have no underlying
source column, are likewise not filterable or orderable as bare identifiers.

A column may also be written as an expression over a selected column, in the forms the
dialect allows: followed by its JSON path operators, each with a quoted key or an index,
such as `doc->'name'` on PostgreSQL; a call of its JSON path function with a quoted path,
`json_extract(doc, '$.name')` on MySQL, MariaDB, SQLite and DuckDB; or converted by one of
its casts. The builder writes the selected column's source at the column's own place in
the expression, so a column whose name is also part of the function's name is still
replaced correctly. A quoted key or path may hold neither a quote nor a backslash, and an
expression holding a backslash or a NUL anywhere is refused with `ErrSglUnknownColumn`
before the allow-list is read. MySQL and MariaDB read a backslash in a string literal as
an escape, so a key ending in one would otherwise let the literal run on into the SQL
written after it. An expression may be at most 256 bytes long.
