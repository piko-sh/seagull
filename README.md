<div align="center">

# Seagull

**Plain SQL. Type-safe Go.**

Seagull checks queries against your migrations and generates the Go to call them. Pure Go. No database needed to generate.

<img src="docs/images/seagull-chip.svg" alt="Seagull" width="320"/>

[![Go Version](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-Alpha-orange.svg)]()

[![Library Coverage](https://img.shields.io/badge/Library_Coverage-92%25-brightgreen?logo=go)](hack/test/coverage.sh)
[![CLI Coverage](https://img.shields.io/badge/CLI_Coverage-95%25-brightgreen?logo=go)](hack/test/coverage.sh)

[Getting Started](#getting-started) |
[Documentation](docs/) |
[Examples](examples/) |
[Contributing](CONTRIBUTING.md)

</div>

---

> **Alpha:** Seagull is under active development. Expect breaking changes between releases.

> **Standalone:** Seagull began as the SQL code generator inside the [Piko](https://github.com/piko-sh/piko) web
> framework. It is now its own project, and Piko uses it through a bridge.

---

## About

Keep your schema and queries in ordinary `.sql` files. Seagull turns them into typed Go methods, so your application
calls `GetUser` instead of assembling SQL strings and scanning rows by hand.

> Screams at your queries so production doesn't have to. Will steal your chips.

A one-line comment names each query and says what it returns:

```sql
-- sgl.query(GetUser, one)
SELECT id, email, display_name FROM users WHERE id = $1;
```

`sgl generate` writes the Go method, its parameters and its result type:

```go
func (queries *Queries) GetUser(ctx context.Context, id int64) (GetUserRow, error)
```

Parameter types and nullable fields come from your schema. The SQL stays runnable in your database client.

### Why Seagull

Your Go application talks directly to the database through its driver. Seagull generates the code to bind parameters
and read results, giving you a typed API without a separate service between your application and the database.

You write the SQL, so you choose the joins, filters, selected columns and grouping. An ORM can make complex joins
awkward to express or hide the queries it runs. With Seagull, the query stays visible: you can run it in your database
client, inspect its query plan and tune it yourself.

[sqlc](https://sqlc.dev/) is a good tool that takes the same approach, and I recommend trying it. I kept finding issues
with how it typed complex queries, which I felt were largely due to its core's focus on PostgreSQL. Those issues may
have been resolved since, and I still recommend the project.

I also wanted to embed the generator in Piko without cgo. When I started development on Seagull (Piko Querier), this was a
dependency that sqlc had. Avoiding cgo as a mandatory dependency, was a hard-requirement for Piko.

---

## How it works

Seagull reads your migrations to build a schema, then checks every query against it. Each dialect has its own parser
and type system, written in Go. Types and nullability flow through joins, CTEs, subqueries, expressions and functions.

A misspelt column, an unknown table or a malformed directive is reported with its file, line and column. Run
`sgl vet --strict` to check SQL in CI, and `sgl generate --check` to catch stale generated code.

The generated methods call your database driver directly and include the helpers they need. Executing them does not
link the generator. You can also [embed the generator](docs/reference/generator-api.md) in your own Go tools.

Seagull also runs migrations and seeds from the CLI. You can
[apply migrations at startup](docs/how-to/startup-migrations.md) in your own binary, with checksums and database locks
where supported.

### Dialects

All eight dialects support code generation and migrations:

| SQL dialects | Go drivers |
|---|---|
| PostgreSQL, CockroachDB, TimescaleDB | `database/sql`, `pgx` |
| MySQL, MariaDB, SQLite, DuckDB | `database/sql` |
| ClickHouse | `database/sql`, native |

The stock `sgl` generates code for every dialect. DuckDB's driver needs cgo, so its `sgl migrate` and `sgl seed`
commands need a [custom binary](docs/reference/cli.md#custom-binaries).

---

## Getting started

### Prerequisites

- Go 1.27

### Installation

```bash
# Add sgl as a tool of an existing Go module
go get -tool seagull.sh/seagull/cmd/sgl@latest

# Start a project and generate its Go package
go tool sgl init --dialect postgres
go tool sgl generate
```

`sgl init` writes a configuration, a first migration, example queries and seed data. `sgl generate` writes the Go
package to `internal/store`. `go install seagull.sh/seagull/cmd/seagull@latest` installs the same tool as `seagull`.

The [first-query tutorial](docs/tutorials/01-first-query.md) walks through a complete SQLite application.
See [installation](docs/get-started/install.md) for a standalone executable or a build from source.

---

## Usage

### Embedding

The generator is a Go library too, so a build tool can write the package itself, without `sgl`:

```go
package main

import (
	"context"
	"log"
	"os"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

func main() {
	generator, err := seagull.New(dialect_engine_postgres.New(), seagull.WithEmitter(emit_driver_pgx.New()))
	if err != nil {
		log.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), seagull.Input{
		Migrations:   os.DirFS("."),
		Package:      "store",
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := result.Err(); err != nil {
		log.Fatal(err)
	}
	if err := seagull.WriteFiles("internal/store", result); err != nil {
		log.Fatal(err)
	}
}
```

An application applies its migrations at start-up with the runner in `sdk/migrate`, which links none of the generator:

```go
migrator, err := migrate.NewMigrator(db, migrate_backend_postgres.New(), migrations, "db/migrations")
if err != nil {
	return err
}
_, err = migrator.Up(ctx)
```

The implementation lives under `internal/`. Everything outside the module reaches it through the root package,
`seagull.sh/seagull`, and the kit under `sdk/`, which has one area per extension point: `sdk/dialect`, `sdk/emit`,
`sdk/migrate`, `sdk/schema` and `sdk/observe`. Each area holds its contracts and the providers that implement them,
named `<area>_<role>_<impl>` (`dialect_engine_postgres`, `emit_driver_pgx`, `migrate_backend_postgres`), so a program
links only the engines and backends it imports. See [the generator API](docs/reference/generator-api.md) and
[migrations at application start-up](docs/how-to/startup-migrations.md).

### Examples

The [examples](examples/) cover first queries, joins, transactions, migrations and embedding, with generated Go
checked in. Browse the walkthroughs at [seagull.sh/examples](https://seagull.sh/examples), or run one locally:

```bash
cd examples/sqlite
go run ./001_first_query
```

Run that command from a [prepared source checkout](docs/get-started/install.md#work-from-a-checkout). SQLite needs no
separate database server. The [examples README](examples/README.md) explains the other engines.

---

## Documentation

- [Getting started](docs/tutorials/01-first-query.md) - build a first SQLite application.
- [CLI reference](docs/reference/cli.md) - commands and flags.
- [Configuration](docs/reference/configuration.md) - dialects, drivers and output settings.
- [SQL directives](docs/reference/directives.md) - query names, result shapes and type overrides.
- [Type mapping](docs/reference/type-mapping.md) - SQL types and their Go equivalents.
- [Migrations](docs/reference/migration-runtime.md) - apply migrations and seeds from Go.
- [Embedding Seagull](docs/reference/generator-api.md) - use the generator as a library.
- [Known limitations](docs/reference/limitations.md) - where analysis is less precise than the engine, with workarounds.
- [Full documentation](docs/index.md) - tutorials, guides and reference.

---

## Contributing

Contributions are welcome. Read the [Contributing Guide](CONTRIBUTING.md) for details on the Seagull development process,
how to submit pull requests, and coding standards.

---

## License

Distributed under the Apache 2.0 License. See [LICENSE](LICENSE) for more information.
