---
title: Investigate checksum mismatches
description: Compare the on-disk migration with the exact version used by the deployment
  that recorded it. Check line endings, edited SQL and whether the wrong source directory
  or database was selected.
section: How-to guides
subsection: Migrations
order: 890
---

# Investigate checksum mismatches

Compare the migration on disk with the exact version used by the deployment that recorded it. Check edited SQL, line endings and the selected database or source directory before changing recorded history.

## Identify the changed or missing file

Run against the intended database:

```sh
sgl migrate status
sgl migrate validate
```

`Up` and `UpTo` refuse an edited applied file with `migrate.ChecksumMismatchError`, or a deleted one with `migrate.MissingMigrationFileError`, even when no migration is pending. From Go, use `errors.As` to inspect the typed error's version and filename.

## Restore history when the edit was accidental

Restore the applied file from the deployment's version control revision. Put any intended new schema change in a new migration version, then validate again. A missing file must be restored; repair cannot update a record whose migration file is absent.

`sgl fmt` leaves migrations alone unless passed `--include-migrations`. Keep applied migration bytes stable rather than treating formatting as exempt from checksums.

## Accept a harmless edit only after review

If the edit changed no database effect, such as a comment-only correction, review it and run:

```sh
sgl migrate repair
```

Repair lists recorded and current checksums and asks before writing. It changes history without rerunning migration SQL. From Go, use `Migrator.PlanRepair` to review the proposed changes and `Migrator.Repair` to apply the reviewed plan.

Changing a checksum does not make the schema match the file. If the database itself differs, inspect that drift and choose a corrective migration. For a dirty or partially completed version, use [interrupted-run recovery](recover-migration.md) to establish the completed statement count first.

See [recovery contracts](../reference/migration-runtime.md#recovering-with-force-and-repair) for repair flags and refusal conditions.
