---
title: Implement a migration backend or executor
description: "Describe an engine's migration SQL for migrate.CustomBackend, or implement migrate.ExecutorPort for another store, and verify its contracts."
section: How-to guides
subsection: Extensions
order: 950
---

# Implement a migration backend or executor

Use `migrate.CustomBackend` with a `migrate.BackendSpec` when the driver uses database/sql but its engine has no built-in migration backend. Implement `migrate.ExecutorPort` instead when a native driver or another history store needs different database operations.

## What you import

A backend is written against the migrate area of the `sdk` kit, `seagull.sh/seagull/sdk/migrate`. The area never links the analyser, a dialect or the code generator, so a service that only migrates stays small, and it names the lexical rule types a backend needs:

| Package | Import path | What it gives a backend |
|---|---|---|
| `migrate` | `seagull.sh/seagull/sdk/migrate` | `BackendSpec`, `AdvisoryLockSQL`, `TableLockSQL` and `CustomBackend`; the backend options and `LockMode`; `NewMigrator` and `NewSeeder`; for a store of your own, `ExecutorPort`, `SeedExecutorPort`, `HistoryEditorPort`, `SeedLockTrierPort`, `NewMigratorFor` and `NewSeederFor`; the lexical `Rules` and the errors |
| `migrate_backend_*` | `seagull.sh/seagull/sdk/migrate/migrate_backend_postgres` and the others | The built-in backends, one provider package per engine |
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_postgres` and the others | The engine's lexical rules, `New().Info().Lexical`, when the database writes SQL text as a built-in dialect does |
| `seagull` | `seagull.sh/seagull` | `ErrInvalidConfig` and `ErrPanic`, which the errors for an unusable spec or a panic match |

Check the built-in providers first: `migrate_backend_postgres` (and its `NewPgBouncer`), `migrate_backend_timescaledb`, `migrate_backend_cockroachdb`, `migrate_backend_mysql` (for MariaDB too), `migrate_backend_sqlite`, `migrate_backend_clickhouse` and `migrate_backend_duckdb`. Each one's `New` returns a `migrate.Backend`. DuckDB's is itself a [`BackendSpec`](../../internal/migrate/sqlexec/dialect.go) built through the same path as yours.

## Describe the engine's SQL

Define a `BackendSpec` with its placeholder renderer, history and seed DDL, idempotent seed-history insert and table-existence query. These are required fields. This spec is for HarbourDB, the database [configure a built-in dialect](configure-a-dialect.md) gives a PostgreSQL dialect of its own, whose server runs PostgreSQL's SQL:

```go
package harbourmigrate

import (
	"strconv"

	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/migrate"
)

var harbourSpec = migrate.BackendSpec{
	Name:        "HarbourDB",
	Placeholder: func(index int) string { return "$" + strconv.Itoa(index) },
	CreateHistoryTable: func(table string) string {
		return "CREATE TABLE IF NOT EXISTS " + table + ` (
    version        BIGINT      NOT NULL PRIMARY KEY,
    name           TEXT        NOT NULL,
    checksum       TEXT        NOT NULL,
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms    BIGINT      NOT NULL,
    down_checksum  TEXT,
    last_statement INTEGER,
    dirty          BOOLEAN     NOT NULL DEFAULT FALSE
)`
	},
	CreateSeedTable: func(table string) string {
		return "CREATE TABLE IF NOT EXISTS " + table + ` (
    version     BIGINT      NOT NULL PRIMARY KEY,
    name        TEXT        NOT NULL,
    checksum    TEXT        NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms BIGINT      NOT NULL
)`
	},
	InsertSeed: func(table, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string) string {
		return "INSERT INTO " + table + " (version, name, checksum, duration_ms) VALUES (" +
			versionPlaceholder + ", " + namePlaceholder + ", " + checksumPlaceholder + ", " +
			durationPlaceholder + ") ON CONFLICT (version) DO NOTHING"
	},
	TableExists: func(tablePlaceholder string) string {
		return "SELECT count(*) FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = " + tablePlaceholder
	},
	TableLock: &migrate.TableLockSQL{
		Create: func(table, constraint string) string {
			return "CREATE TABLE IF NOT EXISTS " + table + " (lock_id INTEGER NOT NULL PRIMARY KEY DEFAULT 1, " +
				"CONSTRAINT " + constraint + " CHECK (lock_id = 1))"
		},
		InsertRow: func(table string) string {
			return "INSERT INTO " + table + " (lock_id) VALUES (1) ON CONFLICT DO NOTHING"
		},
		Lock:    func(table string) string { return "SELECT lock_id FROM " + table + " FOR UPDATE" },
		TryLock: func(table string) string { return "SELECT lock_id FROM " + table + " FOR UPDATE NOWAIT" },
	},
	DefaultLock:        migrate.LockTable,
	Rules:              dialect_engine_postgres.New().Info().Lexical,
	TransactionalDDL:   true,
	TransactionalSeeds: true,
}
```

Table names reach the renderers after the same safe-identifier check the built-in backends use, so a renderer can interpolate them directly; bind values arrive as the placeholders `Placeholder` renders. The history table needs the columns shown. Leave `AddHistoryColumn` nil when the engine accepts `ALTER TABLE ... ADD COLUMN` with `INTEGER` and `VARCHAR(64)`: the executor adds the `splitter_version` and `statements_hash` columns, which record resumable progress, that way to a table that lacks them.

Set lexical rules for the engine and its true transaction capabilities. `Rules` decides how files split into statements and which statements must run outside a transaction; a built-in engine's rules, such as `dialect_engine_postgres.New().Info().Lexical`, suit a database that writes SQL text as that dialect does, and a `migrate.Rules` value of your own suits any other. `TransactionalDDL` controls whether migrations and their history can commit together; `TransactionalSeeds` controls seeds independently. Set `AppendOnlyHistory` only when completed files must be recorded without resumable in-place progress updates.

Choose an advisory or table lock if the engine supports one. A spec with neither must use `LockNone`, which leaves cross-process coordination to the application. The [migrate API reference](../reference/migrate-api.md) lists renderer and lock signatures.

- An advisory lock's `Wait` query should wait a bounded time on the server for the lock, as MySQL's `GET_LOCK(name, 5)` does. When it returns without the lock, the runner tries again, starting each try at least half a second after the one before.
- A table lock's transaction must accept `SELECT 1`: the executor runs it there before each migration and seed, and stops the run with `migrate.ErrLockLost` when the transaction has ended.
- A table lock's `TryLock` must report a held lock as SQLSTATE `55P03`, through a `SQLState` method or as `SQLSTATE 55P03` in the error message, or with a message containing `could not obtain lock`.

## Construct and exercise the backend

Pass the completed spec to `migrate.CustomBackend`, then construct the migrator:

```go
backend := migrate.CustomBackend(harbourSpec, migrate.WithNamespace("app"))
migrator, err := migrate.NewMigrator(database, backend, files, "db/migrations")
if err != nil {
    return err
}
_, err = migrator.Up(ctx)
```

`CustomBackend` returns a backend value. The migrator, seeder or executor constructor reports an incomplete spec with an error wrapping `seagull.ErrInvalidConfig`; invalid names and lock modes report `migrate.ErrInvalidIdentifier` and `migrate.ErrUnsupportedLock`. Verify the SQL against the actual engine before using it on a shared database.

`CustomBackend` calls each renderer once, while it builds the backend, and the executors reuse the SQL it returned, so a renderer must not depend on when it is called. A renderer that panics makes the constructors return an error wrapping `seagull.ErrInvalidConfig` and `seagull.ErrPanic` rather than crashing the program. `Split` and the `Rules` callbacks run on each file: a panic in `Split` fails that call with an error matching `seagull.ErrPanic`, and one in a `Rules` callback with an error naming the callback and the panic value, before any of the file runs. Either way the stack is logged once at warning level and the error carries none.

To run the backend from `sgl migrate`, register it in a [custom CLI](custom-cli.md) with `cli.WithMigrationBackend`. Its factory has the shape of a provider's `New`, so a function that returns `migrate.CustomBackend(harbourSpec, options...)` is one, and the CLI passes it the namespace, history table and lock that `sgl.yaml` sets.

## Implement the executor when SQL configuration is insufficient

Implement `migrate.ExecutorPort` and pass it to `migrate.NewMigratorFor(executor, files, directory, options...)`. Implement `migrate.SeedExecutorPort` and pass it to `migrate.NewSeederFor` when seeds are needed.

Return an empty history when its table is absent, because `Status` and `Validate` must work without creating tables. `EnsureMigrationTable` is called during the acquired run, while statement execution must apply both the `no_transaction` directive and the engine's own restrictions.

For resumable non-transactional work, record the completed statement index, splitter version and digest. Verify that prefix before skipping statements on retry. Add `migrate.HistoryEditorPort` if the executor supports force and repair, and `migrate.SeedLockTrierPort` if it supports non-blocking seed locks. See [executor contracts](../reference/migration-runtime.md#other-databases-and-drivers), including the acquisition-context lifetime rule for transaction locks.

## Verify failure and compatibility paths

Test concurrent acquisition through separate executors, cancellation, interrupted SQL, checkpoint failures, checksum validation and seed history. For a table lock, also end its transaction from the server part-way through a run, and check that the run stops with `migrate.ErrLockLost` before the next migration. Check that acquisition errors run no migration SQL and that release does not leave a held database lock in a pooled connection.

Keep persisted names and lock keys stable across deployments, or provide a reviewed transition. The executor ports are Evolving; check the [compatibility reference](../reference/compatibility.md) when upgrading.
