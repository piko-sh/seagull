---
title: Generate SQL wrappers from Go
description: Construct seagull.New with a dialect, supply input file systems, call
  Generate and inspect both the returned error and Result.Err. Publish successful
  output through WriteFiles.
section: How-to guides
subsection: Embedding
order: 760
---

# Generate SQL wrappers from Go

Use the root package, `seagull.sh/seagull`, when your build tool needs to choose the input file system and decide when to publish generated files. This guide assumes a project with annotated queries in `db/queries` and schema migrations in `db/migrations`.

## Create a generator command

Save this as `cmd/generate/main.go` in a Go module that depends on Seagull. It targets SQLite; select the dialect that matches your SQL when adapting it.

<!-- example: generate -->
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func main() {
	project := flag.String("project", ".", "project directory")
	output := flag.String("out", "internal/store", "generated output directory")
	flag.Parse()
	if err := generate(context.Background(), *project, *output); err != nil {
		log.Fatal(err)
	}
}

func generate(ctx context.Context, directory, output string) error {
	generator, err := seagull.New(dialect_engine_sqlite.New())
	if err != nil {
		return err
	}
	result, err := generator.Generate(ctx, seagull.Input{
		Migrations: os.DirFS(directory), Package: "store",
		MigrationDir: "db/migrations", QueryDir: "db/queries",
	})
	if err != nil {
		return fmt.Errorf("generating: %w", err)
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(os.Stderr, "%s:%d:%d: [%s] %s\n", diagnostic.Filename,
			diagnostic.Line, diagnostic.Column, diagnostic.Code, diagnostic.Message)
	}
	if err := result.Err(); err != nil {
		return err
	}
	if err := seagull.WriteFiles(output, result); err != nil {
		return err
	}
	fmt.Printf("generated %d queries\n", result.QueryCount)
	return nil
}
```

`Generate` reads the SQL without opening a database. An operational error stops the run; SQL problems arrive in `result.Diagnostics`. The command prints those diagnostics and checks `result.Err()` before writing. A run reports at most 10,000 diagnostics and summarises the rest in one Q066 at the highest severity among them, so `result.Err()` still fails when an error was left out; `seagull.WithMaxDiagnostics` changes the cap. A panic in the dialect or the emitter is recovered too: it comes back as a Q070 diagnostic or an error matching `seagull.ErrPanic`, and its stack is logged once at warning level.

## Run and review the output

From your project root:

```sh
go run ./cmd/generate -project . -out internal/store
go test ./...
```

Review the generated API and compile its callers. The library's default `database/sql` emitter writes `prepared.go`; pass `seagull.WithEmitter(emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false)))` if you need the CLI's default instead.

Use a dedicated output directory. `WriteFiles` replaces existing files with the same names and prunes stale files bearing its generated header. A hand-written file with a colliding name can be overwritten.

## Add a check mode

For a CI check, compare `result.Files` with files in the output directory before calling `WriteFiles`, and return a failing status when they differ. The [complete example](../../examples/embedding/046_library_generate/example.go) implements this comparison. If you already use `sgl.yaml`, prefer `sgl generate --check`.

See the [generator API](../reference/generator-api.md) for options and [output writing](../reference/output-writing.md) for partial results and pruning.
