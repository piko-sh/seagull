---
title: Apply seeds, namespaces and hooks
description: "Configure a seed directory and a namespace that belongs to the application."
section: How-to guides
subsection: Migrations
order: 590
---

# Apply seeds, namespaces and hooks

Keep reference data in seed files and apply it separately from schema migrations. Use one namespace for the application's migration and seed history, and configure the CLI and embedded runner to agree on it.

From Go, the seeder, the migrator, their hooks and the backend options are in `seagull.sh/seagull/sdk/migrate`, and the backend for each database is a `migrate_backend_*` provider under it, such as `seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite`. [Run migrations at application start-up](startup-migrations.md#what-you-import) lists the backend for each database.

## Give the application its own history names

Construct the backend with the chosen namespace:

```go
backend := migrate_backend_sqlite.New(migrate.WithNamespace("chippy"))
```

Set `migrate.namespace: chippy` in the corresponding CLI database configuration. The backend then uses `chippy_migrations`, `chippy_seeds` and namespace-derived lock names. Changing the namespace creates a different history and coordination identity; follow [configure locks](configure-locks.md) before changing it in a deployment.

## Apply the seed directory

Put numbered seed files such as `001_menu.sql` in a separate `db/seeds` directory. Prefer idempotent data writes when a seed might be retried or deliberately rerun. Construct a seeder using the same backend as the migrator:

```go
	seeder, err := migrate.NewSeeder(database, backend, files, "db/seeds")
	if err != nil {
		return err
	}
```

Call it after the schema is ready:

```go
applied, err := seeder.Apply(ctx)
if err != nil {
    return err
}
```

A successful seed is recorded in its own history, so later calls skip it. A seed and its history normally share a transaction; ClickHouse lacks that guarantee, and schema-changing seeds can break it on engines with implicit DDL commits. Review a failed seed's database effects before retrying.

From the CLI, inspect and apply the configured directory with:

```sh
sgl seed status
sgl seed apply
sgl seed status
```

## Rerun seeds deliberately

`sgl seed reseed`, or `Seeder.Reseed(ctx)`, clears the recorded seed history and applies the files again. It does not restore earlier data values before doing so. Inspect the writes first and confirm that applying them again is intended.

To observe schema migration boundaries, add hooks to the migrator with `migrate.WithBeforeRun`, `migrate.WithAfterMigration` and the other hook options, as [migration observability](migration-observability.md) shows. Migration-only hooks are ignored by seeders. See [migration and seed files](../reference/migration-files.md#seed-files) for naming rules.

The [working example](../../examples/sqlite/029_seeds_namespaces_hooks) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/029_seeds_namespaces_hooks
go test .
```
