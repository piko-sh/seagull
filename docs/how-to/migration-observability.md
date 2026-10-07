---
title: Use migration hooks and observability
description: "Observe migration progress and connect lifecycle events to application logging."
section: How-to guides
subsection: Migrations
order: 910
---

# Use migration hooks and observability

Attach hooks for events the application must handle at migration boundaries. Use an observer for stage timing and a logger for operational records.

## Attach lifecycle hooks to the migrator

Pass hooks when constructing the migrator. This tested caller logs pending versions before the run and each applied version afterwards:

```go
	migrator, err := migrate.NewMigrator(database, backend, files, "db/migrations",
		migrate.WithBeforeRun(func(_ context.Context, pending migrate.MigrationRunHookContext) error {
			fmt.Fprintf(output, "migrating: %d pending, versions %v\n", pending.PendingCount, pending.PendingVersions)
			return nil
		}),
		migrate.WithAfterMigration(func(_ context.Context, migration migrate.MigrationHookContext) error {
			fmt.Fprintf(output, "  applied %04d %s\n", migration.Version, migration.Name)
			return nil
		}),
	)
	if err != nil {
		return err
	}
```

Then call `migrator.Up(ctx)` and check its result. `WithBeforeMigration` and `WithAfterRun` cover the other boundaries. The constructor refuses a nil hook with an error matching `seagull.ErrInvalidConfig`. A hook returning an error stops the run; a panic stops it with a `*migrate.HookPanicError`, which matches `seagull.ErrPanic`, rather than escaping the runner. The error carries the panic value but not the stack: the stack is logged once, at warning level, in a `recovered a panic` record whose `operation`, `panic` and `stack` attributes hold the hook's name, the panic value and the stack. A failed after-hook does not undo SQL that has already committed.

Hooks do not create an extra transaction. Use them to observe or gate work, and avoid treating a log message as proof that database effects rolled back.

## Attach stage timing and operational logging

Supply an `ObserverPort` from `seagull.sh/seagull/sdk/observe` and a `*slog.Logger` through migration options. The [observability example](../../examples/embedding/051_generator_observability) uses both on its migrator:

```go
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), project, "db/migrations",
		migrate.WithObserver(observer),
		migrate.WithLogger(logger),
	)
	if err != nil {
		return err
	}
```

Without explicit options, the runner uses the observer and logger carried by the context, or `slog.Default()` for logging. Keep connection credentials out of operational records.

Warning records mark what an operator should look at, among them `recovered a panic` from a hook or a custom backend's callback, `best-effort statement progress update failed` when a resume checkpoint could not be written, and `applied_at could not be read, so the zero time is reported in its place`, logged once per read of the history with the `table`, the number of `rows` affected and the `first_version` among them. A failed transaction rollback is not logged: it is joined to the error the call returns.

The same options can be passed to `migrate.NewMigratorFor` and to the seeder constructors, `NewSeeder` and `NewSeederFor`, though seeders ignore migration-only hooks. See the [observe API](../reference/observe-api.md) for implementing stage timing and the [migrate API](../reference/migrate-api.md) for callback signatures. Configure acquisition policy separately in [configure locks](configure-locks.md).
