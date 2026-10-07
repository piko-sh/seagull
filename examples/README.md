# Examples

The examples are a curriculum: 53 concepts, from a first query to a custom `sgl` binary,
each shown on every engine where it applies. Every variant is a small program you can
run, with its generated code checked in so you can read it without running anything.
[seagull.sh/examples](https://seagull.sh/examples) renders the same files as walkthroughs,
with each engine's version side by side.

## Layout

```
examples/
  concepts.yaml     the curriculum: every concept, and what each engine does with it
  catalogue/        the curriculum's loader, validator, export and twin check
  sqlite/           one Go module per engine, one directory per variant
  postgres/
  cockroachdb/
  timescaledb/
  mysql/
  mariadb/
  duckdb/
  clickhouse/
  tooling/          sgl on the command line, recorded as terminal transcripts
  embedding/        Seagull as a library, and custom sgl binaries
```

A variant lives in `<module>/<NNN>_<slug>[_suffix]/`, where `NNN` is the concept's number
in `concepts.yaml`, the slug is the concept's with underscores for hyphens, and the suffix
names a driver other than `database/sql`: `_pgx` for pgx, `_native` for ClickHouse's
native driver. So `postgres/001_first_query` and `postgres/001_first_query_pgx` are the
first concept on PostgreSQL through each driver.

## What a variant contains

| File | What it is |
|---|---|
| `example.yaml` | The variant's manifest: a summary, how this engine differs, the query the walkthrough starts from, whether the playground runs it, and how it is tested |
| `sgl.yaml` | The `sgl` configuration, with the engine's dialect and the column's driver |
| `db/migrations/`, `db/queries/` | The SQL; some variants also have `db/seeds/` |
| `internal/store/` | The code `sgl generate` writes, checked in |
| `example.go` | The example itself: a `run` function that takes an open database and writes what it does |
| `main.go` | The wiring: open and migrate a database, then call `run` |
| `main_test.go` | Runs `run` against a fresh database and compares what it wrote with `output.txt` |
| `output.txt` | The expected output |

`example.yaml`'s `test:` says how a variant runs:

| `test:` | Engines | How |
|---|---|---|
| `in-process` | SQLite, embedding | The pure-Go SQLite driver, on a temporary file |
| `cgo` | DuckDB | The cgo DuckDB driver, in memory; needs a C compiler |
| `integration` | PostgreSQL, CockroachDB, TimescaleDB, MySQL, MariaDB, ClickHouse | A real server, named by `SGL_EXAMPLE_DSN`; without one the test skips |
| `transcript` | tooling | `transcript.txt` is a terminal session, and `transcript_test.go` replays it with `sgl` in-process on a temporary copy of the directory |
| `generate-only` | embedding/053 | The test runs the custom `sgl` itself |

A few embedding variants generate their code from Go rather than with `sgl`: 046 with
`go run .` (and `go run . -check`), 052 with `go run . -write`, and 053 with its own
`go run . generate`. 047 and 049 to 051 commit no generated code; they generate in memory
or into a temporary directory when they run.

## Running a variant

Use a [Seagull checkout](../docs/get-started/install.md#work-from-a-checkout). From the
repository root, run a SQLite example without a separate database server:

```sh
cd examples/sqlite
go run ./001_first_query
go test ./001_first_query
```

`go run ./001_first_query -db tasks.db` keeps the database. DuckDB runs the same way from
`examples/duckdb`, given a C compiler. Run the embedding and tooling variants from their
own directory, since they read `sgl.yaml` and the SQL from where they run:

```sh
(cd examples/embedding/046_library_generate && go run .)
(cd examples/embedding/053_custom_sgl_binary && go run . generate --check)
(cd examples/tooling/041_vet_and_diagnostics && go tool sgl vet)
```

053 is an `sgl` binary, so plain `go run .` only prints its usage. The tooling module pins
`sgl` with a `tool` directive, so `go tool sgl` runs the one this checkout builds.

The server engines' programs take `-dsn`, and all but ClickHouse's also read
`DATABASE_URL`. Give each one an empty database of its own, as the migrations create its
tables and the example adds rows:

```sh
cd examples/postgres
go run ./001_first_query -dsn 'postgres://postgres:secret@localhost:5432/example?sslmode=disable'
```

The ClickHouse programs default to `clickhouse://localhost:9000/default`, with
`?mutations_sync=2` where the example runs a mutation.

## Testing against a server

The server engines' tests create a database with a unique name for each variant, migrate
it, run the program, compare its output, and drop the database. Three environment
variables point them at a server:

| Variable | Meaning |
|---|---|
| `SGL_EXAMPLE_DSN` | A server connection string for a user that may create databases, such as `postgres://postgres:secret@localhost:5432/postgres?sslmode=disable`, `postgresql://root@localhost:26257/defaultdb?sslmode=disable`, `root:secret@tcp(localhost:3306)/` or `clickhouse://default:secret@localhost:9000/default` |
| `SGL_EXAMPLE_ENGINE` | The engine behind the DSN, which picks the migration backend: `postgres` (the default), `cockroachdb` or `timescaledb` for `examples/postgres`, and `mysql` (the default) or `mariadb` for `examples/mysql` |
| `SGL_EXAMPLE_REQUIRE_DSN` | `1` makes a test fail, rather than skip, when `SGL_EXAMPLE_DSN` is empty |

```sh
cd examples/postgres
SGL_EXAMPLE_DSN='postgres://postgres:secret@localhost:5432/postgres?sslmode=disable' go test ./...
```

`go test ./... -update` rewrites `output.txt`, or a tooling variant's `transcript.txt`,
from what the program prints now; read the diff before keeping it.

From the repository root:

| Target | What it does |
|---|---|
| `make test-examples` | Vets, builds and tests every examples module. SQLite, DuckDB, embedding and tooling run in full; the server variants skip |
| `make test-integration ARGS=postgres` | Runs `examples/postgres` against the suite's embedded PostgreSQL. The container suites (`cockroachdb`, `timescaledb`, `mysql`, `mariadb`, `clickhouse`) run their engine's module against their server, and the CockroachDB, TimescaleDB and MariaDB suites also run the PostgreSQL or MySQL variants their equivalent cells reuse |
| `make generate-examples` | Regenerates every variant's checked-in code |
| `make generate-examples-validate` | Checks every variant's generated code and directive formatting, validates the curriculum, and runs the twin check |

## The curriculum

`concepts.yaml` is the one place concept numbers, titles and tracks live. It lists the
engines, the columns (an engine and a driver), the tracks the website groups concepts
under, and the concepts in learning order. Each concept has a cell for every column, and
a cell's status says what that engine does with it:

| Status | Meaning |
|---|---|
| `variant` | The column has its own directory |
| `equivalent` | No directory: the generated code is identical to the `twin` column's variant, and that variant's test runs against this engine too |
| `unsupported` | The engine cannot do it; `reason` says why, `diagnostic` names the code Seagull reports, and `see` points to a related concept |
| `omitted` | It works, but a variant would teach nothing new |
| `blocked` | A variant is waiting on a change to Seagull; `reason` says which |

`catalogue` is the curriculum's own module. `concepts.yaml` and every `example.yaml` are
read strictly: a key the catalogue does not know, such as a misspelt
`expected_diagnostic`, or a file holding other than one YAML document fails to load.
`catalogue.Validate`, which `TestCurriculumIsValid` runs, also fails when `concepts.yaml`
and the directories disagree: a variant cell without a directory or a directory no cell
names, numbering that is not contiguous, an `sgl.yaml` whose dialect or driver does not
match its column, a test mode whose files are missing, an entry query that is not in its
file, or a hand-written Go file without the licence header. Run it with:

```sh
cd examples/catalogue && go test ./...
```

`go run ./cmd/twins -sgl <path to sgl>` checks every `equivalent` cell: it copies the
twin variant to a temporary directory, sets `sgl.yaml`'s dialect to the cell's engine,
and runs `sgl generate --check` there, so the claim holds only while that engine generates
the same code. `-diff` shows what differs. `go run ./cmd/export -out <dir>` writes the
manifest and files the website reads into a directory that must be empty or absent.

To add a variant, create its directory with the files above, set its cell to `variant`
in `concepts.yaml`, run `make generate-examples`, then `go test . -update` in the
variant's directory to record its output, and check it with
`make generate-examples-validate`.

Each module has its own `go.work` and relative `replace` directives, so it builds against
this checkout without the repository's workspace; in your own project, require a
released `seagull.sh/seagull` instead.
