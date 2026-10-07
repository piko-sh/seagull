---
title: Run migrations at application start-up
description: "Embed migration files in the application, construct the migrator with the backend for your database, and apply Up before making the application ready."
section: How-to guides
subsection: Migrations
order: 570
---

# Run migrations at application start-up

Embed the migration files the application was built against, then apply them before accepting traffic that requires their schema. Pass the startup context through to migration execution and return any failure to the startup owner.

## What you import

The migration runtime is the migrate area of the `sdk` kit, with one backend provider for each database:

| Package | Import path | What it gives you |
|---|---|---|
| `migrate` | `seagull.sh/seagull/sdk/migrate` | `NewMigrator`, the `Migrator` with `Up`, `Down` and `Status`, the options and hooks, and the typed errors such as `ChecksumMismatchError` |
| `migrate_backend_*` | `seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite`, or the one for your database | `New`, which returns the `migrate.Backend` for `NewMigrator` |

| Database | Backend provider | Constructor |
|---|---|---|
| PostgreSQL | `migrate_backend_postgres` | `New()`, or `NewPgBouncer()` behind a transaction-mode pooler |
| CockroachDB | `migrate_backend_cockroachdb` | `New()` |
| TimescaleDB | `migrate_backend_timescaledb` | `New()` |
| MySQL and MariaDB | `migrate_backend_mysql` | `New()` |
| SQLite | `migrate_backend_sqlite` | `New()` |
| DuckDB | `migrate_backend_duckdb` | `New()` |
| ClickHouse | `migrate_backend_clickhouse` | `New()` |

Each `New` takes the options every backend accepts, such as `migrate.WithNamespace`, `migrate.WithHistoryTable` and `migrate.WithLock`. The [migrate API](../reference/migrate-api.md#backend-providers) describes each backend. A database none of these covers takes `migrate.CustomBackend`; see [implementing a migration backend](migration-backend.md).

## Embed the migration directory

In a package beside the migration directory, expose an embedded filesystem:

```go
//go:embed migrations/*.sql
var Migrations embed.FS
```

Import `embed` in that package. The directory passed to the migrator must match the path inside this filesystem. Generation and migration remain separate operations: `sgl generate` does not change the running database.

## Apply the embedded files before readiness

Open the application's database, choose its backend and construct the migrator. The SQLite example receives a filesystem whose directory is `db/migrations`:

```go
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
```

Apply the files before running queries that need the new schema:

```go
	applied, err := migrator.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	fmt.Fprintf(output, "start-up: applied %d migrations\n", applied)
```

`Up` returns the number applied during this call. An up-to-date database returns zero, but applied files are still checksum-checked. A binary embedding an edited applied migration fails with `migrate.ChecksumMismatchError`; do not mark the application ready after that failure.

## Coordinate concurrent starts

Use the same history names and lock identity in every deployment. With advisory or table locks, history creation happens after acquisition, so concurrent first boots are coordinated. `WithLockTimeout` bounds acquisition; the startup context bounds the complete call.

`LockNone` adds no cross-process lock. SQLite serialises writers, DuckDB limits writing processes, and ClickHouse needs external coordination; choose a startup policy suited to the backend rather than assuming every replica can migrate concurrently. See [configure locks](configure-locks.md).

After success, run a representative application query before accepting traffic. Keep database ownership and closure with the application. The [runtime reference](../reference/migration-runtime.md) describes transaction, session and recovery behaviour.

The [working example](../../examples/sqlite/027_migrations_at_startup) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/027_migrations_at_startup
go test .
```
