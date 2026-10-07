---
title: Use the playground and open an example
description: "Inspect generated Go and SQL diagnostics in the existing browser playground."
section: How-to guides
subsection: Interactive tools
order: 980
---

# Use the playground and open an example

Try a query in the [playground](https://seagull.sh/playground) to inspect its diagnostics and generated Go. Start from a complete project in the [example browser](https://seagull.sh/examples/) so the query has a schema to analyse.

## Try a query change

Choose the dialect and driver for your application. Open an example, edit its query and inspect the updated result type beside the SQL. If a diagnostic appears, follow its source location and correct the input.

Keep the migration SQL with the query when copying an example: a `SELECT` alone does not describe the column types.

## Try a generation setting

The playground's settings override `sgl.yaml` for that run. Use them to explore a result shape, such as JSON tags, an interface or nullable elements, before saving the equivalent option in your project.

The catalogue view shows the schema Seagull inferred. It is useful when a generated field differs from what you expected. For available settings, see [configuration](../reference/configuration.md).

## Move the result into your application

Copy the SQL and selected settings into the local project, then check and generate it:

```sh
sgl vet --strict
sgl generate
go test ./...
```

The browser generates code without connecting to your database. Test the copied query through your application's driver to check its execution too.

Large projects and multiple-database configurations have browser-specific limits; see [the playground reference](../reference/playground-wasm.md) before moving a whole project into it. If your `sgl.yaml` raises `max_parse_depth`, `max_tokens_per_statement` or `max_type_parse_depth` above the dialect's default, the playground uses the default instead and says so in a configuration warning, so a deeply nested query that generates locally may be refused there.
