---
title: CLI
description: "Commands, flags, exit codes and output formats for the sgl command."
section: Reference
subsection: Core
order: 10
---

# CLI

Install `sgl` as a tool dependency of your module, so everyone on the project runs the
same version:

```sh
go get -tool seagull.sh/seagull/cmd/sgl
go tool sgl help
```

Or install it globally with `go install seagull.sh/seagull/cmd/sgl@latest`, or download a
signed binary from the [releases page](https://github.com/piko-sh/seagull/releases).
`go install seagull.sh/seagull/cmd/seagull@latest` installs the same tool as `seagull`.

`sgl` finds its configuration by looking for `sgl.yaml` (see [configuration](configuration.md)) in
the working directory and then each parent directory, so every command works from anywhere
inside a project.

## Commands

| Command | What it does |
|---|---|
| `sgl init [--dialect postgres]` | Write `sgl.yaml`, a first migration, an example query file and a seed file. |
| `sgl generate [--check] [--diff] [--dry-run] [--timings]` | Generate Go code for every database. `--check` exits 1 when the generated code is out of date, for CI. |
| `sgl vet [--strict] [--format text\|json\|github\|sarif]` | Analyse migrations and queries and report problems without writing anything. |
| `sgl fmt [-l\|-w\|--check] [--rewrite-prefix p] [paths]` | Canonicalise directive comments: alias prefixes and spacing. SQL bodies are never touched. |
| `sgl explain [--format text\|markdown] [Q012]` | Explain a diagnostic code, or list them all. |
| `sgl describe [--format text\|json] <Query>` | Show a query's parameters, result columns, nullability and Go signature. |
| `sgl schema dump [--from-db DSN] [--format json\|sql]` | Print the schema built from the migrations or read from a live database. |
| `sgl schema diff [--dsn DSN]` | Compare a live database with the schema the migrations describe. |
| `sgl migrate up\|down [n]\|to V\|status\|validate\|new NAME\|force V\|repair [V]` | Apply, roll back, inspect, validate, create or repair migrations. |
| `sgl seed apply\|status\|reseed` | Apply and inspect seed data. |
| `sgl dialects` | List dialects and their capabilities. |
| `sgl config schema\|validate [path]` | Print the configuration JSON Schema, or validate a configuration file. |
| `sgl version` | Print the CLI, library, generated-code, configuration-schema and Go versions. |

Global flags go before the command:

| Flag | Meaning |
|---|---|
| `-c path` | Use this configuration file instead of searching for one. |
| `--database name` | Restrict the command to one database. Commands that take a database also accept `--database` after their name. |
| `--log-level debug\|info\|warn\|error` | Log verbosity on stderr. Without it, `sgl migrate` and `sgl seed` log warnings, and code generation logs only errors, since its warnings already reach you as diagnostics. The stack of a recovered panic is logged at `warn` (see [Exit codes](#exit-codes)). |

A command's own flags may come before or after its arguments, so `sgl migrate up --dsn
file:app.db` and `sgl migrate --dsn file:app.db up` are the same. `-h` or `--help` works
on every command and command group (`sgl migrate -h`, `sgl migrate up --help`): it prints
the usage and flags on stdout and exits 0.

An error in the configuration file names the file relative to the working directory when
it lies beneath it, and by its full path otherwise.

### init

`sgl init` writes a starter project for a dialect (`postgres` when `--dialect` is
omitted): `sgl.yaml`, `db/migrations/0001_init.up.sql` and `0001_init.down.sql`,
`db/queries/example.sql` with three annotated queries, and `db/seeds/0001_authors.sql`. The
SQL is written for the dialect's family, so it vets cleanly as it stands. `sgl init`
never overwrites a file: if any of them exists it stops with exit status 2 and writes
nothing.

### generate

`sgl generate` replays each database's migrations (or reads `schema_from`), analyses every
query, and writes the Go into `go.out`. Files a previous run generated and this run no
longer produces are removed when they carry a recognised generated header. Filenames in
the current result are replaced regardless of their previous header; other handwritten
files are left alone, and a symbolic link, named pipe, socket or device in `go.out` is
never opened or removed. Existing files are read only as far as `sgl` needs: enough to
tell whether a file it writes changed, and the header of any other. Diagnostics are
printed on stderr. A query with an error of its own, such as SQL the dialect cannot
analyse or a query the emitter cannot generate, is left out: the rest of the package is
written, and the exit status is 1, so a CI run still fails until the query is fixed or
removed. Any other error, from a migration, the schema, two queries of one name, an
analysis limit or the package's names, stops generation: nothing is written for that
database and the exit status is 1. `sgl vet` reports the same errors without writing
anything.

The prepared-statement cache, `prepared.go`, is written only for a database that sets
`go.prepared: true`; the default is `false`. The library's `emit_driver_databasesql.New()` defaults the
other way, on purpose; see [configuration](configuration.md#drivers).

| Flag | Meaning |
|---|---|
| `--check` | Write nothing; exit 1 and list the files that differ when the generated code is out of date. |
| `--diff` | Write nothing; print a unified diff of what would change. Combine with `--check` to fail as well. |
| `--dry-run` | Write nothing; list the files that would be added, modified or removed. |
| `--timings` | Print how long each stage of the run took, on stderr: one row per stage, in the order the stages first started, with its span count, total time and failures. |
| `--database name` | Generate only this database. |

### vet

`sgl vet` runs the same analysis as `generate` and reports every diagnostic, writing
nothing, including what the configured emitter would refuse before it generates, such as
[Q059](diagnostics.md#q059) and [Q060](diagnostics.md#q060), so a project `vet` passes
also generates. The exit status is 1 when any diagnostic is an error, or, with `--strict`, a
warning. `--format` chooses the output (see [Diagnostics](#diagnostics)).

### fmt

`sgl fmt` rewrites directive comments into their canonical form: alias prefixes become the
canonical prefix (the first entry of `directives.prefixes`), and spacing is normalised.
Positional and keyword arguments keep their order. SQL is never changed: only comments that are directives are rewritten.
Each file is read as its database's dialect writes SQL, with the dialect's quotes,
comments and line comment prefix, so a directive-like line inside one of its strings, or a
`--` that the dialect does not read as a comment, is left alone; a file outside every
configured directory is read with neutral rules.

With no paths it formats the query and seed directories of every configured database;
given paths, it formats those files and the `.sql` files beneath those directories, and
needs no configuration. Named pipes, sockets and devices met in a directory are skipped,
and one run formats at most 10,000 files. A file larger than 4 MiB, or a named pipe or
device given as a path, is refused. A symbolic link is formatted through: its target is
rewritten and the link kept. An interrupt stops the run before the next file.

Migrations are left out unless `--include-migrations` is given. Seagull records the
checksum of every migration it applies and refuses to migrate when an applied file has
changed, so rewriting even an alias prefix in an applied migration would make the next
`sgl migrate up` fail with a checksum mismatch. A path inside a configured migrations
directory is refused without the flag, and a directory that holds one is formatted
without it, with a note. Use the flag only for migrations that have not been applied
anywhere, or record the new checksums afterwards with `sgl migrate repair`.

| Flag | Meaning |
|---|---|
| (none) | Print the formatted files to stdout. |
| `-l` | List the files whose directives are not canonical. |
| `-w` | Rewrite the files in place. With `-l` or `--check`, list each changed file and rewrite it. |
| `--check` | List the files that need formatting and exit 1 when there are any, with `-w` too. |
| `--rewrite-prefix p` | Rewrite every directive to the prefix `p`, for example when renaming a project's prefix. |
| `--include-migrations` | Format the configured migrations directories too. |

To adopt directives written with another tool's prefix, list that prefix in
`directives.prefixes` after the canonical one, then run `sgl fmt -w`: the directives are
accepted as aliases and rewritten to the canonical prefix. `--rewrite-prefix` is not the
tool for this, since it rewrites every directive to the prefix it names.

### explain

`sgl explain Q012` prints a code's title, usual severity and an explanation of its cause and
fix; `q12` and `12` work too. With no code it lists every code. `--format markdown` prints
Markdown instead: the [diagnostics reference](diagnostics.md) is generated that way.

### describe

`sgl describe GetUser` finds a query in the configured databases by the name its `sgl.query`
header gives, as written there, or by the name of its generated method: a query declared
`get_user_by_id` is found as `get_user_by_id` and as `GetUserByID`, and a query named
`WithTx`, whose method becomes `WithTx2` because the generated code declares a `WithTx` of
its own, is found by both names. It prints its SQL as the generated code writes it, without
the directive lines of its header, its parameters and result columns with their SQL types
and nullability, and the Go it generates: the SQL constant, the method's signature and the
parameter and row structs it uses. Each parameter is shown as the generated SQL writes it,
in the dialect's placeholder form: `$1` on PostgreSQL, `?1` on SQLite, `?` on MySQL,
`{p_id:UInt64}` on ClickHouse.
`--format json` prints the same as JSON, with the form in each parameter's `placeholder`.
A name that matches no query is a usage error with a suggestion. A query named like a
method a runtime builder declares, such as `Count` or `Where`, shows its own method.

### schema

`sgl schema dump` prints the schema catalogue, built by replaying the migrations, or with
`--from-db DSN` by introspecting a live database. `--format json` (the default) prints
tables, views and enums with their columns, types and nullability; `--format sql` prints
approximate DDL for reading.

A catalogue read from a live database, by `--from-db`, `schema_from` or `schema diff`,
leaves out the history, seed and lock tables the migration and seed runtime keeps, by the
names `migrate.namespace` and `migrate.history_table` give them, so a table that only
looks like the default `sgl_migrations` is kept. SQLite reports a view's columns only with
the types they are declared with, so `sgl` types each SQLite view from its definition, as
replaying the migrations does: an expression gets its type, and a column is nullable only
when it can hold NULL, through an outer join for example.

`sgl schema diff` connects to a live database and compares its tables and columns with the
schema the migrations describe: tables and columns that exist on one side only, and
columns whose type or nullability differ. The migration and seed bookkeeping tables are
ignored. It reads the live schema through the connection it opened and prints the
diagnostics introspection reports, as `schema dump` does. It exits 0 when they match and
1 when they drift. Introspection, and so
`--from-db`, `schema_from` and `schema diff`, is available for the PostgreSQL family and
SQLite. MySQL, MariaDB, ClickHouse and DuckDB have none, so their schema always comes from
the migrations.

For the PostgreSQL family, introspection reads only your own objects. It leaves out the
system schemas, TimescaleDB's internal schemas (so hypertable chunks never show as
drift), and any schema or relation an extension created. Partitioned tables and their
partitions appear as tables, each partition naming the table it partitions, as replaying
the migrations records them (see [Schema API](schema-api.md#partitions));
foreign tables appear as tables. Array
columns read as their element type with `[]` per dimension (`text[]`, `int4[][]`), as
they do from the migrations, and enum columns carry their enum. A domain reads as the
type it is declared over, with its `NOT NULL`, as it does from the migrations.

### migrate and seed

| Command | What it does |
|---|---|
| `sgl migrate up` | Apply every pending migration. |
| `sgl migrate down [n]` | Roll back the most recent `n` migrations (default 1). |
| `sgl migrate to V` | Migrate up or down until version `V` is the newest applied. |
| `sgl migrate status` | List every migration: `pending`, `applied`, `dirty`, or `changed` when the file no longer matches its recorded checksum. |
| `sgl migrate validate` | Compare the applied migrations with the files without running anything; exit 1 when they disagree. |
| `sgl migrate new NAME` | Create the next `NNNN_name.up.sql` and `.down.sql` pair; needs no database. |
| `sgl migrate force V` | Record migration `V` as applied and clean, with its files' checksums, without running it. Refused when a later migration is recorded. |
| `sgl migrate repair [V]` | Record the current checksums of applied migrations whose files changed, and resolve a dirty migration by how many of its statements ran. `V` limits it to one migration. |
| `sgl seed apply` | Apply every pending seed. |
| `sgl seed status` | List every seed and whether it ran. |
| `sgl seed reseed` | Clear the seed history and apply every seed again. |

`sgl migrate up` prints a line for each migration it applies and then `N applied`;
`sgl migrate down`, and `sgl migrate to` when it moves down, print a line for each one it
reverts and then `N rolled back`. `sgl seed apply` and `reseed` print `N applied`. When a
run fails or is interrupted after changing something, the count is printed before the
error, so you know what ran.

Before running anything, `up`, `down` and `to` refuse a migration about to run whose
`sgl.migration` directive is malformed, such as `no_transacton: true`, or names an
unknown role, naming the file and line. `down`, and `to` when it moves down, refuse to
roll back while a migration is dirty; finish it with `up` or resolve it with `repair`
first.

| Flag | Meaning |
|---|---|
| `--lock-timeout d` | Give up, changing nothing, when another process still holds the lock after `d`, such as `30s`. Applies to `up`, `down`, `to`, `force`, `repair`, `seed apply` and `seed reseed`. Without it they wait until interrupted. |
| `--non-blocking` | Give up at once when another process holds the lock. |
| `--yes` | Run `force` or `repair` without asking for confirmation. |
| `--completed n` | For `repair`: how many statements of the one dirty migration ran, for running it without a terminal. |

`force` and `repair` change only the history table, under the migration lock. They print
what they will change on standard error and ask for confirmation when standard input is a
terminal; without one, which includes a pipe or a device such as `/dev/zero`, they refuse
to run unless `--yes` is given, and `repair` needs `--completed` for a dirty migration.
An answer is one line of at most 1 KiB. Declining changes nothing and exits 1. See
[Recovering with force and repair](migration-runtime.md#recovering-with-force-and-repair)
for what each records.

The migration backend is chosen by the name the dialect reports (its `Info().Name`), then
by its family, not by the `dialect:` key in `sgl.yaml`: PostgreSQL, CockroachDB and
TimescaleDB each get their own backend, MySQL and MariaDB share MySQL's, and SQLite,
ClickHouse and DuckDB get theirs. It takes `migrate.namespace`, `migrate.history_table` and
`migrate.lock` from the configuration. SQLite, ClickHouse and DuckDB take no lock, so
`migrate.lock` can only be `none` for them. A dialect with neither a stock backend nor one
registered with `cli.WithMigrationBackend` cannot be migrated. DuckDB's backend needs a
binary with a DuckDB driver; see [custom binaries](#custom-binaries). See
[Migrations and seeds](migration-runtime.md) for how runs are locked and recorded. On the
PostgreSQL family, MySQL and MariaDB a run applies its migrations on one session, so a
`SET search_path` or a `USE` in one migration lasts for the later ones of the run, while the
history and seed tables stay in the schema or database the connection started in (see
[Sessions and the search path](migration-runtime.md#sessions-and-the-search-path)).

### dialects

`sgl dialects` lists every dialect the binary knows, with its family, parameter style,
whether it supports `RETURNING`, whether it has a migration backend and introspection, and
the database driver the binary opens for it.

### config

`sgl config schema` prints the JSON Schema of `sgl.yaml` (also published as
[`sgl.schema.json`](sgl.schema.json)). `sgl config validate [path]` reads a configuration
file, by default the one `sgl` would find, and checks it, including that every dialect and
`go.driver` is one this binary knows and that they fit together, that each dialect option
applies to its dialect, and everything else `sgl generate` checks before it reads a file,
such as a `go.interface.name` or `go.query_names.func` that collides with a name the
generated package declares. Every other command refuses such a file the same way.
`sgl version` prints the configuration `version` this binary reads as `config schema`.

## Connection strings

`sgl migrate`, `sgl seed` and `sgl schema diff` read the connection string from `--dsn`,
then the `SGL_DATABASE_URL` environment variable, then `migrate.dsn` in the configuration.
They work on one database: when the configuration has several, choose one with
`--database`. Connection strings, and the passwords inside them, are always redacted when
`sgl` prints them, including inside database driver errors.

A `${env:NAME}` reference to a variable that is not set is reported only by a command
that uses that connection string, with exit status 2, naming the database, the key and
the variable. `migrate`, `seed` and `schema diff` use `migrate.dsn` when neither `--dsn`
nor `SGL_DATABASE_URL` is given; `generate`, `vet`, `describe` and `schema dump` without
`--from-db` use `schema_from`, and never fall back to the migrations in its place. See
[environment expansion](configuration.md#environment-expansion).

A relative SQLite path resolves according to where it was written. In `migrate.dsn` or
`schema_from` it is relative to the directory of `sgl.yaml`, so `file:app.db` names the same
file whichever directory `sgl` runs from. In `--dsn`, `--from-db` or `$SGL_DATABASE_URL` it
is relative to the working directory: the process's own, or the one `cli.WithDirectory`
sets in a custom binary.

The stock binary includes pure-Go drivers, so it builds without cgo:

| Dialect family | Driver |
|---|---|
| PostgreSQL, CockroachDB, TimescaleDB | `github.com/jackc/pgx/v5/stdlib` |
| MySQL, MariaDB | `github.com/go-sql-driver/mysql` |
| SQLite | `modernc.org/sqlite` |
| ClickHouse | `github.com/ClickHouse/clickhouse-go/v2` |

DuckDB needs cgo, so the stock binary generates code for DuckDB but cannot connect to it;
build a custom binary (below) to add a DuckDB driver, and it runs `sgl migrate` and
`sgl seed` against DuckDB. The `cli` package links none of these drivers, so a custom
binary links only the drivers it imports.

## Diagnostics

Problems are printed as the file, line and column, the severity and code, and the message;
then the source line, numbered in a gutter, with carets under the span at fault; then a
`help:` line when there is a suggestion:

```
db/queries/authors.sql:1:30: error[Q027]: unknown keyword argument "nullabel" on sgl.query
   |
 1 | -- sgl.query(GetAuthor, one, nullabel: true)
   |                              ^^^^^^^^
help: did you mean "nullable"?
```

A source line longer than 160 characters is quoted as a window of 160 around the span,
with `...` where it was cut, and a line longer than 1 MiB as `(line too long to show)`.
`sgl vet` ends with a count of errors, warnings and hints.

Diagnostics that come from the configuration rather than a SQL file, such as a type
override naming a column that does not exist, are reported against `sgl.yaml`.

`--format json` writes one JSON object per diagnostic (`file`, `database`, `severity`,
`code`, `message`, `line` and `column`, and `suggestion`, `end_line` and `end_column` when
they are set),
`--format github` writes GitHub Actions workflow annotations, and `--format sarif` writes a
SARIF 2.1.0 log for code-scanning tools. Every code is documented in the
[diagnostics reference](diagnostics.md) and by `sgl explain`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Findings: error diagnostics (or warnings under `vet --strict`), generated code out of date under `--check`, unformatted files under `fmt --check`, a schema that drifts under `schema diff`, migrations that fail `migrate validate`, or a `force` or `repair` that was declined or that the history refused. |
| 2 | Usage error: unknown command or flag, invalid or missing configuration, unknown dialect or driver, a `${env:NAME}` in a connection string the command uses that names an unset variable, a file `init` will not overwrite, a migration path `fmt` leaves alone, a file `fmt` will not read, a migration or output directory holding more than 10,000 entries, an unknown migration version, `force` or `repair` without a terminal or `--yes`, an answer longer than 1 KiB, or in a custom binary an option with an empty name or a nil factory. |
| 3 | Internal error: a failure inside `sgl`, including a panic in `sgl` or in code it runs, such as a registered dialect, emitter or driver. |
| 4 | Database error: the database cannot be reached, or a migration or seed fails or is refused before it runs. |
| 130 | Interrupted by Ctrl-C, or by cancelling the context given to `cli.MainContext`. A second Ctrl-C or SIGTERM ends the process at once, with 130. |
| 143 | Stopped by SIGTERM, which a CI system sends when it cancels a job: 128 plus the signal's number, as a shell reports it. |

An error is printed on stderr as `sgl: ...`, followed by a `hint:` line when another
`sgl` command resolves it:

| Error | Hint |
|---|---|
| `migrate force` refused because a later migration is recorded | Run `sgl migrate down` first, or force the later migration instead; run `sgl migrate repair N` first when that one is dirty. |
| A dirty migration whose file no longer matches the statements that ran | Run `sgl migrate repair N` once you know how many of its statements ran. |
| A migration interrupted while it ran | Run `sgl migrate status`, and `sgl migrate repair N` if the migration is dirty. |
| `migrate down` or `migrate to` refused while a migration is dirty | Finish it with `sgl migrate up`, or record how many of its statements ran with `sgl migrate repair N`, then roll back. |
| The lock's transaction ended while the run needed it | Run `sgl migrate status` to see where the run stopped, and check the server's and any pooler's idle timeouts before running again. |

A [Q051](diagnostics.md#q051) diagnostic for another tool's prefix gets a `help:` line
saying that `sgl fmt -w` rewrites it once the prefix is accepted.

A panic never escapes as a crash and its stack is never printed with the error: `sgl`
prints `sgl: internal error: ...`, or the error naming the panic, and logs the stack
once on stderr at `warn` level, which `--log-level error` hides. Code generation logs
only errors by default, so run it again with `--log-level warn` to see the stack of a
panic there. A panic while one statement or query is handled is reported as diagnostic
[Q070](diagnostics.md#q070) on it instead.

On the first Ctrl-C or SIGTERM, `sgl` says it is cancelling and lets the command stop
cleanly. The command then prints `sgl: interrupted`, or `sgl: interrupted: ...` naming
what was running, such as the migration, with a hint when one applies.

## Custom binaries

`sgl` is a thin wrapper around the importable `seagull.sh/seagull/cmd/sgl/cli` package. To
register a configured dialect, an emitter or a database driver, write your own `main`:

```go
package main

import (
    "os"

    _ "github.com/duckdb/duckdb-go/v2"

    "seagull.sh/seagull/cmd/sgl/cli"
)

func main() {
    os.Exit(cli.Main(os.Args[1:], cli.WithDriver("duckdb", "duckdb")))
}
```

| Option | What it adds |
|---|---|
| `cli.WithDialect(name, factory)` | A dialect that `dialect:` can name. The factory receives the database's `dialect_options`. |
| `cli.WithEmitter(name, factory)` | An emitter that `go.driver:` can name. The factory receives a `*cli.EmitterRequest` with the dialect's facts and `go.prepared`. |
| `cli.WithMigrationBackend(name, factory)` | The migration backend for a dialect's `Info().Name`, or for a family. The factory has the shape of the `migrate_backend_*` constructors, such as `migrate_backend_cockroachdb.New`, and receives the options from the `migrate` section. |
| `cli.WithDriver(dialect, driver)` | The `database/sql` driver to open for a dialect, or for every dialect of a family such as `postgres`. A dialect's own name wins over its family. |
| `cli.WithIO(stdin, stdout, stderr)` | Replacement streams, for programs that capture the output. The error stream also receives the messages `cli.Main` prints about interrupts. |
| `cli.WithDirectory(dir)` | Run as if started in `dir`. |
| `cli.WithName(name)` | The program name the CLI prints in help, usage, errors, hints and `version`; `sgl` by default, `seagull` in `cmd/seagull`. |

The `cli` package links no database driver of its own. A custom binary imports each driver
it needs and names it with `cli.WithDriver`. To keep the stock drivers, import the four in
[connection strings](#connection-strings) and register each for its family, as
`cmd/seagull` does.

A registration with an empty name, a nil factory or an empty driver name makes every
command exit 2, naming the option, before anything runs; a nil option is skipped. A panic
in a registered dialect, emitter or driver is recovered like any other (see
[Exit codes](#exit-codes)).

`cli.MainContext(ctx, args, options...)` runs one command under a context you own. With
the DuckDB binary above, `sgl migrate` and `sgl seed` work on DuckDB, while
`sgl schema dump --from-db` and `sgl schema diff` still refuse it, as Seagull cannot
introspect DuckDB. The [custom sgl binary example](../../examples/embedding/053_custom_sgl_binary)
uses three of them: a SQLite dialect with tighter parser limits, a database/sql emitter
that writes an extra file, and the pure-Go SQLite driver.

`sgl migrate`, `sgl seed` and the runtime tables `schema` leaves out use the migration
backend of the dialect's `Info().Name`, so a stock dialect registered under another key
keeps its backend: a `dialect_engine_cockroachdb.New()` registered as `crdb` still takes the CockroachDB
lock table. A dialect given a name of its own with `WithName` falls back to its family's
backend; register one under that name, built from a `migrate.BackendSpec`, when the
database it names needs different migration SQL:

```go
cli.WithMigrationBackend("harbourdb", func(options ...migrate.BackendOption) migrate.Backend {
    return migrate.CustomBackend(harbourSpec, options...)
})
```

An emitter factory receives a pointer to a `cli.EmitterRequest` holding the dialect's facts,
`Dialect`, and `go.prepared`; it must not keep the pointer. The stock database/sql
emitter chooses how it binds and scans values from the drivers the dialect lists in
`Dialect.Drivers`, so a factory that wraps it passes only the settings it changes, and
`emit_driver_databasesql.WithDriver` when the binary links a driver other than the dialect's first:

```go
cli.WithEmitter("database/sql", func(request *cli.EmitterRequest) (emit.EmitterPort, error) {
    return emit_driver_databasesql.New(
        emit_driver_databasesql.WithPrepared(request.Prepared),
        emit_driver_databasesql.WithDriver("modernc.org/sqlite"),
    ), nil
})
```
