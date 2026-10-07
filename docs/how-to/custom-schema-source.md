---
title: Provide a custom schema source
description: "Supply a schema catalogue from a custom source when embedding the generator."
section: How-to guides
subsection: Embedding
order: 920
---

# Implement a custom schema source

Implement `schema.SchemaSourcePort` when another system already describes your schema and migration replay is not the right input. The source converts that description into a catalogue of tables, views and functions; Seagull then analyses ordinary SQL queries against it.

## What you import

A source is written against the schema area of the `sdk` kit, `seagull.sh/seagull/sdk/schema`,
whose built-in providers, `schema_source_postgres` and `schema_source_sqlite`, read a live
database as [generate from an existing database](live-schema.md) shows. A source of your
own imports:

| Package | Import path | What it gives a source |
|---|---|---|
| `schema` | `seagull.sh/seagull/sdk/schema` | `SchemaSourcePort`, and `SourceError`, the diagnostics its one method returns beside the catalogue |
| `dialect` | `seagull.sh/seagull/sdk/dialect` | `Catalogue`, which the method returns, and the model types that fill it, such as `Schema`, `Table` and `NewColumn` |
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite` and the others | The dialect whose identifier and type rules the catalogue follows |
| `seagull` | `seagull.sh/seagull` | `seagull.New`, `WithSchemaSource` and `Input`, to run the generator with the source |

## Build and return the catalogue

This complete command supplies an in-memory SQLite catalogue. Replace the catalogue construction with your adapter's file or network reads.

<!-- example: custom-source -->
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"testing/fstest"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/schema"
)

type cachedSource struct{ catalogue *dialect.Catalogue }

var _ schema.SchemaSourcePort = cachedSource{}

func (source cachedSource) BuildCatalogue(ctx context.Context) (*dialect.Catalogue, []schema.SourceError, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return source.catalogue, nil, nil
}

func main() {
	output := flag.String("out", "internal/store", "generated output directory")
	flag.Parse()
	if err := generate(context.Background(), *output); err != nil {
		log.Fatal(err)
	}
}

func generate(ctx context.Context, output string) error {
	engine := dialect_engine_sqlite.New()
	table := &dialect.Table{Name: "items", Schema: "main", Columns: []dialect.Column{
		dialect.NewColumn("id", engine.NormaliseTypeName("bigint"), false),
		dialect.NewColumn("name", engine.NormaliseTypeName("text"), false),
	}, PrimaryKey: []string{"id"}}
	source := cachedSource{catalogue: &dialect.Catalogue{
		Schemas:       map[string]*dialect.Schema{"main": {Name: "main", Tables: map[string]*dialect.Table{"items": table}}},
		DefaultSchema: "main", SearchPath: []string{"main"}, Identifiers: engine.Info().Identifiers,
	}}
	generator, err := seagull.New(engine, seagull.WithSchemaSource(source),
		seagull.WithTypeOverrides(seagull.TypeOverride{Column: "items.name", GoName: "string"}))
	if err != nil {
		return err
	}
	queries := fstest.MapFS{"items.sql": {Data: []byte("-- sgl.query(ListItems, many)\nSELECT id, name FROM items;")}}
	result, err := generator.Generate(ctx, seagull.Input{Queries: queries, Package: "store"})
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}
	if table.Columns[1].GoTypeOverride != nil {
		return fmt.Errorf("source catalogue was modified")
	}
	if err := seagull.WriteFiles(output, result); err != nil {
		return err
	}
	fmt.Printf("generated %d queries; source catalogue preserved\n", result.QueryCount)
	return nil
}
```

The catalogue is `dialect.Catalogue`, so the model types from `sdk/dialect` fill it directly; a source needs no other kit. The catalogue's names must use the dialect's identifier rules: key tables and functions with `Catalogue.Key(name)` rather than lower-casing them yourself, although the generator files an existing catalogue's tables and functions under those keys again. Set its default schema and search path to match the database, and normalise SQL types through the same dialect used for generation. SQLite uses its `main` schema in this example.

## Preserve cached schema state

The source retains ownership of its returned catalogue. The generator clones it before applying overrides, custom functions and runtime-table filtering. This example checks that its column override changed only the generator's copy.

A source can return the same cached catalogue to several runs, provided it does not mutate that catalogue while a run may be reading it. Honour cancellation during adapter reads; return SQL/schema diagnostics in the second result and operational failures in the third. A nil catalogue without an error fails the run with an error matching `seagull.ErrNoSource`. A panic in `BuildCatalogue` fails the run with an error matching `seagull.ErrPanic` rather than crashing the program, and its stack is logged once at warning level, so look for it in the log. Nil schemas, tables, views and other nil entries in the catalogue are passed over.

## Generate without migrations

Save the command in `cmd/generate/main.go` and run:

```sh
go run ./cmd/generate -out internal/store
```

`Input.Migrations` is unnecessary with a source. Supply `Queries` even when the file system is empty, so the generator can distinguish a model-only project from missing input.

If your source includes migration bookkeeping tables with custom names, pass those names to `WithRuntimeTables`. For an existing PostgreSQL or SQLite database, use a [built-in live source](live-schema.md) instead. The [schema API reference](../reference/schema-api.md) describes how views and partitions are prepared.
