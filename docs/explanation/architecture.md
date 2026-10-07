---
title: Architecture
description: "Seagull is a compiler from SQL to Go."
section: Explanation
subsection: Core
order: 50
---

# Architecture

Seagull has two paths: a compiler that turns SQL into Go, and a smaller runtime that applies migrations and seeds. Both use the same lexical rules, so they agree on where SQL statements begin and end.

## The pipeline

![SQL passes through reading, catalogue construction, analysis, checking, mapping and emission.](../diagrams/pipeline.svg)

The compiler builds a catalogue from migration DDL, then analyses each query against it. The resolver follows nested scopes and infers the parameters and output columns. Checks report problems at their SQL source locations.

The next stages map those SQL types to Go, plan the package's names and ask the emitter to write files. SQL reaches the emitter as a template with its parameter and dynamic-query positions already recorded; an emitter does not search the query text to rediscover them.

`internal/pipeline` runs these stages. `internal/app` puts it together with the defaults a caller leaves out, and the root package's `Generator` exposes it as `Generate`, `Vet` and `Catalogue`. File publication is a separate call through `WriteFiles`, which lets an embedding tool inspect the result first. See [from SQL to Go](from-sql-to-go.md) for how the intermediate model connects the stages.

## Packages

The public surface is the root package, `seagull.sh/seagull`, and the kit under `sdk/`. The implementation lives under `internal/`, which programs outside the module cannot import.

### The root facade

The root package is a thin facade over `internal/app`, the composition root. `New` builds the generator `internal/app` makes, each method of `Generator` forwards to it, the `With*` options forward to its options, and the input, result and preset types are aliases of its types. `internal/app` picks every default a caller leaves out: the database/sql emitter, the system clock, the default preset and the migration runtime tables it leaves out of a schema source's catalogue. It is the one place a built-in adapter meets the core.

The root also carries the diagnostics API: `SourceError`, the severities, `Codes`, `LookupCode` and every `Code*` constant, so one import covers generation and its findings. Its behaviour tests live in the `tests/facade` module, which uses only the public packages.

### The sdk kit

`sdk/` has one area for each extension point. Each area's facade, one `facade.go`, is a hand-picked set of type aliases and documented wrappers over `internal/`. It names only what a program uses or implements, because every exported name is a promise to keep; a value a program only reads, such as a catalogue's views, stays reachable through the field that holds it without a name of its own. Beneath the facade sit the providers, the implementations a program picks by importing one. A provider is named `<area>_<role>_<impl>`, such as `dialect_engine_postgres`, `emit_driver_pgx`, `migrate_backend_sqlite` or `schema_source_postgres`; the kits name only the area and the role, as in `emit_kit` and `emit_testkit`. Each provider forwards to its adapter under `internal/`.

| Area | Contracts and model | Providers |
|---|---|---|
| `sdk/dialect` | `Dialect`, the opaque handle to a built-in engine, its `Facts`, and the schema and type model | `dialect_engine_postgres`, `dialect_engine_cockroachdb`, `dialect_engine_timescaledb`, `dialect_engine_mysql`, `dialect_engine_mariadb`, `dialect_engine_sqlite`, `dialect_engine_duckdb`, `dialect_engine_clickhouse`, `dialect_registry` |
| `sdk/emit` | `EmitterPort` and its optional ports, the query and Go-type model | `emit_driver_databasesql`, `emit_driver_pgx`, `emit_driver_clickhouse`, `emit_kit`, `emit_testkit` |
| `sdk/migrate` | `Migrator`, `Seeder`, `ExecutorPort`, `SeedExecutorPort` and their optional ports, `Backend` and `CustomBackend`, and the lexical rules a backend splits its files under | `migrate_backend_postgres`, `migrate_backend_timescaledb`, `migrate_backend_cockroachdb`, `migrate_backend_mysql`, `migrate_backend_sqlite`, `migrate_backend_clickhouse`, `migrate_backend_duckdb` |
| `sdk/schema` | `SchemaSourcePort` | `schema_source_postgres`, `schema_source_sqlite` |
| `sdk/observe` | `ObserverPort`, the stages, `Clock` and `SystemClock` | none |

An engine provider's `New` returns a `dialect.Dialect`, an opaque handle. A program reads the engine's `Facts` and type names through it and hands it to the generator, which reaches the engine's dialect port, and the optional ports it finds on it with a type assertion, inside the module. The dialect ports stay internal, so dialects written outside Seagull are not supported yet. The root package and each area's facade have a `facade_test.go` that names each exported identifier and records the surface in `facade_test.golden.yaml`, so a change to the public API shows up in review, and `facade_closure_test.go` at the root checks that every type a caller must write, such as a parameter of a port it implements, has a name in a facade. The providers only forward to `internal/`, so they carry no tests of their own.

### Contracts

Each contract is an interface named for its role with a `Port` suffix, declared in the `ports.go` of its internal package: `internal/dialect`, `internal/emit`, `internal/migrate`, `internal/schema` and `internal/observe`, with PostgreSQL's `StatementExtensionPort` in `internal/dialect/postgres/pgext`. The built-in emitters implement `EmitterPort` in `driven_emitter.go`, and the migration executor implements the migrate ports in the `driven_*.go` files of `internal/migrate/sqlexec`. Each engine states the dialect ports it implements with a compile-time assertion such as `var _ dialect.DialectPort = (*Dialect)(nil)`; `sdk/dialect` names none of these ports, only the handle that holds an engine.

### Inside internal

The packages under `internal/` are flat and layered. A package imports only its own layer and the layers below it:

| Layer | Packages | Responsibility |
|---|---|---|
| Leaves | `internal/diag`, `logging`, `namespace`, `sqllex`, `fault`, `panicguard` and `observe`, with `diagbuild`, `structtag`, `sqlfile` and `directive/syntax` | Diagnostics, errors, logging, panic recovery, observation, and scanning SQL files and directives. |
| Model | `internal/sqlmodel` | The schema, type, query and Go-type model every stage shares. |
| AST and lexical leaves | `internal/dialect/ast`, `internal/dialect/<engine>/lexical`, `internal/gogen/goast` | What a dialect hands the analyser, each engine's lexical rules, and the Go syntax the generator writes. |
| Contracts | `internal/dialect`, `internal/emit`, `internal/schema`, `internal/migrate` | The ports and the values that cross them. |
| Kits | `internal/dialect/dialectkit`, `internal/emit/emitkit`, `internal/gogen` | The building blocks the engines and the emitters share. |
| Adapters | `internal/dialect/<engine>`, `internal/emit/<driver>`, `internal/schema/postgres`, `internal/schema/sqlite`, `internal/migrate/sqlexec` | The built-in implementations of the contracts. |
| Core | `internal/resolve`, `check`, `analyse`, `catalogue`, `gotypes`, `sqltemplate` and `pipeline`, with `directive`, `preset` and `output` | The compiler's stages. They read only the contracts and link no built-in adapter. |
| Composition root | `internal/app` | Picks the defaults and connects the core to the adapters. |
| Tooling | `internal/dialect/dialects`, `internal/dialect/dialecttest`, `internal/emit/emittest`, `internal/dialect/martian`, `internal/testkit` | The registry that builds an engine by name, the conformance suites and the test helpers. |

Imports across the module point the same way, which keeps the graph acyclic:

- Code under `internal/` never imports `sdk/` or the root, in its tests too.
- Of the module's packages, an `sdk/<area>` facade imports only `internal/`, and a provider only its own facade and `internal/`.
- Of the module's packages, the root imports only `internal/`.
- `cmd/sgl`, `cmd/wasm`, `tests/` and `examples/` use the root and `sdk/`, and the commands share `cmd/internal`, which is built on the same two. Beyond the test helpers, they reach only the logging and panic-recovery leaves of `internal/`.

The [reference](../reference/index.md) describes the public contracts. [Emitter authoring](../how-to/write-an-emitter.md) shows how to add an emitter, and [configuring a built-in dialect](../how-to/configure-a-dialect.md) how to fit an engine to a database.

## No reference engine

The core works with structural types and dialect capabilities rather than translating every database into PostgreSQL's model. An integer carries width and signedness; a name follows the dialect's folding rules; an operator's result comes from its type-system hook.

That matters when two engines accept similar SQL with different meanings. The resolver asks the dialect how to promote or cast types, and the Go mapper reads the resulting structure. Display names remain available for diagnostics and configured overrides.

An invented dialect, martian (`internal/dialect/martian`, which `emit_testkit.Run` adds to every emitter's suite), runs through the pipeline with unfamiliar type names, quoting and placeholders. Together with the neutrality tests, it catches assumptions that would make a real engine the hidden default.

## Boundaries

Lower layers do not import the stages above them. Dialects, emitters and schema sources implement the ports without reaching into the pipeline, and the core stages read only the ports, never a built-in adapter. The analyser reads a finished catalogue; emitters receive analysed queries. `internal/app` connects them, and the root package and `sdk/` are the only ways in from outside the module.

Each call across one of these contracts is guarded, because the code on the other side may be the host's. A panic in a dialect, an emitter, a schema source or an observer is recovered where the call is made and its stack logged once at warning level. A panic while one statement or query is handled becomes a Q070 diagnostic on it, so the rest of the run still reports; any other becomes an error matching `seagull.ErrPanic`, and an observer's panic leaves its stage unobserved. Limits apply at the same boundaries: the files read, the query blocks split and the diagnostics reported are all capped, so hostile or generated input cannot exhaust a host.

The migration runtime, `sdk/migrate` over `internal/migrate` and `internal/migrate/sqlexec`, links neither the analyser, a dialect's parser nor the emitters. It reaches each engine only through the engine's lexical leaf, `internal/dialect/<engine>/lexical`, which depends on `internal/sqllex`, which uses only the standard library. This lets a runtime split scripts using the same rules as generation without bringing in the compiler, and is why `sdk/migrate` names the lexical rule types a custom backend needs rather than importing `sdk/dialect`.

`depguard` in `.golangci.yml` enforces the import boundaries, and `independence_test.go` at the root checks that each engine builds only itself and its declared base and that the migration runtime links no analyser or dialect parser. Conformance tests check that dialect statement splitting agrees with the shared scanner, while the dialect and emitter suites exercise the contracts through the whole pipeline.

## Generated files

A generated package contains the driver calls and any runtime helpers its queries need. Its file layout is listed below; the [generated Go reference](../reference/generated-go.md) describes their calling contracts.

<!-- generated-file-table: overview (make generate-all) -->
| File | Contents |
|---|---|
| `querier.go` | `DBTX`, `Queries`, constructors, transaction helpers. |
| `querier_interface.go` | The `Querier` interface listing every query method, when `go.interface.enabled` is set. |
| `models.go` | One struct per table, and the named types the plan declares: a string type with a constant per value and a `Valid` method for each enum, and a struct type for each DuckDB `STRUCT` or ClickHouse named `Tuple` the mapping reads as a struct. PostgreSQL composite types map to `any`. |
| `prepared.go` | The prepared-statement cache, when enabled (`go.prepared: true` in `sgl`, on by default in the library's `emit_driver_databasesql.New()`). |
| `<name>.sql.go` | The methods for one query file. |
| `sortable.go`, `bind_limits.go`, `slice_helpers.go`, `runtime_helpers.go`, `sgl_json.go`, `driver_values.go`, `driver_composites.go`, `clickhouse_values.go`, `clickhouse_appending.go`, `clickhouse_composites.go`, `clickhouse_documents.go`, `value_reading.go`, `error_wrapping.go`, `convert_strings.go`, `query_names.go` | Small runtime helpers, each written only when a query needs it, so generated code needs nothing from Seagull. They are real Go sources in the repository, compiled and tested, and rendered into the package (see [generated Go](../reference/generated-go.md#helper-files)). |
<!-- end generated-file-table -->
