---
title: Seagull in the browser
description: The browser generator uses Go compiled to WebAssembly. It shares configuration
  assembly, starter generation and diagnostics with the CLI, rather than implementing
  another SQL analyser in JavaScript.
section: Explanation
subsection: Design
order: 1210
---

# Seagull in the browser

The browser generator runs the Go engine compiled to WebAssembly. Shared configuration assembly, starters and diagnostics keep browser analysis aligned with the CLI without maintaining a second SQL analyser in JavaScript. The worker, `cmd/wasm`, uses the same public packages as any other program, the root facade and `sdk/`, and shares `cmd/internal/config`, `cmd/internal/assemble` and `cmd/internal/starter` with the CLI.

## A constrained project interface

A worker receives an in-memory map of project files and returns diagnostics and generated output. File and query limits bound its work, as do limits on the size of each request and its overrides, and the parser limits are held to the dialects' defaults, so a project cannot make one run use more memory than the browser can spare. Generating only the first configured database keeps each run focused on one package.

The browser derives schemas from migrations and ignores live `schema_from` connections. Generating PostgreSQL code therefore needs no PostgreSQL server. This makes the browser useful for inspection and experimentation, while an application's driver and deployment still need local compilation and database tests.

The front ends also make different default choices when a query fails: the CLI generates the remaining valid queries, while the worker does so only when `skipFailingQueries` is enabled. Shared assembly establishes consistency for matching supported inputs and settings, rather than equivalence for every CLI project.

## Version alignment matters

An example, its generated output and the browser engine should describe the same source. A synchronised bundle can record that provenance so a displayed result can be traced back to the engine that produced it.

The repository's WASM parity tests compare supported example output with committed files. Integration tests answer a different question: whether generated code runs correctly against a database.

See the [WASM reference](../reference/playground-wasm.md) for the protocol and [playground guide](../how-to/playground.md) for the workflow.
