---
title: MariaDB
description: 'Select the dialect with dialect: mariadb in sgl.yaml, or dialect_registry.New("mariadb",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 160
---

# MariaDB

Select the dialect with `dialect: mariadb` in `sgl.yaml`, or `dialect_registry.New("mariadb", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | MySQL |
| Generated API | database/sql |
| Live introspection | No built-in source |
| Runtime migrations | Uses the MySQL-family backend; migrations are non-transactional. RETURNING support differs from MySQL and by SQL form. |

[Known limitations](../limitations.md#mariadb) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/mysql-mariadb.md) and [runnable examples](../../../examples/mariadb) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

- Everything the MySQL dialect understands, except where MariaDB itself differs (below).
- `RETURNING` on `INSERT`, `REPLACE` and `DELETE`, and MariaDB-only functions such as `UUID()` variants. `INSERT IGNORE ... RETURNING` on a `one` query gets the [Q046](../diagnostics.md#q046) warning, since a row the insert ignores returns nothing.
- MariaDB's own column types, read as MariaDB sends them: `UUID` as a UUID, bound from one
  and read from its text; `INET4` and `INET6` as the address's text, which a query compares
  with a string and passes to `IS_IPV4` and `IS_IPV6` as it is (`INET6_NTOA` of such a
  column is `NULL`, since MariaDB passes it the text, so Seagull reports that call as
  matching no overload); and `VECTOR(n)` as its packed bytes, converted with `VEC_FromText`
  and `VEC_ToText`. A `VECTOR INDEX` (or `VECTOR KEY`) element of a `CREATE TABLE`, with
  its `M` and `DISTANCE` options, declares an index, not a column.
- `IS_IPV4`, `IS_IPV6`, `IS_IPV4_MAPPED` and `IS_IPV4_COMPAT` are an `INT` that is 0, not
  `NULL`, for a `NULL` argument, where MySQL returns a `BIGINT` that is `NULL`.
- Sequences: `NEXT VALUE FOR s` and `NEXTVAL(s)` advance the sequence and read a `BIGINT`
  that is never `NULL`; `PREVIOUS VALUE FOR s` and `LASTVAL(s)` read the value the session
  read last, `NULL` before it reads one; and `SETVAL(s, n [, is_used [, round]])` moves it
  and reads `NULL` for a value below its next one. The name may be qualified by its
  database. A query that advances or moves a sequence changes data, so it runs on the
  writer connection. `SETVAL` takes its values as numbers written in the statement, which
  MariaDB's grammar requires, so a parameter there is [Q010](../diagnostics.md#q010).
- `OFFSET n ROWS` and `FETCH {FIRST | NEXT} n ROWS {ONLY | WITH TIES}` limit the rows, with
  parameters for `n` bound as an offset and a limit, beside `LIMIT`; the `mysql` dialect rejects these forms.
- `PERCENTILE_CONT(f) WITHIN GROUP (ORDER BY v) OVER (...)` and `MEDIAN(v) OVER (...)` are a
  nullable `DOUBLE`, and `PERCENTILE_DISC(f) WITHIN GROUP (ORDER BY v) OVER (...)` takes the
  type of `v`, nullable, as MariaDB gives them; MariaDB has only their window forms.
- `VALUES (...)` lists are queries and arms of set operations, as in
  `VALUES ('none', 0) UNION ALL SELECT shop, items FROM invoices`, as `VALUES ROW(...)` lists
  are on MySQL.
- A select-list alias may be an operand in `GROUP BY`, `HAVING` and `ORDER BY`, as on
  MySQL, except that MariaDB rejects an alias of an aggregate in an `ORDER BY` expression,
  as in `ORDER BY total * 2`, which the dialect does not report.
- `AS stored` and `AS virtual` are aliases MariaDB accepts.
- `CREATE OR REPLACE TABLE` replaces a table with the new definition, in its column list,
  `LIKE` and `AS SELECT` forms and for a `TEMPORARY` table, as MariaDB drops the old table
  first; `CREATE OR REPLACE INDEX`, `DATABASE` and `TRIGGER` replay too. The `mysql` dialect takes `OR REPLACE` only on views.
- A column read outside every aggregate of a query that aggregates without `GROUP BY`,
  as in `SELECT title, COUNT(*) FROM books`, is nullable without a diagnostic: MariaDB's
  default mode returns one row, in which the column is `NULL` when nothing matches.

MariaDB has no `UPDATE ... RETURNING`, so the `mariadb` dialect reports one as error
[Q052](../diagnostics.md#q052), pointing at the `RETURNING` keyword. Update, then read the
rows back with a separate query. `sgl dialects` reports support as `INSERT, DELETE`.

MariaDB also lacks MySQL's `->` and `->>` JSON operators, the MySQL 8 row alias
(`INSERT ... VALUES (...) AS new ON DUPLICATE KEY UPDATE`) and the `RETURNING` clause of
`JSON_VALUE`. The `mariadb` dialect reports each as an error
([Q010](../diagnostics.md#q010)) that names the replacement: use
`JSON_EXTRACT(doc, '$.name')` for `->`, `JSON_UNQUOTE(JSON_EXTRACT(doc, '$.name'))` for `->>`,
`VALUES(column)` to refer to the inserted values in `ON DUPLICATE KEY UPDATE`, and
`CAST(JSON_VALUE(...) AS type)` for `JSON_VALUE ... RETURNING type`. MariaDB's own JSON
functions, such as `JSON_QUERY`, `JSON_EXISTS`, `JSON_DETAILED` and `JSON_OBJECT_TO_ARRAY`, are
known.

The Oracle-compatible functions MariaDB accepts in its default SQL mode are known too:
`NVL` is typed as `IFNULL`, `NULL` only when both arguments may be; `NVL2` and
`DECODE_ORACLE` take the type of their results, and `DECODE_ORACLE` without a default may be
`NULL`; `ADD_MONTHS`, `TO_CHAR`, `LENGTHB`, `CHR` and the `_ORACLE` string functions, such as
`LTRIM_ORACLE`, are typed as MariaDB types them. So are MariaDB's other own functions,
`SFORMAT`, `NATURAL_SORT_KEY`, `CRC32C`, `UUID_V4`, `UUID_V7`, `KDF`, the vector functions
`VEC_FROMTEXT`, `VEC_TOTEXT` and `VEC_DISTANCE_EUCLIDEAN`/`_COSINE`, and the `MEDIAN` window
function. The `mysql` dialect keeps reporting them as unknown, as MySQL rejects them.

The MySQL functions MariaDB rejects are unknown to the `mariadb` dialect, which reports a
call of one as [Q005](../diagnostics.md#q005) rather than accepting it to fail on the
server: `ANY_VALUE`, `GROUPING`, `REGEXP_LIKE`, `UUID_TO_BIN`, `BIN_TO_UUID`, `IS_UUID`,
`JSON_STORAGE_SIZE` and `JSON_SCHEMA_VALIDATION_REPORT`. `REGEXP_INSTR` and `REGEXP_SUBSTR`
take only the subject and the pattern, `REGEXP_REPLACE` only those and the replacement, and
`LAG` and `LEAD` an offset but no default, as on MariaDB, so a call passing MySQL's further
arguments matches no overload.

A string or a number passed to a date or time function is read as a date or time, as on
MySQL. `DATE_ADD` and its kin over a parameter are a string, since MariaDB returns the bound
value shifted as text where MySQL returns a `DATETIME`.

## Parameters

`?` positional parameters and named `:name` parameters, as on MySQL.

## Migrations

The MySQL backend: `migrate_backend_mysql.New()`, with the same session and history rules: a run
qualifies the history and seed tables with the database `DATABASE()` names when it starts,
and the generator does not follow `USE`. See [MySQL migrations](mysql.md#migrations).

## Drivers

`database/sql` with `github.com/go-sql-driver/mysql`.

The DSN must set `parseTime=true`, as on MySQL: the generated code reads `DATETIME`, `DATE`
and `TIMESTAMP` columns into `time.Time`, which the driver returns only with that setting.
See [MySQL drivers](mysql.md#drivers).

A binary column, such as `BINARY(16)`, whose Go type an override chose binds the bytes the
value marshals itself to (`encoding.BinaryMarshaler`), so a `github.com/google/uuid.UUID`
override on a `BINARY(16)` key stores its 16 bytes and a lookup by the UUID finds the row,
where its `driver.Valuer`'s 36 characters would not fit. See
[What an override type must support](../type-mapping.md#what-an-override-type-must-support).

## Exported declarations

### dialect_engine_mariadb

```go
func New(options ...Option) dialect.Dialect
type Option = mysql.Option
    func WithANSIQuotes(enabled bool) Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
    func WithReturning(statements dialect.DataChange) Option
```
