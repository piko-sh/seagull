---
title: Build a useful data layer
description: Add nullable notes and an optional lookup to the task application. You
  will distinguish a NULL column from a row that does not exist.
section: Tutorials
subsection: Build a task application
order: 20
---

# Build a useful data layer

Add notes to the task and look up a task that may not exist. You will see the difference between a NULL column and a missing row.

## Starting point

Continue in the practice application's `01_first_query` directory after [your first query](01-first-query.md).

## Step 1: Add notes

Create `db/migrations/0002_notes.up.sql`:

```sql
ALTER TABLE tasks ADD COLUMN notes TEXT;
```

There is no `NOT NULL` constraint, so a task can have no note. Seagull represents this value as a string pointer.

Append these queries to `db/queries/tasks.sql`:

```sql
-- sgl.query(SetNotes, exec)
UPDATE tasks SET notes = ? WHERE id = ?;

-- sgl.query(FindTask, one, optional: true)
SELECT id, title, notes FROM tasks WHERE id = ?;
```

`SetNotes` has two inputs, so it receives a parameter struct. `FindTask` uses `optional: true`, so it returns a presence boolean along with the row and error.

```sh
../sgl generate
```

## Step 2: Set and read a note

Append this code inside `run`, before its final `return nil`:

```go
	if err := queries.SetNotes(ctx, store.SetNotesParams{Notes: new("Bring chips"), ID: task.ID}); err != nil {
		return err
	}
	found, ok, err := queries.FindTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if !ok || found.Notes == nil {
		return fmt.Errorf("task or notes missing")
	}
	fmt.Fprintln(out, *found.Notes)
	_, ok, err = queries.FindTask(ctx, 999)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "missing:", !ok)
```

The first lookup checks that both the task and its note exist before reading the pointer. The second asks for ID 999, which this application has not created.

```sh
go run .
```

```text
1 Feed the seagulls
Bring chips
missing: true
```

The note is present, while the second row is missing. A present task with NULL notes would instead have `ok == true` and `found.Notes == nil`.

Continue with [search and results](03-search-results.md). See [null and missing rows](../how-to/null-and-missing-rows.md) when choosing the contract for an application query.
