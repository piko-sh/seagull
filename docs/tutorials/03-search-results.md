---
title: Search and shape results
description: Join tasks to projects, add an optional title filter and bound the result
  size. You will see how an outer join becomes a nested optional object.
section: Tutorials
subsection: Build a task application
order: 30
---

# Search and shape results

Put the task in a project, then search for tasks with their project details. You will add an optional filter and a limit to the results.

## Starting point

Continue in the same practice directory after [building the data layer](02-data-layer.md).

## Step 1: Add projects

Create `db/migrations/0003_projects.up.sql`:

```sql
CREATE TABLE projects (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
ALTER TABLE tasks ADD COLUMN project_id INTEGER REFERENCES projects(id);
```

A task can have no project because `project_id` is nullable.

## Step 2: Define the search

Append these queries to `db/queries/tasks.sql`:

```sql
-- sgl.query(AddProject, exec)
INSERT INTO projects (name) VALUES (?);

-- sgl.query(AssignProject, exec)
UPDATE tasks SET project_id = ? WHERE id = ?;

-- sgl.query(SearchTasks, many)
-- sgl.embed(projects, from: p)
-- ?1 as sgl.param(title, optional: true)
-- ?2 as sgl.param(page_size, default: 10, max: 50)
SELECT t.id, t.title, p.id, p.name
FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
WHERE (?1 IS NULL OR t.title = ?1)
ORDER BY t.id LIMIT ?2;
```

The SQL disables the title filter when its value is NULL. The page size defaults to ten and is capped at fifty. The `embed` directive puts the project's selected columns in a nested object.

```sh
../sgl generate
```

Open the generated `SearchTasksRow`. Its `Projects` field is a pointer because the `LEFT JOIN` can return a task without a matching project.

## Step 3: Search from Go

Append this code before the final `return nil` in `run`:

```go
	if err := queries.AddProject(ctx, "Harbour"); err != nil {
		return err
	}
	if err := queries.AssignProject(ctx, store.AssignProjectParams{ProjectID: new(int64(1)), ID: task.ID}); err != nil {
		return err
	}
	rows, err := queries.SearchTasks(ctx, store.SearchTasksParams{})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Projects == nil || row.Projects.Name == nil {
			return fmt.Errorf("project missing")
		}
		fmt.Fprintln(out, row.Title, "in", *row.Projects.Name)
	}
```

The empty parameter struct uses the default page size and leaves the title filter off. The caller checks the nested pointers before reading the project name.

```sh
go run .
```

```text
1 Feed the seagulls
Bring chips
missing: true
Feed the seagulls in Harbour
```

The task now appears with its project. Continue with [migrations](04-migrations.md), or see [optional filters](../how-to/optional-filters.md) for the same pattern in your own queries.
