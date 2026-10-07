---
title: PostgreSQL
description: 'Select the dialect with dialect: postgres in sgl.yaml, or dialect_registry.New("postgres",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 120
---

# PostgreSQL

Select the dialect with `dialect: postgres` in `sgl.yaml`, or `dialect_registry.New("postgres", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | PostgreSQL |
| Generated API | database/sql or pgx |
| Live introspection | Yes, PostgreSQL-family source |
| Runtime migrations | Transactional DDL with advisory locks by default. Concurrent index operations and other recognised exceptions run without a transaction. |

[Known limitations](../limitations.md#postgresql) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/postgres.md) and [runnable examples](../../../examples/postgres) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

### Schema and query forms

Seagull replays schemas, enums, domains, composite types, sequences, identity columns,
views, functions, triggers and extensions. Domains use their base type and declared
`NOT NULL`. Declarative partitions are queryable, but generated models belong to the
parent; see [partition modelling](../schema-api.md#partitions). Attaching a table as
a partition of itself, attaching one that is a partition already, and an attachment that
would make a table a partition of itself are refused ([Q010](../diagnostics.md#q010)).
A change to a table reaches every table that inherits from or partitions it once, however
the inheritance branches and joins again.

Replay refuses what PostgreSQL refuses. Creating a table or adding a column or an enum
value under a name already taken, or renaming or moving a table, view, sequence, type,
column or enum value onto one, is [Q067](../diagnostics.md#q067). Altering, renaming
or dropping a table, view, sequence, type, domain, function, column or enum value that
does not exist, or an object of another kind than the statement names, as `DROP VIEW` of
a table, is [Q068](../diagnostics.md#q068), unless the statement says `IF EXISTS`.
`ALTER SEQUENCE ... RENAME TO` or `SET SCHEMA` changes only a sequence, so naming a table
there is Q068. `ALTER TYPE ... ADD VALUE` and `RENAME VALUE` reach every column, view
column, composite field, domain and function type of the enum. A function's argument and
return types, and a composite type's fields, that name an enum, a composite type or a
domain carry that type, so a call passing an enum column to a function taking the enum
resolves. A composite type or a `RETURNS TABLE` with more than 1664 fields fails.

A type's name folds as PostgreSQL folds it: an unquoted name to lower case, while a quoted
one keeps its letters, so a column, argument, return or `SETOF` type, a cast or a column
definition list written `"Mood"` or `"Shapes"."Pair"` names the type created under that
name, apart from `mood` or `pair`. A quoted name names a built-in type only when written
in lower case, so `"Point"` is a type of its own, apart from `point`, and a quoted word is
never a keyword, so `"double" precision` is a syntax error. A cast to an enum, a composite
type or a domain, and a column definition list naming one, gives that type:
`'calm'::mood` is the enum `mood`.

A type keeps its modifiers wherever it is written: in a column, a cast, a column
definition list and a type a directive names, as in `$1::numeric(10, 2)`,
`CAST(x AS varchar(40))`, `f() AS x (seen timestamp(3) with time zone)` and
`type: 'numeric(10,2)'`. A cast may write a time zone after its precision, as in
`$1::timestamp(3) with time zone`. A negative scale keeps its sign, as in
`numeric(5, -2)`, and a modifier written as a fraction, an exponent or a `0x`, `0o` or `0b`
integer is read as its value, so `numeric(10.5, 2)` has precision 10. A modifier is held
at 1,073,741,824, and only the first two of a list are kept; the ranges PostgreSQL allows
are not checked (see [known limitations](../limitations.md#postgresql)).

A view or `CREATE TABLE ... AS` body is analysed whether it is a query, `VALUES`, `TABLE`
or a parenthesised query. `CREATE TABLE t (a, b) AS ...` names the query's columns, and
`CREATE [MATERIALIZED] VIEW` with `WITH (...)`, `USING` or `TABLESPACE` before `AS` is
read; without `AS` it is a syntax error. A body with a syntax error fails the statement. A
body Seagull cannot analyse, such as one nested past the parse depth, is
[Q069](../diagnostics.md#q069): the view keeps only the column names it declares,
without types. The storage parameters of `WITH (...)` must be well formed.

Queries support `RETURNING`, `ON CONFLICT`, recursive and data-modifying CTEs, `LATERAL`,
set operations, parenthesised queries, `TABLE name`, and `VALUES` wherever a query is
allowed. VALUES columns are named `column1`, `column2` and so on. An empty select list
(`SELECT FROM t`) returns rows without columns. `INSERT INTO t (a, b) (SELECT ...)` is
analysed as the unparenthesised form is, so its parameters take the target columns'
types; a parenthesis after the column list that opens no query is a syntax error, and so
is an `ON CONFLICT` clause without `DO NOTHING` or `DO UPDATE`.

Nested scopes retain the CTEs visible to them. Alias column lists rename leading columns
by position; a shorter list keeps the remaining names. An alias on a parenthesised join
names all its columns and hides the names inside it.

### Function and row results

Built-in catalogues cover common string, maths, date, JSON, regular expression and
aggregate functions, plus settings, advisory locks and notifications. A missing function
is [Q005](../diagnostics.md#q005); declare it under `custom_functions`. Calls to
`nextval`, advisory locks or `pg_notify` route to the writer. A SELECT whose only result
is such a function's `void` fits `exec`.

A quoted function argument can take its parameter's type, so
`date_bin('15 minutes', ts, '2000-01-01')` types without casts. Numeric arguments can
convert to `double precision`. Operator operands retain their literal types:
`ts + '1 day'` needs an interval cast.

Functions declared in migrations include SQL, PL/pgSQL and SQL-standard bodies. Their
return signatures supply types; their data access controls routing, not volatility. A
function's name folds as PostgreSQL folds it: `"Label"()` and `label()` are two functions,
and a call reaches the one its own name names, so `"Label"(x)` is typed, routed and has its
placeholders typed from `"Label"`, and `label(x)` or `LABEL(x)` from `label`.
`CALL` with OUT or INOUT parameters returns those values as a row. A procedure without
them fits `exec`.

FROM-functions can return a base value, record, composite or table row. A one-column
set-returning base function such as `unnest(tags)` uses its FROM alias as its column
name. Functions with named outputs, such as `jsonb_array_elements` (`value`) or OUT
parameters, keep those names. `WITH ORDINALITY` appends a non-null `bigint` column;
an alias column list can rename it.

`ROWS FROM` concatenates each function's columns and pads shorter results with NULL.
Multi-array `unnest` has the same padding. Table-valued overloads are selected by their
argument types. `jsonb_populate_record(NULL::results, doc)` expands the declared row
fields, all nullable.

A whole-row reference, such as `row_to_json(t)` or `SELECT t`, reads the relation's row.
Tables and views have named composite types; derived tables and CTEs have `record`.
These map to `any`. The row of a single-value function is that value, and a row on the
nullable side of an outer join may be NULL. A column of the same name wins over the row.

Composite field selections such as `(address).street` use the field's declared type.
Whole composite values and `(address).*` remain `any`. Set-returning calls in a select
list use their value type, or `any` for record results such as `jsonb_each`.

### Expressions and parameters

Arrays can contain NULL elements independently of their declaration. Results and array
parameters therefore generally use pointers for elements. Quantified comparisons and
`kind: slice` inputs use plain elements; `array_agg` and array literals of non-null
values can also prove their elements non-null. `ARRAY(SELECT ...)` is non-null itself.
See [element mapping](../type-mapping.md#elements-that-may-be-null).

Strings and quoted names are read as PostgreSQL reads them: `E'...'` decodes its
backslash escapes (`\n`, `\t`, octal, `\x`, `\u` and `\U`), which also changes the text
of a function body or comment written as one; a `U&'...'` string and a `U&"..."` name
decode their Unicode escapes, with `UESCAPE` naming another escape character; `X'...'` is
a bit string, as `B'...'` is; and `N'...'` is an ordinary string. `COMMENT ... IS` takes
an `E'...'` or a dollar-quoted string as well as a plain one.

JSON operators, NULL-safe comparisons, pattern tests and distance operators are typed.
Ordinary comparisons and logical operations over nullable values may return NULL;
`IS NULL`, `IS [NOT] DISTINCT FROM` and `EXISTS` are non-null. Outer joins propagate
nullability to tables, CTEs and derived tables alike.

A scalar subquery that aggregates without GROUP BY returns one row and is nullable only
when its column is. Reading a column outside every aggregate without GROUP BY is an
error ([Q062](../diagnostics.md#q062)).

Assignment expressions type their inner parameters from the expression rather than the
destination column. `SET n = n + $1` takes the number's type, while a parameter beside a
timestamp may remain `any`. Row comparisons and row assignments type values by position;
array-subscript assignments type the subscript as an integer and the value as an element.
LIMIT, OFFSET and FETCH accept expressions over parameters; an uncast parameter there
is an integer. PostgreSQL considers OFFSET before LIMIT, so cast ambiguous repeated
parameters.

### Search paths and migration SQL

`SET search_path` controls subsequent migration replay. Unqualified creations use the
first existing schema, or the first entry if none exists in the catalogue. `SET LOCAL`
lasts to the end of its migration; RESET and DEFAULT restore the starting path.
`SET SCHEMA` selects a one-schema path, and `"$user"` is omitted.

Query analysis uses `dialect_options.search_path` (`public` by default), independently
of migration changes. A table on that path has a model name without a schema prefix. An
unqualified set-returning function in FROM resolves through that path in order, as
PostgreSQL resolves it, and a schema-qualified call reaches the schema it names.
Database-qualified names are treated as schema-qualified names; the server still checks
the database. See [runtime sessions](../migration-runtime.md#sessions-and-the-search-path).

psql meta-commands at statement boundaries are skipped, including pg_dump's restrict
lines. Statements such as SET, TRUNCATE and LISTEN generate no result columns. Unsupported
statements or malformed conditions are [Q010](../diagnostics.md#q010).

### Parse limits

`max_parse_depth` (256 by default, at most 4,096) and `max_tokens_per_statement` (100,000
by default, at most 1,000,000) under
[`dialect_options`](../configuration.md#dialect-options-dialect_options), or
`dialect_engine_postgres.WithMaxParseDepth` and `WithMaxTokensPerStatement` in Go, bound what one
statement may cost. A statement nested deeper than the depth, counting chains of prefix
operators such as `- - - 1`, nested `POSITION` calls and nested `GROUPING SETS`, `ROLLUP`
or `CUBE`, is refused ([Q010](../diagnostics.md#q010)) with an error matching
`seagull.ErrLimit`. A statement is tokenised no further than the token budget, and one
past it is refused when Seagull parses it, while a large seed `INSERT` in a migration,
which replay passes over, is still accepted. Such a statement is still read whole for a
missing semicolon, so a statement it runs on into is reported
([Q010](../diagnostics.md#q010)) at that statement's first word. The Go options hold a
value above the ceiling at the ceiling; `sgl.yaml` and `dialect_registry.New` refuse it. One
statement reports at most 32 of the dialect's warnings, then one saying how many more were
left out. The same limits apply to CockroachDB and TimescaleDB.

## Parameters

`$1`, `$2`, ... and named `:name` parameters. A named parameter gives the Go parameter its
name and is sent as `$N`, so a name used twice binds one value. In a query that writes both
forms, the names are numbered after the highest `$N` it writes anywhere, a `WITH` body
included, so `SET name = :new_name WHERE id = $1` sends `:new_name` as `$2`. `:name` is a
parameter where a value is expected; a colon written directly after a name, number, quote
or closing bracket separates the bounds of an array slice, so `tags[lo:hi]` holds no
parameter, while `tags[lo:hi] = :wanted` holds one. PostgreSQL types a parameter where it
first meets it, and cannot type one it meets first as the operand of `IS NULL`, as an
argument of a function that takes any type, such as `concat` or `json_build_object`, or as
either operand of arithmetic between two parameters, including in an `OFFSET`, which it
types before the `LIMIT`, so every call fails with "could not determine data type of
parameter"; Seagull warns of such a parameter ([Q063](../diagnostics.md#q063)) where
every place the statement writes it is in its own clauses rather than a subquery or a
FROM item. An
[optional filter](../directives.md#parameter-directives) therefore casts it:
`($1::text IS NULL OR status = $1)`, or `(:status::text IS NULL OR status = :status)`.
`@name` and `?`, the placeholders of other tools, are no parameters on PostgreSQL: it reads
`@name` as the absolute value of the column `name` and `?` as the jsonb key test, so
Seagull warns of each ([Q063](../diagnostics.md#q063)), and a syntax error in a
statement that writes one is reported at it.

A placeholder compared with each element of an array, as in `id = ANY($1)` or
`id <> ALL($1)`, is an array of the compared value's type (`[]int64` for a `bigint` id),
as PostgreSQL types it, and is named after the column. A placeholder compared with a
`CAST` takes the type the `CAST` converts to, so `CAST(born AS text) = $1` takes a
`string`, not the `date` column's `time.Time`, and is named after the column the `CAST`
reads, as one compared with a call such as `length(p.name)` or with a parenthesised
column is. A placeholder after a column and an operator whose operands are of one type,
the distances (`<->`, `<=>`, `<#>` and the others above) and the bitwise `&`, `|` and
`#`, takes the column's type and name, as PostgreSQL types it, so
`ORDER BY embedding <-> $1` takes the type of `embedding`; so does one after a column and
an arithmetic operator, in the select list and `RETURNING` as in a `WHERE` clause, so
`SELECT n + $1` takes `n`'s type, unless a cast after the placeholder gives one. A
placeholder before such an operator, as in `$1 + n`, is not typed this way. PostgreSQL takes no
parameter in `SET`, `SET LOCAL`, `NOTIFY` or any other statement Seagull runs without
analysis, so a placeholder there is [Q010](../diagnostics.md#q010), which names the
form to write instead: `SELECT set_config('app.user_id', $1, true)` for `SET LOCAL` (or
`false` for the session) and `SELECT pg_notify('channel', $1)` for `NOTIFY`, where the
server has `pg_notify`.

## Migrations

Advisory locks (`migrate_backend_postgres.New()`), or a lock table behind PgBouncer (`migrate_backend_postgres.NewPgBouncer()`). DDL is transactional.

A run applies its migrations on one session, so a `SET search_path` lasts for the later
migrations of the run, as the generator replays them, and it qualifies the history and seed
tables with the schema `current_schema()` names when the run starts, so such a `SET` cannot
move them. See [Sessions and the search path](../migration-runtime.md#sessions-and-the-search-path).

## Introspection

`sgl schema dump --from-db`, `sgl schema diff` and `schema_from` read the schema from a
live database through `pg_catalog`. They cover every schema the role may use except the
system schemas, TimescaleDB's internal schemas and the schemas an extension created, and
they leave out relations an extension owns. Tables, partitioned tables, partitions (each
naming the table it partitions), foreign tables, views and materialised views are read,
and a column the server hides from `SELECT *`, as CockroachDB hides `rowid`, is read as
hidden. Array columns keep
their element type and dimensions, enum and composite columns are matched to their
types, and a domain is read as the type it is declared over, including its `NOT NULL`.
The same applies to CockroachDB and TimescaleDB.

## Drivers

`database/sql` (with `github.com/jackc/pgx/v5/stdlib`) or `pgx` (native `pgx/v5`, with `COPY FROM` and batch support).

pgx has no encoding for an array of a type the connection has not loaded, such as an
enum's array type, so the generated code binds a slice of a generated enum type that pgx
would send as an array as `[]string`: with `driver: pgx`, a slice parameter or an enum
array parameter; behind `database/sql`, which expands a slice parameter into one
placeholder per element, an enum array parameter. `driver: pgx` reads an enum array
column the same way, and `database/sql` reads arrays as JSON. pgx sends and reads the
array as text, which PostgreSQL parses as the enum's array type, so the connection needs
no `LoadType`. Two cases cannot work that way, and the generated code refuses them with
[Q059](../diagnostics.md#q059):

- A `copyfrom` into an enum array column. `COPY` sends every value in binary, which pgx
  writes for an enum array only on a connection that has loaded the enum and its array
  type (`LoadType` for `mood` and `_mood`, then `TypeMap().RegisterType`), and the
  generated code cannot rely on that. Use `command: batch`, which binds the array as text,
  or insert the column with another query. A `copyfrom` into an enum column itself works:
  `COPY` sends the label the value holds.
- A column or parameter of an enum array of more than one dimension, such as `mood[][]`,
  which pgx reads and writes only through a loaded type. `driver: pgx` refuses both.
  Behind `database/sql` such a parameter is refused too, as pgx's `stdlib` binds it with
  the same encoder (a slice parameter is judged by its elements, which `database/sql`
  binds one by one), while such a column works, since `database/sql` reads arrays as
  JSON.

`driver: pgx` also refuses, with [Q059](../diagnostics.md#q059), a parameter that is an array of a composite type or
of a table's row type, such as `$1::pair[]` or `unnest($1::users[])`, whose Go type no
override chose: pgx encodes such an array only on a connection that has loaded and
registered the type. Pass each field as an array of its own and build the rows with
`unnest(...)`, or override the parameter's Go type.

Both emitters read `money`, `timetz`, `bit` and `varbit` into a `string`: `money` as the
text the server writes, with the currency symbol `lc_monetary` gives (`$12.50` in the `C`
locale), and a `string` parameter as text the server parses, such as `12.50`; `timetz` as
its text, such as `09:15:00+02`; and a bit string as its 0s and 1s. pgx decodes `bit`,
`varbit`, `tsvector` and `interval` from their binary form only into its own types, so a
`driver: pgx` package reads a bit string, a `tsvector`, and an interval whose Go type is a
string, through a cast to `text`. A `bytea` parameter whose Go type an override chose, such
as a UUID, is bound as the bytes the value marshals itself to (`encoding.BinaryMarshaler`)
on both emitters, rather than the text its `driver.Valuer` writes. Behind `database/sql` an array is read through `to_json`, whose timestamps without
a zone, dates, numbers and `bytea` elements the generated code reads into `time.Time`,
decimal and `[]byte` elements. See [Type mapping](../type-mapping.md#defaults).

## Exported declarations

### dialect_engine_postgres

```go
const Family = postgres.Family
func New(options ...Option) dialect.Dialect
type Option = postgres.Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
    func WithSearchPath(schemas ...string) Option
```
