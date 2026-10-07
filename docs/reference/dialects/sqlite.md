---
title: SQLite
description: 'Select the dialect with dialect: sqlite in sgl.yaml, or dialect_registry.New("sqlite",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 110
---

# SQLite

Select the dialect with `dialect: sqlite` in `sgl.yaml`, or `dialect_registry.New("sqlite", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | SQLite |
| Generated API | database/sql |
| Live introspection | Yes, SQLite source |
| Runtime migrations | SQLite serialises writers; DDL migrations transact except recognised no-transaction cases. Connection lifetime matters for in-memory databases. |

[Known limitations](../limitations.md#sqlite) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/sqlite.md) and [runnable examples](../../../examples/sqlite) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

### Column types and casts

Every integer spelling maps to signed `int64`; identifiers match without regard to
case. Most other declarations follow SQLite's affinity. Some retain a useful driver
representation: UUID reads from text or 16 bytes; DATE, DATETIME and TIMESTAMP scan as
`time.Time` only when declared without modifiers. TIME, INTERVAL and network spellings
are text; BYTEA, BINARY and VARBINARY are bytes. See the [type defaults](../type-mapping.md#defaults).

A type's modifiers are numbers, as in `DECIMAL(10, 2)`, and keep their places: a sign is
kept, a modifier too large is held at 1,073,741,824, and a modifier that is not a number,
an empty one or a third one is a syntax error ([Q010](../diagnostics.md#q010)). A type
a directive names, such as `type: 'decimal(10,2)'` or `type: 'varchar(255)'`, resolves as
a column declared with it does, its modifiers included, so `type: 'timestamp(3)'` is text.

JSON and JSONB declarations use Seagull's JSON type, which binds text. SQLite's binary
JSONB bytes need `json(column)` to read as text. A STRING declaration maps to string,
but SQLite's numeric affinity can still change `'012'` into `12`.

CAST uses affinity rather than those column representations: `CAST(x AS UUID)`,
`CAST(x AS DATETIME)` and `CAST(x AS BOOLEAN)` are numeric. Use `CAST(x AS TEXT)` for
text, `json(x)` for a document, or `x <> 0` for a truth value. A placeholder cast as
BOOLEAN accepts a Go bool, bound as 1 or 0. A CAST to a type typed by its own spelling
keeps the modifiers written with it, as a column declared with that type does:
`CAST(x AS VARCHAR(10))` is text of length 10 and `CAST(x AS DECIMAL(10, 2))` has precision
10 and scale 2, and a placeholder cast so takes that type. A CAST typed by its affinity,
such as `CAST(x AS TIMESTAMP(3))`, takes none.

### Queries and schema objects

Unqualified names resolve in `main`, then `temp`. Seagull understands `RETURNING`,
`ON CONFLICT`, `UPDATE ... FROM`, generated columns, STRICT tables, views, triggers,
FTS5 and R*Tree virtual tables, and table-valued functions such as `json_each`. An
unquoted name may hold `$` after its first character, as in `price$usd`. Table options
are a comma-separated list of `STRICT` and `WITHOUT ROWID` in either order, as SQLite
reads them; any other word after the table body is a syntax error.

DDL is read to the end of the statement, and what SQLite refuses is a syntax error
([Q010](../diagnostics.md#q010)): words left after a complete statement, a
`CREATE TABLE` body or column list without its `)`, a `CREATE VIRTUAL TABLE` without
`USING` or a module, a `CREATE TRIGGER` without a table after `ON`, a `CREATE INDEX`
without its column list, and an incomplete `REFERENCES`, `FOREIGN KEY`, `NOT NULL`,
`PRIMARY KEY`, `DEFAULT`, `CHECK`, `COLLATE`, `ON CONFLICT` or `IF [NOT] EXISTS`.
Creating a table under a name a table already holds, or renaming one onto a name in use,
is [Q067](../diagnostics.md#q067), and altering a table, or dropping a table or view,
that does not exist without `IF EXISTS` is [Q068](../diagnostics.md#q068).

A view or `CREATE TABLE ... AS` query with a syntax error fails the statement, and so
does `CREATE VIEW v AS (SELECT ...)`, which SQLite refuses. A `VALUES` query is typed. A
query nested past `max_parse_depth` applies with untyped columns and is reported as
[Q069](../diagnostics.md#q069).

Views follow changes to their source tables. A later column added to a table reaches a
view's star projection, including with `keep_stars`. If the view's declared column list
no longer matches, SQLite rejects the view while Seagull keeps the extra columns under
their own names. After each change, Seagull derives again only the views that read a
changed table or view, round by round while their columns still change; views that keep
changing each other, as views reading each other in a cycle do, stop at a round limit
with a [Q071](../diagnostics.md#q071) warning.

VALUES can be a statement, set-operation arm, CTE, derived table or subquery. Its columns
are `column1`, `column2` and so on. ORDER BY or LIMIT directly after VALUES is refused
([Q010](../diagnostics.md#q010)). Named windows, FILTER, GLOB, REGEXP and `==` are
understood. Driver builds determine whether extension functions are available.

An `ON CONFLICT` clause must be complete (`DO NOTHING`, or `DO UPDATE SET ...`), the `OR`
of `INSERT OR ...` and `UPDATE OR ...` names `ROLLBACK`, `ABORT`, `FAIL`, `IGNORE` or
`REPLACE`, and several `ON CONFLICT` clauses are read in turn.
`INSERT OR IGNORE ... RETURNING` on a `one` query gets the
[Q046](../diagnostics.md#q046) warning that `ON CONFLICT DO NOTHING` does.

Row comparisons and assignments type their parameters by position. Select-list aliases
are visible in WHERE, GROUP BY, HAVING and ORDER BY; a table column of the same name
wins. An unresolvable double-quoted name is treated as a string and warned about
([Q063](../diagnostics.md#q063)); use single quotes for strings.

### Expressions and results

Date functions accept text and numbers, with nullable results for invalid values.
JSON `->` returns JSON text; `->>` can return several SQL kinds and is typed `any`.
Division and remainder may be NULL unless the literal divisor cannot become zero:
`a / 2` is non-null, while `a % 0.5` is nullable because SQLite truncates the divisor.

Comparisons and logical operations propagate operand nullability. IS, IS NOT and IS NULL
are non-null. An ungrouped column beside an aggregate is nullable because SQLite returns
one row even when nothing matches. Parameters compared with expressions take their types;
inside an assignment they are typed from the expression rather than blindly from the
column.

Scalar subqueries keep the selected expression's type and become nullable when they can
return no row. For example, `(SELECT count(*) FROM products)` is non-null, while
`(SELECT max(price) FROM products)` can return NULL.

PRAGMA results use declared metadata, for example `user_version` or `table_info(t)`.
Pragmas with parameters, and deprecated or debugging result forms whose columns are not
known, are Q010. Settings without result rows and unknown pragmas return no columns.
VACUUM and BEGIN are accepted without query analysis; placeholders in such statements
cannot be bound. Unsupported statements, malformed conditions and `::` casts are Q010.

### Parse limits

`max_parse_depth` (256 by default, at most 4,096) and `max_tokens_per_statement` (100,000
by default, at most 1,000,000) under
[`dialect_options`](../configuration.md#dialect-options-dialect_options), or
`dialect_engine_sqlite.WithMaxParseDepth` and `WithMaxTokensPerStatement` in Go, bound what one statement
may cost; a statement past either is refused with an error matching `seagull.ErrLimit`.
Seagull keeps no more of a statement than the token budget, and a statement over it fails
when it is analysed, or applied as DDL; a seed `INSERT` and other statements replay
passes over still apply. Such a statement is still read to its end for a missing
semicolon, so a statement it runs on into is reported ([Q010](../diagnostics.md#q010))
at that statement's first word. The Go options hold a value above the ceiling at the
ceiling; `sgl.yaml` and `dialect_registry.New` refuse it.

## Primary keys

A single-column `INTEGER PRIMARY KEY` in an ordinary table is an alias for SQLite's signed
64-bit rowid: it maps to `int64`, and SQLite assigns it when an `INSERT` leaves it out, so
Seagull reports it as having a default. No other primary key gets a value automatically:
an `INT`, `BIGINT` or `TEXT` key, a composite key and the key of a `WITHOUT ROWID` table
have a default only when they declare one.

Seagull reads a primary-key column as SQLite keeps it. For historical reasons SQLite lets
an ordinary table store NULL in a primary-key column unless the column is the rowid alias
or is declared `NOT NULL`, or the table is `WITHOUT ROWID` or `STRICT`, so such a key,
whether declared on the column or as a table constraint, is nullable and reads into a
pointer (`*string` for a `TEXT PRIMARY KEY`); an `INTEGER PRIMARY KEY DESC` or `INT
PRIMARY KEY` column is not the rowid alias and is nullable too. Declare a key `NOT NULL`,
or make the table `WITHOUT ROWID` or `STRICT`, for a plain field.

## Parameters

`?`, `?NNN`, `:name`, `@name` and `$name` parameters. Every placeholder is sent numbered,
as `?N`, so `WHERE id = ? AND owner = :owner` reaches SQLite as
`WHERE id = ?1 AND owner = ?2`, and a name used twice binds one value. A parameter's name
continues through `$` after its first character, as a column's does, so `:low$usd` is one
parameter; its Go field drops the `$`. As in SQLite, a sigil followed by a name is a parameter
wherever it is written, even directly after another name, so `$1` names a parameter `1`
rather than the first placeholder: a directive anchored at `$1` that the query does not
write is [Q042](../diagnostics.md#q042) at the directive, which says the first
placeholder is written `?1`. A placeholder in a statement Seagull runs without analysis,
such as `ATTACH DATABASE ? AS archive` or `VACUUM INTO ?`, cannot be bound and is
[Q010](../diagnostics.md#q010).

## Migrations

No lock, since SQLite serialises writers itself (`migrate_backend_sqlite.New()`); DDL is transactional.

## Drivers

`database/sql` with `modernc.org/sqlite` (pure Go) or `github.com/mattn/go-sqlite3` (cgo).

The two drivers bundle SQLite built with different options. `modernc.org/sqlite` includes
the math functions (`sqrt`, `ceil`, `floor`, `pow`, `ln`, `log`, the trigonometric and
hyperbolic functions) and FTS5. The default build of `github.com/mattn/go-sqlite3` has
neither: a query calling `sqrt` fails with `no such function: sqrt`, and a migration
creating an FTS5 table fails, at run time, although Seagull accepts both. Build with the
driver's tags to include them:

```sh
go build -tags "sqlite_math_functions sqlite_fts5" ./...
```

Both builds include FTS3 and R*Tree. A function newer than a driver's bundled SQLite, such
as `if` (SQLite 3.48), needs a driver release that bundles that version.

Seagull reads `generate_series(start, stop[, step])` as the table function the SQLite
`series` extension provides, with one `value` column, but neither driver's build includes
that extension, so a query reading it fails with `no such table: generate_series` unless the
program loads the extension. A recursive common table expression counts without it:

```sql
WITH RECURSIVE series(value) AS (VALUES (1) UNION ALL SELECT value + 1 FROM series WHERE value < 10)
SELECT value FROM series;
```

A `BLOB` column whose Go type an override chose binds the bytes the value marshals itself
to (`encoding.BinaryMarshaler`), so a `github.com/google/uuid.UUID` key stores its 16
bytes. Code generated before stored the UUID's 36 characters as text; see
[What an override type must support](../type-mapping.md#what-an-override-type-must-support)
for the `UPDATE` that converts such rows.

## Exported declarations

### dialect_engine_sqlite

```go
const Family = sqlite.Family
func New(options ...Option) dialect.Dialect
type Option = sqlite.Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
```
