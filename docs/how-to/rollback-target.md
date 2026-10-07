---
title: Roll back or target a migration version
description: Inspect status and the down SQL before choosing the target. Use a disposable
  database to exercise the complete path first.
section: How-to guides
subsection: Migrations
order: 900
---

# Roll back or target a migration version

Inspect the applied versions and the down SQL before choosing a rollback. Exercise the complete path on a disposable database, including an application query that uses the resulting schema.

The steps below use `sgl migrate`. From Go, the `Migrator` in `seagull.sh/seagull/sdk/migrate` has the same operations, `Status`, `Down`, `DownTo`, `UpTo` and `Up`, over the backend from the database's `migrate_backend_*` provider, and the errors named below are in that package.

## Choose which versions should change

Start with `sgl migrate status`. `sgl migrate down 1` rolls back the newest applied version; `sgl migrate to V` moves towards the numeric target V, applying up files or rolling back newer versions as needed.

Review every down file the operation will need, and check that the application deployed alongside the change can still use the target schema. A missing down file returns `migrate.NoDownMigrationError`; an existing file still needs review for destructive effects.

A rollback is refused while `status` shows a dirty migration, one an earlier run left part-way, and nothing runs: `migrate.ErrDirtyRollback`. Finish it with `sgl migrate up`, or record what ran with `sgl migrate repair`, then roll back; see [recovery](recover-migration.md).

## Run and verify the rollback

After reviewing one version's down SQL, run:

```sh
sgl migrate down 1
sgl migrate status
```

Check the command's error and the resulting history. Transaction support varies by backend, so a failed down operation can leave SQL effects without a complete rollback. Inspect the database before retrying; see [recovery](recover-migration.md).

To reapply a rolled-back version, run `sgl migrate up`. This reruns its up SQL; it does not recover application data deleted by the down SQL. Prefer a new forward migration when the old effects cannot safely be reversed.

See the [runtime reference](../reference/migration-runtime.md) for target semantics and backend transaction limits.
