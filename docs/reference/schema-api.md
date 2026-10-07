---
title: Schema API
description: "sdk/schema lets the generator read its schema catalogue from a live database or another source instead of replaying the migrations."
section: Reference
subsection: Extensions
order: 1085
---

# Schema API

By default Seagull builds its schema catalogue by replaying the DDL in the migration
files. The schema area of the `sdk` kit, `seagull.sh/seagull/sdk/schema`, lets the
generator read it from somewhere else instead, such as a live database: a
`SchemaSourcePort` replaces migration replay when constructing the catalogue. The package
names what a source works with beside the port: `SourceError`, with the severities
`SeverityError` and `SeverityWarning`, for the diagnostics it reports beside the
catalogue, and `TypeNormaliserPort`, the part of a dialect that turns the database's type
names into SQL types, which a `dialect.Dialect` satisfies. The catalogue a source returns
is the `dialect.Catalogue` of [`sdk/dialect`](dialect-api.md), whose model types fill it.

## Providers

The built-in sources read a live database through `database/sql`. Each is a provider
package under `sdk/schema`, imported as `seagull.sh/seagull/sdk/schema/<sub-package>`.

| Sub-package | What it provides | When to use it |
|---|---|---|
| `schema_source_postgres` | `New(database, typeNormaliser)`, a `schema.SchemaSourcePort` reading `pg_catalog` | A live PostgreSQL, CockroachDB or TimescaleDB database is the schema authority |
| `schema_source_sqlite` | `New(database, typeNormaliser)`, a `schema.SchemaSourcePort` reading SQLite's `PRAGMA` table-valued functions | A live SQLite database is the schema authority |

Any other schema authority, such as a schema registry or a cached catalogue, implements
`SchemaSourcePort` itself; [custom sources](../how-to/custom-schema-source.md) builds one.

Source: [`sdk/schema/facade.go`](../../sdk/schema/facade.go).

## Source contract

```go
package schema // import "seagull.sh/seagull/sdk/schema"

type SchemaSourcePort = schema.SchemaSourcePort
    SchemaSourcePort is the port to implement to give the generator
    its schema catalogue from somewhere other than the migration files.
    schema_source_postgres and schema_source_sqlite implement it for a live
    database; pass any implementation to seagull.WithSchemaSource.

    Its one method, BuildCatalogue(ctx), returns the catalogue,
    the Catalogue of sdk/dialect, the diagnostics found while building it
    as SourceError values, and an error when it cannot be built: a schema
    problem belongs in the diagnostics, an operational failure in the error.
    The generator calls it on every run; honour the context's cancellation
    during slow reads. A nil catalogue without an error fails the run with an
    error matching seagull.ErrNoSource, and a panic fails it with one matching
    seagull.ErrPanic, its stack logged once at warning level.

    A view the source cannot type precisely may carry its definition, the SELECT
    text, in its Definition field: the generator analyses it to type the view's
    columns.

    The source keeps ownership of the catalogue it returns. The generator
    copies it before adding custom functions and column type overrides and
    never changes the original, so a source may cache one catalogue and return
    it from every call, and concurrent runs, of one generator or of several,
    may share it. The source must not change a catalogue it has returned while a
    run may still be reading it.

    A source needs no kit beyond the model types sdk/dialect names;
    docs/how-to/custom-schema-source.md walks through a complete one.
```

The declaration `SchemaSourcePort` aliases, written with the names `sdk/schema` exports:

```go
type SchemaSourcePort interface {
	// BuildCatalogue builds the schema catalogue.
	//
	// Returns *dialect.Catalogue which holds the schema; the source keeps ownership, and
	// the generator works on a copy.
	// Returns []SourceError which holds any diagnostics found while building it.
	// Returns error when the catalogue cannot be built.
	BuildCatalogue(ctx context.Context) (*dialect.Catalogue, []SourceError, error)
}
```

The method returns a catalogue, schema diagnostics and an operational error. A caller
supplies the dialect that interprets its SQL types and identifier rules. With a source,
`Input.Migrations` may be nil; queries still need a file system.

The source retains ownership of its catalogue. The generator clones it before applying
custom functions, overrides and runtime-table filtering. A source may cache and share a
catalogue provided it does not mutate it while a run may be reading it. The generator
passes over nil entries in the catalogue, such as a nil schema, table or view, and a nil
catalogue fails the run with an error wrapping `seagull.ErrNoSource`. A source that panics
fails the run with an error matching `seagull.ErrPanic` instead of crashing the process,
and its stack is logged once at warning level.

Built-in sources take a caller-owned `*sql.DB` and a type normaliser, normally the same
dialect passed to `seagull.New`. They do not close that connection. See [live schemas](../how-to/live-schema.md)
for setup and [custom sources](../how-to/custom-schema-source.md) for an implementation.

## Exported declarations

These declarations are the current `go doc -short` output of each package.

### sdk/schema

[Package source](../../sdk/schema) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/schema)

```go
const SeverityError = diag.SeverityError ...
type SchemaSourcePort = schema.SchemaSourcePort
type SourceError = diag.SourceError
type TypeNormaliserPort = dialect.TypeNormaliserPort
```

### schema_source_postgres

[Package source](../../sdk/schema/schema_source_postgres) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/schema/schema_source_postgres)

```go
func New(database *sql.DB, typeNormaliser schema.TypeNormaliserPort) schema.SchemaSourcePort
```

### schema_source_sqlite

[Package source](../../sdk/schema/schema_source_sqlite) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/schema/schema_source_sqlite)

```go
func New(database *sql.DB, typeNormaliser schema.TypeNormaliserPort) schema.SchemaSourcePort
```

## What introspection reads

Introspection aims to describe the same schema that replaying the migrations builds, and
the integration suites compare the two for every migration they run.

`schema_source_postgres` reads `pg_catalog`. It leaves out the system schemas, TimescaleDB's
internal schemas, and every schema or relation an extension created, so hypertable chunks
and extension tables never appear. Foreign tables read as tables, materialised views as
views, and standalone composite types as composite types. Array columns read as their
element type with their dimensions, enum columns carry their enum, and a domain reads as
the type it is declared over, with its `NOT NULL`. A chain of domains declared over
domains is followed 16 deep; a column whose chain goes deeper takes the type reached
there and gets a [Q054](diagnostics.md#q054) warning naming the column and the domain
where reading stopped. Each catalogue query runs once for every schema together, so the
number of round trips does not grow with the number of schemas.

`schema_source_sqlite` reads `pragma_table_list`, `sqlite_master` and the `PRAGMA
table_xinfo` and `PRAGMA index_list` table-valued functions, in four queries however many
tables the database holds. A declared type's modifiers, such as the length of
`VARCHAR(100)` or the precision and scale of `DECIMAL(10, 2)`, reach the type normaliser
as replaying the `CREATE TABLE` passes them. Only a single-column `INTEGER PRIMARY KEY`,
the rowid alias, reads as a 64-bit integer with a default. SQLite reports a view's columns
only with their declared types and never says which can hold NULL, so a view's columns
read as nullable with those types, and each view in the catalogue carries its definition,
the `SELECT` of its `CREATE VIEW` statement.

A `WITHOUT ROWID` table sets `dialect.Table.IsWithoutRowID`, and a virtual table sets
`IsVirtual` and carries its module, such as `fts5`, in `VirtualModuleName`. The shadow
tables an FTS5 or R*Tree table keeps its data in are left out, as replay leaves them out.
Columns SQLite hides from `SELECT *`, such as an FTS5 table's `rank` and the column named
after the table, are marked `Hidden`, so a `SELECT *` row type leaves them out. An FTS5
table declares its columns without types, so they read as replay types them: `text` for
each declared column and the column named after the table, and `real` for `rank`, which
reads as a generated column.

## What the generator does with a source's catalogue

The generator prepares its copy by adding custom functions and column overrides,
filtering runtime tables and analysing available view definitions. It also files the
copy's tables and functions under the dialect's keys for their names, so a source that
keys `Schema.Functions` by a lower-cased name still resolves; a source building a
catalogue by hand should key them with `Catalogue.Key(name)`.

### Runtime tables

History, seed and lock tables are excluded by name in every schema, without regard to
case. The default names are `sgl_migrations`, `sgl_seeds`, `sgl_migration_lock` and
`sgl_seed_lock`. For a custom namespace or history table, pass the backend's `Names`
values to `WithRuntimeTables`. The CLI uses the names selected by its `migrate` section.

### Views

View definitions are analysed after overrides, so a projection of an unchanged table
column carries its override. Analysis can supply more precise types and nullability
than the database's introspection metadata. Views over views are revisited until stable,
for at most one round more than there are views; views that still change each other then
keep what they have, with a [Q071](diagnostics.md#q071) warning.

When a definition cannot be analysed successfully or yields a different column count,
the view keeps the source's columns. Where analysis has no better type information,
the source's declaration remains the fallback. A definition whose parsing or analysis
panics in the dialect is reported as a [Q070](diagnostics.md#q070) error, and one whose
analysis the dialect leaves with a gap as [Q030](diagnostics.md#q030); either way the
view keeps the source's columns.

### Partitions

A partition is a table a query can read, as it is in the database: it holds its parent's
columns, primary key and constraints, and the catalogue records which table it
partitions (`dialect.Table.PartitionOf`). Generated code models only the parent, since
a model per partition would repeat it, so a query reading a partition gets a row type of
its own.

Replay and PostgreSQL introspection preserve this relationship. Parent column changes
and column overrides reach descendants; a partition-local change stays local. Detaching
a partition preserves its current schema but makes it an independent table. Dropping a
partitioned table also drops its descendants.

The CockroachDB dialect rejects `PARTITION OF`, `ATTACH PARTITION` and `DETACH PARTITION`
as CockroachDB does, since it partitions a table with the `PARTITION BY` clause of the
table itself, which creates no table per partition. MySQL and MariaDB partitions are
likewise part of their table, so their partition clauses and `ALTER TABLE ... PARTITION`
actions leave the catalogue unchanged.
