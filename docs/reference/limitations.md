---
title: Known limitations
description: Where Seagull's analysis is less precise than the engine's, SQL it accepts that the engine rejects, and features it does not support yet, each with a workaround.
section: Reference
subsection: Core
order: 42
---

# Known limitations

Seagull reads SQL without a database. In a few places its analysis is less precise than
the engine's: a column or parameter is typed `any`, a value that can never be NULL becomes
a pointer, a name differs from the one the engine gives, or SQL the engine would reject
passes without a diagnostic. The engine always has the final word. This page lists the
known cases, grouped by topic and then by engine, each with what you see and how to work
around it, followed by the features Seagull does not support yet.

If you meet a case that is not listed, or one listed here makes generated code fail at run
time, please [open an issue](https://github.com/piko-sh/seagull/issues) with the migration,
the query, the `sgl vet` output and the engine version.

## Working around a gap

Most entries below come down to one of these:

- **A column typed `any`**: cast it in the SQL (`CAST(x AS bigint)`, `x::bigint`) or pin
  it in the query header with
  [`-- sgl.column(name, type: bigint)`](directives.md#type-override-sglcolumn).
- **A parameter typed `any` or badly named**: cast the placeholder (`$1::bigint`,
  `CAST(? AS SIGNED)`) or name and type it with
  [`-- $1 as sgl.param(name, type: int8)`](directives.md#sglparamname-type-nullable-default).
- **A pointer for a value that is never NULL**, or the reverse: set `nullable:` on the
  same `sgl.column` or `sgl.param` directive.
- **An unknown function** ([Q005](diagnostics.md#q005)): declare it under
  `custom_functions` in [the database's configuration](configuration.md#database). Each
  dialect's built-in catalogue holds the functions applications commonly call, checked
  against the engine, rather than every function the engine has: administrative and
  niche families are the usual gaps. Until it is declared, such a call is typed `any` and
  its query goes to the writer.
- **A query Seagull cannot analyse**: `sgl generate` leaves that query out and writes the
  rest of the package ([CLI](cli.md)); build it at run time with the
  [runtime query builder](runtime-builder.md), or keep it in hand-written Go.

## Generated code and drivers

- Seagull generates code without a driver, so it cannot know how the driver is built. The
  default build of the cgo SQLite driver, `github.com/mattn/go-sqlite3`, has neither the
  math functions nor FTS5, so a query using them generates but fails at run time. Build with
  `-tags "sqlite_math_functions sqlite_fts5"` (see [SQLite](dialects/sqlite.md#drivers)).
  DuckDB's driver needs cgo, and the stock `sgl` binary does not link it.

- `SELECT *` and `RETURNING *` are expanded to their columns, except a star over a CTE, a
  derived table, a table function, `VALUES` or a set-operation branch, which stays as
  written. With `keep_stars`, a star that holds a column the driver reads through a
  conversion is still expanded. List the columns where it matters.
- A `sgl.embed` field has no `json` tag, so JSON output mixes the embed's Go name with the
  columns' snake_case keys. Encode through your own struct where the JSON shape matters.
- `Prepare` in `prepared.go` reports `preparing statement: ...` without naming the query
  whose statement failed.
- With database/sql, a package whose override names the standard library's `uuid.UUID`
  imports it as `uuid2`, since the emitter keeps the `uuid` name free for
  `github.com/google/uuid` even when the package does not use it. The code compiles.
- A query's description comment stays in the SQL constant, so it reaches the server as a
  comment.
- With database/sql, `copyfrom` into an upsert sends one multi-row statement per chunk, so
  a key repeated within a chunk fails on PostgreSQL; use `command: batch`, which sends
  each row on its own. MySQL and SQLite upsert batches send one statement per row.
- A value passed to a runtime builder's `Where` reaches the driver as given; pass a
  decimal by value rather than as a pointer.
- JSON comes back as the engine stores it. PostgreSQL `jsonb` rewrites key order and
  spacing but preserves a number such as `2.50`; other engines and helpers can change
  numeric formatting too. Re-encoding may lose key order and escape `<`, `>` and `&`.
  See [JSON values](type-mapping.md#json-values) for each path's behaviour.

## Overrides

- A column override does not follow the column through a CTE or a derived table: the
  outer query's column keeps the base type. Add `sgl.column(name, go_type: ...)` to the
  query.
- A placeholder that is part of a larger expression compared with an overridden column
  (`label = $1 || 'x'`) still takes the override. Cast it (`$1::text`) to keep the base
  type.
- On DuckDB, `CAST(... AS ...)` of a placeholder keeps no length, precision or scale, so
  `CAST($1 AS VARCHAR(4))` restates a `VARCHAR(8)` column and takes its override. Write
  `$1::VARCHAR(4)`, which keeps its length, to give the placeholder its own type.
- A `type:` in `sgl.column` or `sgl.param` that names a type the migrations create, such
  as an enum (`type: mood`), is not resolved against the schema, so the column or
  parameter is typed `any`. Name a built-in type, or set `go_type:` for the Go side.
- `kind: slice` lists of `[]byte`-kind override types are not converted on SQLite and
  DuckDB.
- A binary-column override is bound through `MarshalBinary`, but read only through
  `sql.Scanner` or as bytes; `UnmarshalBinary` alone is not used.
- A defined type of another kind than the column's, or one that implements only
  `json.Unmarshaler`, is outside the
  [override contract](type-mapping.md#what-an-override-type-must-support).

## Nullability and typing

- Some built-in function results have a different width or kind than the engine gives,
  for example an integer `abs` typed `numeric`; the value still scans. The engine sections
  list the known ones.
- A result that can never be NULL is sometimes still a pointer (SQLite `ifnull`/`iif`,
  narrowing by `IS NOT NULL`). Set `nullable: false` with `sgl.column`.
- A CTE inside a subquery cannot read the outer query's columns; the result is typed
  `any`.
- `non_null_elements` on a SQL type override matches arrays by element type, so a domain
  over an array is not matched; the inner arrays of a multi-dimensional array are not
  pointers.

## Parameters

- A parameter used only inside an array literal (`ARRAY[$1, 2]`) or a non-parameter
  element of a row assignment is typed `any`. Cast it. Parameters compared with a
  select-list alias take its type in the clauses the dialect supports, such as MySQL's
  `HAVING`.
- A parameter of a set operation's later branch is typed from the first branch's column
  of the same name.
- A parameter passed to a built-in function with several same-arity overloads
  (`length($1)`, `json_each(:x)`), compared through a JSON key operator (`doc ->> $1`),
  or carried through a CTE into `INSERT ... SELECT` is typed `any`. Cast it.
- A cast parameter inserted into a nullable column is not a pointer
  (`VALUES ($1::text)`); use `sgl.param(name, nullable: true)`.
- A parameter used as two different types is reported ([Q004](diagnostics.md#q004))
  only where it is used directly, not through a cast, a call or a larger expression.

## Schema replay and DDL

- Creating a view, sequence, type or function under a name already in use, without
  `OR REPLACE` or `IF NOT EXISTS`, is accepted and the later definition wins, as is a
  table named like an existing view; only a second table of one name is
  [Q067](diagnostics.md#q067). Dropping an index or a trigger that does not exist is
  accepted; dropping a missing table, view or other object the catalogue holds is
  [Q068](diagnostics.md#q068).
- `DROP TABLE ... CASCADE` leaves views that read the table in the catalogue.
- On SQLite and DuckDB, views that read each other in a cycle stop being derived again at
  a round limit, with a [Q071](diagnostics.md#q071) warning, and their columns may not
  match the engine's. Break the cycle.
- A view whose query Seagull cannot analyse, such as one nested past `max_parse_depth`,
  keeps only the column names it declares, without types
  ([Q069](diagnostics.md#q069)). Flatten the query, or pin the types a query reads
  with `sgl.column`.

## Migrations at run time

- On the PostgreSQL family, `SET LOCAL` in a migration that runs without a transaction
  ends with its statement; generation replays every migration as one session; and a
  session `SET` behind PgBouncer in transaction mode is unreliable. See
  [sessions and the search path](migration-runtime.md#sessions-and-the-search-path).
- Generation does not follow `USE` on MySQL and MariaDB, or a DuckDB `SET search_path`:
  name the database or schema in the migration instead. On MySQL and MariaDB the runtime
  keeps its history tables in the database the run started in.
- A psql meta-command in the middle of a statement (`\gset`) is not skipped.

## Diagnostics

Seagull reports what its analysis proves and does not repeat the engine's semantic
checks. These are accepted without a diagnostic and fail when the server runs them:

- a missing comma in `SET`, a `JOIN ... ON` naming a table joined later, an aggregate in
  `RETURNING`, comparing text with a number, a cast to a type that does not exist;
- a derived table without `LATERAL` reading an earlier table in the same `FROM`;
- a CTE column list shorter than its query (MySQL, SQLite);
- unknown `excluded.col` references and upsert `SET` targets outside PostgreSQL, and an
  `INSERT` that leaves out a `NOT NULL` column without a default.

Also:

- A false [Q005](diagnostics.md#q005) on a read query marks it as possibly writing, so
  [the reader and writer routing](directives.md#reader-and-writer-routing) sends it to the
  writer. Declare the function under `custom_functions` with `data_access: read_only`.
- `sgl generate` reads the top level of the query directory only: files in
  subdirectories are ignored without a warning, and globs are not supported.
- The missing-semicolon diagnostic ([Q010](diagnostics.md#q010)) is a heuristic and
  does not catch every run-on statement: it reads a statement whole, however long, but
  finds the next statement only where its first word starts a line outside any
  parentheses, so `CREATE TABLE a (id int) CREATE TABLE b (id int);` on one line is not
  caught. Start each statement on a line of its own, so a missing semicolon is found.

## PostgreSQL

Typing:

- Typed `any`: an array slice (`tags[1:2]`); `inet`, `cidr` and `macaddr`; range and
  multirange types; geometric types; system columns (`xmin`, `ctid`) and `xid`, `cid` and
  `tid`; `ts + '1 day'` (write `'1 day'::interval`); `(jsonb_each(x)).key`;
  `substring(... similar ... escape ...)`, which also gives a Q005 warning. Cast the
  expression.
- A cast to an interval with fields is not read: `CAST(x AS interval day to second)` and
  its parameter are typed `any`, as an unknown type of that name, and
  `$1::interval day to second` is a syntax error ([Q010](diagnostics.md#q010)). Cast to
  `interval`.
- A word that can continue a type's name (`with`, `without`, `time`, `zone`, `varying`,
  `precision`, `character`, `double`, `timestamp`, `interval`) written straight after a
  `::` cast is read as part of the cast's type: `SELECT id::int zone FROM t` is typed `any`
  and the column is named `id`. Write `AS` before such an alias (`id::int AS zone`).
- Type modifiers are not checked against the ranges PostgreSQL allows: `varchar(0)`,
  `varchar(-1)`, `numeric(1001)` and `numeric(10, 2, 3)` are accepted, the last with its
  first two modifiers, where PostgreSQL refuses each.
- `enum_range(NULL::mood)` types its elements as possibly `NULL`, though an enum's labels
  never are, so the column is a slice of pointers, and `non_null_elements: true` does not
  change it. Cast the call, `enum_range(NULL::mood)::mood[]`, and set
  `non_null_elements: true` on that.
- Composite types are not read structurally (`(x).*`, whole composite values, fields
  typed by a table's row, `unnest` of a composite array): they are typed `any`, and a
  column of a row type is always nullable. See [not supported yet](#not-supported-yet).
- `jsonb_path_*_tz` functions are unknown (Q005); declare them under `custom_functions`.
- The `~~` (`LIKE`) operator's precedence is not modelled.
- Array columns are typed with the dimensions they declare, which PostgreSQL does not
  enforce.
- A user function used in `FROM` with several overloads takes the first one that returns
  rows; a procedure in `FROM` is accepted.

Parameters:

- Typed `any`: `generate_series(1, $1)`; a parameter before an operator (`$1 + n`,
  `$1 <-> location`) or beside `||` and the JSON operators. Cast it.
- A `jsonb` column named after an SQL keyword such as `first` or `next`, followed by the
  `?` operator, gets a false [Q063](diagnostics.md#q063) warning that `?` is a
  placeholder; quote the column (`"first" ? 'k'`).
- The Q063 warning for parameters PostgreSQL cannot type does not look inside subqueries,
  `FROM`, CTEs or `FETCH`.
- A parameter tested with `IS NULL` inside a `VALUES` row is named after that row's
  column, and one inside a cast expression such as `CONCAT('1 ', $1)::interval` is typed
  `any` (with the [Q063](diagnostics.md#q063) warning, since PostgreSQL cannot type it
  either). Cast the parameter itself (`$1::text`).

Drivers:

- With pgx, `interval` maps to `time.Duration`, which turns `'1 month'` into 720 hours;
  set `go.types.interval: string` to keep the server's text.
- pgx refuses `copyfrom` into an enum-array column ([Q059](diagnostics.md#q059); use
  `command: batch`) and multi-dimensional enum arrays.
- pgx does not convert bit-string arrays, `tsvector[]` or string-slot interval arrays to
  text; select them with a cast (`col::text[]`).
- A `[]byte` override on a `bytea[]` column names the whole value, not each element.
- `xid8` reads through `int64` on database/sql.

Schema replay:

- DDL inside a `DO` block is not replayed; write it as plain statements.
- `ALTER DOMAIN ... SET NOT NULL` and domain constraints are not applied.
- `ALTER TABLE ... INHERIT` and `NO INHERIT`, inherited `CHECK` constraints and a type
  mismatch between merged inherited columns are not modelled; dropping an inheritance
  parent is treated as `CASCADE`.
- `ALTER TYPE ... ATTRIBUTE` on composite types and `LIKE ... INCLUDING`/`EXCLUDING`
  options other than `INDEXES` and `ALL` are not modelled.
- The sequence behind a `serial` or identity column is not in the catalogue, so selecting
  from it gives a false [Q003](diagnostics.md#q003) warning; create the sequence with
  `CREATE SEQUENCE` if you query it.
- `search_path` changed through `set_config`, a path naming no existing schema, an empty
  path and `"$user"` are not modelled.
- Temporary tables: one created in a migration becomes a model, and one created by a
  query is unknown to later queries.
- Partitions are approximate: changes made to one partition alone are accepted, an
  attached table's own defaults and a parent's column directives for earlier partitions
  are not carried over, and dropping a parent's primary key by constraint name is not
  followed.
- The `postgres` dialect accepts CockroachDB-only DDL (an inline `INDEX` element, a column
  `ON UPDATE`).
- An alias of the select list used inside an expression in `GROUP BY` or `ORDER BY` is not
  reported, although PostgreSQL rejects it.

## CockroachDB

- Accepted though CockroachDB rejects them: `JSONB[]` columns, `ROLLUP`, a column
  definition list on a function, a typed alias list on a multi-array `unnest`, an
  unaliased one-column function used as a row value, `random()` and `to_tsvector` over
  JSON, and some `bytea` overloads.
- Some result types differ from CockroachDB's own; JSON columns are described as `json`
  rather than `jsonb` (the Go type is the same).
- `AS OF SYSTEM TIME` in one branch of a set operation, and bounded-staleness reads, are
  not checked.
- `db.table` names and `USE` are not followed in migrations.

## TimescaleDB

- Some toolkit functions and overloads are absent from the catalogue, including
  `to_epoch`, `into_int_values`, accessor-object overloads and `approx_percentile` over a
  continuous aggregate column (Q005). Declare missing signatures under `custom_functions`.
  The documented `uddsketch(100, 0.01, value)` literal form is supported.
- `min_n`, `max_n` and `approx_percentiles` results are typed `any`, as is a column aliased
  on `unnest((SELECT timevector ...))`.

## MySQL

Typing:

- `IF(...)` is typed as text; `AVG(int)` as a double (MySQL gives `DECIMAL`); `SUM(FLOAT)`
  as a decimal (MySQL gives `DOUBLE`); `EXTRACT` always as `BIGINT`; `YEAR()` as `INT`.
- `md5` and similar functions are never NULL in Seagull; `'2024-01-01' + INTERVAL 1 DAY`
  is non-null text; `DATE_ADD` of a `DATE` by days is typed as a date-time;
  `UNIX_TIMESTAMP` of a whole-number literal is a decimal; date functions given a string
  are nullable even for a valid literal, as is `CAST('2024-01-01' AS DATE)`.
- A date plus an integer is typed `any`; `a / (2)` is nullable although the divisor is a
  constant.
- `IFNULL` and `CEIL` over integers keep the argument's width, where MySQL and MariaDB
  differ.
- `CREATE TABLE ... AS SELECT` columns are all nullable.
- A view takes a table's new column types at query time, which Seagull does not model.
- A view defined by a `TABLE` statement keeps only the column names it declares, without
  types ([Q069](diagnostics.md#q069)); write `SELECT * FROM` the table instead.
- `INT1`, `INT2`, `INT3`, `INT4`, `INT8` and `MIDDLEINT`, MySQL's synonyms for `TINYINT`,
  `SMALLINT`, `MEDIUMINT`, `INT`, `BIGINT` and `MEDIUMINT`, are unknown types, so a column
  declared `INT8` is typed `any`; declare it with the type's own name.
- An `ENUM` or `SET` member written as a hexadecimal or bit literal is kept as the bytes it
  spells, whatever the column's character set: `ENUM(0xFF)` holds `"\xff"`, which is not
  valid UTF-8, where the server reads the bytes in the column's character set. Write the
  member as a string.

Parameters and names:

- A placeholder inside a function call in `INSERT ... VALUES` (`UPPER(?)`) takes the
  column's type, and an interval count there (`DATE_ADD(d, INTERVAL ? DAY)`) is typed
  `any`; cast it (`CAST(? AS SIGNED)`).
- `VALUES ROW(...)` columns are named `column1`, `column2`, while MySQL names them
  `column_0`, `column_1`; name them with a column list (`AS v (a, b)`). `SHOW TABLES`
  names its column `Tables_in_database` whatever the database is called. Rows scan by
  position, so only the Go and JSON names differ.
- An `ORDER BY` name that is both a select alias and a column may resolve differently
  from MySQL; give the alias a name no column has.

Diagnostics:

- `||` is typed as logical OR, with a warning ([Q063](diagnostics.md#q063)) only when an
  operand beside it is text by its form (a string literal, a cast to text, a text
  function), alone or at the edge of a comparison (`name || 'x' = ?`); between columns or
  parameters there is no warning, and `PIPES_AS_CONCAT` is not modelled. Use
  `CONCAT(...)`.
- Accepted though MySQL rejects them: a row-valued `SET (a, b) = (...)` and the alias
  `AS stored`.
- A type modifier's range is checked only for `FLOAT(p)`: `DECIMAL(66, 2)`,
  `VARCHAR(70000)`, `DATETIME(7)` and `FLOAT(256, 2)` are accepted, where MySQL refuses
  each, and so are `DOUBLE(10)` and `REAL(10)`, which MySQL takes with no modifier or two.
- An `ENUM` or `SET` member that is not a string, hexadecimal or bit literal is passed over,
  where MySQL refuses the statement: `ENUM(1, 'a')` holds only `a`, and `ENUM(0X61)` and
  `SET(0B1)`, whose upper-case prefixes MySQL does not read as literals, hold nothing for
  those members. A nested group, as in `ENUM('a', ('b'), 'c')`, ends the list at its `)`,
  so the statement fails later, with a syntax error near a `)`, rather than with one about
  the group.
- A user variable assigned from a parameter (`SET @x = ?`) is refused
  ([Q010](diagnostics.md#q010)), and so is any `SELECT ... INTO` (variables, `OUTFILE`
  or `DUMPFILE`), each with a message saying why. A pooled connection does not keep a
  variable between calls anyway: select the values as columns, or pass the value as a
  parameter of the statement that uses it.
- Spatial functions (`ST_*`, `MBR*`, `Point()`) are unknown (Q005), although `GEOMETRY`
  and `POINT` columns are read.

## MariaDB

The [MySQL](#mysql) entries apply to MariaDB too, except where MariaDB's own functions
differ. Also:

- Functions not in the catalogue: `TO_NUMBER`, `TRUNC` of a date, `ROWNUM`,
  `VEC_DISTANCE` and the aggregate form of `PERCENTILE_CONT`/`PERCENTILE_DISC` (Q005).
- `MEDIAN` and `PERCENTILE_*` without `OVER` are accepted, although MariaDB has only the
  window forms.
- An aggregate alias inside an `ORDER BY` expression is not resolved.
- Sequences are not relations, so `SELECT ... FROM seq` is unknown.
- `CAST(x AS FLOAT(p))` follows MySQL's rule, a `DOUBLE` past 24 bits and a syntax error
  ([Q010](diagnostics.md#q010)) past 53, which has not been checked against MariaDB's
  own handling of a precision in a cast, as in `SELECT CAST(1 AS FLOAT(30))`.

## SQLite

- Declared types with numeric affinity that Seagull has no reading for (`MONEY` and
  unknown names) are typed as decimals; temporal names with a modifier (`TIMESTAMP(3)`,
  `DATETIME(6)`) are text, since the drivers return text for them.
- `CAST(x AS BOOLEAN)` is a number, as SQLite converts it; write `x <> 0` for a boolean.
- SQLite does not enforce declared types: a `TIMESTAMP` column holding integers fails to
  scan into `time.Time`.
- `jsonb(...)` values read as bytes; `COALESCE(blob, text)` is typed `any`.
- Parameters in a `VALUES` CTE body feeding `INSERT ... SELECT` are typed `any`.
- `iif(?, a, b)` types the condition parameter like its branches.
- `json_array_insert` is unknown (Q005).
- `generate_series` is typed as a table function, but neither stock driver ships it, so
  it fails with "no such table" at run time.
- The rules for double-quoted text (identifier or string) are approximated.
- `PRAGMA OPTIMIZE` and other `PRAGMA` statements are routed to the writer.
- A view with a declared column list over `SELECT *` is not followed after `ADD COLUMN`,
  nor through renames.

## DuckDB

Typing:

- Typed `any`: `median`, `quantile_cont`, `quantile_disc`, `list_median`, `map()` and
  `MAP {}`, `lag(x) OVER` a named window, a typed literal of an unknown type name,
  `list_aggregate` and `regexp_extract` with group names, and `greatest(FLOAT, 1)`.
- `stddev` and `variance` are decimals (DuckDB gives `DOUBLE`); `date_part` and `extract`
  are doubles or decimals (DuckDB gives `BIGINT`); `CASE` and `COALESCE` over `DECIMAL`
  and `DOUBLE` take the first branch's type; distances over `FLOAT` arrays are `DOUBLE`.
- Over `FLOAT` (single precision): `abs`, `round` and `FLOAT + DECIMAL` are typed
  `DECIMAL`, and `FLOAT / 2` `DOUBLE`, where DuckDB keeps `FLOAT`. The values still scan.
- A call DuckDB rejects as ambiguous (`year('2024-01-01')`) is typed from the first
  overload instead of being reported.
- `list_zip` is unknown (Q005).
- Duplicate column names from an aliased join are numbered (`v2`) where DuckDB writes
  `v_1`.
- A view over a `PIVOT` that takes its columns from the data, or an `UNPIVOT` that does
  not list its columns, keeps only the column names it declares, without types
  ([Q069](diagnostics.md#q069)); list the `IN` values or the columns.
- A type created with `CREATE TYPE name AS STRUCT(...)` is not modelled, so a column of
  that type is `any`, with no diagnostic; declare the column as `STRUCT(...)` itself, or pin
  it with `sgl.column(..., go_type: ...)`.
- A map built from values that are never `NULL` (`MAP {'a': 1}`, `map([...], [...])`,
  `map_from_entries`) still reads its values as pointers, where a list literal of such
  values reads plain elements.

Parameters:

- Typed `any`: `x -> x + $1` inside a lambda, a struct literal's field (`{'a': $1}`), a
  `map()` argument, an enum-list parameter that nothing types (cast it, `$1::mood[]`), and
  a parameter compared with a macro whose body Seagull cannot type (cast it).
- A parameter compared with a lambda parameter is named `p1`.
- A `MAP` or `STRUCT` parameter holding an enum, a `DECIMAL`, a `BLOB`, a `UUID` or an
  `INTERVAL` is refused ([Q059](diagnostics.md#q059)), since duckdb-go cannot build it;
  write the value in the SQL: `map($1::VARCHAR[], $2::mood[])`,
  `{'note': $1::VARCHAR, 'feeling': $2::mood}`.

Accepted though DuckDB rejects them:

- `at` as a column or `STRUCT` field name, `sum(alias)` over a select-list alias,
  `time - time`, and `sum` or `avg` of an `INTERVAL`.

Drivers:

- JSON inside a list, struct or map decodes numbers through `float64`, losing precision
  beyond 2^53; a `UNION` inside a composite reads as `{"value": ..., "tag": ...}`; a
  bare JSON string document reads as the string.
- Nested `[][]uint8` parameters are sent as blobs.

## ClickHouse

Typing and names:

- An item that names the alias of a later item reads a column of that name, or gets
  [Q001](diagnostics.md#q001) and is typed `any` when there is none, where ClickHouse
  reads the alias; order the items so an alias comes first.
- Unaliased positional and qualified nested reads (`tup.1`, `t.n.x`) may be named
  differently from the engine; alias them.
- Tuple naming assumes `enable_named_columns_in_function_tuple = 0`.
- Typed `any` or unknown (Q005): `trim(BOTH ' ' FROM s)`, `toInterval(n, unit)`, `gcd`,
  `lcm`, `DATE()`, `nullIn`, `arrayZipUnaligned`, `INET6_NTOA(INET6_ATON(x))`.
- `variantElement(x, 'T')` of a `Variant` with several members is typed `any`.
- A call of a `CREATE FUNCTION` lambda that calls another such function which calls it
  back is typed `any`, as is one whose typing passes a fixed budget; pin it with
  `sgl.column`.
- `Variant` and `AggregateFunction` values are refused ([Q059](diagnostics.md#q059))
  wherever a result holds them, since clickhouse-go reads neither: select `toString(x)` or
  `variantType(x)`, or `finalizeAggregation(x)` or `<f>Merge(x)`.

SQL Seagull refuses or accepts wrongly:

- The keys and values of a map literal in the rows of `INSERT ... VALUES` are not
  checked.
- `untuple(...)` as a select item is refused (Q010); name the tuple's elements.
- An `EPHEMERAL` column can be selected without a diagnostic.

Drivers (see [the ClickHouse page](dialects/clickhouse.md#json-columns)):

- Through database/sql, a named tuple reads only as `map[string]any`, a 128-bit integer
  inside it encodes to JSON as `{}`, and an IPv4-mapped IPv6 address prints as IPv4;
  `net.IP` cannot tell the two forms apart.
- An enum in a native map of named tuples is refused ([Q059](diagnostics.md#q059)); a
  `Map` keyed by `DateTime64` with a time zone fails in the driver.
- IPv6 zones are dropped; refusals of named parameters come from the server after a round
  trip; a multi-row batch split over several statements keeps the statements before a
  failing one (a native batch is all or nothing).
- JSON reads come back normalised (keys sorted, numbers in their inferred type); the
  driver decodes a date path inside a `Map` column's JSON as days;
  `output_format_native_write_json_as_string = 1` breaks composite columns in the driver;
  `Map(K, JSON)` cannot be read through database/sql (Q059).
- The database/sql driver also replaces `$N` inside heredoc strings.

## Not supported yet

These features are planned after the first alpha. Each has a workaround today:

- **Models in a separate package**, or sharing models across packages: generate one
  package per database and share types through `go_type` overrides.
- **PostgreSQL composite types read structurally** (records, arrays of rows, `unnest` of
  row arrays): select the fields you need, or `to_jsonb(x)` and a JSON override.
- **Batch commands returning rows** (`batchone`, `batchmany`): use `command: batch` and
  read back with a query, or `RETURNING` on a single-row command.
- **Checking queries against a live database** in `sgl vet`: use the
  [live schema](../how-to/live-schema.md) as the schema source, and the integration tests
  of your application.
- **Other migration layouts** (goose, dbmate, tern, sql-migrate single files,
  directory-per-migration): Seagull reports them ([Q048](diagnostics.md#q048)); split
  them into `NNN_name.up.sql` and `NNN_name.down.sql`.
- **PostgreSQL 18 `RETURNING old.col` / `new.col`**: return the columns you need from a
  CTE.
- **`MERGE`** (PostgreSQL 15+, DuckDB 1.4+): write `INSERT ... ON CONFLICT` or separate
  statements.
- **DuckDB `SELECT * EXCLUDE`/`REPLACE` and `COLUMNS(...)`**: list the columns.
- **DuckDB `from_json`/`json_transform` typed from the structure**: cast the result or
  pin it with `sgl.column`.
- **Extension types, operators and functions** (pgvector, PostGIS, hstore, ltree,
  fuzzystrmatch): declare the functions under `custom_functions` and map the types with
  overrides.
- **System catalogues as relations** (`pg_catalog`, `information_schema`,
  `timescaledb_information`): query them through a hand-written function or keep these
  queries in Go.
- **Optional slice parameters** (`$1 IS NULL OR id IN ($1)` with `kind: slice` is
  [Q055](diagnostics.md#q055)): use the runtime query builder, or two queries.
- **PostgreSQL 17 `JSON_TABLE`**: use `jsonb_to_recordset` or `jsonb_array_elements`.
- **Naming controls** (all-capitals runs such as `HTTPID`, configurable initialisms, a
  project-wide rename map): alias columns and name parameters with `sgl.param`.
- **Column override globs** (`*.id`): list each column, or override the SQL type.
- **Result columns of a MySQL `CALL`** that returns a result set: run the query the
  procedure runs.
- **MariaDB system-versioned and application-time tables** (`FOR SYSTEM_TIME AS OF`,
  `PERIOD FOR ... WITHOUT OVERLAPS`): keep these queries in Go.
- **MySQL and MariaDB spatial functions**: declare the ones you use under
  `custom_functions`.

## Declined

- ClickHouse `runningDifference` and `neighbor` are unknown: both are deprecated and off
  by default since ClickHouse 25.4. Use window functions such as `lagInFrame`.
- Spanner and BigQuery (GoogleSQL) and SQL Server are not built-in dialects, and
  dialects written outside Seagull are not supported yet.
