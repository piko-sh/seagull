---
title: Create, apply and roll back migrations
description: Create a new version with migrate new, edit its up/down SQL, validate,
  inspect status, and apply up. Use down or to only after reviewing which versions
  will change in a disposable database.
section: How-to guides
subsection: Migrations
order: 580
---

# Create, apply and roll back migrations

Keep each intended schema change in a new version. Review its SQL, validate the recorded history and apply it in version order. A down file is useful only when it can safely reverse that version's effects.

## Create the up and down files

From the configured project, create the next pair:

```sh
sgl migrate new add_home_port
```

Edit the generated `.up.sql` and `.down.sql` files under the configured migration directory. Only `NNN_name.up.sql` and `NNN_name.down.sql` names are migrations; other SQL files are ignored with Q048. A down file is optional, but without it that version cannot be rolled back.

For a change that destroys information, do not invent a rollback that claims to restore it. The [lifecycle example](../../examples/sqlite/028_migration_lifecycle) lower-cases and trims every species name, which loses the original spelling, so it deliberately leaves this migration without a down file:

```sql
UPDATE sightings SET species = lower(trim(species));
```

## Apply and inspect the new version

Test the SQL against a disposable database, then inspect the destination and apply it:

```sh
sgl migrate status
sgl migrate validate
sgl migrate up
sgl migrate status
```

`validate` checks applied files against their recorded checksums; it does not prove pending SQL will succeed. Test a representative query after `up`, then run `sgl generate` if the schema change affects generated code. Generating code does not apply migrations.

From Go, `UpTo` applies pending versions through the target, inclusive. The tested example applies only its first version before inserting data:

```go
	applied, err := migrator.UpTo(ctx, 1)
	if err != nil {
		return fmt.Errorf("migrating to version 1: %w", err)
	}
```

## Review the rollback before running it

Inspect the relevant down files and check that the deployed application can use the older schema. `sgl migrate down 1` rolls back the newest applied version; `sgl migrate to V` moves towards a target. Neither rolls back while `status` shows a dirty migration: resolve that through [interrupted-run recovery](recover-migration.md) first. Follow [rollback targets](rollback-target.md) for the complete operation and its data-loss limits.

Keep applied files immutable. Restore accidental edits, and describe a new change in another migration rather than rewriting history. Follow [checksum investigation](checksum-mismatches.md) or [interrupted-run recovery](recover-migration.md) when the recorded state needs attention.

The [working example](../../examples/sqlite/028_migration_lifecycle) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/028_migration_lifecycle
go test .
```
