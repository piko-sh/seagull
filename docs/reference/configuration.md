---
title: Configuration
description: "Configuration fields, defaults and validation rules for sgl.yaml."
section: Reference
subsection: Core
order: 0
---

# Configuration

`sgl` reads its settings from `sgl.yaml` (or `sgl.yml` or `sgl.json`; `seagull.yaml`,
`seagull.yml` and `seagull.json` are accepted as aliases). It looks in the working
directory and then each parent directory in turn, so commands work from anywhere inside a
project. Pass `-c path/to/sgl.yaml` to choose a file explicitly. Relative directories in
the file (`migrations`, `queries`, `seeds` and `go.out`) are resolved against the
directory the file is in.

The file is decoded strictly: an unknown key is an error, with a suggestion when the key
is close to a known one. Files larger than 1 MiB are refused. A YAML file holds one
document, so a second `---` is refused, and a JSON file one value with nothing after it.
A file with many mistakes lists the first 50 problems and then how many more there are.
Every key is listed below with an example value. Database settings, including `go` and
`migrate`, belong beneath a `databases` entry.

## Example

```yaml
version: 1

directives:
  prefixes: [sgl, seagull]        # the first entry is canonical
  require_canonical: false        # true reports aliases as a hint (Q050)

databases:
  - name: app
    dialect: postgres             # see `sgl dialects`
    dialect_options:
      search_path: [app, public]
    migrations: db/migrations
    queries: db/queries
    seeds: db/seeds
    custom_functions:
      - name: similarity
        returns: real
        args: [text, text]
        nullable: null_on_null
    go:
      package: store
      out: internal/store
      driver: database/sql        # database/sql | pgx | clickhouse
      prepared: true
      query_names:
        enabled: true
      interface:
        enabled: true
      json_tags:
        case: camel               # snake | camel | pascal; unset keeps the column's name
        omitempty: true
      struct_tags:
        db: "{name}"
      empty_slices: true
      doc_comments: true
      reuse_models: true
      types:
        decimal: string
        overrides:
          - sql: uuid
            go: github.com/gofrs/uuid/v5.UUID
          - column: users.settings
            go: example.com/app/settings.Settings
          - column: users.email
            struct_tags:
              validate: "required,email"
    migrate:
      dsn: ${env:DATABASE_URL}
      lock: advisory              # advisory | table | none
```

## Top level

| Key | Type | Example | Meaning |
|---|---|---|---|
| `$schema` | string | `https://seagull.sh/sgl.schema.json` | The JSON Schema an editor validates the file against. `sgl` accepts it and ignores it; see [JSON Schema](#json-schema). |
| `version` | integer | `1` | The schema version of this file. `1` is the only version, and a file without the key is read as version 1. Any other version is refused, naming the one this `sgl` reads, which `sgl version` prints as `config schema`. |
| `directives.prefixes` | list of strings | `[sgl, acme]` | Directive prefixes accepted in SQL comments, each lower-case letters, digits and underscores, starting with a letter. The first is canonical, so `sgl fmt` rewrites the others to it and diagnostics use it. Default `[sgl, seagull]`. Listing another tool's prefix after the canonical one accepts its directives instead of reporting [Q051](diagnostics.md#q051), and `sgl fmt -w` then rewrites them to the canonical prefix. |
| `directives.require_canonical` | boolean | `true` | Report a hint ([Q050](diagnostics.md#q050)) wherever an alias prefix is used. Default `false`. |
| `databases` | list | see [Database](#database) | One entry per database, at least one and at most 256. Generation and analysis commands can process all entries; `--database` selects one. Connecting commands such as `migrate`, `seed` and `schema diff` require a selection when several entries exist. |

## Database

| Key | Type | Example | Meaning |
|---|---|---|---|
| `name` | string | `app` | Identifies the database in output and `--database`. Required, and unique in the file. |
| `dialect` | string | `postgres` | The SQL dialect: `postgres`, `cockroachdb`, `timescaledb`, `mysql`, `mariadb`, `sqlite`, `duckdb` or `clickhouse`, or one a [custom `sgl` binary](cli.md#custom-binaries) registers. Required. |
| `migrations` | path | `db/migrations` | Directory of `NNN_name.up.sql` and optional `NNN_name.down.sql` files (see [migration files](migration-files.md)), `db/migrations` by default. It must exist for every command that reads the migrations (`generate` and `vet` unless `schema_from` is set, `schema`, `migrate`); a missing one is a usage error naming it and this file. |
| `queries` | path | `db/queries` | Directory of annotated query files, `db/queries` by default; the `.sql` files directly inside it are read, not those of its subdirectories. Without the key and without that directory the database has no queries and only its models are generated; a directory the key names must exist. |
| `seeds` | path | `db/seeds` | Directory of seed files for `sgl seed`, optional. |
| `schema_from` | DSN | `${env:SCHEMA_DATABASE_URL}` | Build the catalogue from a live database instead of replaying the migrations, for the PostgreSQL family and SQLite, which Seagull can introspect. Supports `${env:NAME}`. A relative SQLite path resolves against the directory of `sgl.yaml`, and a SQLite file that does not exist is refused rather than created empty. |
| `dialect_options` | map | see [Dialect options](#dialect-options-dialect_options) | Settings of the dialect's parser and of the server the analysis assumes. |
| `custom_functions` | list | see [Custom functions](#custom-functions-custom_functions) | SQL functions the analyser cannot discover, such as ones from extensions. |
| `go` | map | see [Generated code](#generated-code-go) | How the Go code is generated. |
| `migrate` | map | see [Migrations](#migrations-migrate) | How `sgl migrate` and `sgl seed` run. |

### Dialect options (`dialect_options`)

Each option applies to the dialects listed beside it; setting it for another dialect is a
configuration error, which names the key (`dialect_options.returning applies to mysql and
mariadb, not postgres`). A parser limit of zero keeps the default, and a negative one, or
one above its maximum, is refused.

| Key | Dialects | Example | Meaning |
|---|---|---|---|
| `max_parse_depth` | every dialect | `256` | How deeply the parser follows nested expressions before it refuses a statement. Default `256`, at most `4096`. |
| `max_tokens_per_statement` | every dialect | `100000` | How many tokens one statement may hold before the parser refuses it. A statement is tokenised no further than this, so one huge statement costs no more than the limit. Default `100000`, at most `1000000`. |
| `max_type_parse_depth` | ClickHouse | `64` | How deeply a type name may nest, as in `Array(Nullable(...))`. Default `64`, which is also the most. |
| `returning` | MySQL, MariaDB | `true` | `true` accepts `RETURNING` on `INSERT`, `UPDATE` and `DELETE`, and `false` on none. Unset keeps the dialect's own: none on MySQL, and `INSERT` and `DELETE` on MariaDB. |
| `ansi_quotes` | MySQL, MariaDB | `true` | True when the server's `sql_mode` holds `ANSI_QUOTES`, so `"..."` is a quoted identifier rather than a string. `sgl migrate` and `sgl seed` split files under the same rules (`migrate_backend_mysql.WithANSIQuotes`). Default `false`, as on the server. |
| `join_use_nulls` | ClickHouse | `true` | The server's `join_use_nulls` setting, which the analysis assumes for every query: true fills an outer join's unmatched row with `NULL`, false with each column's default value. Default `false`, as on the server. |
| `group_by_use_nulls` | ClickHouse | `true` | The server's `group_by_use_nulls` setting: true fills the keys a `ROLLUP`, `CUBE` or `GROUPING SETS` row leaves out with `NULL`, false with each key's default value. Default `false`, as on the server. |
| `search_path` | PostgreSQL, CockroachDB, TimescaleDB | `[app, public]` | The schemas the server's sessions start with on their search path, in order, as the role's, database's or connection's `search_path` gives it. The migrations create unqualified objects in the first that exists until a migration sets another path, unqualified names in the queries are looked up in them, and the models of their tables are named without the schema. `"$user"` is left out. Default `public` alone. See [PostgreSQL](dialects/postgres.md) for how a `SET search_path` in a migration is followed. |

### Custom functions (`custom_functions`)

Each entry declares one function, typed as the analyser types a built-in:

| Key | Type | Example | Meaning |
|---|---|---|---|
| `name` | string | `similarity` | The function's name, as queries call it, read as the dialect reads an unquoted name: folded to lower case in PostgreSQL, case-sensitive in ClickHouse, and matched in any case in MySQL, MariaDB, SQLite and DuckDB. Required. |
| `returns` | SQL type | `real` | The type of its result. Required. |
| `args` | list of SQL types | `[text, text]` | The type of each argument, in order. |
| `min_args` | integer | `1` | The fewest arguments a call may pass, when the trailing ones are optional; at most the number of `args`. Default: every argument. |
| `variadic` | boolean | `true` | The last argument may repeat. Needs at least one argument. |
| `aggregate` | boolean | `true` | The function aggregates rows, as `count` does. |
| `nullable` | string | `never_null` | When the result is NULL: `null_on_null`, the default, when any argument is NULL; `never_null`, never; or `called_on_null`, whatever its arguments. |
| `data_access` | string | `modifies_data` | `read_only`, the default, or `modifies_data`, which sends a query calling the function to the writer (see [reader and writer routing](directives.md#reader-and-writer-routing)). |

## Generated code (`go`)

| Key | Type | Example | Meaning |
|---|---|---|---|
| `package` | string | `store` | Package name of the generated code, a lower-case Go identifier. Default `store`. |
| `out` | path | `internal/store` | Output directory, default `internal/store`. Result filenames replace existing files, regardless of their headers. Stale files with the generated header are removed; other filenames remain. Each database needs a directory of its own: two databases may not share an `out`, or have one inside the other, so with more than one database set `out` on each. Validation reports each shared directory once, naming the databases that write to it, and each nested one with the nearest directory that holds it. |
| `driver` | string | `pgx` | The emitter and with it the database API the code calls: `database/sql` (default), `pgx` (PostgreSQL family only) or `clickhouse` for the native ClickHouse driver; see [Drivers](#drivers). A [custom `sgl` binary](cli.md#custom-binaries) can register more. |
| `prepared` | boolean | `true` | Emit `prepared.go`, a prepared-statement cache that is used only once you call `store.Prepare(ctx, db)` and pass its result to `New` (see [generated Go](generated-go.md#preparedgo)). Default `false`. `database/sql` only: setting it with `pgx` or `clickhouse`, or for ClickHouse, whose `database/sql` driver prepares only `INSERT` statements, is a configuration error. |
| `keep_stars` | boolean | `true` | Write `SELECT *`, `RETURNING *` and `table.*` in the generated SQL as the queries write them. Default `false`: each star is written as the columns it stands for, so code deployed before a migration that adds a column keeps working until it is regenerated (see [generated Go](generated-go.md#namesqlgo)). |
| `query_names.enabled` | boolean | `true` | Generate a function that maps a statement's SQL to its query's name, for labelling telemetry. Default `false`. |
| `query_names.file` | string | `names.go` | The file that function is written to. Default `query_names.go`. |
| `query_names.func` | string | `StatementName` | The function's name. Default `QueryName`. |
| `interface.enabled` | boolean | `true` | Generate `querier_interface.go`, an interface listing the method of every query, which `Queries` implements. Default `false`. See [generated Go](generated-go.md#querier_interfacego). |
| `interface.name` | string | `Store` | The interface's name, default `Querier`: a Go identifier the package declares no other way. A name it already declares, such as `Queries` or `New`, is a configuration error; a model whose table has the name gives way to it (`Querier2`). |
| `json_tags.case` | string | `camel` | Write the name in the `json` tag of every generated field that holds a column, in the models and in the rows, in `snake`, `camel` or `pascal` case. Unset, the default, writes the column's name as it is written. |
| `json_tags.omitempty` | boolean | `true` | Add the `omitempty` option to those `json` tags. Default `false`. |
| `json_tags.omit` | boolean | `true` | Leave the `json` tags out. Default `false`. |
| `struct_tags` | map | `{db: "{name}"}` | Struct tags every generated field that holds a column carries after its `json` tag, by key: `db: "{name}"` gives `db:"author_id"`. In each value, `{name}` stands for the name the `json` tag is made from, the column's name made safe for a tag and unique in its struct, whatever `json_tags.case` says. A column's own struct tags, from a column override or a [`sgl.column` directive](directives.md#type-override-sglcolumn), take precedence key by key. `json` is not allowed here; `json_tags` shapes it. |
| `empty_slices` | boolean | `true` | Make a `many` method, and a runtime builder's `All`, return an empty slice rather than `nil` when no row matches, so `encoding/json` writes `[]` rather than `null`. Default `false`. |
| `doc_comments` | boolean | `true` | Write doc comments from the SQL: a query's description, the prose comment lines of its header, on its method (and on the interface's method), and a table's or a column's comment on its model or field, as `COMMENT ON TABLE` and `COMMENT ON COLUMN` give them (PostgreSQL, CockroachDB, TimescaleDB and DuckDB) or a `COMMENT` clause does (MySQL, MariaDB and ClickHouse). Default `false`. |
| `wrap_errors` | boolean | `true` | Wrap every error a generated method returns with a label naming the method, as `fmt.Errorf("%s: %w", label, err)` does, so a timeout or syntax error says which query met it while `errors.Is` and `errors.As` still find the error beneath; see [labelled errors](generated-go.md#labelled-errors-wraperrors). Default `false`. |
| `reuse_models` | boolean | `true` | Read the rows of every query whose rows are a table's model into the model rather than a Row struct of the query's own: a query that reads only that table's columns, whose Row struct would have the model's fields, by name, of the same Go types and with the same struct tags, in any order, such as `SELECT * FROM users`. A query's own `model:` or `row:` keyword takes precedence; see [directives](directives.md#rows-read-into-a-model-or-a-shared-row-type). Default `false`. |
| `types.decimal` | Go type | `string` | Go type for `NUMERIC` and `DECIMAL` columns. Default `github.com/cockroachdb/apd/v3.Decimal`, and `github.com/shopspring/decimal.Decimal` on ClickHouse; `string` needs no dependency. |
| `types.money` | Go type | `example.com/app/billing.Money` | Go type for money, such as PostgreSQL's `money`. Default `string`, the text the engine writes with its currency symbol, whatever `types.decimal` says. |
| `types.bigint` | Go type | `encoding/json.Number` | Go type for integers wider than 64 bits, such as `Int128` or `HUGEINT`. Default `string`, and `*big.Int` on ClickHouse and DuckDB, whose drivers return one. |
| `types.json` | Go type | `encoding/json.RawMessage` | Go type for JSON documents. Default the `JSON` type the package declares in `sgl_json.go` (see [JSON values](type-mapping.md#json-values)). |
| `types.interval` | Go type | `string` | Go type for intervals. Default `string`, and `time.Duration` with `pgx`. |
| `types.time_of_day` | Go type | `string` | Go type for a time of day, such as `TIME`. Default `time.Time`, and `string` behind `database/sql` for PostgreSQL and MySQL, whose drivers return it as text, and for a zone-aware time of day such as PostgreSQL's `timetz`, which pgx has no codec for; DuckDB reads both into `time.Time`. Setting it applies to both kinds. |
| `types.ip_address` | Go type | `net.IP` | Go type for a column holding a single IP address, such as ClickHouse `IPv4`. Default `net/netip.Addr`, and `net.IP` for ClickHouse behind `database/sql`. |
| `types.enum` | Go type | `string` | Go type for every enum. Unset, each enum maps to the named Go type the generated package declares for it, with a constant for each value, or to `string` where the package declares none. |
| `types.non_null_elements` | boolean | `true` | Declare the elements of arrays and lists, and the values of maps, non-null where the engine can hold NULL among them, as in a PostgreSQL array or a DuckDB list, so they read into plain values rather than pointers, in rows, models and parameters alike; see [elements that may be NULL](type-mapping.md#elements-that-may-be-null). Default `false`. |
| `types.overrides` | list | see [Type overrides](#type-overrides-typesoverrides) | Replace the Go type, the element nullability or the struct tags of a SQL type or of one column. |
| `types.package_names` | map | `{k8s.io/api/core/v1: v1}` | The name a package declares, keyed by its import path, for a package an override or a type slot names whose name its path does not imply, such as a directory named apart from its package or `k8s.io/api/core/v1` (package `v1`); the generated code imports it under that name. A path such as `gopkg.in/guregu/null.v4` (`null`) or `github.com/twpayne/go-geom` (`geom`) needs no entry. |
| `legacy_headers` | list of strings | `["// Code generated by dbgen. DO NOT EDIT."]` | Earlier generated-file headers that also mark a file as safe to prune: a file in `out` whose first line is one of them, and that this run does not write, is removed. |

A Go type is written `import/path.Name`, such as `github.com/shopspring/decimal.Decimal`
or `encoding/json.RawMessage`, or as a built-in such as `string` or `[]byte`. Name the
value type: a nullable column becomes a pointer to it automatically, and a leading `*` is
refused. A qualified name must be exported, and the generated code imports the package
under the name its path implies (see [changing a type](type-mapping.md#changing-a-type)),
or the one `types.package_names` gives. Composite types such as `map[string]int` are
refused; declare a named type. An override the generator cannot write is error
[Q049](diagnostics.md#q049) against `sgl.yaml`, from `sgl vet` and `sgl generate`
alike.

### Type overrides (`types.overrides`)

Each entry of `go.types.overrides` sets exactly one of `sql` and `column`, and at least one
of `go`, `non_null_elements` and `struct_tags`:

| Key | Type | Example | Meaning |
|---|---|---|---|
| `sql` | SQL type | `uuid` | The SQL type the entry applies to: a built-in type, or a type a migration creates, such as an enum. `mood` follows the schema search path and `audit.mood` names the schema. A `sql` that names no type the dialect knows and no column's type is warning [Q061](diagnostics.md#q061). |
| `column` | `table.column` | `users.settings` | The one column the entry applies to, as `table.column` or `schema.table.column`. A column that does not exist is error [Q037](diagnostics.md#q037) against `sgl.yaml`. |
| `go` | Go type | `github.com/gofrs/uuid/v5.UUID` | The Go type, written as above. |
| `non_null_elements` | boolean | `true` | Declare the type's or column's array elements and map values non-null (`true`) or nullable (`false`). |
| `struct_tags` | map | `{validate: "required,email"}` | For a column only: struct tags, by key, for every generated field holding the column. A `json` among them replaces the generated `json` tag, so `json: "-"` keeps a column out of JSON. |

A column override affects its model field, query results and parameters inferred from
that column. A `go_type:` directive on the same column takes precedence; a SQL-type
override applies wherever that type appears. See
[type mapping](type-mapping.md#where-each-override-applies) for precedence and parameters
with an explicit cast or typed placeholder.

The Go type an override names works with every driver, for parameters and results when it is a defined type over
the column's mapped Go type (`type Label string` for text, `json.RawMessage` for text, JSON or
binary, `json.Number` for an IP address), a type the driver itself reads and writes, or one
that implements `sql.Scanner` and `driver.Valuer`; the generated code converts where a driver
does not. Any other type is yours to make work (see [what an override type must
support](type-mapping.md#what-an-override-type-must-support)).

### Drivers

`go.driver` chooses the emitter, and with it the API the generated code calls:
`database/sql` serves every dialect, `pgx` the PostgreSQL family and `clickhouse`
ClickHouse. The commands each one generates, how it batches, and which constructors it
offers differ; see the [dialect and driver matrix](driver-matrix.md). `sgl config validate`
checks that the driver fits the dialect.

`sgl` and the library differ on prepared statements: `go.prepared` defaults to `false`,
while the library's `emit_driver_databasesql.New()` writes `prepared.go` unless given
`emit_driver_databasesql.WithPrepared(false)`. The library keeps its default so that programs written
against it before the setting existed generate the same code; see
[embedding](generator-api.md#emitters).

## Migrations (`migrate`)

| Key | Type | Example | Meaning |
|---|---|---|---|
| `dsn` | string | `${env:DATABASE_URL}` | Connection string. `sgl migrate`, `sgl seed` and `sgl schema diff` take `--dsn`, then `$SGL_DATABASE_URL`, then this value. Supports `${env:NAME}`. A relative SQLite path here resolves against the directory of `sgl.yaml`; one given in `--dsn` or `$SGL_DATABASE_URL` resolves against the working directory. |
| `namespace` | string | `app` | Prefix for persisted names: `<ns>_migrations`, `<ns>_seeds` and the lock tables and keys. A SQL identifier of letters, digits and underscores. Default `sgl`. |
| `history_table` | string | `schema_history` | Rename the migration history table, `<ns>_migrations` by default. The migration lock is keyed on this name. |
| `lock` | string | `table` | `advisory` (default where supported), `table` (for PgBouncer in transaction mode, and the default for CockroachDB) or `none`. SQLite, ClickHouse and DuckDB take no lock, so only `none` applies to them. See [migration runtime](migration-runtime.md). |

## Environment expansion

`${env:NAME}` is expanded only in `dsn` and `schema_from`, so secrets never need to live in
the file. Connection strings, and the passwords inside them, are redacted whenever `sgl`
prints them, including inside database driver errors.

A reference to a variable that is not set is left as written, never replaced with an
empty string, and the command that uses the connection string fails with exit status 2,
naming the database, the key and the variable. `schema_from` is used by `generate`,
`vet`, `describe` and `schema dump` without `--from-db`, so an unset variable never makes
them fall back to the migrations. `migrate.dsn` is used by `migrate`, `seed` and
`schema diff` when neither `--dsn` nor `$SGL_DATABASE_URL` is given. Other commands do
not need the variable.

`sgl` expands references when it loads the file.

## JSON Schema

`sgl config schema` prints the JSON Schema for this file, for editor completion and
validation; the same schema is published as [`sgl.schema.json`](sgl.schema.json). It
lists the accepted values of `migrate.lock`, `go.json_tags.case` and a custom function's
`nullable` and `data_access`, and the maximum of each parser limit. Point an editor at it
with a comment on the first line of `sgl.yaml`:

```yaml
# yaml-language-server: $schema=https://seagull.sh/sgl.schema.json
```

In `sgl.json`, name it with the `$schema` key:

```json
{
  "$schema": "https://seagull.sh/sgl.schema.json",
  "version": 1,
  "databases": [{"name": "app", "dialect": "sqlite"}]
}
```

`sgl config validate` checks a file without running anything else, including that every
`dialect` and `go.driver` is one the `sgl` binary knows and that they fit together, and
everything else `sgl generate` checks before it reads a file, such as a
`go.interface.name` that collides with a name the generated package declares.
