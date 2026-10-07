---
title: Vet and inspect a catalogue from Go
description: Use Vet when a tool needs diagnostics and analysed queries but no generated
  files. Use Catalogue when it only needs the schema. Supply the same dialect, source
  and limits used for generation.
section: How-to guides
subsection: Embedding
order: 770
---

# Vet and inspect a catalogue from Go

Use `Vet` to inspect query analysis without generating files. Use `Catalogue` when a tool only needs the schema. Both read the same input file systems and dialect as generation.

## Analyse the project

Save this command as `cmd/inspect/main.go`. It expects SQLite migrations and annotated queries under `db/migrations` and `db/queries`.

<!-- example: vet-catalogue -->
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
	flag.Parse()
	if err := inspect(context.Background(), *project); err != nil {
		log.Fatal(err)
	}
}

func inspect(ctx context.Context, directory string) error {
	generator, err := seagull.New(dialect_engine_sqlite.New())
	if err != nil {
		return err
	}
	input := seagull.Input{Migrations: os.DirFS(directory), Package: "store",
		MigrationDir: "db/migrations", QueryDir: "db/queries"}
	report, err := generator.Vet(ctx, input)
	if err != nil {
		return fmt.Errorf("vetting: %w", err)
	}
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(os.Stderr, "%s:%d: [%s] %s\n", diagnostic.Filename,
			diagnostic.Line, diagnostic.Code, diagnostic.Message)
	}
	if err := report.Err(); err != nil {
		return err
	}
	for _, query := range report.Queries {
		fmt.Printf("query %s: %d parameters, %d output columns\n",
			query.Name, len(query.Parameters), len(query.OutputColumns))
	}
	catalogue, diagnostics, err := generator.Catalogue(ctx, input)
	if err != nil {
		return fmt.Errorf("reading catalogue: %w", err)
	}
	schemaReport := seagull.Report{Diagnostics: diagnostics}
	if err := schemaReport.Err(); err != nil {
		return err
	}
	for _, schema := range catalogue.Schemas {
		fmt.Printf("schema %q: %d tables\n", schema.Name, len(schema.Tables))
	}
	return nil
}
```

Supply the package you would generate. A valid `Input.Package` enables naming and emitter checks, including problems that would prevent code generation. Leaving it empty requests analysis without those checks.

`Catalogue` returns diagnostics separately from its operational error. Wrapping them in a `seagull.Report` lets the command use the same `Err()` check as `Vet`.

## Run your checks

```sh
go run ./cmd/inspect -project .
```

The command prints query shapes and schema table counts. Add your own checks against `report.Queries` or `catalogue`; neither call publishes files. For stable output, sort map keys before displaying catalogue objects.

The [working example](../../examples/embedding/047_library_vet_and_catalogue/example.go) inspects parameter types, nullability and column origins. See the [generator API](../reference/generator-api.md#check-without-generating) and [SQL model](../reference/sql-model.md) for their contracts.
