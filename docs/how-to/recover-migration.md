---
title: Recover an interrupted migration
description: First stop competing deployment attempts and confirm which database,
  namespace and history table the failed process used. Save the reported version and
  error before retrying.
section: How-to guides
subsection: Migrations
order: 880
---

# Recover an interrupted migration

Inspect the migration files, recorded history and actual database effects before retrying or editing history. A failed transactional migration and a partially committed non-transactional migration require different recovery decisions.

The steps below use `sgl migrate`. A program that runs its own migrations does the same through `seagull.sh/seagull/sdk/migrate`: the `Migrator`'s `Status`, `Validate`, `Up`, `PlanRepair`, `Repair` and `Force`, the executor options, and the typed errors named below, over the backend from its `migrate_backend_*` provider.

## Inspect the failure and the recorded state

Select the intended database and run:

```sh
sgl migrate status
sgl migrate validate
```

Keep the original file unchanged while investigating. Identify the failing statement and check which effects are present in the database. A transaction can roll back a migration's SQL; without one, earlier statements can remain committed.

## Retry from a verified checkpoint

For a resumable non-transactional migration, `Up` starts after the last successfully recorded statement checkpoint. Committed statements without a recorded checkpoint can run again. Progress writes after successful SQL are best-effort: failures are logged and execution continues, so the recorded index can lag the actual effects.

When a statement fails, or the run is cancelled or reaches its deadline part-way through such a migration, the runner still writes the checkpoint of the last completed statement, waiting at most five seconds, and a failure to write it is part of the returned error. The statement that was running is not recorded as completed, so `Up` runs it again: check whether it took effect before retrying.

`migrate.WithProgressBatchSize(n)`, an option of `migrate.NewExecutor` whose executor `migrate.NewMigratorFor` runs, reduces checkpoint writes. Even when periodic writes succeed, a process dying after the nth statement commits but before its checkpoint can leave up to `n` completed statements to replay. Failed checkpoint writes can leave more. Check whether replay is safe; make statements idempotent where possible.

If the SQL is correct, fix the cause of the failure without editing the file, such as data that prevented creation of a unique index. Then retry and inspect the result:

```sh
sgl migrate up
sgl migrate status
```

ClickHouse records a migration only after the complete file succeeds, so it retries a failed file from the beginning. A non-transactional down migration also has no forward-resume checkpoint. Account for effects already committed before rerunning either operation.

Rolling back is not a way out of a dirty migration. `sgl migrate down`, and `sgl migrate to` an earlier version, refuse while any migration is dirty, with `migrate.ErrDirtyRollback`, and run nothing, because a down file assumes its up file ran in full. Resolve the dirty migration first, by finishing it with `up` or recording what ran with `repair`, then roll back.

## Repair an edited or incompatible resume record

The runtime checks the statement splitter version and a digest of the recorded completed prefix before skipping it. `migrate.ResumeMismatchError` means those boundaries cannot be verified; `migrate.DirtyMigrationError` can mean the dirty record no longer matches the next pending file. Neither is permission to guess how much ran.

When the failed SQL itself needs correction, inspect its effects, edit the necessary statement and run:

```sh
sgl migrate repair
```

Repair shows changed checksums and dirty statements as the file splits now. Record exactly how many statements actually completed: zero makes the version pending, all records it as applied, and an intermediate count leaves it dirty for resumption. For one dirty migration without a terminal, supply `--completed N` and `--yes` only after establishing that count.

Repair changes history without running migration SQL or hooks. A missing applied file must be restored before it can be repaired. For accidental edits to an applied file, follow [checksum investigation](checksum-mismatches.md) instead of rewriting valid history.

## Record manually completed work only after verification

Use `sgl migrate force VERSION` only when the version's database effects have been completed by hand. It records that one version as applied and clean without executing its SQL, and refuses when a later version is recorded. Review the planned history change before confirming it.

Both repair and force use the migration acquisition path; `LockNone` still requires external coordination. The [runtime reference](../reference/migration-runtime.md#recovering-with-force-and-repair) describes `PlanRepair`, `Repair` and `Force` for Go callers and their error contracts.

After recovery, inspect status and run a representative application query. A successful command exit alone does not verify the intended schema and data.
