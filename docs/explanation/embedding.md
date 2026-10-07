---
title: Seagull inside other tools
description: Embedding gives another tool control over input file systems, configuration,
  diagnostics and output publication. The tool can use generation without adopting
  the CLI's user interface.
section: Explanation
subsection: Design
order: 1200
---

# Seagull inside other tools

Embedding gives another tool control over input file systems, configuration, diagnostics and output publication. Editors can request analysis without publishing files, while build tools can decide when a checked result becomes part of a project.

## Adapt at public boundaries

The root package's generator runs the pipeline and reaches each replaceable part through a port. A schema source (`SchemaSourcePort` in `sdk/schema`) changes where its catalogue comes from, and an emitter (`EmitterPort` in `sdk/emit`) changes the output API. A dialect (`Dialect` in `sdk/dialect`) describes the SQL vocabulary: it is one of the built-in engines, configured through its options, since dialects written outside Seagull are not supported yet. A preset changes generated names and type defaults. These boundaries let an integration replace the part it needs while sharing the rest of the compiler.

Piko uses that boundary to retain its directive vocabulary and generated API. A standalone project can use the default preset. Keeping vocabulary and schema concerns separate avoids making a naming change into a compiler fork.

## Preserve information across the boundary

Source locations, severities and stable diagnostic codes let an editor or CI interface present useful feedback. Collapsing them into a generic error loses that connection to the user's SQL. Operational errors and diagnostic errors also mean different things: one stopped the work, while the other describes problems found during it.

The host controls resource policy through cancellation and limits. The generator's copy of a schema-source catalogue isolates its overrides from a shared cache. Output publication is another deliberate boundary: generation can be reviewed before the writer replaces files or prunes stale output.

See the [generation workflow](../how-to/generate-from-go.md), [generator API](../reference/generator-api.md) and [custom schema sources](../how-to/custom-schema-source.md).
