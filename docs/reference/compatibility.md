---
title: Compatibility
description: "Versioning and compatibility contracts for the generator, configuration and public APIs."
section: Reference
subsection: Core
order: 40
---

# Compatibility

Seagull is in alpha. Until v1, any release may change the public API, but each change is
deliberate and listed in the notes of the [release](https://github.com/piko-sh/seagull/releases)
that makes it. From v1 on, the guarantees below apply.

## Stability tiers

The public surface is the root package, `seagull.sh/seagull`, and the kit under `sdk/`,
which has one area for each extension point: `sdk/dialect`, `sdk/emit`, `sdk/migrate`,
`sdk/schema` and `sdk/observe`. Each area is a facade that names only what a program uses
or implements: its contracts, named `...Port`, and the values that cross them. Its
providers are sub-packages named `<area>_<role>_<impl>`, such as
`sdk/dialect/dialect_engine_postgres`. The implementation lives under `internal/`, which
no program imports, so a name the facades leave out carries no promise.

| Tier | Packages | Promise |
|---|---|---|
| Stable | `seagull.sh/seagull` (the root facade, with its diagnostic codes), `sdk/observe`, `sdk/migrate` (apart from the executor ports and lexical rule types below) and its `migrate_backend_*` providers, `sdk/dialect/dialect_registry` (whose `Options` may gain fields), the `dialect.Dialect` handle, and each `dialect_engine_*` and `emit_driver_*` provider's `New` function with its options | Changes only in a new major version. |
| Evolving | `sdk/dialect`'s `Facts` and its schema and type model, `sdk/emit` (the emitter ports and the query and Go-type model), `sdk/emit/emit_kit`, `sdk/emit/emit_testkit`, `sdk/schema` and its `schema_source_postgres` and `schema_source_sqlite` providers, `cmd/sgl/cli`, the lexical rule types of `sdk/migrate` (`Rules` and its companions), and the `sdk/migrate` executor ports: `ExecutorPort`, `SeedExecutorPort`, the optional `HistoryEditorPort` and `SeedLockTrierPort`, `MigrationRecord` and `AppliedMigration` | May gain fields and methods in minor versions; existing names keep their meaning. Code that implements an emitter, a schema source or a migration executor may need small updates between minor versions. |
| Internal | Everything under an `internal/` directory, the implementation behind the root facade and the `sdk/` areas | No promise. |

### Dialects

`sdk/dialect` has no port to implement. A `dialect.Dialect` is an opaque handle to one of
the built-in engines, which a program configures through the engine provider's options or
`dialect_registry.Options`; dialects written outside Seagull are not supported yet. A
`Facts` value may gain fields in a minor version, as a new kind of fact reaches generated
code.

A dialect's options apply in the order they are given, after the defaults of a flavour
such as CockroachDB, and each sets one value, the last one given winning:
`dialect_engine_cockroachdb.New(dialect_engine_cockroachdb.WithSearchPath("app"))` keeps
CockroachDB's own settings and resolves unqualified names in the `app` schema. A new
option arrives in a minor version with a default that leaves existing output unchanged;
an option that changes generated code is opt-in, or its change is listed in the release
notes.

### Migration executors

`sdk/migrate` drives any `ExecutorPort`, so a third-party engine gets the migration
runner by implementing that port, or by describing its SQL to
`migrate.CustomBackend(spec)`, which splits statements with the engine's lexical
`migrate.Rules`. The contract, what each method must do under the lock and how a resumed
migration is checked, is in the [migrate API](migrate-api.md#executor-contract). The
optional ports `HistoryEditorPort` (behind `Force` and `Repair`) and `SeedLockTrierPort` (behind
non-blocking seed locks) are found by a type assertion; an executor without them gets a
clear error from the operations that need them, never a silent no-op.

### Emitter packages

The emitter interface, `emit.EmitterPort`, has one method so that it never has to change;
the optional `TypeDefaulterPort`, `ReserverPort`, `CheckerPort` and
`NullElementBinderPort` keep theirs. Both `sdk/emit` and `emit_kit` are Evolving rather
than Stable because they grow with code generation: a new query shape brings new names to
the naming plan (`emit.QueryNaming`), new places to the rendered SQL (`emit_kit.Marks`) and
new choices to the renderer (`emit_kit.Renderer`).
These arrive as new fields and functions, and an existing name keeps its meaning, so an
emitter that writes keyed struct literals and reads the fields it knows keeps compiling;
it may need a small change to take part in a feature it did not know about.

`emit_testkit` is Evolving too, and a minor version may add a check, as a new class of
bug is found and guarded against for every emitter. An emitter that fails a new check
records it in `Suite.KnownFailures`, with the reason, until it is fixed. The martian
dialect that `emit_testkit.Run` adds to every suite, an invented engine that shares no
convention with a real database, may likewise widen its grammar or change what it reports
in a minor version, as a new guard against a reference engine needs it.

## Diagnostic codes

Diagnostic codes (`Q001` and up) never change meaning and are never reused. A code that is
retired leaves a gap.

## Generated code

`seagull.CodegenVersion` changes whenever identical input would produce different Go. Tools
that check generated code into a repository can compare it to decide whether a regeneration
is due. The generated-file header names the tool: `// Code generated by sgl. DO NOT EDIT.`

## Persisted names

The tables, constraints and lock keys a migration backend uses (`Backend.Names`) are part
of the stable surface: a release that changes them would leave existing deployments with a
second history. They change only with a migration path and a release note. MySQL lock
keys are scoped to the current schema; earlier builds, and Piko's own runner, used
server-wide keys, which `migrate_backend_mysql.WithServerWideLockKey` keeps so that old and new
processes take the same lock during a rolling upgrade.

The migration history table's original eight columns never change. Columns added since
(`splitter_version` and `statements_hash`, which let an interrupted migration resume
safely) are added by the first `Up` that holds the lock, to tables created by earlier
releases as well as new ones; earlier releases ignore them.

## Configuration

`sgl.yaml` carries a `version` field, which is `1` today. A new schema version is
introduced only alongside a migration path, and `sgl config validate`, like every command
that reads the file, rejects a version the `sgl` running it does not read, naming the
version it does.

## History

Seagull began as the SQL code generator inside the [Piko](https://github.com/piko-sh/piko)
web framework, where its directives used the `piko.` prefix and its tables the `piko_`
prefix. Piko now consumes Seagull through a bridge that keeps its own vocabulary, so
existing Piko projects see no change.
