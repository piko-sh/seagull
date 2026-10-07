---
title: Directives
description: "SQL comment directives for queries, parameters, result fields and generated types."
section: Reference
subsection: Core
order: 20
---

# Directives

Directives are SQL comments that attach generation metadata to queries and migrations.
They name methods and parameters, choose execution patterns and shape generated types.

Directives use the `sgl.` prefix. `seagull.` is accepted as a permanent alias, so
`-- seagull.query(GetUser, one)` and `-- sgl.query(GetUser, one)` mean the same thing;
`sgl fmt` rewrites aliases to the canonical `sgl.` form. A project can configure other
prefixes in `sgl.yaml` (`directives.prefixes`); see [configuration](configuration.md).

## Query annotations

Every query in a query file (the `.sql` files of the `queries` directory) starts with a
single `sgl.query(...)` header that carries the query name plus the command kind:

```sql
-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at
FROM tasks
ORDER BY created_at DESC;
```

### Where directives go

A query's directives are the comment lines above its SQL: every line comment before the
statement's first token, blank lines and prose comments included. A directive comment
stands alone on its line: only white space (any Unicode space, a tab, form feed,
vertical tab or carriage return) or a byte order mark may come before it, and only white
space after it. Comment lines directly
above a `sgl.query` header, with no blank line between, belong to that query rather than
to the SQL of the query above, so a description can sit on top of its header. Comments at
the top of a file that come before the first header and hold no directive, such as a
licence notice, belong to no query.

```sql
-- Copyright notice: belongs to no query.

-- Lists the open tasks, newest first.
-- sgl.query(ListOpenTasks, many)

-- $1 as sgl.param(owner_id)
SELECT id, title FROM tasks WHERE owner_id = $1 ORDER BY created_at DESC;
```

The prose comment lines of the header, above or among its directives, are the query's
description: `Lists the open tasks, newest first.` above. With `go.doc_comments: true`
(`Preset.DocComments` when embedding) the description becomes the doc comment of the
query's method, a line of comment for each line of prose, with a blank comment line where
an empty comment line or a blank line parts two paragraphs; directive lines part nothing.
The description stays in the SQL the method sends, as it is today.

A directive written after the SQL has started is not read. A misspelt or unknown one,
such as `-- sgl.qeury(PurgeTasks, exec)`, is an error ([Q026](diagnostics.md#q026))
reported at its line, and it starts a block of its own, so the statement below it never
merges into the query above. A known directive there is ignored with a warning
([Q044](diagnostics.md#q044)); move it into the header.

Files are read with the dialect's own lexical rules, so a line inside a string literal, a
quoted identifier, a dollar-quoted body or a block comment is never a directive, and a
leading byte order mark is ignored. Reading resumes on the next line after a string or
comment left unterminated, up to 64 times in a file; past that the rest of the file is
read as SQL without directives, and [Q039](diagnostics.md#q039) marks the line where
reading stopped.

### Grammar

The header is a function-call-with-keyword-arguments grammar. Each
directive accepts zero or more positional arguments followed by zero
or more `key: value` keyword arguments:

```
-- sgl.query(<positionals>, <key>: <value>, ...)
-- $N as sgl.<role>(<positionals>, <key>: <value>, ...)
-- sgl.<role>(<positionals>, <key>: <value>, ...)
```

Required arguments are positional. Optional arguments are keyword
arguments. **You can also name every positional**, so all
three of these are equivalent:

```sql
-- sgl.query(GetUser, one)                     -- positional
-- sgl.query(name: GetUser, command: one)      -- keyword-as-positional
-- sgl.query(GetUser, command: one)            -- mixed
```

The three forms are interchangeable. The generator accepts a positional
argument and its named keyword-as-positional equivalent for any required
argument.

One directive call reports at most 32 problems, then one
[Q066](diagnostics.md#q066) saying how many more were left out, at the highest
severity among them.

### Top-level header: `sgl.query(...)`

Multi-line layout works when the call gets long. The parser
continues across comment-prefixed lines while the paren depth stays
open:

```sql
-- sgl.query(
--   SearchOrders,
--   many,
--   dynamic: runtime,
--   group_by: orders.id,
-- )
```

**Positionals** (in order):

| Position | Name | Type | Purpose |
|---|---|---|---|
| 1 | `name` | name (required) | Go method name on `Queries`; see the naming rules below. |
| 2 | `command` | enum (required) | Execution pattern. See command kinds below. |

Names start with a letter and contain letters, digits and underscores. Snake_case names
become PascalCase with common initialisms: `get_user_by_id` becomes `GetUserByID`; other
names have their first letter upper-cased. Names that produce the same method, such as
`get_user` and `GetUser`, are duplicates ([Q006](diagnostics.md#q006)). Invalid names,
such as `_hidden` or `get.user`, are [Q028](diagnostics.md#q028).

**Keyword arguments**:

| Key | Type | Purpose |
|---|---|---|
| `dynamic` | `runtime` | Emit a fluent runtime builder instead of a static method. |
| `readonly` | `true`/`false` | Override automatic read/write detection. |
| `nullable` | `true`/`false` | Override automatic nullability propagation across the output columns. |
| `optional` | `true`/`false` | For `command: one`, return `(row, false, nil)` on a zero-row result instead of the driver no-rows sentinel. Only valid on a static `command: one`. |
| `group_by` | qualified column (for example `orders.id`) | Declare the grouping column that collapses repeated parent rows in an embed join. |
| `model` | table (for example `users` or `auth.users`) | Read the rows into the model of the table, the struct `models.go` declares for it, rather than a Row struct of the query's own; see [rows read into a model or a shared row type](#rows-read-into-a-model-or-a-shared-row-type). Excludes `row`. |
| `row` | name (for example `UserSummary`) | Name the struct the rows are read into; every query naming the same row type shares one struct. Excludes `model`. |

### Command kinds

| Kind | Return value | Behaviour |
|---|---|---|
| `one` | `(<Name>Row, error)` | One row; a miss returns the driver's no-rows error. |
| `many` | `([]<Name>Row, error)` | All matching rows. |
| `exec` | `error` | Executes without reading result rows. |
| `asyncexec` | `error` | Executes a ClickHouse mutation that completes asynchronously. |
| `execrows` | `(int64, error)` | Affected-row count; refused on ClickHouse. |
| `execresult` | `(sql.Result, error)` or `(pgconn.CommandTag, error)` | Driver result; refused on ClickHouse. |
| `stream` | `iter.Seq2[<Name>Row, error]` | A range-over-func iterator that scans one row at a time. |
| `batch` | `error` | Executes for a slice of `<Name>Params`; see [batch execution](query-commands.md#batch-execution). |
| `copyfrom` | `(int64, error)` with pgx; `error` otherwise | Bulk insertion; see [copying rows](query-commands.md#copying-rows). |

The [query-command reference](query-commands.md) defines the emitter-specific contracts,
including batch shapes, COPY restrictions and discarded results. A `stream` method returns
`func(func(<Name>Row, error) bool)`, the same underlying type, when the preset turns
`ModernIdioms` off.

### Reader and writer routing

Every query is judged read-only or writing when it is analysed, and a database/sql
`Queries` built with `NewWithReplica` sends the read-only ones to the reader. A query
writes when its statement changes data or schema (`INSERT`, `UPDATE`, `DELETE`, DDL and the
like), when it holds a writing CTE, when it calls a function that may change data, or when
it locks rows anywhere: `FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR SHARE`, `FOR KEY SHARE` or
`LOCK IN SHARE MODE` at its end or at the end of a CTE body, a derived table or a subquery,
since a hot standby refuses row locks and a lock on a replica guards nothing on the
primary.

Whether a function may change data is its data access, not its volatility. A
PostgreSQL function declared `VOLATILE`, or left at that default, still goes to the reader
when nothing it runs writes: volatility is about caching and re-evaluation, not about
writing. The data access comes from:

- a [`sgl.migration(readonly: true)` or `readonly: false`](#migration-directive-sglmigration)
  directive above the `CREATE FUNCTION`, which states the function's data access
  outright;
- otherwise a declaration in the function's DDL: MySQL's `MODIFIES SQL DATA`, or
  `READS SQL DATA` and `NO SQL`, which declare it read-only, and PostgreSQL's `IMMUTABLE`
  and `STABLE`, which declare it read-only too, since PostgreSQL refuses writes in such a
  function; `VOLATILE`, the default, declares nothing;
- otherwise the function's body, read when the migration creates it: a body that changes
  data (or schema), or that calls a function which may, makes it a writing function. The
  same holds for a DuckDB macro. Every statement of a SQL-language body is analysed, and
  the function writes when any of them does, so a `DELETE` before the final `SELECT`
  counts; so does reading a writing table-valued function in `FROM`, while a table
  function the catalogue does not know, such as DuckDB's `read_csv`, does not. A body
  that cannot be analysed as statements, such as a procedural one, is scanned outside its
  strings and comments: it writes when it holds `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`,
  `MERGE`, `CREATE`, `DROP` or `ALTER` anywhere, or starts a statement with `EXECUTE`
  (dynamic SQL, also after `RETURN QUERY` or `FOR ... IN`), `CALL`, `COPY`,
  `REFRESH MATERIALIZED VIEW`, `LOCK`, `NOTIFY`, `GRANT`, `REVOKE`, `VACUUM`, `REINDEX`,
  `CLUSTER`, `IMPORT FOREIGN SCHEMA`, `COMMENT ON` or `SECURITY LABEL`. A column named
  `comment`, `lock`, `call` or `copy` does not count;
- the dialect's built-in catalogue, where the few functions that change state (such as
  PostgreSQL's `nextval`, `setval`, `pg_notify` and the advisory-lock functions, or
  MariaDB's sequence functions) are marked as writing and every other built-in reads;
- `data_access` on a [custom function](configuration.md) (`read_only` by default, or
  `modifies_data`).

A call to a function the analyser does not know, reported as [Q005](diagnostics.md#q005),
counts as writing, since it may change data. So does a query nested too deeply for the
analyser to check every call in it, which also gets a [Q054](diagnostics.md#q054)
warning. `readonly: true` or `readonly: false` on the query header overrides the result
either way.

### Rows read into a model or a shared row type

Each query that returns rows reads them into a Row struct of its own, `<Name>Row`, by default.
The `model:` keyword reads them into the model of a table instead, the struct `models.go`
declares for it, and the `row:` keyword into a struct of the name it gives, which every query
naming that row type shares:

```sql
-- sgl.query(ListRoles, many, model: roles)
SELECT * FROM roles;

-- sgl.query(GetRole, one, model: roles)
SELECT description, display_name, role_id FROM roles WHERE role_id = $1;

-- sgl.query(ListRoleNames, many, row: RoleName)
SELECT role_id, display_name FROM roles ORDER BY display_name;

-- sgl.query(SearchRoleNames, many, row: RoleName)
SELECT display_name, role_id FROM roles WHERE display_name LIKE $1;
```

```go
func (queries *Queries) ListRoles(ctx context.Context) ([]Roles, error)
func (queries *Queries) GetRole(ctx context.Context, roleID uuid.UUID) (Roles, error)

type RoleName struct {
    RoleID      uuid.UUID `json:"role_id"`
    DisplayName string    `json:"display_name"`
}

func (queries *Queries) ListRoleNames(ctx context.Context) ([]RoleName, error)
func (queries *Queries) SearchRoleNames(ctx context.Context, displayNameLike string) ([]RoleName, error)
```

The rows fit a struct when the Row struct the query would declare has its fields: the same
field names, each of the same Go type, nullability included, and with the same struct tags. The
order of the columns does not matter, since each column is read into the field of its name. So a
model takes a query that selects every column of the table it does not hide, under the column's
own name, as the table types it: an outer join that makes a column nullable, an alias that
renames one or a cast to another Go type does not fit. A row type is declared from the columns of
the first query naming it, by file and then by name, and every other query naming it must give
the same fields. A `row:` name is written as a query's name is, so `role_name` names `RoleName`;
it must be a name the package does not already declare, such as a model's, and to read the rows
into a table's model, name the table with `model:` instead.

A query whose rows do not fit is error [Q065](diagnostics.md#q065), listing each field the
struct has that no column gives, each column the struct has no field for, and each column of
another Go type or other struct tags. `model:` naming a table the schema does not hold or a view,
which has no model, and either keyword on a query that returns no rows (`exec`, `execrows`,
`execresult`, `asyncexec`, `batch`, `copyfrom`) or embeds a table with `sgl.embed`, are Q065 too.
`model:` and `row:` work with `one`, `many`, `stream` and a `dynamic: runtime` builder. A
partition has no model of its own, so `model:` naming one reads the rows into the model of
the table it partitions.

With `go.reuse_models: true` in `sgl.yaml` (`Preset.ReuseModels` when embedding) every query
whose rows are a table's model reads them into the model without the keyword: a query that reads
only that table's columns and whose rows fit its model, such as `SELECT * FROM roles` or
`INSERT ... RETURNING *`. Any other query keeps a Row struct of its own, with no diagnostic.

### Dynamic runtime builder

`dynamic: runtime` emits a fluent builder instead of a fixed method, so callers compose the
filter, ordering, and pagination at runtime:

```go
rows, err := queries.SearchPosts().
    Where("category", "=", "tech").
    Where("views", ">=", 100).
    OrderBy("published", "DESC").
    Limit(20).
    Offset(40).
    All(ctx) // also .One(ctx) and .Count(ctx)
```

The query's method takes only the query's own parameters; the context goes to the
terminal, `All`, `One` or `Count`. The builder checks every column, operator and direction
against a closed allow-list, and a column must be one the query selects. A column
expression holding a backslash or a NUL is refused whatever else it holds.
[Runtime query builder](runtime-builder.md) is the full reference: its methods, where it
writes what the caller adds, its operators and the errors it returns.

### Parameter directives

Parameter placeholders in the SQL (`$1`/`$2` on the PostgreSQL family and DuckDB, `?` or
`?1` on SQLite, `?` on MySQL and MariaDB, `{name:Type}` on ClickHouse, `:name` for a named
parameter) become typed Go parameters. A per-placeholder directive line names one and shapes it:

```sql
-- sgl.query(SearchOrders, many)
-- $1 as sgl.param(status, optional: true)
-- $2 as sgl.param(ids, type: int, kind: slice)
-- sgl.sortable(order_by, [name, total, placed_at])
-- $3 as sgl.param(page_size, default: 10, max: 100)
-- $4 as sgl.param(skip)
SELECT id, name, status, total, placed_at FROM orders
WHERE ($1::text IS NULL OR status = $1) AND id IN ($2)
LIMIT $3 OFFSET $4;
```

Each role:

#### `sgl.param(name, type:, nullable:, default:)`

The default binding. Use it when you want to give the placeholder a
Go-friendly name and optionally override its inferred SQL type or
nullability.

| Position / Key | Type | Purpose |
|---|---|---|
| 1 (positional) | name | The Go parameter's name, such as `user_id` (field `UserID`). A named placeholder may leave it out (see below). |
| `type:` | SQL type | Override the inferred SQL type, such as `type: int8`. A parameter that declares a type takes no column override. |
| `nullable:` | `true`/`false` | Override the inferred nullability. |
| `optional:` | `true`/`false` | Make the Go value a pointer the caller may pass as `nil`. Excludes `default:`. |
| `default:` | integer | The value a zero is replaced with at call time, for a numeric parameter such as a `LIMIT`. Non-negative and no greater than `max:`. Excludes `optional:`. |
| `max:` | integer | The largest value passed at call time, for a numeric parameter such as a `LIMIT`. Non-negative. |
| `kind:` | `scalar` or `slice` | `slice` takes a Go slice for an `IN ($1)` list. Default `scalar`. |
| `non_null_elements:` | `true`/`false` | Declare an array, list or map parameter's elements non-null or nullable. |

```sql
-- $1 as sgl.param(user_id, type: int8, nullable: false)
```

#### Named placeholders

A named placeholder is bound by its name, and its directives may come in any order. The
name positional can be left out, so the parameter keeps the placeholder's name and the
directive only adds qualities. A name given in the directive renames the Go parameter
alone; the SQL goes on using the placeholder's name:

```sql
-- sgl.query(ListOrders, many)
-- :customer as sgl.param(buyer)
-- :status as sgl.param(optional: true)
SELECT id FROM orders
WHERE customer = :customer AND (:status::text IS NULL OR status = :status);
```

This generates a `ListOrdersParams` with `Buyer string` and `Status *string`. A name used
more than once is one parameter. The SQL is sent with the dialect's own placeholders: `$N` on
the PostgreSQL family and DuckDB, `?N` on SQLite, and `?` on MySQL and MariaDB, where the
value is bound once for every occurrence. A positional placeholder has no name to keep, so
its directive must give one.

#### `sgl.param` quality: `non_null_elements:`

A parameter of an array, list or map type binds elements that may be NULL, a Go slice of
pointers such as `[]*string`, where the engine can hold NULL among them, as the column of that
type reads; see [elements that may be NULL](type-mapping.md#elements-that-may-be-null).
`non_null_elements: true` declares them non-null, so the parameter binds plain elements, and
`false` declares them nullable again under a wider `true`:

```sql
-- sgl.query(UpsertAuthors, exec)
-- :ids as sgl.param(non_null_elements: true)
-- :names as sgl.param(non_null_elements: true)
INSERT INTO authors (id, name, bio)
SELECT * FROM unnest(:ids::bigint[], :names::text[], :bios::text[])
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, bio = EXCLUDED.bio;
```

This takes `IDs []int64`, `Names []string` and `Bios []*string`, so a nil bio writes NULL.

#### `sgl.param` qualities: `optional:` and `kind: slice`

`sgl.param` carries keyword qualities that shape the input instead of acting as separate
directives.

`optional: true` makes the Go field a pointer, so the caller can pass nil. It does not change
the SQL: write the predicate so that a NULL switches it off, with the
`($1 IS NULL OR column = $1)` idiom. PostgreSQL cannot infer the type of `$1 IS NULL`, so cast
the parameter there. It is mutually exclusive with `default:`.

```sql
-- $1 as sgl.param(status, optional: true)
SELECT id, status FROM orders WHERE ($1::text IS NULL OR status = $1);
```

`kind: slice` takes a Go slice for an `IN ($1)` list. The element type can be overridden with
`type:` when inference is too loose.

```sql
-- $1 as sgl.param(ids, type: int, kind: slice)
SELECT * FROM users WHERE id IN ($1);
```

How the list reaches the database depends on the emitter. database/sql writes one
placeholder per element at call time, spliced into the generated SQL at the list's place, and
binds the elements in order. With numbered placeholders (`$N`, `?N`) a list takes the numbers
after every other parameter, so the rest of the statement keeps its numbers however long the
list grows. A call whose lists would bind more values than the engine accepts returns
`ErrSglTooManyBindVariables` (named after the preset's error prefix) without reaching the
database.

An empty slice replaces the whole `IN (...)` with a form that matches no row, and a
`NOT IN (...)` with one that excludes no row, as the empty list means:

| Dialect | Empty `IN` | Empty `NOT IN` |
|---|---|---|
| PostgreSQL, CockroachDB, TimescaleDB | `= ANY('{}')` | `<> ALL('{}')` |
| MySQL, MariaDB | `IN (SELECT NULL FROM DUAL WHERE FALSE)` | `NOT IN (SELECT NULL FROM DUAL WHERE FALSE)` |
| SQLite | `IN (SELECT NULL WHERE 0)` | `NOT IN (SELECT NULL WHERE 0)` |
| DuckDB | `IN (SELECT NULL WHERE FALSE)` | `NOT IN (SELECT NULL WHERE FALSE)` |

pgx rewrites `IN ($1)` to `= ANY($1)` (and `NOT IN ($1)` to `<> ALL($1)`) and binds the slice
as one array. It sends a nil slice in a `NOT IN` list as an empty array, so it excludes no row
as an empty list does. ClickHouse needs no `kind: slice`: compare with an `Array` parameter,
`id IN {ids:Array(UInt64)}`, and the method takes a Go slice as it stands.

#### `sgl.sortable(name, [columns])`

A validated dynamic ORDER BY. The second positional is a bracketed list of allowed column
names. The params struct gains a field named after the directive (`order_by` gives `OrderBy`),
typed as a generated string type with one constant per column (`ListProductsOrderByName`),
and a direction field (`OrderByDirection`) of type `OrderDirection`, whose constants are
`OrderAsc` and `OrderDesc`.

The generated method appends `ORDER BY` only when the field holds a listed column. A value
outside the list is not an error: the method silently leaves the `ORDER BY` out and runs the
query unordered. A direction other than `OrderAsc` or `OrderDesc` is dropped the same way,
keeping the column. Validate the input yourself when an unknown value should fail.

`sgl.sortable` is a standalone header. It binds no placeholder, so it carries no `$N as`
prefix. The generator removes any top-level `ORDER BY` from the SELECT and inserts its own
where that clause belongs: after any clause the engine applies before ordering, such as
`QUALIFY` or a named `WINDOW`, and before the top-level `LIMIT`, `OFFSET`, `FETCH`, ClickHouse's
`LIMIT n BY`, a locking clause such as `FOR UPDATE`, ClickHouse's `SETTINGS` and `FORMAT`, and
the trailing semicolon.

```sql
-- sgl.query(ListProducts, many)
-- sgl.sortable(order_by, [name, price, created_at])
-- $1 as sgl.param(page_size, default: 20, max: 100)
SELECT id, name, price, created_at FROM products
LIMIT $1;
```

You can also supply the `columns` list as a keyword argument
(`columns: [name, price, created_at]`) for long lists where
breaking the call across lines reads better.

#### LIMIT / OFFSET parameters

The generator types a `sgl.param` placed in a `LIMIT` or `OFFSET` clause as a non-negative
integer from its clause position. No special directive applies. Add the `default:` and `max:`
qualities to clamp the value at call time. A default fills a zero value, and max caps it.

Each must be a non-negative integer, and the default no greater than the max; a negative
value, or a default above the max, is error [Q028](diagnostics.md#q028) on the value
rather than generated code that sends `LIMIT -5` or caps every default. The generated field
of a `LIMIT` or `OFFSET` parameter is a Go `int`, so its default and max may be at most
2147483647, the most an `int` holds on 32-bit platforms (Q028 too).

```sql
-- $1 as sgl.param(page_size, default: 10, max: 100)
-- $2 as sgl.param(page_offset)
SELECT id, name FROM products LIMIT $1 OFFSET $2;
```

### Header directive: `sgl.embed`

Groups the columns of one table reference into a nested Go struct. The required
positional is the source table or view. The `from:` keyword names the FROM-clause
alias whose columns belong to this embed group, and `as:` names the generated field
and struct type:

```sql
-- sgl.query(GetBookWithAuthor, one)
-- sgl.embed(authors, from: a)
SELECT b.id, b.title, a.id, a.name
FROM books b
INNER JOIN authors a ON a.id = b.author_id
WHERE b.id = $1;
```

The generated row type carries `Authors GetBookWithAuthorAuthors`: the field is named
after the embed, and its struct type after the query and the embed.

| Position / Key | Type | Purpose |
|---|---|---|
| 1 (positional) | `table` | Source table or view whose columns the embed group holds. |
| `from:` | identifier | FROM-clause alias whose columns belong to this embed group. Optional when the table appears once in the `FROM` clause; required when it appears more than once. |
| `as:` | identifier | Name of the generated field and struct type (defaults to the table name in Go-cased form). Required when another embed of the query would otherwise share the name. |

An embed holds exactly the columns its alias qualifies, whether they are listed one by
one or selected with `a.*`. Without `from:`, Seagull takes the alias of the table's only
reference in the `FROM` clause. An embed whose reference is joined with `LEFT`, `RIGHT`,
`FULL` or `POSITIONAL` is a pointer, set to `nil` when the join matched no row. The join
of the embed's own reference decides this, so on a self-join only the outer side is a
pointer. `group_by` qualifies its column with the table name, not the `as:` name.

A self-join references one table twice, so each embed names its side with `from:`, and
`as:` keeps their names apart:

```sql
-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON m.id = e.manager_id;
```

```go
type ListEmployeesManager struct {
    ID   *int64  `json:"id"`
    Name *string `json:"name"`
}

type ListEmployeesRow struct {
    ID      int64  `json:"id"`
    Name    string `json:"name"`
    Manager *ListEmployeesManager
}
```

Seagull reports an embed it cannot bind as an error. A table the query does not read,
`from:` naming an alias the `FROM` clause does not hold, an alias of another table, or an
alias another embed already holds, and `as:` repeating another embed's name, are
[Q028](diagnostics.md#q028). A table referenced more than once without `from:`, or a
second embed whose name is already taken and that has no `as:`, is
[Q031](diagnostics.md#q031). A schema-qualified table, as in
`sgl.embed(inventory.articles)`, binds the reference to that schema's table and is named
after the table.

#### Inline form

An embed can also be written as a comment in the `SELECT` list, `/* sgl.embed(table) */`;
spaces after `/*` and before the parenthesis are accepted, as a formatter may write
`/* sgl.embed (table) */`:

```sql
-- sgl.query(GetBookWithAuthor, one)
SELECT b.id, b.title, /* sgl.embed(authors) */ a.id, a.name
FROM books b
INNER JOIN authors a ON a.id = b.author_id
WHERE b.id = $1;
```

The inline form takes only the table, so it binds to the table's only reference and is
named after the table. Its position in the list does not matter: it embeds every column
that reference qualifies. Seagull reads inline markers only when the query header declares
no `sgl.embed`. Prefer the header form, which also accepts `from:` and `as:`.

A marker is read only from a block comment, with the dialect's lexical rules, so one in a
string literal or a `--` comment is ignored. Its name runs to the first `)` of its own
comment; a comment that closes first is [Q032](diagnostics.md#q032). The name must be a
table or `schema.table` of at most 128 characters, or it is
[Q028](diagnostics.md#q028). A query reads at most 256 markers, and more are
[Q039](diagnostics.md#q039).

### Type override: `sgl.column`

Override the inferred SQL type, the custom Go destination type, the
nullability or the struct tags of one output column. Valid both in
query headers (per-query override) and in migration files
(catalogue-wide override that propagates through every passthrough
query).

**Per-query override**, which pins a derived column's type when the
analyser cannot infer it precisely:

```sql
-- sgl.query(GetUserEmailLower, one)
-- sgl.column(email_lower, type: text)
SELECT id, LOWER(email) AS email_lower FROM users WHERE id = $1;
```

**Custom Go destination type**, which maps a column to a user-defined
Go type (the generated file imports the package automatically):

```sql
-- sgl.query(GetUserUUID, one)
-- sgl.column(id, go_type: "github.com/google/uuid.UUID")
SELECT id, email FROM users WHERE id = $1;
```

The generated row struct uses `uuid.UUID` and the file gains
`import "github.com/google/uuid"`. Nullable columns become
`*uuid.UUID` automatically.

A `go_type:` value is a type name, optionally qualified by the import
path of its package, optionally preceded by `*` or `[]`, and optionally
followed by type arguments in brackets, each written the same way: `string`,
`[]byte`, `time.Time`, `*example.com/app/money.Pence`. A qualified name
must be exported. A name with no `*`, `[]` or type arguments may be written
without quotes, as in `go_type: github.com/google/uuid.UUID`, in a query
header as in a migration; quote any other value. The generated code imports
the package under the name its path implies, or the one
`go.types.package_names` gives (see
[changing a type](type-mapping.md#changing-a-type)). Composite types such as
`map[string]int` are refused; declare a named type and use that. A value the generator
cannot write, one longer than 1024 characters, or one whose type arguments nest more
than 16 levels deep is error [Q049](diagnostics.md#q049), which `sgl vet` and
`sgl generate` report wherever `go_type` appears: a query header, a
migration, or a type override in `sgl.yaml`.

**Migration-level (schema-wide) override**, declared once in a
migration, above or below the `CREATE TABLE`. Every passthrough query
that selects the column inherits the override:

```sql
-- sgl.column(users.id, go_type: "github.com/google/uuid.UUID")
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL
);
```

```sql
-- sgl.query(GetUser, one)
SELECT id, email FROM users WHERE id = $1;   -- ID field is uuid.UUID

-- sgl.query(GetUserAsString, one)
SELECT id::text AS id_str FROM users;        -- IDStr field is string
```

A migration directive is read from its own line: the whole call fits on that line, with
nothing after its closing parenthesis, and it names the column as `table.column` (or
`schema.table.column`). The name keeps the case it is written in, and a part may be
quoted, as in `sgl.column(events."UserID", ...)` for a mixed-case PostgreSQL column; an
unquoted part folds as the dialect folds an unquoted identifier. Values follow the
grammar of query headers, so a type of more than one word or with parentheses is quoted,
`type: 'double precision'` or `type: 'varchar(255)'`, and a backslash in a quoted value
escapes the character after it; write `\\` for a backslash. A type written with its
modifiers resolves with them, as a column declared with it does, so
`type: 'numeric(10,2)'` has precision 10 and scale 2.

The override propagates only through *direct passthrough
projections*, directly or through a view. A cast to the column's own
type (`id::uuid`) keeps it; a cast to another type (`id::text`,
`CAST(id AS text)`), including the column's type with other modifiers
(`code::varchar(4)` of a `varchar(8)` column), a function call or an
arithmetic operation drops it and falls back to the analyser's inferred
type, as it does through a CTE or a derived table. Re-apply per-query
with another `sgl.column` line when you need the override on a derived
column. The Go type must
be one the driver can bind and read; see [what an override type must
support](type-mapping.md#what-an-override-type-must-support).

**Precedence**:

1. Query-level `sgl.column` directive wins over migration-level.
2. Migration-level `sgl.column` wins over inferred type.
3. `type:` and `go_type:` are mutually exclusive on one directive
   (the generator raises a Q038 diagnostic when a directive declares both).
4. `nullable:` works alongside either `type:` or `go_type:`.

| Position / Key | Type | Purpose |
|---|---|---|
| 1 (positional) | `name` | Output column name (query header) or qualified `table.column` (migration). |
| `type:` | SQL type | Override the analyser's inferred SQL type; mapped through the engine's type registry. Mutually exclusive with `go_type:`. |
| `go_type:` | Go type | Explicit Go destination type: an optional `*` or `[]`, then a type name, qualified by its import path when it is not a built-in (for example `"github.com/google/uuid.UUID"`, or the same unquoted). Quote a value with `*`, `[]` or type arguments. The generator imports the package automatically. An unusable value is Q049. Mutually exclusive with `type:`. |
| `nullable:` | `true`/`false` | Override inferred nullability. Nullable + custom `go_type:` produces `*Type` wrapping, unless the type already starts with `*`. |
| `non_null_elements:` | `true`/`false` | Declare the column's array or list elements and map values non-null (`true`), so they are plain values rather than pointers, or nullable (`false`), where the engine can hold NULL there; see [elements that may be NULL](type-mapping.md#elements-that-may-be-null). |
| `struct_tags:` | quoted string | Struct tags for the column's generated fields, written as Go writes a struct tag, `key:"value"` pairs separated by spaces, inside a quoted string: `'validate:"required,email" db:"email"'`. A `json` tag among them replaces the generated one (`'json:"-"'` keeps the column out of JSON), and `{name}` in a value stands for the column's name. A value that is not written that way, repeats a key or holds a back quote is Q028. |

**Struct tags.** In a migration, `struct_tags:` gives the column's
field in the model, and every row field that reads the column,
directly, through a view or through a cast, its tags; in a query
header it gives the one row field its tags, over the migration's key
by key. A [column override](configuration.md#generated-code-go) with
`struct_tags` gives the same, below the migration directive key by
key, and `go.struct_tags` gives every field project-wide tags below
both. The generated tag writes `json` first and the other keys by name:

```sql
CREATE TABLE users (id BIGINT PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL);
-- sgl.column(users.email, struct_tags: 'validate:"required,email"')
-- sgl.column(users.password_hash, struct_tags: 'json:"-"')
```

```go
type Users struct {
	ID           int64  `json:"id"`
	Email        string `json:"email" validate:"required,email"`
	PasswordHash string `json:"-"`
}
```

A `Params` struct and the field holding an embed group carry no
tags, as they carry no `json` tag.

### Migration directive: `sgl.migration`

In a migration file (`*.up.sql`), `sgl.migration(readonly: ...)` overrides the read-only
detection on the `CREATE FUNCTION` or `CREATE PROCEDURE` statement immediately following
it, and on no other: another overload of the same name, or a function of that name in
another schema, keeps its own detection, so put a directive above each one you mean to
mark. Useful when a function's declaration and body do not show what it does, such as a
function written in C, or one that is declared `MODIFIES SQL DATA` but only reads.
Detection follows the function's data access (its declaration, its body and the functions
it calls), never its volatility; see
[read-only analysis and replicas](../explanation/replicas.md#how-a-function-call-is-routed).

```sql
-- sgl.migration(readonly: true)
CREATE FUNCTION calculate_score(item_id INT) RETURNS INT
LANGUAGE SQL AS $$ SELECT item_id * 10 $$;

-- sgl.migration(readonly: false)
CREATE FUNCTION announce_change() RETURNS VOID
LANGUAGE SQL AS $$ SELECT pg_notify('changes', 'updated') $$;
```

`sgl.migration` also accepts `no_transaction: true` above a statement that cannot run inside a
transaction. The generator ignores `sgl.migration` in a query file and ignores `sgl.query`
in a migration file (each raises a Q044 warning), as it does a query directive such as
`sgl.param` in a migration.

Migration directives are checked as query headers are, and each problem is reported at
the directive's line: a misspelt key such as `no_transacton` is
[Q027](diagnostics.md#q027) with the key it most resembles, a value other than `true`
or `false` and an `sgl.column` without its table are [Q028](diagnostics.md#q028), an
unclosed call or one continued on the next line is [Q032](diagnostics.md#q032), an
open string [Q035](diagnostics.md#q035), text after the call
[Q007](diagnostics.md#q007), and an unknown role such as `sgl.migraton`
[Q026](diagnostics.md#q026). A migration that leaves more than 64 strings, quoted
identifiers or comments unterminated is read only up to the 65th: the directives after it
are not read, and [Q039](diagnostics.md#q039) marks the line where reading stopped. One
migration file reports at most 256 such problems, then one Q066. A call that does not
parse, being unclosed, continued, followed by text or holding an open string, sets
nothing; one that parses keeps its well-formed arguments, the first of a repeated key.
The migration runner goes further for `sgl.migration`: before anything runs, it refuses a
migration whose `sgl.migration` directive is malformed or names an unknown role, with
[an error naming the file, the line and the problem](migration-runtime.md#errors).

For a query's read-only flag, use the keyword on the query header instead:

```sql
-- sgl.query(BareReadOnly, many, readonly: true)
SELECT id, name FROM items;
```

## Parameter types

Each placeholder becomes a Go parameter. Seagull names it from the first of these that
applies:

1. the name its binding directive gives (`-- $1 as sgl.param(user_id)` gives `user_id`);
2. the placeholder's own name (`:email` gives `email`);
3. for a bound of a `BETWEEN`, the column it bounds with `_from` or `_to` added
   (`published BETWEEN $1 AND $2` gives `published_from` and `published_to`);
4. the column it is compared with or assigned to, on either side of the comparison
   (`WHERE status = $1` and `WHERE $1 < status` give `status`,
   `INSERT INTO tasks (title) VALUES ($1)` gives `title`, and an `INSERT` without a column
   list names each value after the column at its position), with `_like` added for a
   `LIKE` pattern (`name LIKE $1` gives `name_like`); a placeholder passed alone to a call
   compared with a column or a call over one column (`email = lower($1)`,
   `lower(name) = lower($1)`), and one beside a column in `COALESCE`, `NULLIF`,
   `GREATEST` or `LEAST`, are named after that column;
5. the name the function's signature gives the argument it is passed as, where every
   overload the call's argument count reaches agrees on it;
6. `limit` or `offset` in those clauses;
7. `pN`, from the placeholder's position, when nothing else names it.

When two parameters share a name, the later ones gain a suffix: `id`, `id_2`, `id_3`.

A query with one parameter takes it as a method argument. On SQLite:

```sql
-- sgl.query(DeleteTask, exec)
DELETE FROM tasks WHERE id = ?;
```

```go
func (queries *Queries) DeleteTask(ctx context.Context, id int64) error
```

Two or more parameters make a `<Query>Params` struct:

```sql
-- sgl.query(CreateTask, one)
INSERT INTO tasks (title, created_at) VALUES (?, ?) RETURNING id, title, created_at;
```

```go
type CreateTaskParams struct {
    Title     string
    CreatedAt int64
}

func (queries *Queries) CreateTask(ctx context.Context, params CreateTaskParams) (CreateTaskRow, error)
```

`RETURNING` works on the PostgreSQL family, SQLite and DuckDB, and on MariaDB after `INSERT`
and `DELETE`. MySQL and ClickHouse have none, so the same query there is error
[Q052](diagnostics.md#q052); use `exec` or `execresult` and read the row back
separately. `dialect_options.returning` changes what MySQL and MariaDB accept (see
[configuration](configuration.md#dialect-options-dialect_options)).

A single parameter also makes a params struct when it is `optional: true`, `kind: slice`, or a
`LIMIT` or `OFFSET` with `default:` or `max:`, and so does `sgl.sortable`. `batch` and
`copyfrom` take a slice of the struct.

The types on the params struct come from the engine's type inference over the SQL expression. Override them with type hints if the inference is too loose (see [type mapping](type-mapping.md)).

## Generated types

`sgl generate` writes these files into the output directory configured for the database
(`go.out` in `sgl.yaml`). [Generated Go API](generated-go.md) is the full reference,
including the helper files a query may add.

### `querier.go`

Defines the `DBTX` interface and the `Queries` struct, built with `New(db)`. With
database/sql it also has `NewWithReplica(writer, reader)`, which sends each query to the
reader or the writer as [reader and writer routing](#reader-and-writer-routing) decides,
and `WithTx` and `RunInTx`; pgx has `WithTx` and `RunInTx`, and the native ClickHouse
driver neither. See [`querier.go`](generated-go.md#queriergo) for each emitter's methods
and for wrapping a transaction with `WrapDBTX`. With `go.interface.enabled: true`,
`querier_interface.go` declares an interface of the query methods; see
[`querier_interface.go`](generated-go.md#querier_interfacego).

### `models.go`

A struct for every table the migrations define, but not for a view or a partition, with a
field per column, and the named types the queries and tables use: each enum as a string
type with a constant per value, and each struct of named fields as a struct type. See
[`models.go`](generated-go.md#modelsgo).

### `prepared.go`

A prepared-statement cache for the database/sql emitter, written only when the database
sets `go.prepared: true`, and used once you pass the result of `store.Prepare(ctx, db)` to
`New`. See [`prepared.go`](generated-go.md#preparedgo).

### `<name>.sql.go`

One file per query file, named after it, holding each query's SQL constant, its parameter
and row structs and its method. See [`<name>.sql.go`](generated-go.md#namesqlgo).
