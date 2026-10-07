---
title: Documentation
description: Learn Seagull, accomplish a task, look up a contract or understand the engine.
section: Get started
order: -10
---

# Seagull documentation

Seagull turns plain SQL into type-safe Go. These docs cover the whole system: writing and checking queries, using generated code, operating migrations, embedding the compiler and extending its dialects and emitters.

## Start here

[Install Seagull](get-started/install.md), learn the [core concepts](get-started/concepts.md), then build [your first query](tutorials/01-first-query.md). The learning sequence uses SQLite and needs no separate database server.

## Find what you need

| Your need | Where to go |
|---|---|
| Learn through a complete application | [Tutorials](tutorials/index.md) |
| Complete a specific task | [How-to guides](how-to/index.md) |
| Look up syntax, defaults or contracts | [Reference](reference/index.md) |
| Understand the design | [Explanation](explanation/index.md) |
| Explore runnable SQL and Go | [Examples](../examples/README.md) |
| Inspect SQL in the browser | [Playground](https://seagull.sh/playground) |

## Common destinations

- [Configuration](reference/configuration.md), [CLI](reference/cli.md) and [directives](reference/directives.md).
- [Generated Go API](reference/generated-go.md), [type mapping](reference/type-mapping.md) and [dialects](reference/dialects/index.md).
- [Migrations](how-to/migration-lifecycle.md), [recovery](how-to/recover-migration.md) and [CI checks](how-to/ci.md).
- [Generator API](reference/generator-api.md), [dialect configuration](how-to/configure-a-dialect.md) and [emitter authoring](how-to/write-an-emitter.md).
- [Diagnostic codes](reference/diagnostics.md), [compatibility](reference/compatibility.md) and [known limitations](reference/limitations.md).

Seagull is in alpha. Documentation describes the accompanying source; pin versions and review generated changes when upgrading.
