---
title: Introduction
description: "Seagull turns schema migrations and SQL queries into a typed Go data layer."
section: Get started
subsection: Overview
order: 280
---

# Introduction

Seagull turns schema migrations and SQL queries into a typed Go data layer. You keep control of the SQL and the database connection. Seagull resolves names, infers result and parameter types, reports diagnostics, and emits Go methods you can read.

```sql
-- sgl.query(GetTask, one)
SELECT id, title FROM tasks WHERE id = ?;
```

A query like this becomes a method taking a context and an ID and returning a typed row and an error. The exact types follow the schema, dialect and emitter.

## Three ways to use Seagull

- Use `sgl` to generate packages, check SQL in CI, inspect schemas and run migrations.
- Call the Go library from a build tool, editor or framework.
- Use the browser playground to inspect SQL and generated Go before adding a tool to a project.

Generating code normally needs no running database. Running that code does need the relevant driver and a database. Live introspection also needs a database connection. Generated queries do not require the Seagull generator at runtime; an application that embeds migrations deliberately links the smaller migration runtime.

## Choose a starting point

Start with [installation](install.md), then [your first query](../tutorials/01-first-query.md). Already using Seagull? Browse [task guides](../how-to/index.md) or the [reference](../reference/index.md). For the design, read [from SQL to Go](../explanation/from-sql-to-go.md).

Seagull began inside Piko and now works independently. Piko's bridge retains its `piko.` directives and framework settings; standalone projects use `sgl.` comments and Seagull configuration. If you are working inside Piko, start with its database guides for the framework's connection and build workflow. Seagull is in alpha: pin the tool version and read [compatibility](../reference/compatibility.md) before upgrading.
