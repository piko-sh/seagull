---
title: Use CockroachDB
description: Choose the cockroachdb dialect and a PostgreSQL-family emitter. Start
  with the CockroachDB first-query and transaction examples instead of assuming every
  PostgreSQL example transfers unchanged.
section: How-to guides
subsection: Dialects
order: 1000
---

# Use CockroachDB

Generate a task query for CockroachDB and call it through database/sql. Choose the exact
`cockroachdb` dialect so Seagull uses its types, syntax and migration behaviour rather
than assuming PostgreSQL compatibility is complete.

## Configure the CockroachDB project

Use this database entry in `sgl.yaml`. Its default emitter is database/sql; set
`go.driver: pgx` instead when your application uses pgx's native API.

```yaml
version: 1

databases:
  - name: tasks
    dialect: cockroachdb
    migrations: db/migrations
    queries: db/queries
    go:
      package: store
      out: internal/store
```

Add the task table to the configured migrations directory:

```sql
CREATE TABLE tasks (
    id         INT8        NOT NULL DEFAULT unique_rowid() PRIMARY KEY,
    title      STRING      NOT NULL,
    completed  BOOL        NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Write and call the task queries

Put the annotated statements in `db/queries/tasks.sql`:

```sql
-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES ($1) RETURNING id, title, completed, created_at;

-- sgl.query(ListTasks, many)
SELECT id, title, completed, created_at FROM tasks ORDER BY created_at, id;

-- sgl.query(CompleteTask, execrows)
UPDATE tasks SET completed = true WHERE id = $1;
```

Run `sgl generate`, apply the migration and open a CockroachDB connection through your
chosen driver. The database/sql caller constructs `store.New(database)` and uses the
identifier returned by the insert:

```go
	queries := store.New(database)

	feed, err := queries.CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return fmt.Errorf("creating a task: %w", err)
	}
	if _, err := queries.CreateTask(ctx, "Count the chips"); err != nil {
		return fmt.Errorf("creating a task: %w", err)
	}

	completed, err := queries.CompleteTask(ctx, feed.ID)
	if err != nil {
		return fmt.Errorf("completing task %d: %w", feed.ID, err)
	}
	fmt.Fprintf(output, "completed %d task\n", completed)
```

Do not assume the generated identifier starts at one or increments by one: this schema
uses CockroachDB's `unique_rowid()`.

## Check the query against CockroachDB

From the Seagull repository root, run the working example:

```sh
cd examples/cockroachdb/001_first_query
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable CockroachDB database; without it the test skips.
See [running examples](run-examples.md) for connection setup. Its expected result is:

```text
completed 1 task
Feed the seagulls  completed=true
Count the chips    completed=false
```

## Handle transactions and schema changes separately

A generated `RunInTx` helper does not retry a transaction. For an application operation
that can encounter a serialisation failure, follow the
[transaction example](../../examples/cockroachdb/023_transactions) and retry the whole
operation at the application boundary.

CockroachDB's migration backend uses table locking by default and runs schema migrations
without transactions. Use the [dialect reference](../reference/dialects/cockroachdb.md)
and [migration runtime](../reference/migration-runtime.md) when planning schema changes.
