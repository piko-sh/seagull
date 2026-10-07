---
title: DuckDB
description: 'Select the dialect with dialect: duckdb in sgl.yaml, or dialect_registry.New("duckdb",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 170
---

# DuckDB

Select the dialect with `dialect: duckdb` in `sgl.yaml`, or `dialect_registry.New("duckdb", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | DuckDB |
| Generated API | database/sql |
| Live introspection | No built-in source |
| Runtime migrations | A backend exists, but the stock CLI does not link its cgo driver. A custom CLI or application can run migrations. |

[Known limitations](../limitations.md#duckdb) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/duckdb.md) and [runnable examples](../../../examples/duckdb) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

### Collections, structures and rows

STRUCT, MAP, LIST, fixed-size arrays and UNION can nest, up to 63 levels below the
column: a declared type nesting 64 or more levels, counting list dimensions, fails its
migration with an error matching `seagull.ErrLimit`, and a result column whose type nests
that deep is refused as unreadable ([Q059](../diagnostics.md#q059)). Every part of a
declared type needs its type, so `CREATE TABLE t (a)`, `STRUCT(a, b INT)`, `MAP(INT)` and
an empty `STRUCT()` or `UNION()` are syntax errors, as DuckDB reports them.

Field access, list subscripts and map lookup are typed from the declared fields or
elements. Enums retain their Go type inside these values. Struct fields can always hold
NULL; list elements and map values in results generally use pointers unless declared
non-null. DuckDB parameters use plain elements because its driver cannot bind NULL
elements or map values ([Q059](../diagnostics.md#q059)).

Struct, list and map literals, list comprehensions and lambdas are understood.
`struct_pack(a := x)` and `{'a': x}` name and type their fields, however many there are;
`struct_insert` adds named fields. Maps combine their keys' and values' types.
`map_from_entries` reads each entry's first field as its key and second as its value.

ROW and parenthesised values create unnamed structs. They can be compared, but the
driver cannot read one in a result, even inside another composite (Q059). Name its fields
with `struct_pack`. Map keys cannot be LIST, STRUCT, MAP or UNION (Q059); select
`map_entries`, `map_keys` or `map_values` instead.

A whole-row reference such as `to_json(boats)` is a STRUCT of the relation's columns.
The row itself is non-null even on an outer join's nullable side, where its fields are
nullable. A visible column wins over a relation with the same name.

### Query forms and aliases

Seagull understands FROM-first queries, TABLE, trailing select-list commas, GROUP BY ALL,
ORDER BY ALL, QUALIFY, named windows, sampling, UNION BY NAME, INSERT BY NAME, and
positional, ASOF, SEMI and ANTI joins. VALUES is a query wherever one is allowed, with
columns named `col0`, `col1` and so on. Table functions with WITH ORDINALITY gain a
non-null BIGINT column that alias column lists can rename.

Later select items can reference earlier aliases, including their inferred nullability.
WHERE, HAVING, QUALIFY and ORDER BY can use an alias within an expression; GROUP BY takes
one only as a whole key. Visible columns win over aliases, and forward aliases are
unknown columns. An earlier item used by a later one is not cast to VARCHAR for driver
scanning because that would change the later expression's type.

An alias on a parenthesised join hides its inner table names. If its last join uses
USING, DuckDB ignores the alias instead; Seagull follows that rule and warns
([Q063](../diagnostics.md#q063)). A table alias column list renames leading columns
and hides the replaced names.

### Functions and inferred types

Scalar macros are typed from their bodies with the call's argument types. Each macro is
typed once for each set of argument types, a macro that calls itself stays `any`, and
typing one call types at most 256 macro calls in all, leaving any further ones `any`.
Table macros have typed columns when their bodies project plain catalogue columns. A call
of a scalar macro, or of a table macro in FROM, resolves through the search path in order
when it is unqualified, and a schema-qualified call, such as `reports.f()`, in the schema
it names, whatever the case it is written in.
`DROP MACRO TABLE name` drops a table macro. Unknown constructs or recursion can leave a
macro result as `any`. Read/write routing follows the body's data access, a `nextval` call
inside a lambda included.

Lambdas receive list elements as parameters. Transform returns the lambda's result type,
filter retains the list type, and reduce returns the accumulator's type. Transform and
filter can receive an element position as a second parameter; reduce can receive it as
a third. Lambda variables hide same-name columns, and a field of a struct the lambda
receives, as in `x -> x.a`, is read from the struct. A comparison with a lambda parameter
types the query placeholder from that parameter.

The built-in catalogue includes analytics aggregates, date, string, JSON and list
functions. For example, `histogram` returns a map of values to UBIGINT counts, quantile
functions can return a list for a list of fractions, and `range` returns a list as a
value or rows in FROM. File table functions include `read_parquet` and `read_csv`;
INSTALL and LOAD are accepted statements. Missing function metadata is
[Q005](../diagnostics.md#q005).

A decimal literal is DECIMAL, an exponent literal DOUBLE, and an integer the narrowest
of INTEGER, BIGINT and HUGEINT that holds it. Typed literals and structured casts retain
their SQL types. FLOAT, REAL and FLOAT4 map to `float32`; DOUBLE and FLOAT8 map to
`float64`, as does FLOAT with more than 24 bits of precision.

TRY_CAST and `try(expression)` can return NULL despite non-null inputs. Date functions
can infer a quoted argument from the other arguments; an ambiguous literal-only call
such as `year('2024-01-01')` should be cast explicitly. List aggregates and literals of
non-null values can prove their elements non-null, as can `map_keys`.

### Schema replay and rejected syntax

Sequences, enums, generated columns, ON CONFLICT and changes to column nullability are
replayed. Views follow their source tables' current columns and types. If a view names
a column later renamed, Seagull keeps its old columns while DuckDB fails to read it.
After each change Seagull derives again only the views that read a changed table or view,
and views that keep changing each other stop at a round limit with a
[Q071](../diagnostics.md#q071) warning.

A view, `CREATE TABLE ... AS` or table macro body with a syntax error fails its
migration. A body Seagull cannot analyse, such as a `PIVOT` that takes its columns from
the data, an `UNPIVOT` that does not list its columns, or one nested past the parse
depth, is [Q069](../diagnostics.md#q069), and the view keeps the column names it
declares, without types. Creating a table under a name a table already holds, or renaming
one onto a name in use, is [Q067](../diagnostics.md#q067), and altering a table, or
dropping a table, view, sequence, type or macro, that does not exist without `IF EXISTS`
is [Q068](../diagnostics.md#q068).

Unsupported clauses are rejected rather than silently discarded. Malformed statements
and conditions are [Q010](../diagnostics.md#q010), including locking clauses,
table-qualified SET targets, LIST(T) declarations, empty select lists, a `CAST` without
`AS`, its type or its closing parenthesis, and a migration statement that runs on into
the next because the semicolon between them is missing. Reading a column outside every
aggregate without GROUP BY is Q062, an error. `INSERT OR IGNORE ... RETURNING` on a `one`
query gets the [Q046](../diagnostics.md#q046) warning that `ON CONFLICT DO NOTHING`
does.

### PIVOT and UNPIVOT

A `PIVOT` produces one column per value in its `IN` lists, typed by its aggregate, after
the columns it groups by:

```sql
PIVOT landings ON species IN ('cod', 'hake') USING sum(kg) GROUP BY harbour;
```

A pivoted column without an `IN` list takes its columns from the data, which Seagull cannot
know when it generates code, so the query fails with a diagnostic that asks for the values.
Without `GROUP BY`, a `PIVOT` groups by every other column of what it reads, and an `UNPIVOT`
keeps them; that needs a table the migrations create, or a subquery that names its columns.
`UNPIVOT` must list the columns it unpivots by name.

A `PIVOT` makes one column for each combination of its `IN` lists' values and each
aggregate, at most 4,096. One that would make more fails with an error matching
`seagull.ErrLimit`, placed at the `PIVOT` keyword: pivot on fewer values.

### Not supported

- `SELECT *` with `EXCLUDE`, `REPLACE` or `RENAME`, and `COLUMNS(...)` in a select list:
  Seagull expands a star from the catalogue and cannot leave out, replace, rename or match
  columns, so each fails with a syntax error asking for the columns to be listed.
  `COLUMNS(...)` in a `WHERE` or other clause is accepted, since it does not change what the
  query returns.
- `ALTER TABLE ... ADD COLUMN` with a constraint or as a generated column, which DuckDB itself
  refuses: add the column, then `ALTER COLUMN ... SET NOT NULL`.
- `MERGE` runs without analysis: it takes no parameter ([Q010](../diagnostics.md#q010))
  and returns no columns. See [not supported yet](../limitations.md#not-supported-yet).

### Parse limits

`max_parse_depth` (256 by default, at most 4,096) and `max_tokens_per_statement` (100,000
by default, at most 1,000,000) under
[`dialect_options`](../configuration.md#dialect-options-dialect_options), or
`dialect_engine_duckdb.WithMaxParseDepth` and `WithMaxTokensPerStatement` in Go, bound what one statement
may cost; a statement past either is refused with an error matching `seagull.ErrLimit`.
A statement replay passes over, such as a large seed `INSERT`, is never refused for its
length, and is still read to its end for a missing semicolon, so a statement it runs on
into is reported ([Q010](../diagnostics.md#q010)) at that statement's first word. The
Go options lower a value above the ceiling to the ceiling; `sgl.yaml` and `dialect_registry.New`
refuse it.

## Parameters

`$1`, `$2`, ..., `?` and `?N`, and named `:name` and `$name` parameters, all sent as `$N`.
A bare `?` takes the number after the largest one before it, as DuckDB binds it: in
`WHERE a = $1 OR b = ?` the `?` is the second parameter, and in `WHERE a = ? OR b = $1`
both bind the first. A named parameter takes the number after the largest numbered one
anywhere in the statement, so in `WHERE city = :city AND id > $1` the name is the second
parameter, wherever it is written. A name gives the Go parameter its name, and a name used twice, in
either form, binds one value. `$name` followed directly by another `$`, as in
`$tag$...$tag$`, opens a dollar-quoted string instead, and a `:` written directly after an
operand separates rather than starting a parameter, so `lambda x:x + 1`, `{'k':v}` and
`list[a:b]` hold no parameter while `id=:id` does. A placeholder in a statement Seagull runs
without analysis, such as `SET threads = $1` or `COPY ... TO $1`, cannot be bound and is
[Q010](../diagnostics.md#q010).

A nil Go slice passed for a list or array parameter binds `NULL`, and an empty one an empty
list, as on the PostgreSQL family (see
[nil and empty slices](../type-mapping.md#nil-and-empty-slices)); a `kind: slice` list writes
DuckDB's empty-list test either way.

The DuckDB driver binds a `MAP` only from its own `duckdb.Map` or `duckdb.OrderedMap`, a
`STRUCT` only from a map keyed by field name, and a list of either only from a `[]any`, and
it panics on a list element of a type it does not know, such as the generated `JSON` type.
The generated code therefore passes `MAP`, `STRUCT`, list-of-`STRUCT` and list-of-`JSON`
parameters through the generated `sglDriverComposite`. It copies a Go map into a
`duckdb.Map`, a struct into a map keyed by its fields' `json` names, and a slice into a
`[]any`, converting nested values the same way. A nil map, slice or pointer binds as `NULL`,
and a `driver.Valuer`, such as a `JSON` document, binds as its `Value`, the document's text.
A value that is already one of the driver's own types, a byte slice and a struct with
unexported fields, such as a `time.Time`, are passed through as they are. The generated
package then imports `github.com/duckdb/duckdb-go/v2`.

The driver builds a `LIST` or `ARRAY` parameter from the type the statement gives it, and
cannot build an element of an enum, a `DECIMAL`, a `BLOB`, a `UUID` or an `INTERVAL` that
way: binding one fails with "unsupported data type". It does bind a list a
`driver.Valuer` returns, typed by the Go values the list holds, and DuckDB casts that list
to the parameter's type. A list parameter of such elements, nested lists and fixed-size
arrays included, therefore goes through the generated `sglDriverInferredList`, which
converts each element as `sglDriverComposite` does, an enum to its string and a decimal or
a UUID to the text its `Value` writes, and hands the list over that way; a nil slice binds
as `NULL`. So does a `UTINYINT` list, whose Go `[]uint8` the driver would otherwise take
for a `BLOB`. The driver infers no map or struct, so a `MAP` or `STRUCT` parameter holding
such an element, at any depth, an enum included, is an error (Q059) naming the parameter:
build the value in the SQL from parameters instead, a struct as `{'amount': $1::DECIMAL(10,
2), 'currency': $2::VARCHAR}` or `{'note': $1::VARCHAR, 'feeling': $2::mood}` and a map as
`map($1::VARCHAR[], $2::DECIMAL(10, 2)[])` or `map($1::VARCHAR[], $2::mood[])`, whose list
parameters bind as above.

## Dialect options

`dialect_engine_duckdb.New` takes `WithName` and the two parser limits,
`WithMaxParseDepth` and `WithMaxTokensPerStatement`, as the
[exported declarations](#exported-declarations) list. `WithName` changes the name the
dialect reports, and its family stays `duckdb`. Options apply in the order they are given,
so a later one of the same kind wins.

## Migrations

`migrate_backend_duckdb.New()` runs migrations and seeds over `database/sql`:

```go
migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), migrations, "db/migrations")
```

- Each migration runs in a transaction with its history row, since DuckDB's DDL is transactional. A file with `-- sgl.migration(no_transaction: true)` runs statement by statement instead, and an interrupted run resumes after the last statement that completed.
- Seed files run whole: the driver runs every statement of one call.
- There is no lock. DuckDB has neither advisory locks nor row locks, and a database file is opened for writing by one process at a time, which keeps other processes out. Within one process, run a database's migrations through one `Migrator`. `WithLock(LockAdvisory)` and `WithLock(LockTable)` fail with `migrate.ErrUnsupportedLock`.
- The history tables are `sgl_migrations` and `sgl_seeds` by default, in the session's current schema; `WithNamespace` and `WithHistoryTable` rename them as on every other backend.

See [Migrations and seeds](../migration-runtime.md#duckdb) for the rest.

## Drivers

`database/sql` with `github.com/duckdb/duckdb-go/v2` (cgo). The stock `sgl` binary cannot connect to DuckDB; a [custom binary](../cli.md#custom-binaries) that links the driver with `cli.WithDriver("duckdb", "duckdb")` runs `sgl migrate` and `sgl seed` against it.

The driver returns a `LIST`, a `STRUCT` and a `MAP` as a `[]any`, a map keyed by field
name and its own `duckdb.OrderedMap`, holding its own Go values. The generated code reads
such a column through the generated `sglScanDriverComposite`, which converts those values
into the column's Go type at any depth: time values stay `time.Time`, `DATE`, `DOUBLE` and
`BOOLEAN` map keys keep their types, and `HUGEINT`, `BLOB` and `NULL` elements read
exactly; a `DECIMAL` keeps every digit of its scale and an `INTERVAL` reads as the text
DuckDB casts it to. The column's SQL is left as it is, so a compound query or an embed
reads it too.

The driver returns a `DECIMAL` and an `INTERVAL` in types of its own and decodes a `JSON`
value, returning a JSON string without its quotes and a number as a `float64`. The
generated query casts such a column to `VARCHAR` and reads the text, so a `JSON` column
holds the document exactly as stored. Where the query cannot cast the column, in a
compound query or an embed, `sglScanDriverComposite` converts the driver's value instead:
a `JSON` value is encoded again, so it stays valid JSON, but a number in it keeps only the
digits a `float64` holds, as does a `JSON` value inside a `LIST`, `STRUCT` or `MAP`.

Seagull has no DuckDB introspection, so `sgl schema dump --from-db`, `sgl schema diff` and `schema_from` do not support DuckDB; `sgl schema dump` builds the schema from the migrations.

## Exported declarations

### dialect_engine_duckdb

```go
const Family = duckdb.Family
func New(options ...Option) dialect.Dialect
type Option = duckdb.Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
```
