---
title: Configure migration locks and namespaces
description: "Configure migration history, namespaces and locking for your database."
section: How-to guides
subsection: Migrations
order: 870
---

# Configure migration locks and namespaces

Choose names and a lock mode before several deployments share a migration history. Every runner for that history must use the same persisted names and lock identity.

## Set the history names

Build the backend with the namespace the application owns. Each engine's backend comes from its provider package in the migrate area of the `sdk` kit, such as `seagull.sh/seagull/sdk/migrate/migrate_backend_postgres`, and the options every backend accepts are in `seagull.sh/seagull/sdk/migrate` itself. Use `WithHistoryTable` only when the migration history needs a separate name:

```go
backend := migrate_backend_postgres.New(
    migrate.WithNamespace("app"),
    migrate.WithHistoryTable("app_history"),
    migrate.WithLock(migrate.LockTable),
)
fmt.Println(backend.Names())
```

`WithNamespace` names the migration, seed and lock tables. `WithHistoryTable` changes only the migration history and its lock key; the seed names remain namespace-derived. Option order does not change the result. Give CLI configuration the same `migrate.namespace` and `migrate.history_table` values.

The history starts in the connection's schema or database. To place it elsewhere, configure the connection's PostgreSQL search path or MySQL database accordingly; see [sessions and the search path](../reference/migration-runtime.md#sessions-and-the-search-path).

## Select coordination for the deployment

Use advisory locks for ordinary PostgreSQL or MySQL deployments. Use `migrate_backend_postgres.NewPgBouncer()` behind a transaction-mode pooler, where the runner uses a table lock. A table lock needs at least two pool connections: one holds the lock transaction and another runs migrations.

The lock transaction stays open and idle for the whole run. The PostgreSQL and CockroachDB table locks turn off `idle_in_transaction_session_timeout` for that transaction, and before each migration or seed the runner checks with `SELECT 1` that the transaction is still open. If the server or a pooler has ended it, the run stops before that migration or seed with `migrate.ErrLockLost` rather than carrying on while another process may hold the lock. Behind PgBouncer, set PgBouncer's own `idle_transaction_timeout` to 0 (its default) or above your longest run, since a session cannot change it. On PostgreSQL 17 or later, do the same with `transaction_timeout` where the server, database or role sets it. The [runtime reference](../reference/migration-runtime.md#locking) describes the check and its limits.

Advisory and table locks exclude other runners using the same lock identity. `LockNone` adds no cross-process lock, so coordinate concurrent runners externally. Calls through one shared executor still admit one lock holder at a time.

SQLite, ClickHouse and DuckDB support only `LockNone`. SQLite serialises database writers; ClickHouse requires external migration coordination. DuckDB permits one writing process per file, but separate executors in that process can conflict. Use one migrator for a database's runs. These engine constraints are not a Seagull cross-process lock.

`migrate_backend_cockroachdb.New()` uses a table lock and needs CockroachDB v23.2 or later for durable locking. On older servers the lock fails with `migrate.ErrUnsupportedLock`; use `LockNone` only with external coordination. The [backend reference](../reference/migration-runtime.md#cockroachdb) describes its schema-change and transaction limits.

## Bound how long a run waits

Pass `migrate.WithLockTimeout(30*time.Second)` to the migrator or seeder, or use `--lock-timeout 30s` with the CLI. The timeout bounds acquisition only; it does not limit SQL execution after the lock is acquired. If a holder prevents acquisition until that deadline, the call returns `migrate.ErrLockTimeout` without applying migrations. Every acquisition failure comes as a `*migrate.LockAcquisitionError`, whose `Lock` field names the `migration` or `seed` lock, so match it with `errors.As` rather than by its message.

Use `migrate.WithNonBlockingLock()` or `--non-blocking` when the job should fail immediately with `migrate.ErrLockNotAcquired` and retry later. Neither option creates database coordination under `LockNone`.

## Keep old and new deployments on the same lock

Do not change a namespace, history name or lock scheme while another runner may still use the old one. Separate identities can let both runners believe they are alone.

MySQL lock keys are scoped to the current database. When releases using the earlier server-wide key can still run, use `migrate_backend_mysql.WithServerWideLockKey()` until they have retired so both generations take the same lock. This option affects no other engine.

An invalid identifier or unsupported lock mode is reported by `NewMigrator`, `NewSeeder` or the executor constructor, even though the backend constructor itself returns a value. See [persisted names and locks](../reference/migration-runtime.md#persisted-names-and-locks) for supported modes and exact naming rules.
