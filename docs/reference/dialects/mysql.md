---
title: MySQL
description: 'Select the dialect with dialect: mysql in sgl.yaml, or dialect_registry.New("mysql",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 150
---

# MySQL

Select the dialect with `dialect: mysql` in `sgl.yaml`, or `dialect_registry.New("mysql", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | MySQL |
| Generated API | database/sql |
| Live introspection | No built-in source |
| Runtime migrations | Non-transactional migrations and implicit DDL commits. Lock keys are scoped to the current schema unless compatibility options change them. |

[Known limitations](../limitations.md#mysql) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/mysql-mariadb.md) and [runnable examples](../../../examples/mysql) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

- Backtick identifiers, `AUTO_INCREMENT`, unsigned integers, `ENUM` and `SET` columns, generated columns.
- `INSERT ... ON DUPLICATE KEY UPDATE`, `INSERT IGNORE`, `REPLACE INTO`, index hints, `LIMIT`/`OFFSET`.
- JSON columns, functions and the `->` and `->>` operators, `GROUP_CONCAT`, window functions, date and string functions.
- `YEAR` as a 16-bit integer (`int16`) and `BIT(n)` as binary (`[]byte`), the forms go-sql-driver returns them in. `TIME` uses the time-of-day slot, which the database/sql emitter sets to `string` for go-sql-driver, since the driver returns it as text.
- A type's modifiers are one or two unsigned numbers, as in `DECIMAL(10, 2)`; a third one, a sign, a word, a nested or empty list, or a list left open is a syntax error ([Q010](../diagnostics.md#q010)). A modifier written as a fraction, an exponent or a `0x` or `0b` integer is read as its value, so `DECIMAL(10.5, 2)` has precision 10 and `VARCHAR(1e3)` a length of 1000. A type keeps its modifiers wherever it is written: in a column, a routine's argument and return types, a `JSON_TABLE` or table-function column (`price DECIMAL(10, 2) PATH '$.price'`), a type a directive names (`type: 'decimal(10,2)'`) and the target of `CAST`, `CONVERT` and `JSON_VALUE ... RETURNING`, so `CAST(x AS DECIMAL(10, 2))` has precision 10 and scale 2 and `CONVERT(x, CHAR(20))` a length of 20, and a placeholder they convert takes that type.
- `FLOAT(p)` is a `FLOAT` (`float32`) for `p` up to 24 and a `DOUBLE` (`float64`) from 25 to 53, as MySQL makes it, in a column and in a conversion, and a larger `p` is a syntax error ([Q010](../diagnostics.md#q010)); `FLOAT(M, D)` stays a `FLOAT`. `FLOAT4` and `FLOAT8` are `FLOAT` and `DOUBLE`.
- An `ENUM` holds at most 65,535 values and a `SET` at most 64 members, and more is a syntax error ([Q010](../diagnostics.md#q010)). A member written as a hexadecimal or bit literal, such as `0x61`, `X'61'` or `b'1100001'`, is the value it spells, here `a`.
- Stored functions created in migrations and their read-only detection, with or without `DELIMITER`, which the analyser and the migration runner both honour; see [Migrations and seeds](../migration-runtime.md#statements). A routine's body is kept exactly as the migration writes it, so an `IF ... END IF` or a `CASE ... END` inside it never cuts it short, and a function that writes after such a block changes data.
- DDL is read whole, and what MySQL refuses is a syntax error ([Q010](../diagnostics.md#q010)): words left after a complete `CREATE`, `DROP`, `ALTER` or `RENAME`, a `CREATE VIEW` without `AS`, a column list missing a comma or its `)`, `DROP INDEX` or a trigger without `ON` and its table, a `FOREIGN KEY` without `REFERENCES` and a table, and an unknown `CREATE DATABASE` or index option. `DROP TABLE a, b` and `DROP VIEW v1, v2` drop every name in the list, `IF EXISTS` applying to each, `DROP TEMPORARY TABLE` is applied, and `ALTER TABLE t ADD (a INT, b INT)`, with or without `COLUMN` and `IF NOT EXISTS`, adds every column it lists. Creating a table under a name a table already holds, or renaming one onto a name in use, is [Q067](../diagnostics.md#q067), and altering a table, or dropping a table, view or function, that does not exist without `IF EXISTS` is [Q068](../diagnostics.md#q068).
- A view or `CREATE TABLE ... AS` query with a syntax error fails the statement, and a view may end in `WITH [CASCADED | LOCAL] CHECK OPTION`. A view whose query Seagull does not analyse, a `TABLE` statement or one nested past `max_parse_depth`, is [Q069](../diagnostics.md#q069): it keeps only the column names it declares, without types.
- `CALL` statements, whose arguments' parameters are typed from the procedure. Statements with nothing to analyse, such as `SET` or `TRUNCATE`, generate with no output.
- `SHOW WARNINGS` and `SHOW ERRORS` (`Level`, `Code` as an `INT UNSIGNED`, `Message`),
  `SHOW COUNT(*) WARNINGS` and `ERRORS` (a nullable `BIGINT UNSIGNED`),
  `SHOW [GLOBAL | SESSION] VARIABLES` and `STATUS` (`Variable_name`, and `Value`, which
  MySQL may leave `NULL`), `SHOW DATABASES`, `SHOW [FULL] TABLES` (`Tables_in_<database>`,
  named after the database `FROM` or `IN` names and `Tables_in_database` otherwise, and
  `Table_type` with `FULL`) and `SHOW CREATE TABLE` (`Table`, `Create Table`) return those
  columns, as MySQL 8.4 and MariaDB 11 give them. Any other `SHOW` statement, whose columns
  Seagull does not declare, is [Q010](../diagnostics.md#q010), and so is a parameter in
  one; read such data from `information_schema` instead. `SHOW WARNINGS` reads the warnings
  of the connection's last statement, so run both on one connection, such as in a
  transaction.
- `IFNULL(a, b)` is `NULL` only when both arguments may be, so `IFNULL(nullable, 0)` is never `NULL`.
- Numeric functions are typed from their arguments as MySQL types them: `ABS` keeps its argument's type, `CEIL` and `FLOOR` keep a `DOUBLE` or an integer and make a `DECIMAL` a `BIGINT`, `MOD` takes the wider operand's type and may be `NULL` (`MOD(x, 0)`), `ROUND` and `TRUNCATE` keep a `DECIMAL`, and `CRC32` is an `INT UNSIGNED` (`uint32`), so no result is read into a narrower Go integer. A `DECIMAL` passes to a function taking a `DOUBLE`, and a number or a date and time to a string function, as MySQL converts them.
- A parameter compared with an expression that names no column, as in `WHERE price * 2 > ?` or `SELECT id + 1 = ?`, takes that expression's type. A placeholder that is a function argument, or a value of `IN`, `BETWEEN`, `CAST` or `CONVERT`, takes the type and name of the place it is written in: in `LPAD(?, ?, ?)` they are named `string`, `length` and `pad`, and the length is an integer.
- `CURRENT_TIMESTAMP`, `UTC_TIMESTAMP`, `NOW`, `CURTIME`, `SYSDATE` and the other current-time functions accept a fractional seconds precision, as in `UTC_TIMESTAMP(6)`.
- A string or a number passed to a date or time function is read as a date or time, as MySQL reads it: `YEAR('2024-01-01')`, `DATEDIFF(NOW(), '2024-01-01')` or `MONTH(varchar_column)` analyse without a diagnostic, and the call may be `NULL`, since a string that holds no date gives `NULL`. `DATE_ADD`, `DATE_SUB`, `ADDDATE`, `SUBDATE`, `ADDTIME`, `SUBTIME` and `TIMESTAMPADD` give the type of the value they shift: a string for a string or a number, a `TIME` for a `TIME`, and a `DATETIME` otherwise. `UNIX_TIMESTAMP` of a string, a parameter or a `DATETIME` with fractional seconds is a `DECIMAL`, and may be `NULL`. A string still never converts to a date in a `CASE` or `COALESCE`, which MySQL types as a string.
- `COUNT(DISTINCT a, b)` counts the distinct combinations with no `NULL` among them;
  `UUID_TO_BIN(uuid, 1)` and `BIN_TO_UUID(bytes, 1)` take the swap flag that orders
  time-based keys, and are `NULL` only for a `NULL` value; `LAST_INSERT_ID(expression)`
  sets and returns the value a later `LAST_INSERT_ID()` reads, as in
  `UPDATE counters SET value = LAST_INSERT_ID(value + 1)`; and `IS_IPV4`, `IS_IPV6`,
  `IS_IPV4_MAPPED` and `IS_IPV4_COMPAT` test addresses.
- `JSON_EXTRACT`, `JSON_KEYS`, `JSON_LENGTH` and `JSON_CONTAINS` with a path, and the `->` and `->>` operators, are nullable even over a `NOT NULL` document, since a missing path yields `NULL`. `CAST(... AS SIGNED)` and `UNSIGNED` are 64-bit integers.
- A `TABLE t` statement, which Seagull does not analyse, and square-bracket syntax, such as an array subscript, which MySQL does not have, are rejected ([Q010](../diagnostics.md#q010)), and so is a parenthesis left open or closing nothing, as in `SELECT (((1` or an unclosed call in a select list.
- `INSERT INTO t (a) (SELECT ...)` is analysed as the `INSERT`'s query, so its parameters take the target columns' types; a parenthesis after the column list that opens no query, as in `INSERT INTO t (a) (1)`, is a syntax error.
- A system variable's qualifier is read in any case, as in `@@GLOBAL.time_zone` or `@@Session.sql_mode`.
- An executable comment opener (`/*!` or `/*M!`) inside a `--` or `#` comment, or inside an ordinary block comment, stays part of that comment.
- Set operations over queries in parentheses, each with its own `ORDER BY` and `LIMIT`, as
  in `(SELECT ... LIMIT 1) UNION ALL (SELECT ... LIMIT 1) ORDER BY id`, and over
  `VALUES ROW(...)` lists, as in `VALUES ROW(0, 'none') UNION ALL SELECT id, name FROM
  items`, in a statement, a derived table, a CTE (the anchor of a recursive one
  included), a subquery and a view. A set operation in parentheses that is an arm of
  another is read as more arms of it, which types the result alike.
- `"..."` is a string, as MySQL's default `sql_mode` reads it, so `CONCAT(a, ":", b)` joins
  three strings; strings written next to each other, as in `'a' "b"`, are one, and a string
  written after a select-list item names it, as in `SELECT 1 'one'`. A server whose
  `sql_mode` holds `ANSI_QUOTES` reads `"..."` as a quoted identifier; declare it with
  `ansi_quotes: true` under the database's `dialect_options`, or
  `dialect_engine_mysql.WithANSIQuotes(true)` in Go, and give a migration runtime you build yourself
  `migrate_backend_mysql.WithANSIQuotes(true)` so it splits files as the generator reads them
  (`sgl migrate` and `sgl seed` pass it from `sgl.yaml`).
- A division or a remainder (`/`, `%`, `DIV`, `MOD`) is nullable unless its divisor is a
  number other than zero written in the statement, since MySQL gives `NULL` for a division
  by zero: `price / quantity` is nullable and `price / 2` is not. `STR_TO_DATE`, and a
  conversion of a string or a number to a date or time, such as `CAST(born AS DATE)` or
  `CONVERT(born, DATETIME)`, are nullable too, since MySQL gives `NULL` for a value that
  holds no date; a conversion of a date or time is not.
- A select-list alias may be an operand in `GROUP BY`, `HAVING` and `ORDER BY`, as in
  `HAVING total > ?` or `ORDER BY total * 2`, where MySQL resolves it, and a parameter
  compared with one takes the type of the item it names. `WHERE` cannot name an alias, as
  in MySQL.
- A `CHAR`, `VARCHAR` or `TEXT` column declared `CHARACTER SET binary` is the binary type
  MySQL makes of it, `BINARY`, `VARBINARY` or a `BLOB`, read as `[]byte`.
- An unquoted reserved word used as an alias, such as `SELECT 1 AS stored`, is not
  reported, though MySQL rejects it; quote it with backticks.
- A column read outside every aggregate of a query that aggregates without `GROUP BY`,
  as in `SELECT title, COUNT(*) FROM books`, is [Q062](../diagnostics.md#q062), a
  warning: MySQL rejects the query in its default mode, whose `sql_mode` holds
  `ONLY_FULL_GROUP_BY`, and otherwise returns one row in which the column is `NULL` when
  nothing matches, so the column is nullable.
- A comparison, `BETWEEN`, `IN` list, pattern match, `NOT` or logical operator over a
  value that may be `NULL` may be `NULL` itself; `IS NULL` and `<=>` never are. A row
  comparison or a row `IN` list, as in `(published, id) > (?, ?)`, types each parameter
  from the element at its position, and a parameter inside an expression an `INSERT` or
  `SET` assigns, as in `DATE_ADD(published, INTERVAL ? DAY)`, is not typed as the
  column; an argument of `GREATEST`, `LEAST` or `IFNULL` takes the type of the others,
  beside a number it takes the number's type, as in `SET n = n + ?`, while
  `title || ?` leaves it as `any`, since `||` is a logical OR in the default `sql_mode`.
- `||` is a logical OR, as MySQL's default `sql_mode` reads it, and is typed as one. A `||`
  whose operand is text by its form, a string literal, a conversion to a string or a
  string function, is most likely meant to join strings, as it does on other engines, so
  it is [Q063](../diagnostics.md#q063), a warning: `name LIKE '%' || ? || '%'` matches
  every row. Join strings with `CONCAT`. A `||` between two columns or parameters is not
  reported, since the parser does not know their types. A server whose `sql_mode` holds
  `PIPES_AS_CONCAT` is not modelled.

### Parse limits

`max_parse_depth` (256 by default, at most 4,096) and `max_tokens_per_statement` (100,000
by default, at most 1,000,000) under
[`dialect_options`](../configuration.md#dialect-options-dialect_options), or
`dialect_engine_mysql.WithMaxParseDepth` and `WithMaxTokensPerStatement` in Go, bound what one statement
may cost; `JSON_TABLE`'s `NESTED PATH` clauses count towards the depth. Tokenising stops
at the token budget, and a statement past it fails when it is analysed, or applied as DDL
Seagull reads, while a large seed `INSERT` in a migration is still passed over. Such a
statement is still read to its end for a missing semicolon, so a statement it runs on into
is reported ([Q010](../diagnostics.md#q010)) at that statement's first word. Either
limit fails the statement with an error matching `seagull.ErrLimit`. The Go options hold a
value above the ceiling at the ceiling; `sgl.yaml` and `dialect_registry.New` refuse it. The same
limits apply to MariaDB.

## Parameters

`?` positional parameters and named `:name` parameters. A named parameter is sent as `?`
and bound once for each place it appears. `:name` is a parameter where a value is
expected; a colon written directly after a name ends a statement label, as in `l1:LOOP`.
A placeholder that is the whole search of `MATCH (...) AGAINST (?)` is text, and the
amount of `INTERVAL ? DAY` or another single unit a number. A placeholder in a statement
Seagull runs without analysis, such as `SET @user_id = ?`, cannot be bound and is
[Q010](../diagnostics.md#q010).

## Migrations

Advisory locks through `GET_LOCK` (`migrate_backend_mysql.New()`). Files are split into statements as the dialect splits them, `DELIMITER` lines included. Migrations run one statement at a time and outside a transaction, since MySQL commits DDL implicitly, and record their progress so that one that fails part-way resumes at the failed statement. Seeds commit with their history row in a transaction; they are split too, unless `migrate.WithDriverMultiStatements(true)` says the DSN sets `multiStatements=true`, when each seed file runs in one call. See [MySQL and MariaDB](../migration-runtime.md#mysql-and-mariadb).

A run applies its migrations on one session and qualifies the history and seed tables with
the database `DATABASE()` names when it starts, so a `USE` in a migration moves the
statements after it but not the history. The generator does not follow `USE`: name the
database in the statement instead, as in `CREATE TABLE other.widgets`. See
[Sessions and the search path](../migration-runtime.md#sessions-and-the-search-path).

## Drivers

`database/sql` with `github.com/go-sql-driver/mysql`.

The DSN must set `parseTime=true`, as in
`app:secret@tcp(localhost:3306)/shop?parseTime=true`. The generated code reads
`DATETIME`, `DATE` and `TIMESTAMP` columns into `time.Time`, which go-sql-driver returns
only with that setting; without it, it returns their text as bytes and the first read
fails with `unsupported Scan, storing driver.Value type []uint8 into type *time.Time`.
The generated package's doc comment says so. `TIME` reads as a `string` either way.

A binary column, such as `BINARY(16)`, whose Go type an override chose binds the bytes the
value marshals itself to (`encoding.BinaryMarshaler`), so a `github.com/google/uuid.UUID`
override on a `BINARY(16)` key stores its 16 bytes and a lookup by the UUID finds the row,
where its `driver.Valuer`'s 36 characters would not fit. See
[What an override type must support](../type-mapping.md#what-an-override-type-must-support).

## Exported declarations

### dialect_engine_mysql

```go
const Family = mysql.Family
func New(options ...Option) dialect.Dialect
type Option = mysql.Option
    func WithANSIQuotes(enabled bool) Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
    func WithReturning(statements dialect.DataChange) Option
```
