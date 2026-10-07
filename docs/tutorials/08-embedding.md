---
title: Embed the generator
description: Build a Go tool that vets the first tutorial project, generates its code
  and publishes it into a temporary directory.
section: Tutorials
subsection: Go further
order: 80
---

# Embed the generator

Build a Go tool that checks your practice application's SQL and generates its code into a temporary directory.

## Starting point

After the first six lessons, move to the supplied tool:

```sh
cd /tmp/seagull-practice/08_embedding
```

It reads the neighbouring `01_first_query` application and needs no database connection. The supplied `main.go` has the imports and entry point ready.

## Step 1: Construct the generator

Put this code inside `run`, before its final `return nil`:

```go
	generator, err := seagull.New(dialect_engine_sqlite.New())
	if err != nil {
		return err
	}
	input := seagull.Input{Package: "store", Migrations: os.DirFS("../01_first_query"), MigrationDir: "db/migrations", QueryDir: "db/queries"}
	report, err := generator.Vet(ctx, input)
	if err != nil {
		return err
	}
	if err := report.Err(); err != nil {
		return err
	}
	result, err := generator.Generate(ctx, input)
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "seagull-generated-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := seagull.WriteFiles(directory, result); err != nil {
		return err
	}
	fmt.Printf("generated %d queries\n", result.QueryCount)
```

`Vet` and `Generate` return an operational error separately from SQL diagnostics. Check `report.Err()` and `result.Err()` before writing the output too.

`WriteFiles` replaces files named by the result and removes stale files carrying Seagull's generated header. The temporary destination keeps this lesson's output separate from the application.

## Step 2: Run it

```sh
go run .
```

```text
generated 8 queries
```

The tool generated the eight queries you added in the first six lessons. Its supplied test runs the same workflow:

```sh
go test . -count=1
```

## Step 3: Observe a diagnostic

Temporarily introduce a syntax error in the practice application's query file:

```sh
python3 - <<'PYTHON'
from pathlib import Path
path = Path("../01_first_query/db/queries/tasks.sql")
text = path.read_text()
path.write_text(text.replace("SELECT id, title, completed", "SELEC id, title, completed"))
PYTHON
go run .
```

The tool stops at the SQL diagnostic:

```text
Q010
```

Restore the query and run again:

```sh
python3 - <<'PYTHON'
from pathlib import Path
path = Path("../01_first_query/db/queries/tasks.sql")
text = path.read_text()
path.write_text(text.replace("SELEC id, title, completed", "SELECT id, title, completed"))
PYTHON
go run .
```

```text
generated 8 queries
```

For an application tool, continue with [generate from Go](../how-to/generate-from-go.md), [custom schema sources](../how-to/custom-schema-source.md) or the [generator API](../reference/generator-api.md).
