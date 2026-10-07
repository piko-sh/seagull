---
title: Run migrations from the command line
description: Choose a database and DSN, inspect migrate status and validate, then
  run migrate up. Keep credentials in the environment or configured environment references
  rather than committed YAML.
section: How-to guides
subsection: Migrations
order: 750
---

# Run migrations from the command line

Select the database and connection, inspect its history, then apply the pending files. Keep credentials in the environment or configured environment references rather than committed YAML.

## Select the database and connection

Configure `migrate.dsn` for the selected database, or supply the connection through `SGL_DATABASE_URL` or `--dsn`. With several databases in `sgl.yaml`, select one explicitly:

```sh
sgl migrate --database library status
sgl migrate --database library validate
```

Connection selection is `--dsn`, then `SGL_DATABASE_URL`, then `migrate.dsn`. A relative SQLite path in configuration is relative to the `sgl.yaml` directory; one supplied with `--dsn` is relative to the working directory. Confirm the destination before making a change.

## Apply pending migrations

Review the pending SQL and exercise it on a disposable database first. Then run:

```sh
sgl migrate --database library up
sgl migrate --database library status
```

`up` applies pending versions in order and checks the checksums of already applied files, even when no new version is pending. `status` shows which versions are recorded afterwards; test a representative application query as well.

For advisory or table locks, add `--lock-timeout 30s` to bound acquisition, or `--non-blocking` to fail immediately when the lock is held. `LockNone` backends require the coordination described in [configure locks](configure-locks.md). An acquisition timeout does not bound the SQL once the lock is acquired.

## Handle a refused or interrupted run

Restore an accidentally edited applied file, or follow [checksum investigation](checksum-mismatches.md) before changing history. A non-transactional failure can leave statements committed; follow [recovery](recover-migration.md) before retrying. `force` and `repair` change recorded history without executing migration SQL.

Each of these stops before the migration it names starts; fix the cause and run the command again:

- A migration whose `sgl.migration` directive cannot be read for certain, such as `no_transacton: true`, is refused before any migration runs. The error names the file and line; correct the directive, which `sgl vet` also reports.
- A run whose table lock was lost, because the server or PgBouncer ended the lock's transaction, stops before the next migration with `the lock's transaction has ended, so another process may hold the lock`. The migrations before it stay applied. Check the timeouts described in [configure locks](configure-locks.md), then run `up` again.
- `down`, and `to` an earlier version, refuse while a migration is dirty; resolve it as [recovery](recover-migration.md) describes first.

An interrupted run keeps the migrations it completed, and a migration it cut short is rolled back, or left dirty with its checkpoint, as [interrupted runs](../reference/migration-runtime.md#interrupted-runs) describes. Run `status` before trying again.

To create or roll back versions, follow [migration lifecycle](migration-lifecycle.md) and [rollback targets](rollback-target.md). The [CLI reference](../reference/cli.md#migrate-and-seed) lists commands and flags.

The [CLI example](../../examples/tooling/045_migrate_from_the_cli) provides a SQLite configuration and tests the command workflow.
