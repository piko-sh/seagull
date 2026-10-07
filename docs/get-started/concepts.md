---
title: Core concepts
description: A Seagull project contains SQL source, generation settings and a generated
  Go package. The same project can be inspected without writing output.
section: Get started
subsection: Overview
order: 300
---

# Core concepts

A Seagull project contains SQL source, generation settings and a generated Go package. The same project can be inspected without writing output.

| Term | Meaning |
|---|---|
| Dialect | The SQL grammar, types, functions and capabilities the analyser understands. |
| Driver | The database client used by generated code or the migration CLI. |
| Emitter | The component that writes Go for a particular driver API. |
| Migration | A versioned schema change, normally with up and down SQL. |
| Catalogue | Seagull's in-memory description of tables, columns, views, types and functions. |
| Query | One named SQL statement with an `sgl.query` header. |
| Directive | A SQL comment carrying generation or migration metadata. |
| Diagnostic | A located message with severity and a stable Q-code. |
| Preset | An embedder's vocabulary and type choices for generation. |

## Generation is separate from execution

`generate` builds a catalogue and emits files. `vet` analyses SQL and checks the configured emitter without writing files. `migrate up` changes the database. A generated method sends SQL through the connection you give it. Generating a method does not create its tables in a live database.

A SQL error can be a diagnostic without being a Go return error from `Generate`: check both the returned error and `Result.Err()`. At runtime a query can still fail because of constraints, permissions, connectivity, cancellation or concurrent changes.

## Select the right layer

Use the CLI for ordinary projects. Embed the root package, `seagull.sh/seagull`, when integrating generation into another tool. Configure a built-in dialect when your database needs its own name, search path or limits, and implement an `EmitterPort` from `sdk/emit` only when the built-in emitters do not write the Go API you need; see [configure a built-in dialect](../how-to/configure-a-dialect.md) and [write an emitter](../how-to/write-an-emitter.md). Dialects written outside Seagull are not supported yet.

See [dialects, drivers and emitters](../explanation/dialects-drivers-emitters.md), [project structure](project-structure.md) and the [glossary](../reference/glossary.md).
