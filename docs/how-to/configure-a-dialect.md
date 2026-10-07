---
title: Configure a built-in dialect
description: "Configure a built-in engine with WithName, WithSearchPath and its other options, build it by name with dialect_registry, and register it under a name of your own in a custom sgl binary."
section: How-to guides
subsection: Extensions
order: 940
---

# Configure a built-in dialect

Configure a built-in engine when your database speaks one of the built-in dialects' SQL
but needs a name of its own, another search path or tighter parser limits. Each engine's
options set those values, `dialect_registry` sets them from plain data, and a custom
`sgl` binary registers the configured engine under a name `sgl.yaml` can select. The
dialects are the built-in engines: dialects written outside Seagull are not supported
yet.

## What you import

| Package | Import path | What it gives you |
|---|---|---|
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_postgres` and the others | `New` and the engine's options |
| `dialect` | `seagull.sh/seagull/sdk/dialect` | `Dialect`, which `New` returns, and `Facts`, which its `Info` returns |
| `dialect_registry` | `seagull.sh/seagull/sdk/dialect/dialect_registry` | `New` and `Options`, which build a built-in engine by name from plain data, as `sgl.yaml` does |
| `cli` | `seagull.sh/seagull/cmd/sgl/cli` | `WithDialect`, which registers the configured engine in a custom binary |
| `seagull` | `seagull.sh/seagull` | `seagull.New`, which takes the dialect |

## Configure the engine

Every engine takes `WithName`, `WithMaxParseDepth` and `WithMaxTokensPerStatement`. The
rest are the engine's own:

| Engines | Options of their own |
|---|---|
| `dialect_engine_postgres`, `dialect_engine_cockroachdb`, `dialect_engine_timescaledb` | `WithSearchPath`, the schemas unqualified names resolve in, `public` alone by default |
| `dialect_engine_mysql`, `dialect_engine_mariadb` | `WithANSIQuotes`, for a server whose `sql_mode` holds `ANSI_QUOTES`, and `WithReturning`, the statements that accept a `RETURNING` clause |
| `dialect_engine_clickhouse` | `WithJoinUseNulls` and `WithGroupByUseNulls`, the server settings analysis assumes, and `WithMaxTypeParseDepth`, how deep a type name may nest |
| `dialect_engine_sqlite`, `dialect_engine_duckdb` | None beyond the common three |

This constructor is for HarbourDB, a database whose server speaks PostgreSQL's SQL. It
gives the dialect HarbourDB's name, its schemas and a lower parse depth, ahead of the
caller's options:

```go
package harbourdb

import (
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
)

// New returns the PostgreSQL dialect configured for HarbourDB. The caller's options apply
// after HarbourDB's own.
func New(options ...dialect_engine_postgres.Option) dialect.Dialect {
	return dialect_engine_postgres.New(append([]dialect_engine_postgres.Option{
		dialect_engine_postgres.WithName("harbourdb"),
		dialect_engine_postgres.WithSearchPath("harbour", "public"),
		dialect_engine_postgres.WithMaxParseDepth(128),
	}, options...)...)
}
```

Options apply in the order they are given, and each sets one value, the last one given
winning, so a caller's `WithSearchPath` replaces HarbourDB's. `WithName` keeps the
family: `harbourdb.New().Info()` reports `harbourdb` as its `Name` and `postgres` as its
`Family`, so diagnostics and configuration name your dialect while type mappings, drivers
and migration backends keep resolving through the family. The parser limits ignore a
value below 1 and hold one above its ceiling, `dialect_registry.MaxParseDepthCeiling`
(4,096) or `dialect_registry.MaxTokensPerStatementCeiling` (1,000,000); a statement past a
limit is refused with an error matching `seagull.ErrLimit`.

Pass the dialect to the generator as any other:

```go
generator, err := seagull.New(harbourdb.New())
```

## Build it by name

`dialect_registry.New` builds the same engines from a name and a plain-data `Options`,
whose fields mirror the options: `Name`, `SearchPath`, `ANSIQuotes`, `Returning`,
`JoinUseNulls`, `GroupByUseNulls`, `MaxParseDepth`, `MaxTokensPerStatement` and
`MaxTypeParseDepth`. It is what `sgl` calls with a database's `dialect` and
[`dialect_options`](../reference/configuration.md#dialect-options-dialect_options):

```go
engine, err := dialect_registry.New("postgres", dialect_registry.Options{
	Name:       "harbourdb",
	SearchPath: []string{"harbour", "public"},
})
if err != nil {
	return err
}
```

The registry refuses what an engine's options would quietly adjust: a negative limit, a
limit above its ceiling, a blank `Name` or the built-in name of another family, and an
option the named dialect does not take are each a `*dialect_registry.OptionError`
wrapping `dialect_registry.ErrInvalidOption`, which `dialect_registry.OptionErrors` lists. A name `dialect_registry.Names` does not list is
`dialect_registry.ErrUnknownDialect`.

## Register it in a custom CLI

`sgl.yaml` names a dialect by the key it is registered under. Register HarbourDB's in a
[custom binary](custom-cli.md) with `cli.WithDialect`; its factory receives the
database's `dialect_options`, so the binary's defaults apply only where the file sets
nothing:

```go
package main

import (
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func main() {
	os.Exit(cli.Main(os.Args[1:],
		cli.WithDriver("postgres", "pgx"),
		cli.WithDialect("harbourdb", func(options dialect_registry.Options) (dialect.Dialect, error) {
			options.Name = "harbourdb"
			if len(options.SearchPath) == 0 {
				options.SearchPath = []string{"harbour", "public"}
			}
			return dialect_registry.New("postgres", options)
		}),
	))
}
```

A database with `dialect: harbourdb` then generates with it. `cli.WithDriver` registers pgx
for the PostgreSQL family, and the dialect's family selects that driver and PostgreSQL's
migration backend.
When HarbourDB's server needs migration SQL of its own, register a backend under the
dialect's name with `cli.WithMigrationBackend`, as
[migration backends](migration-backend.md) shows. The
[dialect reference](../reference/dialects/index.md) describes what each engine reads, and
the [dialect API](../reference/dialect-api.md) lists the handle and its facts.
