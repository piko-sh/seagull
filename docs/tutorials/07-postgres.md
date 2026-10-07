---
title: Use PostgreSQL and pgx
description: Run the first task application against PostgreSQL using pgx-generated
  code. This lesson makes the database and driver changes explicit.
section: Tutorials
subsection: Go further
order: 70
---

# Use PostgreSQL and pgx

Run a task query against PostgreSQL with the native pgx driver. You will choose the SQL dialect and Go driver separately.

## Before you start

Use the practice workspace from [your first query](01-first-query.md) and a disposable PostgreSQL database. Move to the supplied PostgreSQL application:

```sh
cd /tmp/seagull-practice/07_postgres
export SGL_EXAMPLE_DSN='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=disable'
```

Replace the connection details with your local database's values. The application creates task and migration-history tables in the current schema of that disposable database. It leaves them there after running. It does not convert the SQLite database.

## Step 1: Choose the dialect and emitter

Create `sgl.yaml`:

```yaml
version: 1
databases:
  - name: tasks
    dialect: postgres
    migrations: db/migrations
    queries: db/queries
    go:
      driver: pgx
      package: store
      out: internal/store
```

`dialect: postgres` selects PostgreSQL SQL. `driver: pgx` selects the native Go API for the generated methods.

## Step 2: Write the PostgreSQL queries

Create `db/migrations/0001_tasks.up.sql`:

```sql
CREATE TABLE tasks (id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, completed INTEGER NOT NULL DEFAULT 0);
```

Create `db/queries/tasks.sql`:

```sql
-- sgl.query(CreateTask, one)
INSERT INTO tasks (title) VALUES ($1) RETURNING id, title, completed;

-- sgl.query(ListTasks, many)
SELECT id, title, completed FROM tasks ORDER BY id;
```

`BIGSERIAL` supplies the task ID, and `$1` is PostgreSQL's parameter form.

```sh
../sgl generate
```

## Step 3: Connect and call

In `main.go`, put this code inside `run`, before its final `return nil`:

```go
	if dsn == "" {
		return fmt.Errorf("set SGL_EXAMPLE_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	migrator, err := migrate.NewMigrator(db, migrate_backend_postgres.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	task, err := store.New(connection).CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, task.Title)
```

The migration backend uses a `database/sql` connection. The generated query uses the native pgx connection opened afterwards. Run it against your disposable database:

```sh
go run .
```

Expected output:

```text
Feed the seagulls
```

Run the supplied integration test too:

```sh
go test . -count=1
```

The test creates and removes an isolated schema. Without `SGL_EXAMPLE_DSN` it explicitly skips, while generation still works without a server.

Continue with [embedding](08-embedding.md), or use the [PostgreSQL guide](../how-to/postgres.md) for an existing application.
