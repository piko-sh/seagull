---
title: Inspect queries and compare schemas
description: "Use describe to inspect a named query, schema dump for the catalogue, and schema diff to compare supported live introspection with migration-derived schema."
section: How-to guides
subsection: Tooling
order: 730
---

# Inspect queries and compare schemas

Use `describe` to see a query's inferred Go API and `schema` to inspect the catalogue behind it. These commands do not write generated code.

## Inspect a query

In a configured project, name the query you want to inspect:

```sh
sgl describe ListBooksByAuthor
```

The output includes its SQL, parameters, result columns and generated signature. In the [library example](../../examples/tooling/043_describe_and_schema), the nullable subtitle becomes `*string`, while the author-name parameter is a non-null string.

For machine-readable output or a project with several databases:

```sh
sgl --database library describe ListBooksByAuthor --format json
```

## Read the migration catalogue

```sh
sgl schema dump --format sql
```

This prints approximate DDL for the schema the migrations describe. Use the default JSON format when you need the catalogue's structured fields.

## Compare with the database

For SQLite or the PostgreSQL family, compare a live database with the migration catalogue:

```sh
sgl --database library schema diff --dsn file:library.db
```

Choose the database and connection string for your project. A matching schema exits 0; drift exits 1. In the [tested transcript](../../examples/tooling/043_describe_and_schema/transcript.txt), a changed database produces:

```text
+ column main.books.isbn is in the database but not the migrations
- column main.books.subtitle is in the migrations but not the database
```

Use that comparison to investigate the change. It is not an automatic migration plan. Runtime history, seed and lock tables are excluded from the live catalogue. Diagnostics found while reading either side are printed to standard error, such as a Q054 warning for a PostgreSQL column declared over domains nested too deeply to follow.

See the [schema API](../reference/schema-api.md) for introspection's scope and [CLI connection strings](../reference/cli.md#connection-strings) for path and DSN resolution.
