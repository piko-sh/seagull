---
title: Evolve the database
description: Add a default priority and exercise migration status, rollback and seeds
  on a disposable SQLite file.
section: Tutorials
subsection: Build a task application
order: 40
---

# Evolve the database

Add a priority to tasks, then apply and roll back that change in a local database. You will also record a seed separately from the migrations.

## Starting point

Continue after [search and results](03-search-results.md). The application still creates a fresh database on each run. For the CLI exercise, use a separate `tasks.db` file in your practice directory.

## Step 1: Add the next version

Create `db/migrations/0004_priority.up.sql`:

```sql
ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 1;
```

Create its reverse operation in `db/migrations/0004_priority.down.sql`:

```sql
ALTER TABLE tasks DROP COLUMN priority;
```

Append a query to `db/queries/tasks.sql`:

```sql
-- sgl.query(TaskPriority, one)
SELECT priority FROM tasks WHERE id = ?;
```

Regenerate, then append this code before `run` returns:

```sh
../sgl generate
```

```go
	priority, err := queries.TaskPriority(ctx, task.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "priority:", priority.Priority)
```

```sh
go run .
```

```text
1 Feed the seagulls
Bring chips
missing: true
Feed the seagulls in Harbour
priority: 1
```

The task receives the default priority of one.

## Step 2: Apply and inspect

Point the CLI at the practice database and check its history:

```sh
export SGL_DATABASE_URL="file:$PWD/tasks.db"
../sgl migrate status
../sgl migrate up
../sgl migrate validate
../sgl migrate status
```

The four versions change from pending to applied. `migrate up` created the tables and columns in this database.

## Step 3: Roll back and reapply

```sh
../sgl migrate down 1
../sgl migrate status
../sgl migrate up
```

Status shows version four pending after the rollback. The next `up` adds the priority column again. Dropping a column loses its values, so this reversal is appropriate for our practice database.

## Step 4: Seed this local database

Create `db/seeds/0001_tasks.sql`:

```sql
INSERT INTO tasks (title) VALUES ('Review the migration');
```

Apply it and check the seed history:

```sh
../sgl seed apply
../sgl seed status
```

The seed is recorded as applied. Repeating `seed apply` skips it. Your application still uses its own fresh database, so its output stays the same.

Continue with [testing](05-testing.md). [Migration safety](../explanation/migration-safety.md) explains how this workflow changes for a shared database.
