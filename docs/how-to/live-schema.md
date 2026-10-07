---
title: Generate from an existing database
description: "For the PostgreSQL family or SQLite, use schema_from in configuration or a built-in introspection Source with WithSchemaSource."
section: How-to guides
subsection: Embedding
order: 800
---

# Generate from an existing database

For PostgreSQL, CockroachDB, TimescaleDB or SQLite, generate against a live schema when the database is your schema authority. Query files remain ordinary annotated SQL.

## Configure the CLI source

Add `schema_from` to the selected database in `sgl.yaml`. This SQLite example reads an existing file beside the configuration:

```yaml
version: 1
databases:
  - name: app
    dialect: sqlite
    schema_from: file:app.db
    queries: db/queries
    go:
      package: store
      out: internal/store
```

For the PostgreSQL family, select its dialect and use a connection string such as `${env:SCHEMA_DATABASE_URL}`. The account must be able to inspect the schema your queries use.

```sh
sgl --database app vet --strict
sgl --database app generate
go test ./...
```

Inspect the generated types before adapting callers. Generation reads the database; it does not apply your migrations.

## Supply a source from Go

A program that already owns a connection can pass it to a built-in source. The sources are provider packages in the schema area of the `sdk` kit: `schema_source_sqlite` for SQLite and `schema_source_postgres` for PostgreSQL, CockroachDB and TimescaleDB, each implementing `schema.SchemaSourcePort` from `seagull.sh/seagull/sdk/schema`. This function accepts a connection and a query file system, and returns a checked result for the caller to publish:

```go
package generation

import (
    "context"
    "database/sql"
    "io/fs"

    "seagull.sh/seagull"
    "seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
    "seagull.sh/seagull/sdk/schema/schema_source_sqlite"
)

func Generate(ctx context.Context, database *sql.DB, queries fs.FS) (*seagull.Result, error) {
    engine := dialect_engine_sqlite.New()
    generator, err := seagull.New(engine,
        seagull.WithSchemaSource(schema_source_sqlite.New(database, engine)),
    )
    if err != nil {
        return nil, err
    }
    result, err := generator.Generate(ctx, seagull.Input{
        Queries: queries, QueryDir: "db/queries", Package: "store",
    })
    if err != nil {
        return nil, err
    }
    if err := result.Err(); err != nil {
        return result, err
    }
    return result, nil
}
```

The caller opens and closes the connection and handles a returned diagnostic error before writing. For the PostgreSQL family, use `schema_source_postgres.New(database, engine)` with the corresponding dialect, such as `dialect_engine_postgres.New()` or `dialect_engine_cockroachdb.New()`; the source takes the dialect to normalise the database's type names. The [complete SQLite example](../../examples/embedding/050_schema_from_live_database/example.go) includes setup and catalogue inspection.

## Account for database state

A live schema can change independently of your source checkout. Use the intended database and record or control its schema when reproducible generation matters.

The generator retains a copy of the source's catalogue. It excludes migration history, seed and lock tables; if your runtime uses custom persisted names, pass the names from its backend's `Names` method to `WithRuntimeTables`. Views are analysed from their definitions where available, and partitions share their parent's model rather than generating duplicate models. On SQLite, the shadow tables behind FTS5 and other virtual tables get no model, and a `SELECT *` from an FTS5 table leaves out its hidden `rank` column and the column named after the table, as it does when the schema comes from the migrations.

See the [schema API](../reference/schema-api.md) for the complete contract and [custom schema sources](custom-schema-source.md) for other schema authorities.
