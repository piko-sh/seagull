---
title: Dialect API
description: "sdk/dialect names the SQL dialect the generator works with: the built-in engines and the registry behind an opaque Dialect, the facts generated code reads, and the schema model."
section: Reference
subsection: Extensions
order: 1050
---

# Dialect API

A dialect is everything Seagull knows about one database engine: how it splits and parses
SQL, which types and built-in functions it has, and how it writes identifiers, parameters
and the clauses the generated code adds. The dialect area of the `sdk` kit,
`seagull.sh/seagull/sdk/dialect`, names the dialect a program hands the generator. A
`dialect.Dialect` is an opaque handle to one of the built-in engines: an engine provider's
`New` returns one, and `dialect_registry.New` builds one by name. A program reads two
things from it, the dialect's `Facts` through `Info()` and the SQL type a type name means
through `NormaliseTypeName`; the analyser and the code generator read everything else
inside the module. The analyser holds no engine facts of its own, so every built-in engine
plugs in the same way. Dialects written outside Seagull are not supported yet.

The same package holds the schema and type model a catalogue is built in, listed in
[SQL model](sql-model.md), and the placeholder forms and statement kinds `Facts` reports.

## Providers

The built-in engines, and the registry that builds one by name, are provider packages
under `sdk/dialect`, each imported as `seagull.sh/seagull/sdk/dialect/<sub-package>`.

| Sub-package | What it provides | When to use it |
|---|---|---|
| `dialect_engine_postgres` | The PostgreSQL engine: `New`, `WithSearchPath` and the options every engine takes | PostgreSQL |
| `dialect_engine_cockroachdb` | CockroachDB, built on the PostgreSQL engine, with `WithSearchPath` | CockroachDB |
| `dialect_engine_timescaledb` | TimescaleDB, the PostgreSQL engine with TimescaleDB's statements and functions, with `WithSearchPath` | TimescaleDB |
| `dialect_engine_mysql` | The MySQL engine: `New`, `WithANSIQuotes` and `WithReturning` | MySQL |
| `dialect_engine_mariadb` | MariaDB, built on the MySQL engine, with the same options | MariaDB |
| `dialect_engine_sqlite` | The SQLite engine | SQLite |
| `dialect_engine_duckdb` | The DuckDB engine | DuckDB |
| `dialect_engine_clickhouse` | The ClickHouse engine: `New`, `WithJoinUseNulls`, `WithGroupByUseNulls` and `WithMaxTypeParseDepth` | ClickHouse |
| `dialect_registry` | `New(name, Options)`, `Names`, the parser limits, and the errors for an unknown name or an unusable option | A dialect chosen by name from configuration, as `sgl` chooses one |

Every engine's `New` returns a `dialect.Dialect`, and every engine also takes `WithName`,
`WithMaxParseDepth` and `WithMaxTokensPerStatement`.
[Configure a built-in dialect](../how-to/configure-a-dialect.md) uses them, and the
[dialect reference](dialects/index.md) describes each engine and lists its declarations.

Source: [`sdk/dialect/facade.go`](../../sdk/dialect/facade.go).

## Exported package inventory

These declarations are the current `go doc -short` output of each package. Most are
aliases of the implementation under `internal/`, which a program never imports: run
`go doc` on a name for its doc comment, and see the [key declarations](#key-declarations)
for the fields and methods of the handle and its facts.

### sdk/dialect

[Package source](../../sdk/dialect) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/dialect)

`go doc -short` folds a constant group into its first name: the placeholder kinds
(`PlaceholderNumbered`, `PlaceholderAnonymous` and `PlaceholderNamed`), the statement
kinds of `DataChange` (`DataChangeInsert`, `DataChangeUpdate`, `DataChangeDelete` and
`DataChangeMerge`) and the type categories (`TypeCategoryInteger` and the rest).

```go
const PlaceholderNumbered = dialect.PlaceholderNumbered ...
const DataChangeInsert = ast.DataChangeInsert ...
const TypeCategoryInteger = sqlmodel.TypeCategoryInteger ...
const MaxTypeDepth = sqlmodel.MaxTypeDepth
type Catalogue = sqlmodel.Catalogue
type Column = sqlmodel.Column
    func NewColumn(name string, sqlType SQLType, nullable bool) Column
type DataChange = ast.DataChange
type Dialect = dialect.Dialect
type Facts = dialect.Facts
type MigrationOrigin = sqlmodel.MigrationOrigin
type PlaceholderKind = dialect.PlaceholderKind
type PlaceholderSigil = dialect.PlaceholderSigil
type Placeholders = dialect.Placeholders
type SQLType = sqlmodel.SQLType
    func NewSQLType(category SQLTypeCategory, engineName string) SQLType
type SQLTypeCategory = sqlmodel.SQLTypeCategory
type Schema = sqlmodel.Schema
type Table = sqlmodel.Table
type TableName = sqlmodel.TableName
```

### dialect_registry

[Package source](../../sdk/dialect/dialect_registry) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/dialect/dialect_registry)

The limits group holds `DefaultMaxParseDepth`, `DefaultMaxTokensPerStatement`,
`MaxParseDepthCeiling` and `MaxTokensPerStatementCeiling`, and the error group
`ErrUnknownDialect` and `ErrInvalidOption`.

```go
const DefaultMaxParseDepth = dialectkit.DefaultMaxParseDepth ...
var ErrUnknownDialect = dialects.ErrUnknownDialect ...
func Names() []string
func New(name string, options Options) (dialect.Dialect, error)
type OptionError = dialects.OptionError
    func OptionErrors(err error) []*OptionError
type Options = dialects.Options
```

## Key declarations

`go doc` shows each as an alias of its declaration under `internal/`; the second block of
each is that declaration, written with the names its package exports. A type the facade
leaves unnamed, such as `ListSyntax` or `PaginationSyntax`, keeps its internal name: a
program reads its value where `Facts` gives it and never names it.

### Dialect

```go
package dialect // import "seagull.sh/seagull/sdk/dialect"

type Dialect = dialect.Dialect
    Dialect is a SQL dialect as the generator and the tools around it hold one:
    the facts generated code reads about it, and how it names its types.
    Each dialect_engine_* provider's New returns one, and dialect_registry.New
    builds one by name.

    The zero Dialect holds no dialect: the generator refuses it, its Info is the
    zero Facts, and every type it names is of the unknown category.
```

The declaration `Dialect` aliases, written with the names `sdk/dialect` exports:

```go
type Dialect struct {
	// Has unexported fields.
}

func (handle Dialect) Info() Facts
func (handle Dialect) NormaliseTypeName(name string, modifiers ...int) SQLType
```

A `Dialect` satisfies `schema.TypeNormaliserPort`, so the dialect a program passes to
`seagull.New` is also the type normaliser a schema source takes.

### Facts

```go
package dialect // import "seagull.sh/seagull/sdk/dialect"

type Facts = dialect.Facts
    Facts is what generated code, and the tools that run the generator,
    read about a dialect: its name and family, how it writes parameters,
    identifiers, lists, row limits and the predicates a runtime filter appends,
    the drivers it runs on, the lexical rules its scripts split under and the
    statements that return rows.

    A Dialect's Info returns one, and an emitter reads one from its request.
    Facts belong to the caller, which may keep or change them.
```

The declaration `Facts` aliases, written with the names `sdk/dialect` exports:

```go
type Facts struct {
	// Placeholders describes how the dialect writes query parameters: the forms a query or a
	// directive line may write one in, and the form the generated code executes.
	Placeholders Placeholders

	// IdentifierQuote is how the dialect delimits an identifier, which the generated code
	// writes when it names a column itself.
	IdentifierQuote IdentifierQuote

	// Name identifies the dialect, such as postgres or cockroachdb. A dialect built with a
	// custom name reports that name here.
	Name string

	// Family names the base dialect whose syntax this dialect shares, such as postgres.
	// Tools use it to select shared behaviour.
	Family string

	// CommentPrefix opens a line comment in the dialect's SQL, such as --, which the
	// generated code writes before a comment it adds to a query.
	CommentPrefix string

	// TextType names the SQL type used to cast otherwise unreadable values to text. Empty
	// means the dialect provides no such type.
	TextType string

	// ToJSON names a function used to convert otherwise unreadable column values to JSON for
	// decoding. It is empty when the dialect provides none.
	ToJSON string

	// Filters describes what the generated runtime query builder may write into a predicate
	// it appends: the dialect's extra comparison operators, the functions that read a path
	// from a JSON column, and the conversions a filtered column may take.
	Filters FilterSyntax

	// Lists describes membership tests for caller-supplied lists, including empty IN and NOT
	// IN tests and optional array forms.
	Lists ListSyntax

	// Pagination describes how the dialect writes the row limit and offset the generated
	// runtime query builder appends.
	Pagination PaginationSyntax

	// JSONOperators lists the dialect's operators that read a path from a JSON value, such
	// as -> and ->>, in sorted order, or nothing when it has none. A filtered column may be
	// written with one of them.
	JSONOperators []string

	// Drivers lists supported Go driver module paths in preference order. Emitters use the
	// first recognised driver unless one is explicitly selected.
	Drivers []string

	// Lexical defines the dialect's SQL quoting, comments, placeholders and statement
	// boundaries. The zero value uses standard SQL rules.
	Lexical migrate.Rules

	// MaxParameters is the most parameters one statement may bind, which the generated code
	// respects when it expands a slice or batches rows. Zero means the engine sets no limit,
	// and the generated code keeps to a conservative default.
	MaxParameters int

	// Identifiers is how the dialect treats the letter case of identifiers, which a schema
	// catalogue built for it records.
	Identifiers sqlmodel.IdentifierCase

	// Returning lists statement kinds that support RETURNING. An empty set means none are
	// supported.
	Returning DataChange
}
```

The tools around the generator read the facts to choose what goes with a dialect: `sgl`
picks a migration backend by `Name` or `Family` and a `database/sql` driver from
`Drivers`, and a custom migration backend splits its files under `Lexical`, as
[migration backends](../how-to/migration-backend.md) shows.

## Limits and errors

Each engine bounds how deep a statement may nest and how many tokens it may hold:
`dialect_registry.DefaultMaxParseDepth` (256) and
`dialect_registry.DefaultMaxTokensPerStatement` (100,000) unless set, and never more than
`dialect_registry.MaxParseDepthCeiling` (4,096) and
`dialect_registry.MaxTokensPerStatementCeiling` (1,000,000). An engine's own
`WithMaxParseDepth` and `WithMaxTokensPerStatement` ignore a value below 1 and hold one
above its ceiling. A statement past either limit is refused, reported as
[Q010](diagnostics.md#q010) with an error matching `seagull.ErrLimit`.

`dialect_registry.New`, which `sgl.yaml` goes through, refuses instead: a negative limit,
a limit above its ceiling (`MaxTypeParseDepth` may be at most `dialect.MaxTypeDepth`, 64),
a blank `Name` or the built-in name of another family, and an option the named dialect
does not take. Each is a `*dialect_registry.OptionError` wrapping
`dialect_registry.ErrInvalidOption`, and `dialect_registry.OptionErrors` lists them. A
name `Names` does not list is `dialect_registry.ErrUnknownDialect`. Both errors wrap
`seagull.ErrInvalidConfig`.

A panic inside a dialect never crashes the run. Seagull recovers it wherever it calls the
dialect and logs its stack once at warning level: a panic while one statement is handled
is reported as [Q070](diagnostics.md#q070) on that statement, and any other is returned
as an error matching `seagull.ErrPanic`. A method that takes a context stops once the
context is done and returns its cause, as `context.Cause` reports it.
