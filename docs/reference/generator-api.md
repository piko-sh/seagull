---
title: Generator API
description: "Construct a generator, analyse SQL and handle generated files through the public Go API."
section: Reference
subsection: Core
order: 90
---

# Generator API

The `sgl` command is a thin layer over the `seagull.sh/seagull` package. Build systems,
frameworks and editors can call the same package directly: give it a dialect and the SQL,
get the generated files and every diagnostic back, and decide what to do with them.

The library never needs cgo or a running database to generate code. Its dependencies are
listed under [dependencies](#dependencies).

## Generate code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "seagull.sh/seagull"
    "seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
)

func main() {
    generator, err := seagull.New(dialect_engine_postgres.New())
    if err != nil {
        log.Fatal(err)
    }

    project := os.DirFS(".")
    result, err := generator.Generate(context.Background(), seagull.Input{
        Package:      "store",
        Migrations:   project,
        MigrationDir: "db/migrations",
        QueryDir:     "db/queries",
    })
    if err != nil {
        log.Fatal(err) // unreadable files, a limit exceeded, cancellation
    }
    for _, diagnostic := range result.Diagnostics {
        fmt.Printf("%s:%d:%d: [%s] %s\n", diagnostic.Filename, diagnostic.Line,
            diagnostic.Column, diagnostic.Code, diagnostic.Message)
    }
    if err := result.Err(); err != nil {
        os.Exit(1) // at least one diagnostic has error severity
    }
    if err := seagull.WriteFiles("internal/store", result); err != nil {
        log.Fatal(err)
    }
}
```

`seagull.New` takes the dialect as its first argument, so forgetting one is a compile
error. Other settings are options with defaults listed below.

`Input` names where the SQL lives. `Migrations` and `Queries` are any `fs.FS`, so an
`embed.FS`, `os.DirFS` or `fstest.MapFS` all work; leave `Queries` nil to read the queries
from the same file system as the migrations. Directories are slash-separated paths inside
the file system, and an empty directory means its root. `Package` names the generated
package, and `PackagePath`, its import path, lets an override name a Go type declared in
that package without the package importing itself.

A directory listing leaves out named pipes, sockets and devices, even one named `*.sql`,
so a run never waits on a file another process writes. A symbolic link is followed. A
path that is not a regular file once followed, or a file larger than `WithMaxFileBytes`,
stops the run with an error; the size is checked before the file is read.

### Generate code and inspect the analysis

`GenerateDetailed(ctx, input)` performs one catalogue build and one analysis pass and
returns a `DetailedResult`. Its `Result` is the ordinary generation result; `Report`
contains the first vet report, `Catalogue` is the adopted schema before emission,
and `CatalogueDiagnostics` contains diagnostics from the schema source. The snapshots
are independent of later emitter changes and retries. This is useful for editors that
need generated code, query metadata and a schema browser together.

Use `Generate`, `Vet` or `Catalogue` individually when only that output is needed.
The ordinary generation path does not construct the additional snapshots.

### Errors and diagnostics

`Generate` returns an error only for problems that stop the run: invalid input, a file it
cannot read, a limit exceeded, cancellation, an emitter failure, a recovered panic, or a
`Generator` not made by `New`. An emitter failure reads `emitting package <name>: ...`
and still matches the emitter's own error with `errors.Is`. Problems in your SQL are
diagnostics, not errors. They come back in `Result.Diagnostics`, in source order, each
with a stable code (`Q001` and up, which `seagull.Codes` lists). When any diagnostic has error
severity, `Result.Files` is empty and `Result.Err()` returns a `*seagull.DiagnosticsError`
that matches `seagull.ErrDiagnostics`.

A run reports at most 10,000 diagnostics, or the number `WithMaxDiagnostics` sets. The
rest are left out and summarised in one [Q066](diagnostics.md#q066) diagnostic, with no
location, at the highest severity among them, so a run whose errors were left out still
fails. Smaller caps apply within a run: a query block keeps its first 1,000 diagnostics, a
directive call 32 and the directives of one migration file 256, each followed by a Q066
of its own.

A panic in code the run calls (the dialect, or an emitter, schema source or hook the host
supplies) does not crash the process. Seagull recovers it and logs its stack once at
warning level through the run's logger (`WithLogger`, the context's logger or
`slog.Default()`); the stack is never part of an error or a diagnostic. A panic while a
migration statement or a query is handled, or in an emitter's `Check` or `DefaultTypes`,
is a [Q070](diagnostics.md#q070) error diagnostic, and the run goes on to report the
rest. A panic in `Emit`, `BindsNullElements`, a schema source's `BuildCatalogue` or any
other dialect call stops the run with an error matching `ErrPanic`. `New` returns one for a
panic in the dialect's `Info` or the emitter's naming reservation, and
`FormatDirectivesFor` for a panic in the dialect's `Info`.

`WithSkipFailingQueries()` changes that for errors a query has of its own, such as SQL the
dialect cannot analyse or a query the emitter cannot generate: the query is left out,
`Result.Files` holds the rest of the package, `Result.Partial` is true, and
`Result.Err()` still reports the errors, so a caller that fails on them keeps failing.
Any other error, from a migration, the schema, two queries of one name or the package's
names, still leaves `Result.Files` empty. `sgl generate` runs with this option.

The errors `New` and `Generate` return wrap a small set of sentinels you can test with
`errors.Is`:

| Sentinel | Meaning |
|---|---|
| `ErrInvalidConfig` | An option, the preset or the input cannot be used, an option is nil, or the `Generator` was not made by `New` (a nil or zero one). |
| `ErrNoSource` | No dialect, no migrations or schema source, or no file system for queries. Wraps `ErrInvalidConfig`. |
| `ErrInvalidPrefix` | A directive, helper or error prefix has the wrong shape or repeats. Wraps `ErrInvalidConfig`. |
| `ErrLimit` | Input exceeds a configured limit. |
| `ErrFileTooLarge` | A file exceeds `WithMaxFileBytes`. Wraps `ErrLimit`. |
| `ErrPanic` | A panic Seagull recovered from code it called, such as a dialect, an emitter or a schema source. Its stack is logged once at warning level and is not part of the error. When the panic value was itself an error, the error matches that value too. |
| `ErrDiagnostics` | Error-severity diagnostics, through `Result.Err` or `Report.Err`. |
| `ErrParse` | SQL that cannot be parsed. `Generate` and `Vet` report it as a Q010 diagnostic rather than an error, so it comes back through `Result.Err` or `Report.Err`, which then match both `ErrDiagnostics` and `ErrParse`. |
| `ErrMalformedSQL` | A script with an unterminated string, quoted identifier or block comment, or an unbalanced block, that cannot be split into statements. The migration runner returns an error matching it; the generator reports the same problem as Q010. Wraps `ErrParse`. |

### Check without generating

`Vet` runs the same analysis but emits nothing. Its `Report` holds the diagnostics and the
analysed queries (parameters, output columns, nullability, directives), which is what an
editor integration or a CI check needs:

```go
report, err := generator.Vet(ctx, seagull.Input{
    Migrations: project, Package: "store",
    MigrationDir: "db/migrations", QueryDir: "db/queries",
})
if err != nil {
    return err
}
if err := report.Err(); err != nil {
    return err
}
```

A valid `Input.Package` enables naming and emitter checks. An empty or invalid package
name skips those checks, so it does not establish that the project can generate.

`Catalogue` returns the schema and its diagnostics without analysing queries. It ignores
query and package fields. Check its operational error, then its returned diagnostics,
as the [catalogue how-to](../how-to/vet-catalogue.md) demonstrates.

## Choose a dialect by name

When the dialect comes from configuration, use the registry instead of importing every
engine provider yourself:

```go
import "seagull.sh/seagull/sdk/dialect/dialect_registry"

chosen, err := dialect_registry.New(config.Dialect, dialect_registry.Options{MaxParseDepth: 256})
if err != nil {
    return err // wraps dialect_registry.ErrUnknownDialect and lists dialect_registry.Names()
}
generator, err := seagull.New(chosen)
```

`dialect_registry.Options` is plain data, holding what `sgl.yaml`'s `dialect_options` sets plus a
display `Name`. The `Name` and the parser limits (`MaxParseDepth`,
`MaxTokensPerStatement`) apply to every dialect; `SearchPath`, the schemas a session
starts with, to PostgreSQL, CockroachDB and TimescaleDB; `Returning` and `ANSIQuotes` to
MySQL and MariaDB; and `JoinUseNulls`, `GroupByUseNulls` and `MaxTypeParseDepth` to
ClickHouse. A zero field keeps the dialect's default. An option that does not apply to the
named dialect, a negative limit, a limit above its ceiling (4096 for `MaxParseDepth`,
1,000,000 for `MaxTokensPerStatement`, 64 for `MaxTypeParseDepth`), or a `Name` that is
blank or names a built-in dialect of another family, is an error wrapping
`dialect_registry.ErrInvalidOption` rather than silently ignored.

## Options

| Option | Default | Purpose |
|---|---|---|
| `WithEmitter(emit.EmitterPort)` | `emit_driver_databasesql.New()` | Which driver API the code targets: `emit_driver_databasesql`, `emit_driver_pgx`, `emit_driver_clickhouse`, or your own. |
| `WithPreset(Preset)` | `DefaultPreset()` | The vocabulary of directives and generated code. |
| `WithDirectivePrefixes(canonical, aliases...)` | `sgl`, `seagull` | The directive prefixes, replacing the preset's. |
| `WithForeignDirectivePrefixes(...)` | one legacy prefix | Prefixes of other tools, reported (Q051) rather than parsed. |
| `WithTypeOverrides(...TypeOverride)` | none | Map a SQL type, or one column, to a Go type. A column override that names no column of the schema is an error (Q037), and a type override that matches no type a warning (Q061). |
| `WithPackageNames(map[string]string)` | none | The name each Go package an override or type slot names declares, keyed by import path, for a package whose name is not the one its path implies; the generated code imports it under that name. |
| `WithCustomFunctions(...CustomFunction)` | none | Declare functions the dialect does not know, such as those of a loadable extension. |
| `WithSchemaSource(SchemaSourcePort)` | replay migrations | Build the catalogue another way, such as from a live database. |
| `WithRuntimeTables(...string)` | the `sgl_` history, seed and lock tables | The tables a schema source's catalogue leaves out, matched in every schema regardless of case; pass the table names a backend's `Names` method reports when its namespace or history table differ. No effect without `WithSchemaSource`. |
| `WithLogger(*slog.Logger)` | context, then `slog.Default()` | Where log records go. |
| `WithObserver(observe.ObserverPort)` | context, then none | Spans for each stage, for tracing and metrics. |
| `WithClock(observe.Clock)` | `observe.SystemClock()` | Times each run for the trace log. |
| `WithMaxFileBytes(int64)` | 4 MiB | Cap on each migration and query file, at most 1 TiB; a larger file stops the run with `ErrFileTooLarge`. |
| `WithMaxFiles(int)` | 10,000 | Cap on the entries in each directory. |
| `WithMaxQueries(int)` | 100,000 | Cap on the query blocks one run reads, a block whose header is malformed included; past it the run fails with `ErrLimit`. |
| `WithMaxTotalBytes(int64)` | 64 MiB | Cap on the bytes one run reads across every migration and query file; past it the run fails with `ErrLimit`. Files are read and processed one at a time. |
| `WithMaxDiagnostics(int)` | 10,000 | Cap on the diagnostics one run reports; the rest are summarised in one Q066 diagnostic at the highest severity among them. |
| `WithSkipFailingQueries()` | off | Leave out each query with an error of its own and generate the rest, as `Result.Partial` reports. |

`WithDirectivePrefixes` and `WithForeignDirectivePrefixes` apply on top of the preset
whatever order the options come in. Every limit must be positive; `New` rejects a
non-positive one, a file cap above 1 TiB and a nil option with `ErrInvalidConfig`. `New`
copies the type overrides and custom functions it is given, so changing them afterwards
does not change the generator.

## Emitters

`WithEmitter` chooses the driver API the generated code calls. Each stock emitter is a
provider package under `sdk/emit`: `emit_driver_databasesql.New(options...)`,
`emit_driver_pgx.New()` and `emit_driver_clickhouse.New()`. The [emit API](emit-api.md)
lists them with the tools for writing your own.

```go
import (
    "seagull.sh/seagull"
    "seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
    "seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
)

generator, err := seagull.New(dialect_engine_postgres.New(),
    seagull.WithEmitter(emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false))),
)
```

| | `emit_driver_databasesql` | `emit_driver_pgx` | `emit_driver_clickhouse` (native) |
|---|---|---|---|
| Dialects | every dialect | the PostgreSQL family | ClickHouse |
| Commands | every command, except `execrows` and `execresult` on ClickHouse | every command | every command except `execrows` and `execresult` |
| `execresult` returns | `sql.Result`; refused on ClickHouse | `pgconn.CommandTag` | refused |
| `batch` | chunked multi-row `INSERT`s, returning `error` | a `pgx.Batch`, returning `error` | a `PrepareBatch`, returning `error` |
| `copyfrom` | the same as `batch` | `COPY FROM`, returning `(int64, error)` | the same as `batch` |
| Slices | `kind: slice` expands `IN ($1)` at call time; on ClickHouse, an `Array` parameter | `kind: slice` binds one array to `= ANY($1)` | an `Array` parameter |
| `prepared.go` | yes by default, except on ClickHouse | no | no |
| Runtime builder (`dynamic: runtime`) | yes | yes | yes |
| `NewWithReplica`, `WithTx`, `RunInTx` | all three | `WithTx` and `RunInTx` | none |
| A `DBTX` that wraps transactions ([`WrapDBTX`](generated-go.md#wrapping-a-transaction-wrapdbtx)) | yes | no | no |

`emit_driver_databasesql.New()` writes `prepared.go` unless given
`emit_driver_databasesql.WithPrepared(false)`.
`sgl` does the opposite: its `go.prepared` setting defaults to `false`. The difference is
deliberate. The library keeps the default it has always had, so programs that already call
it generate the same files, while `sgl` makes the cache opt-in. ClickHouse never gets the
file because clickhouse-go prepares only `INSERT` statements. The
generated code uses the cache only once the program calls `Prepare`; see
[directives](directives.md#preparedgo).

The placeholders the generated code writes come from the dialect's `Info().Placeholders`.
How the code binds and scans values follows a per-driver profile inside the emitter,
chosen from the first module in the dialect's `Info().Drivers` the emitter knows, such as
pgx's database/sql adapter for PostgreSQL. `emit_driver_databasesql.WithDriver(module)` pins the
profile to one driver module, such as `github.com/mattn/go-sqlite3`, for a program that
uses a driver other than the dialect's first.

## Presets

A `Preset` defines directive vocabulary, generated headers and helper names, type slots
and optional output features. Start from `DefaultPreset` and pass the changed value to
`WithPreset`. `New` validates names and prefixes before analysing input. `DefaultPreset`
turns `ModernIdioms` on, so stream methods return `iter.Seq2`, and leaves `WrapErrors`,
which labels each returned error with its method's name, off.

```go
preset := seagull.DefaultPreset()
preset.Directives = seagull.Directives{Canonical: "acme"}
preset.Header = "// Code generated by acme. DO NOT EDIT."
preset.HelperPrefix = "acme"
preset.ErrorPrefix = "acme"
generator, err := seagull.New(dialect_engine_postgres.New(), seagull.WithPreset(preset))
```

See [presets](presets.md) for fields and defaults, [type mapping](type-mapping.md) for
type-slot precedence and [customising a preset](../how-to/presets.md) for adoption steps.

## Write the files

`WriteFiles(directory, result, options...)` publishes generated files. It replaces each
result filename atomically, regardless of the existing file's header. It then removes
stale regular files whose first line matches the selected generated or legacy header,
never opening a symbolic link, named pipe, socket or device. Use a dedicated generated
directory to avoid collisions with hand-written filenames.

The writer rejects an ordinary result with error diagnostics, but accepts a marked
partial result. It may remove methods and stale files belonging to rejected queries.
See [output writing](output-writing.md) for options and publication policy.

## Format directives

`FormatDirectivesFor` is what `sgl fmt` runs. It rewrites directives written with an alias
prefix to the canonical one and normalises the spacing inside directive comments, leaving
SQL bodies, prose comments and quoted values untouched. It reads the file as the dialect
writes SQL, so a directive-like line inside one of the dialect's strings, or a `--` the
dialect does not read as a comment, is left alone:

```go
formatted, err := seagull.FormatDirectivesFor(dialect_engine_mysql.New(), source, "sgl", "seagull")
```

`FormatDirectives(source, "sgl", "seagull")` does the same with neutral lexical rules,
for a caller with no dialect at hand. Both are idempotent, so running them in a pre-commit
hook is safe. Neither should run over applied migrations: their checksums are recorded, so
any change makes the next migration run fail.

## Generate from a live database

`WithSchemaSource` replaces migration replay with a `BuildCatalogue` implementation.
Built-in sources use `database/sql` to inspect PostgreSQL-family or SQLite databases.
Pass the same dialect as the source's type normaliser and the generator's dialect.

The source retains ownership of its catalogue. The generator clones it before applying
overrides, custom functions and runtime-table filtering. A shared source must keep the
returned object stable while it may be read.

See the [schema API](schema-api.md) for the interface and [live schema generation](../how-to/live-schema.md)
for connection setup and error handling. `Input.Migrations` may be nil with a source;
`Input.Queries` supplies the query file system.

## Logging and observability

`WithLogger` and `WithObserver` provide a logger and stage observer. Without explicit
options, the generator uses context values, then the default logger and a discard observer.
An observer receives a stage's context and attributes and returns a context and end callback.

See [errors and observability](errors-observability.md) for precedence, stages and callback
behaviour, and [observe generator work](../how-to/observe-generator.md) for a complete example.

## Run migrations

Migrations are a separate, much smaller run time that never links the analyser; see
[Migrations](migration-runtime.md) for `migrate.NewMigrator` and `migrate.NewMigratorFor`, and
the [migrate API](migrate-api.md) for the `sdk/migrate` package.

## Dependencies

Besides the standard library, the packages an embedder imports (the root package and the
`sdk/...` packages, with the implementation under `internal/` they forward to) link exactly
these modules. A test in the repository lists them and fails when
one is added or is no longer needed.

| Module | Why |
|---|---|
| `piko.sh/goastutil` | Builds the generated Go syntax trees. |
| `piko.sh/safeconv` | Checked integer conversions. |
| `piko.sh/safedisk` | File reads and writes confined to a directory. |
| `github.com/cespare/xxhash/v2` | Short hashes goastutil uses to disambiguate identifiers. |
| `golang.org/x/tools` | goastutil rewrites syntax trees through `golang.org/x/tools/go/ast/astutil`, the only package of the module the library links. Seagull formats generated code with `go/format`, so generation never runs the `go` command. |

Generated code needs nothing from Seagull: its helpers are written into the package, so it
imports only the standard library, the driver it was generated for, and the packages of
the Go types it uses (such as `github.com/cockroachdb/apd/v3` for decimals).

The pinned [goastutil cache fix](https://github.com/piko-sh/goastutil/pull/1) limits each
dynamic AST cache to 4096 entries and 1 MiB of key text, and bypasses keys larger than
4 KiB. Reset and insertion are synchronised. Seagull pins the immutable commit version
`v0.1.1-0.20261011054316-ba88dfe3a48e`; it uses no local replacement for this dependency.
Request-specific SQL render caches end with each emission. Prepared helper-source ASTs
are limited to the fixed embedded source files and are immutable between renders.

## Exported declarations

### seagull

The root package `seagull.sh/seagull` is a facade over the implementation under
`internal/`, so most of its declarations are aliases; run `go doc` on a name for its doc
comment. Its diagnostics declarations, from [`diagnostics.go`](../../diagnostics.go), are
listed with the [observe API](observe-api.md#diagnostics-in-the-root-package).

```go
const JSONCaseAsWritten = emit.JSONCaseAsWritten ...
const CodegenVersion = "3" ...
var ErrInvalidConfig = fault.ErrInvalidConfig ...
var Version string
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context
func FormatDirectives(source []byte, canonical string, aliases ...string) ([]byte, error)
func FormatDirectivesFor(engine dialect.Dialect, source []byte, canonical string, aliases ...string) ([]byte, error)
func WriteFiles(directory string, result *Result, options ...WriteOption) error
type CustomFunction = sqlmodel.CustomFunctionConfig
type DetailedResult = app.DetailedResult
type DiagnosticsError = app.DiagnosticsError
type Directives = app.Directives
type Generator struct{ ... }
    func New(engine dialect.Dialect, options ...Option) (*Generator, error)
type GoType = sqlmodel.GoType
type Input = app.Input
type Interface = emit.Interface
type JSONCase = emit.JSONCase
type JSONTags = emit.JSONTags
type Option = app.Option
    func WithClock(clock observe.Clock) Option
    func WithCustomFunctions(functions ...CustomFunction) Option
    func WithDirectivePrefixes(canonical string, aliases ...string) Option
    func WithEmitter(emitter emit.EmitterPort) Option
    func WithForeignDirectivePrefixes(prefixes ...string) Option
    func WithLogger(logger *slog.Logger) Option
    func WithMaxDiagnostics(limit int) Option
    func WithMaxFileBytes(limit int64) Option
    func WithMaxFiles(limit int) Option
    func WithMaxQueries(limit int) Option
    func WithMaxTotalBytes(limit int64) Option
    func WithObserver(observer observe.ObserverPort) Option
    func WithPackageNames(names map[string]string) Option
    func WithPreset(chosen Preset) Option
    func WithRuntimeTables(names ...string) Option
    func WithSchemaSource(source SchemaSourcePort) Option
    func WithSkipFailingQueries() Option
    func WithTypeOverrides(overrides ...TypeOverride) Option
type Preset = app.Preset
    func DefaultPreset() Preset
type QueryNames = emit.QueryNames
type Report = app.Report
type Result = app.Result
type SchemaSourcePort = schema.SchemaSourcePort
type Slot = sqlmodel.TypeSlot
type TypeOverride = sqlmodel.TypeOverride
type TypeSlots = sqlmodel.TypeSlots
type WriteOption = app.WriteOption
    func WithHeader(header string) WriteOption
    func WithLegacyHeaders(headers ...string) WriteOption
    func WithPermissions(permissions fs.FileMode) WriteOption
    func WithPrune(enabled bool) WriteOption
```
