---
title: Your first query
description: Build and run a small SQLite task application. You will write a table
  and two queries, generate their Go API and call it against a real local database.
section: Tutorials
subsection: Build a task application
order: 10
---

# Your first query

Build a small task application with SQLite. You will create its table, name two queries and call the generated Go code.

## Step 1: Prepare the practice workspace

Start with Go 1.27 and a [Seagull checkout](../get-started/install.md#work-from-a-checkout). From the repository root, prepare a new directory outside the checkout:

```sh
go run ./tests/tutorials/cmd/prepare /tmp/seagull-practice
cd /tmp/seagull-practice/01_first_query
```

Choose a different directory if that one already exists. The helper builds `sgl` and supplies the module, imports and connection setup. The first six lessons use this same application directory. Keep using `../sgl` so the generator matches the checkout.

Open `main.go`. Its `run` function is empty. The supplied setup opens a fresh SQLite file, applies the migrations you will write and closes the connection afterwards. Each application run starts with an empty database.

## Step 2: Describe the project

Create `sgl.yaml`:

```yaml
version: 1
databases:
  - name: tasks
    dialect: sqlite
    migrations: db/migrations
    queries: db/queries
    seeds: db/seeds
    go:
      package: store
      out: internal/store
```

This tells Seagull where to read SQL and where to write the `store` package. The dialect describes the SQL; the generated code will use `database/sql`.

## Step 3: Create the table

Create `db/migrations/0001_tasks.up.sql`:

```sql
CREATE TABLE tasks (id INTEGER PRIMARY KEY, title TEXT NOT NULL, completed INTEGER NOT NULL DEFAULT 0);
```

The table has an ID, a title and a completion value that starts at zero. Generation reads this migration to learn the schema. It does not create a database yet.

## Step 4: Name the queries

Create `db/queries/tasks.sql`:

```sql
-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES (?) RETURNING id, title, completed;

-- sgl.query(ListTasks, many)
SELECT id, title, completed FROM tasks ORDER BY id;
```

`CreateTask` inserts a title and returns one row. `ListTasks` returns all the tasks in ID order. The question mark is a parameter supplied by the Go caller.

Generate the package:

```sh
../sgl generate
```

Open `internal/store/tasks.sql.go` and find `CreateTask`. It accepts a title and returns a row with the three selected columns.

## Step 5: Call the generated method

In `main.go`, put this code inside `run`, before its final `return nil`:

```go
	queries := store.New(db)
	task, err := queries.CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, task.ID, task.Title)
```

`store.New(db)` uses the connection supplied by the setup. Run the application:

```sh
go run .
```

```text
1 Feed the seagulls
```

The migration created the table before the call. SQLite assigned the first ID, and the generated method read the returned title.

Continue with [a useful data layer](02-data-layer.md). The [complete checkpoint](../../tests/tutorials/01_first_query/main.go) is available if you want to compare your work.
