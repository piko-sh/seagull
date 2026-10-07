---
title: Build a custom sgl binary
description: Call cli.Main or MainContext from a small main package and register the
  additional dialect, emitter or database/sql driver. Import each runtime driver the
  binary needs.
section: How-to guides
subsection: Extensions
order: 830
---

# Build a custom sgl binary

Build a small command around `cli.Main` when you need registrations the stock binary does not provide. The CLI supplies command parsing and configuration handling; your binary supplies any additional dialects, emitters, migration backends and runtime drivers.

## What you import

The `cli` package is the importable shell of `sgl`. What a binary registers with it comes from the areas of the `sdk` kit and their provider packages:

| Package | Import path | What it gives a custom binary |
|---|---|---|
| `cli` | `seagull.sh/seagull/cmd/sgl/cli` | `Main` and `MainContext`; the registrations `WithDialect`, `WithEmitter`, `WithMigrationBackend` and `WithDriver`; `WithName`, the program name the binary prints in its help, errors and `version`; `WithDirectory` and `WithIO` for tests |
| `dialect` and `dialect_registry` | `seagull.sh/seagull/sdk/dialect`, `seagull.sh/seagull/sdk/dialect/dialect_registry` | `Dialect`, which a dialect factory returns, and `dialect_registry.New` and `Options`, which build a built-in dialect from `dialect_options` |
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_postgres` and the others | A built-in dialect to configure, as [configure a built-in dialect](configure-a-dialect.md) shows |
| `emit` and `emit_driver_*` | `seagull.sh/seagull/sdk/emit`, `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` and the others | `EmitterPort`, which an emitter factory returns, and the built-in emitters to return or wrap |
| `migrate` and `migrate_backend_*` | `seagull.sh/seagull/sdk/migrate`, `seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb` and the others | `Backend` and `BackendOption`, which a migration backend factory returns and takes, and the built-in backends |
| A database/sql driver | such as `modernc.org/sqlite` | The connection the migrate, seed and schema commands open |

## Create the entry point

This command registers the SQLite runtime driver and replaces SQLite's default parser-depth setting. Save it as `cmd/custom-sgl/main.go` in your module:

```go
package main

import (
    "os"

    _ "modernc.org/sqlite"

    "seagull.sh/seagull/cmd/sgl/cli"
    "seagull.sh/seagull/sdk/dialect"
    "seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func main() {
    os.Exit(cli.Main(os.Args[1:],
        cli.WithDriver("sqlite", "sqlite"),
        cli.WithDialect("sqlite", func(options dialect_registry.Options) (dialect.Dialect, error) {
            if options.MaxParseDepth == 0 {
                options.MaxParseDepth = 128
            }
            return dialect_registry.New("sqlite", options)
        }),
    ))
}
```

A registration with a known name replaces the stock one. Here explicit `dialect_options.max_parse_depth` still wins over the binary's default. `cli.Main` handles interrupts; use `MainContext` when a host already owns cancellation.

A registration with an empty name, a nil factory or an empty driver name makes every command exit 2, naming the option, before anything runs; a nil option is skipped. A panic in a registered dialect, emitter or driver does not crash the binary: the command exits 3 with an error naming the panic, or reports the statement it was handling as [Q070](../reference/diagnostics.md#q070). The stack is logged once at warning level; for code generation, run with `--log-level warn` to see it.

## Build and run it

```sh
go get seagull.sh/seagull/cmd/sgl/cli modernc.org/sqlite
go build -o custom-sgl ./cmd/custom-sgl
./custom-sgl config validate
./custom-sgl vet --strict
./custom-sgl generate
```

The `cli` package links no database drivers. Import each driver your connecting commands need, and register the name that `database/sql` opens with `WithDriver`. To keep the stock binary's drivers, import the four the [CLI reference](../reference/cli.md#connection-strings) lists and register each for its family, as `cmd/seagull` does. Generating code alone does not need that connection.

`cli.WithName("custom-sgl")` makes the binary print its own name in its help, usage, error lines, hints and `version` line, where it would otherwise print `sgl`.

## Register other extensions

Use `WithEmitter(name, factory)` for a new `go.driver` value. The factory receives the selected dialect and emitter settings, including `Prepared`, and returns an `emit.EmitterPort`, such as a [wrapper](custom-emitter-example.md) around `emit_driver_databasesql.New`. Register a migration backend with `WithMigrationBackend` when a dialect needs migration SQL other than its stock backend's, such as a dialect renamed for a database of its own, keyed by the dialect's name or its family. The factory has the shape of a provider's `New`, so `migrate_backend_cockroachdb.New` is one, and so is a function that returns `migrate.CustomBackend` of a [spec of your own](migration-backend.md). Adding a runtime driver does not add introspection support.

For in-process tests, call `MainContext` with `WithDirectory` and `WithIO`, then assert its status and captured output. The [complete custom CLI example](../../examples/embedding/053_custom_sgl_binary/main.go) adds an emitter, and its [test](../../examples/embedding/053_custom_sgl_binary/main_test.go) exercises these boundaries.

See the [CLI reference](../reference/cli.md#custom-binaries) for registration contracts.
