---
title: Migration runtime
description: "Migration and seed execution contracts, options, state and error handling."
section: Reference
subsection: Operations
order: 100
---

# Migration runtime

Seagull's migration runner applies SQL files in version order, records a checksum for each
one, and refuses to run when an applied file has changed on disk. The same migrations feed
code generation: the schema Seagull checks your queries against is the one your migrations
build.

## Migration input

Up and down files use `{version}_{name}.{up|down}.sql` names in one directory. Versions
are numeric and applied in order; down files are optional. The [file reference](migration-files.md)
defines naming, lexical boundaries and migration directives.

## CLI operations

| Operation | Behaviour |
|---|---|
| `sgl migrate up` | Apply pending versions and validate recorded applied files. |
| `sgl migrate down N` | Roll back the newest N applied versions. |
| `sgl migrate to V` | Apply or roll back towards numeric version V. |
| `sgl migrate status` | Read file/history status without running DDL. |
| `sgl migrate validate` | Compare recorded applied files with current files. |
| `sgl migrate force V`, `repair [V]` | Edit recorded history without executing migration SQL; see recovery below. |

The [CLI reference](cli.md#migrate-and-seed) lists flags and exit statuses. Follow
[CLI migration](../how-to/migrate-cli.md) for the deployment procedure.

## Go operations and backends

`migrate.NewMigrator(database, backend, files, directory, options...)` returns a
`*migrate.Migrator`. `Up`, `UpTo`, `Down` and `DownTo` return the number of versions
completed and an error (`Down` refuses a negative count with an error matching
`seagull.ErrInvalidConfig`);
`Status` returns per-file status, and `Validate` checks applied history. The
[startup guide](../how-to/startup-migrations.md) shows embedding and readiness.

`sdk/migrate` and its `migrate_backend_*` providers do not link the analyser or generator.

| Backend | Use |
|---|---|
| `migrate_backend_postgres.New()` | PostgreSQL, with advisory locks. |
| `migrate_backend_timescaledb.New()` | TimescaleDB: PostgreSQL's backend, which also runs continuous-aggregate statements outside a transaction. |
| `migrate_backend_cockroachdb.New()` | CockroachDB, with a durable lock table and no migration transactions: CockroachDB has no advisory locks, and does not make schema changes atomic with a transaction. See [CockroachDB](#cockroachdb). |
| `migrate_backend_postgres.NewPgBouncer()` | PostgreSQL behind PgBouncer in transaction mode, with a lock table. |
| `migrate_backend_mysql.New()` | MySQL and MariaDB, with `GET_LOCK` locks keyed within the current schema and no migration transactions, since MySQL commits DDL implicitly. See [MySQL and MariaDB](#mysql-and-mariadb). |
| `migrate_backend_sqlite.New()` | SQLite, with no lock: SQLite serialises writers itself. |
| `migrate_backend_clickhouse.New()` | ClickHouse, with no lock and no transactions: coordinate concurrent runs yourself. |
| `migrate_backend_duckdb.New()` | DuckDB, with transactions and no lock: a DuckDB file is opened for writing by one process at a time. See [DuckDB](#duckdb). |
| `migrate.CustomBackend(spec)` | Any other engine, described by a `migrate.BackendSpec`. See [Other databases and drivers](#other-databases-and-drivers). |

A backend keeps no state between runs, so one value can be built once and shared by any
number of migrators, including ones running at the same time against different databases.

## Persisted names and locks

By default the history table is `sgl_migrations`, seeds are recorded in `sgl_seeds`, and
locks use the `sgl_migration_lock` and `sgl_seed_lock` tables, or advisory locks keyed on
the history table names. Every backend constructor takes options that change them:

```go
backend := migrate_backend_postgres.New(
    migrate.WithNamespace("app"),            // app_migrations, app_seeds, app_migration_lock, ...
    migrate.WithHistoryTable("app_history"), // rename only the history table
    migrate.WithLock(migrate.LockTable),     // LockAdvisory, LockTable or LockNone
)
fmt.Println(backend.Names()) // every table, constraint and lock key the backend touches
```

`WithHistoryTable` renames only the history table, and the migration lock key follows it;
the seed table and seed lock stay namespace-derived. The result does not depend on the
order of the options. PostgreSQL supports all three lock modes, CockroachDB table and
none, MySQL advisory and none, and SQLite, ClickHouse and DuckDB none only.

MySQL's named locks are global to the server, so its lock keys are scoped to the current
schema: the migration lock is named `CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations')`,
replaced by its SHA2-256 digest when that is longer than MySQL's 64-character limit, and
`Names().LockKey` reports that expression (`Names().SeedLockKey` the seed lock's).
Migrators of different schemas on one server therefore never wait for each other. `migrate_backend_mysql.WithServerWideLockKey()` keeps the bare
`sgl_migrations` key used by earlier releases. Use it while those releases may still
migrate against the same server, so every process takes the same lock. Other engines' locks already cover one database, so they ignore the option.

A constructor never fails, so it can be passed inline. An invalid option (a namespace that
is not a plain SQL identifier, a history table that would share a name with the seed or
lock tables, or a lock mode the database cannot provide) is recorded in the backend and
returned by `NewMigrator`, `NewSeeder` and `NewExecutor`, wrapping
`migrate.ErrInvalidIdentifier` or `migrate.ErrUnsupportedLock`.

## Statements

The runner splits each file into statements exactly as the dialect does when `sgl generate`
and `sgl vet` read it: under the engine's lexical rules, with the splitter the generator
uses.
Quoted identifiers such as `"a;b"`, MySQL's double-quoted strings and `#` comments,
PostgreSQL's `E'...'` strings, nested comments and dollar-quoted bodies, and procedural
`BEGIN ... END` bodies all stay within their statement, and a column named `begin` is just a
column. MySQL's `DELIMITER` lines change the terminator, as in the `mysql` client, and are
not sent to the server:

```sql
DELIMITER //
CREATE FUNCTION double_it(x INT) RETURNS INT DETERMINISTIC
BEGIN
    RETURN x * 2;
END //
DELIMITER ;
```

On PostgreSQL, TimescaleDB and CockroachDB a line that starts with a backslash where a
statement could begin is a psql meta-command, such as the `\restrict` and `\unrestrict`
lines `pg_dump` 17.6 and later write around a dump: psql runs it itself, so it belongs to no
statement, and a dumped schema replays and runs without it.

`Backend.Split(content)` returns the statements the runner would run for a file.

## Transactions

Each migration runs in a transaction with its history row where the database supports
transactional DDL: PostgreSQL, TimescaleDB, SQLite and DuckDB. The runner recognises the
statements an engine refuses inside a transaction, and runs a file holding one without
one: `CREATE INDEX CONCURRENTLY`, `REINDEX CONCURRENTLY` and `VACUUM` on PostgreSQL, a
leading `VACUUM` on SQLite and DuckDB, and on `migrate_backend_timescaledb.New()` a continuous
aggregate created with data or refreshed with `refresh_continuous_aggregate`. A mention
inside a string, a quoted identifier or a comment does not count. For anything else, the
directive at the top of a file turns the transaction off:

```sql
-- sgl.migration(no_transaction: true)
CREATE INDEX CONCURRENTLY tasks_title_idx ON tasks (title);
```

A directive the runner cannot read for certain is refused rather than read as asking for
nothing: a misspelt key (`no_transacton: true`), a value other than `true` or `false`, a
call left unclosed or continued on the next line, text after the call, or a misspelt role
such as `sgl.migraton`. Running a migration inside a transaction it asked to avoid, or the
reverse, cannot be undone, so `Up` and `UpTo` check every migration they would run, and
`Down` and `DownTo` every down file, and return an error that names the file, the line
and the problem before anything runs.
`sgl generate` and `sgl vet` report the same directive where it is written.

MySQL, MariaDB, CockroachDB and ClickHouse run no migration in a transaction, so on those
backends every file behaves as if it carried the directive; see
[MySQL and MariaDB](#mysql-and-mariadb) and [CockroachDB](#cockroachdb).

A migration that runs without a transaction is not atomic, so keep a statement that needs
one in a file of its own. A TimescaleDB continuous aggregate can also be created
`WITH NO DATA`, which runs in a transaction, and filled by a refresh policy:

```sql
CREATE MATERIALIZED VIEW hourly_temps
WITH (timescaledb.continuous)
AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature) AS avg_temp
FROM readings
GROUP BY bucket
WITH NO DATA;
```

Data-only seeds and their history rows share a transaction on every built-in backend
except ClickHouse. Schema-changing SQL can break that atomicity on engines whose DDL
commits implicitly.

When a migration or seed fails inside its transaction, the runner rolls the transaction
back; if the rollback fails too, that failure is joined to the error it returns.

## Locking

Advisory and table locks coordinate runners using the same lock identity. `LockNone`
adds no cross-process lock; concurrent runs require external coordination. By default
acquisition waits for as long as the call's context allows. `migrate.WithLockTimeout(d)` bounds
the wait: when another process still holds the lock after `d`, the call fails with an
error wrapping `migrate.ErrLockTimeout` and changes nothing. Only the wait is bounded, not
the migrations that run once the lock is held. `migrate.WithNonBlockingLock()` makes it
fail immediately with `migrate.ErrLockNotAcquired`, which suits short-lived jobs that should
retry later. Both apply to a seeder's lock too. The CLI's `--lock-timeout` and
`--non-blocking` flags set them. A lock that cannot be taken is reported as a
`*migrate.LockAcquisitionError`, whose `Lock` field says whether it was the `migration`
or the `seed` lock.

The table lock holds a row lock inside an open transaction for the whole run, so the
migrations themselves run on a second connection. Give the pool at least two connections
when you use it.

The lock transaction sits idle while the migrations run, and a server can end a
transaction that idles. The PostgreSQL table lock (the default of
`migrate_backend_postgres.NewPgBouncer()` and `migrate_backend_cockroachdb.New()`, and `WithLock(LockTable)` on
`migrate_backend_postgres.New()` and `migrate_backend_timescaledb.New()`) therefore runs
`SET LOCAL idle_in_transaction_session_timeout = 0` before it locks the row, which lifts
that timeout for the lock transaction only. Before each migration and each seed, the
executor runs `SELECT 1` in the transaction of any table lock, a custom one included,
waiting at most five seconds. When the transaction has ended, the run stops before that
migration or seed with an error wrapping `migrate.ErrLockLost`, since another process may
hold the lock by then. The migrations before it stay applied, and the next `Up` carries on
from there.

Two timeouts are out of the lock session's reach:

- PgBouncer's own `idle_transaction_timeout` closes a client that idles in a transaction,
  whatever the server allows. Behind PgBouncer in transaction pooling mode, leave it at 0
  (its default) or set it above your longest run.
- PostgreSQL 17's `transaction_timeout`, which the lock does not change, ends any
  transaction that lasts longer than it, the lock transaction included. Where the server,
  database or role sets it, set it to 0 for the role that migrates, or above your longest
  run.

Either one ending the lock transaction stops the run with `migrate.ErrLockLost`.

`Up` creates the history table after acquisition. With advisory or table locking,
this coordinates replicas booting against an empty database. `LockNone` does not
provide that guarantee. The lock tables of the table lock are
created before the lock exists; a replica that loses that race retries. `Status` and
`Validate` take no lock and run no DDL: against a database without a history table they
report every migration as pending, so they work on a read replica or with a read-only
role. A seeder's `Status` behaves the same way.

Within one process, the calls on a `Migrator` (or a `Seeder`) run one at a time, so a
`Status` from a health check waits for a running `Up` rather than reading through its
locked session. An executor shared by several migrators admits one lock holder at a time,
and `WithNonBlockingLock()` reports a holder in the same process as
`migrate.ErrLockNotAcquired` too. A call whose context has already ended fails before it
starts, without waiting for another call or taking the lock.

Releasing the lock is bounded at five seconds, even after the run's context has ended. An
advisory lock's unlock statement runs on a context of its own with that bound, and a table
lock's commit that takes longer is abandoned by cancelling the context its transaction
began on. A driver that watches that context, such as pgx, then gives the commit up and
the session is discarded, which frees the lock; lib/pq only asks the server to cancel, so
on a connection that no longer answers, its commit can still wait until the network times
out. If releasing the lock fails, the session that held it is closed rather than returned
to the connection pool, so it cannot keep the lock, and `Up` or `Down` returns the release
error joined to its own result, alongside the number of migrations it ran.

### DuckDB

DuckDB has neither advisory locks nor row locks (it rejects `SELECT ... FOR UPDATE`), so
`migrate_backend_duckdb.New()` takes no lock. A DuckDB database file is opened for writing by one
process at a time, so no other process can migrate it while yours has it open. Within one
process, run a database's migrations through one `Migrator`, whose calls take turns.
Migrators built on separate executors are not serialised and concurrent transactions
can fail with a conflict. Use one migrator to coordinate runs and their retries.

Each DuckDB migration runs in a transaction with its history row, since DuckDB's DDL is
transactional. Seed files run whole, because the driver runs every statement of one call.

### MySQL and MariaDB

MySQL DDL commonly commits implicitly, so a transaction cannot make
a migration atomic: a failure part-way would keep the earlier statements and lose the
history row. `migrate_backend_mysql.New()` therefore runs each statement on its own and records its
progress, as for a file with the `no_transaction` directive: a migration that fails
part-way stays dirty, and the next `Up` resumes at the statement that failed (see
[Interrupted runs](#interrupted-runs)).

go-sql-driver/mysql runs one statement per call unless the DSN sets
`multiStatements=true`, so seed files are split into statements. When the DSN does set it,
`migrate_backend_mysql.New(migrate.WithDriverMultiStatements(true))` runs each seed file in one call.
Migrations are split either way, so that their progress can be recorded.

A server whose `sql_mode` holds `ANSI_QUOTES` reads `"..."` as a quoted identifier rather
than a string. `migrate_backend_mysql.New(migrate_backend_mysql.WithANSIQuotes(true))` splits migration and seed
files under the rules the generator reads them with when
`dialect_engine_mysql.WithANSIQuotes(true)` or
`dialect_options.ansi_quotes: true` is set, so each statement the generator sees is a
statement the runtime runs, and a problem such as an unterminated `"` run is reported as
the quoted identifier the server would read. `sgl migrate` and `sgl seed` pass it whenever
`sgl.yaml` sets `dialect_options.ansi_quotes`. Other backends ignore the option.

### CockroachDB

`migrate_backend_cockroachdb.New()` uses the table lock, made safe for the two ways CockroachDB differs
from PostgreSQL.

Under SERIALIZABLE, CockroachDB keeps a `SELECT ... FOR UPDATE` lock only in memory, and
drops it when the range that holds the row splits or moves. A schema change that backfills,
such as `ADD COLUMN ... NOT NULL DEFAULT` or `CREATE INDEX`, splits ranges, so a plain row
lock could be lost halfway through a run and let a second replica in to apply the same
migration again. The lock transaction therefore runs
`SET LOCAL enable_durable_locking_for_serializable = true` before it locks the row, which
replicates the lock so that it survives. That setting needs CockroachDB v23.2 or later; on
an older server the lock fails with `migrate.ErrUnsupportedLock`, and the alternative is
`WithLock(LockNone)` with migrations serialised outside the migrator. The lock transaction
then turns off the idle-in-transaction timeout, as the PostgreSQL table lock does (see
[Locking](#locking)).

Migrations do not run in transactions. From v25.1, CockroachDB commits an open transaction
before each schema change (its `autocommit_before_ddl` setting). With that setting off, a
schema change that fails its validation at `COMMIT`, such as a unique index over duplicate
rows or a `CHECK` that existing rows break, reports SQLSTATE `XXA00` after the
transaction's other writes, the history row among them, have committed. So each statement
runs on its own and the history row records progress, as for a file with the
`no_transaction` directive: a migration that fails part-way stays dirty, and the next `Up`
resumes at the statement that failed (see [Interrupted runs](#interrupted-runs)). A seed
file runs as one batch in a transaction with its history row, since CockroachDB commits
data atomically; a seed that also changes the schema loses that atomicity, as
`autocommit_before_ddl` commits what came before the schema change, but the seed is still
recorded once it has run.

## Sessions and the search path

On PostgreSQL, TimescaleDB and CockroachDB every run (an `Up`, `Down` or seeder `Apply`)
works on one database session: the lock's own session, the second connection a table lock
pins, or, under `LockNone`, a connection the run pins for itself. A `SET` in one migration,
`SET search_path` included, therefore lasts for the later migrations of the same run, which
is what `sgl generate` and `sgl vet` assume when they replay the migrations. `SET LOCAL`
ends with its migration's transaction, or, where a migration runs without one (always on
CockroachDB), with its statement.

```sql
-- 001_tenant.up.sql
CREATE SCHEMA tenant;
SET search_path TO tenant;
CREATE TABLE widgets (id INT PRIMARY KEY);

-- 002_gadgets.up.sql, applied in the same run: gadgets is created in tenant too
CREATE TABLE gadgets (id INT PRIMARY KEY);
```

Before anything runs, the runner reads the session's `current_schema()` and qualifies the
history and seed tables with it for the rest of the run (`"public".sgl_migrations` by
default), so a migration that changes the path cannot move them or make the runner lose
them. Where the tables live does not change: the schema the connection starts in, as
before. Table-lock operations use the separate lock session, and advisory lock keys are fixed
before migrations begin. Neither follows a migration's later search-path changes. `Status` and `Validate` run on the
connection pool and name the tables as the backend does.

When the run ends its session is closed rather than returned to the connection pool, so a
setting a migration made reaches neither the application's queries on the same `*sql.DB`
nor a later run. Each run starts from the search path the connection gives (the DSN's
`search_path`, or the role's or the database's default), so a migration applied by a later
run, after a deployment, does not see a path that an earlier run's migration set, although
the generator replays every migration as one run. A migration that depends on a search path
should set it itself, or name its schemas.

Behind PgBouncer in transaction mode a session-level `SET` is not tied to the client's
connection, so use `SET LOCAL` or schema-qualified names there.

MySQL and MariaDB runs work the same way, with the database in place of the schema: a run
works on one session, reads `DATABASE()` before anything runs and qualifies the history and
seed tables with it (`` `app`.sgl_migrations ``), so a migration's `USE other` moves the
statements after it, in that migration and the later ones of the run, but not the history,
and a later run still finds every migration it applied. The session is closed when the run
ends, so the `USE` reaches neither the connection pool nor a later run. A DSN that names no
database leaves the tables unqualified, as before. The generator does not follow a `USE`
when it replays the migrations: it reads a table a later statement creates without naming
its database as one in the database the queries run against, so name the database instead
(`CREATE TABLE other.widgets ...`), which the generator and the server read alike. The
other engines run as before.

## Interrupted runs

An up migration that runs without a transaction is recorded as dirty before its first
statement, except on an append-only history backend such as ClickHouse. `Up` resumes after the last successfully recorded statement checkpoint;
committed statements without a checkpoint can run again. Progress writes after successful
SQL are best-effort: a failed update logs a warning and execution continues, so recorded
progress can lag the database effects.

`migrate.WithProgressBatchSize(n)` records progress every `n` completed statements. With
successful periodic writes, a crash after the nth statement commits but before its
checkpoint can leave up to `n` statements to replay. Failed checkpoint writes can leave
more. Replay requires inspection of the actual effects and idempotent statements where
possible.

When a statement fails, or the call's context ends part-way through such a migration, the
runner records the last statement that completed before it returns, so the next `Up` does
not run the completed ones again. It writes that checkpoint on a context detached from the
cancellation and bounded at five seconds, so it is recorded after a deadline or an
interrupt too, and a failure to write it is joined to the returned error. The statement
that was running when the context ended is not recorded as completed and runs again on
resume, so check whether it took effect. A migration that runs in a transaction is rolled
back instead and leaves nothing of itself behind.

A run whose context ends between migrations stops before the next one, and its error says
how far it got, such as `migration run stopped after 2 of 5 migrations`, followed by the
context's error and the cause given to `context.WithCancelCause` or
`context.WithTimeoutCause`; it matches `context.Canceled` or `context.DeadlineExceeded`
and that cause with `errors.Is`. The migrations it applied stay applied, and the after-run
hooks do not run. Rollbacks and seed runs stop the same way.

The retried migration is the first of the run, participates in its hooks and is counted
in the result. If the dirty record no longer matches the next pending file, the run stops
with `migrate.DirtyMigrationError` until the record is resolved.

A migration that fails part-way is left dirty in the same way. Fix what made the statement
fail without editing the file, for example by removing duplicate rows a new unique
index rejected, and run `Up` again. It starts after recorded progress, which can precede
the failure if completed statements were not checkpointed. When the statement
itself was wrong, fix it in the file and run `sgl migrate repair`, which records the
edited file's checksum and asks how many of its statements ran, so the next `Up` resumes
with the fixed one; without it, `Up` stops with `migrate.ChecksumMismatchError`.
ClickHouse is the exception: it records a migration only once every statement has run, so
a failed one is simply run again from the start. A non-transactional down migration
also has no forward-resume checkpoint; earlier committed effects can remain when it fails.

With the index, the history row records the version of the statement splitter and a
SHA-256 digest of the statements the run completed. A resume skips those statements only
when the file still splits so that its leading statements match the digest. When it does
not, because the splitter changed between the runs or the row was recorded by a release
that kept no digest, `Up` runs nothing, leaves the migration dirty and returns
`migrate.ResumeMismatchError`, whose message asks you to check which of the file's
statements ran and then repair its history. `sgl` adds the command to run as a hint: resolve
the row with `sgl migrate repair VERSION`, which records the splitter version and digest of
the statements you confirm. A program using the library resolves it with
`Migrator.PlanRepair` and `Migrator.Repair` (see
[recovering with force and repair](#recovering-with-force-and-repair)). A dirty migration that had completed no statement always resumes
from the start.

Releases before this one created the history table without the `splitter_version` and
`statements_hash` columns. The first `Up` adds them while it holds the lock; `Status` and
`Validate` read such a table without them, and earlier releases ignore them.

`Down` and `DownTo` refuse to roll back while any migration is dirty. A down file assumes
its up file ran in full: run over a partly applied migration, it could fail after
dropping objects, and the dirty record would then let the next `Up` resume as if they were
still there. They read the history under the lock and, before any hook or down file runs,
release the lock and return an error wrapping `migrate.ErrDirtyRollback` and a
`*migrate.DirtyMigrationError` naming the migration. Finish the migration with `Up`, or
record how many of its statements ran with `sgl migrate repair` (`Migrator.PlanRepair` and
`Migrator.Repair`), then roll back. A rollback with nothing to roll back still returns 0
and no error.

On ClickHouse a rollback removes the history row with a mutation that finishes before
`Down` returns, so an `Up` or `Status` straight afterwards sees the version as pending.

## Checksum mismatches

Editing a file after it ran makes `Up` and `UpTo` fail with `migrate.ChecksumMismatchError`,
and deleting one makes them fail with `migrate.MissingMigrationFileError`, even when nothing
is pending. They check before taking the lock and again once they hold it. Revert the edit
and add a new migration for the change. Only when the edit changes nothing the migration
did, such as a comment or a reformatted statement, and has been reviewed, record the new
checksum with `sgl migrate repair`. A deleted file must be restored; repair cannot change
a migration that has no file.

`sgl fmt` never rewrites migrations unless asked with `--include-migrations`, for this
reason: see [fmt](cli.md#fmt).

## Recovering with force and repair

Both commands change only the history table, never run a migration's SQL or hooks, and
work under the migration lock, so they wait for (or, with `--non-blocking` or
`--lock-timeout`, give up on) a run in progress. They describe what they will change and
ask for confirmation on a terminal; `--yes` confirms without asking, and without a terminal
they refuse to run unless `--yes` is given.

`sgl migrate force VERSION` records that migration as applied and clean, with its files'
current checksums, as if it had run: the recovery for a migration someone finished by
hand, or for a database whose schema was created outside the migrations. It clears a dirty
flag and any recorded progress, and inserts the row when there is none. It never touches
other rows: earlier pending migrations stay pending and run with the next `up`, and it
refuses, changing nothing, when a later migration is recorded (roll that back first, or
force the later one), so the forced migration is always the last applied one. Unlike
golang-migrate's `force`, which sets one version for the whole database, Seagull keeps a
row per migration, so forcing never marks other migrations applied or pending.

`sgl migrate repair [VERSION]` first lists what it can change, then changes it:

- every applied migration whose up file, or recorded down file, no longer matches its
  checksum gets the files' current checksums (the migration is not run again);
- each dirty migration is shown with its statements, as the file splits now, and the
  progress the history records; you say how many of its statements ran, as the database
  shows, and repair records exactly that: 0 makes the migration pending again, so `up` runs
  it from its first statement; all of them records it as applied; a number in between
  records it as dirty after those statements, with the current splitter version and digest,
  so `up` resumes with the next statement. An empty answer leaves it dirty. Without a
  terminal, `--completed N` answers for the one dirty migration.

`VERSION` limits the repair to one migration. Repair plans under no lock, then applies the
plan under the lock only if the history still matches it; when another run changed the
history meanwhile, it changes nothing and exits 1. An applied migration without a file is
reported but cannot be repaired.

On ClickHouse, whose history is append-only, `force` and `repair` write a migration's new
history row before anything is removed. The new row carries the old row's applied time,
which is what makes it replace the old one, so a failed write leaves the old row in place.
Only when the old row's applied time cannot be read is that row deleted first.

From Go, `Migrator.Force(ctx, version)`, `Migrator.PlanRepair(ctx, versions...)` and
`Migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{...}})`
do the same; they need an executor that implements `migrate.HistoryEditorPort`, as the
executors `migrate.NewExecutor` builds do.

## Options

Every `migrate.Option` works with `migrate.NewMigrator`, `migrate.NewSeeder`,
`migrate.NewMigratorFor` and `migrate.NewSeederFor`; a seeder ignores the migration-only ones.

| Option | Purpose |
|---|---|
| `WithNonBlockingLock()` | Fail at once with `ErrLockNotAcquired` instead of waiting for the migration or seed lock. A seeder needs a seed executor that implements `migrate.SeedLockTrierPort`, as the one `migrate.NewSeeder` builds does. |
| `WithLockTimeout(time.Duration)` | Give up on the migration or seed lock after this long, with an error wrapping `ErrLockTimeout`; zero, the default, waits until the context ends. |
| `WithBeforeMigration`, `WithAfterMigration`, `WithBeforeRun`, `WithAfterRun` | Hooks around each migration or each run, for logging, metrics or environment checks. A nil hook is refused with an error matching `seagull.ErrInvalidConfig`. A hook that returns an error stops the run; a hook that panics stops it with a `*migrate.HookPanicError` rather than crashing the process, and its stack is logged once at warning level. |
| `WithDirectivePrefixes(canonical, aliases...)` | The prefixes recognised in `-- sgl.migration(no_transaction: true)`; the default is `sgl` and `seagull`. |
| `WithLogger(*slog.Logger)`, `WithObserver(observe.ObserverPort)` | Where logs and spans go; by default the ones carried by the context. |

A migrator or seeder reads at most 10,000 entries from its directory, 4 MiB from each
file and 1 GiB across every file in one call. A larger file fails with an error matching
`seagull.ErrFileTooLarge`. A directory with more entries fails with one matching
`seagull.ErrLimit` before any file is read, and so does a call that reads more bytes.
Each call, such as `Up`, `Status` or `Apply`, reads the files afresh within its own cap.

Backend options (`migrate.WithNamespace`, `WithHistoryTable`, `WithLock` and
`WithDriverMultiStatements`, and `migrate_backend_mysql.WithServerWideLockKey` and
`WithANSIQuotes`) configure the backend, and the executor option `WithProgressBatchSize`
the executor that `migrate.NewExecutor` builds for `migrate.NewMigratorFor`.
`NewExecutor` refuses a `WithProgressBatchSize` below one with an error matching `seagull.ErrInvalidConfig`.
Migration and seed durations are measured with the system clock.

## Other databases and drivers

The backends cover `database/sql`. An engine without a built-in backend uses a
`migrate.BackendSpec` passed to `migrate.CustomBackend(spec, options...)`. The spec provides the
history and seed SQL, lexical rules, lock operations and separate transaction capabilities
for migrations and seeds. The [migrate API reference](migrate-api.md#executors-and-backends)
lists its fields; the [implementation guide](../how-to/migration-backend.md) shows construction
and verification against the actual engine.

`AppendOnlyHistory` records completed files without resumable progress updates, as on
ClickHouse. `HistoryReadModifier` supplies a suffix when reading that history, and
`MultiStatementSeeds` allows a seed file to run in one call. An incomplete spec is reported
by the executor constructor with an error matching `seagull.ErrInvalidConfig`.

`CustomBackend` calls each of the spec's renderers once, for the names and bind-parameter
markers the backend uses, and the executors reuse the SQL they returned. A renderer that
panics is recorded as an incomplete spec is: the constructors return an error matching
`seagull.ErrInvalidConfig` and `seagull.ErrPanic`, and the stack is logged once at warning
level. `Split` and the `Rules` callbacks run on each file instead. A panic in `Split`
fails that call with an error matching `seagull.ErrPanic`, its stack logged once at
warning level, and a panic in a `Rules` callback fails it with an error naming the
callback and the panic value, which does not match `seagull.ErrMalformedSQL`, its stack
also logged once; either way the migration or seed runs nothing.

A table lock must accept `SELECT 1` in its transaction, which the executor runs before
each migration and seed to confirm the lock is still held (see [Locking](#locking)). An
advisory lock's `Wait` query should wait a bounded time on the server, as MySQL's
`GET_LOCK(name, 5)` does. When it returns without the lock, the next try starts at least
half a second after the one before, so a `Wait` that returns at once does not poll in a
tight loop.

For anything else (a native driver, another store for the history), implement
`migrate.ExecutorPort` and `migrate.SeedExecutorPort` and pass them to
`migrate.NewMigratorFor(executor, files, directory, options...)` and `migrate.NewSeederFor`. The migrator
calls `EnsureMigrationTable` only while it holds the lock, and never from `Status` or
`Validate`, so `AppliedVersions` and `AppliedSeeds` must return no rows, not an error, when
their table does not exist yet. An executor splits files under the engine's lexical
rules, as the backends do, records a dirty migration's progress with `LastStatement`,
`SplitterVersion` and `StatementsHash`, and resumes a retried one only when those still
identify the file's leading statements; the `migrate.ExecutorPort` documentation spells
out the contract, which is Evolving (see [Compatibility](compatibility.md)). The
`useTransaction` argument reflects only the `no_transaction` directive, so an executor
also runs outside a transaction a statement its engine refuses inside one. The
database/sql migration executor is available on its own as `migrate.NewExecutor`;
`migrate.NewSeeder` builds the database/sql seed executor itself.

Two interfaces are optional. An executor that also implements `migrate.HistoryEditorPort`
(`SplitStatements`, `RecordApplied` and `RecordProgress`) supports `Force`, `PlanRepair`
and `Repair`; without it they return an error matching `seagull.ErrInvalidConfig` that
says the executor cannot rewrite the history. A seed
executor that implements `migrate.SeedLockTrierPort` supports `WithNonBlockingLock` on a
seeder. With `WithLockTimeout`, the context `AcquireLock` receives ends once the lock is
held, so an executor whose lock lives in a transaction begins that transaction on
`context.WithoutCancel(ctx)` and ends it only in `ReleaseLock`.

## Errors

| Error | When |
|---|---|
| `migrate.ErrLockNotAcquired` | The lock is held and `WithNonBlockingLock` is set. |
| `migrate.ErrLockTimeout` | The lock was still held after `WithLockTimeout`'s timeout. |
| `*migrate.UnknownMigrationError` | `Force` or `PlanRepair` named a version no file has. |
| `*migrate.LaterMigrationError` | `Force` was asked for a migration with a later one recorded. |
| `migrate.ErrRepairOutOfDate` | The history changed between `PlanRepair` and `Repair`; nothing was changed. |
| `seagull.ErrInvalidConfig` | `Force` or repair with an executor that is not a `migrate.HistoryEditorPort`; the error says the executor cannot rewrite the history. |
| `*migrate.NoDownMigrationError` | A rollback reached a version without a down file; `Version` names it. |
| `*migrate.ChecksumMismatchError`, `*migrate.DownChecksumMismatchError` | An applied file changed on disk. |
| `*migrate.DirtyMigrationError` | An interrupted migration no longer lines up with the files, or, with `migrate.ErrDirtyRollback`, names the dirty migration that blocks a rollback. |
| `migrate.ErrDirtyRollback` | `Down` or `DownTo` was asked for while a migration is dirty; nothing ran. Finish it with `Up`, or repair it, then roll back. |
| `*migrate.ResumeMismatchError` | An interrupted migration's recorded progress no longer matches how its file splits, so it is not resumed; resolve it with `Migrator.Repair` (`sgl migrate repair VERSION`). |
| `*migrate.MissingMigrationFileError` | An applied version has no file any more. |
| `*migrate.MigrationExecutionError`, `*migrate.SeedExecutionError` | A statement failed; the error names the file and statement. |
| `*migrate.LockAcquisitionError` | The migration lock, or from a seeder the seed lock, could not be taken; `Lock` names which. |
| `migrate.ErrLockLost` | A table lock's transaction ended during the run, so another process may hold the lock; the run stopped before the migration or seed the error names. |
| `*migrate.DuplicateMigrationVersionError`, `*migrate.DuplicateSeedVersionError` | Two files share a version. |
| `*migrate.HookPanicError` | A hook panicked; the error carries the panic value, and the stack is logged once at warning level through the migrator's logger (`WithLogger`'s, or the context's). |
| `seagull.ErrPanic` | A panic recovered from code the migrator called, such as a hook or a backend's callback; `*migrate.HookPanicError` wraps it. |
| `seagull.ErrInvalidConfig`, `seagull.ErrFileTooLarge`, `seagull.ErrLimit` | Unusable configuration, a file over the size limit, or more files or bytes than the migrator reads in one call. |

Two refusals have no type of their own, and nothing has run when either is returned. A
migration about to run whose `sgl.migration` directive cannot be read for certain, such as
`no_transacton: true`, or whose directive has an unknown role, such as `sgl.migraton`,
stops the run with an error naming the file, the line and the problem. A migration about
to be applied that holds another tool's down section, after a marker such as
`-- +goose Down`, stops it with an error naming the file, the marker's line and the tool.

## Seeds

Seeds hold reference data and fixtures. They live in their own directory, use the same
numbered names (`001_countries.sql`). `Apply` skips versions already recorded as
successfully applied:

```sh
sgl seed apply
sgl seed status
sgl seed reseed     # clear the seed history and apply every seed again
```

From Go, `migrate.NewSeeder(database, migrate_backend_postgres.New(), seeds, "db/seeds")` returns a
`*migrate.Seeder` with the same three operations: `Apply`, `Status` and `Reseed`. Write seeds to be idempotent
(`INSERT ... ON CONFLICT DO NOTHING`), since `reseed` runs them again.
