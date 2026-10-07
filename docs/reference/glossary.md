---
title: Glossary
description: "Terms used throughout Seagull’s documentation and public APIs."
section: Reference
subsection: Orientation
order: 1010
---

# Glossary

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
| Model | The struct `models.go` declares for a table, with a field per column. |
| Row struct | The struct a query's rows are read into: `<Name>Row` by default, or a model or a shared row type a `model:` or `row:` keyword names. |
| Type slot | The Go type one category of SQL values maps to, such as decimals or intervals, set under `go.types` or in a preset. |
| Type override | A Go type, element nullability or struct tags chosen for one SQL type or one column, in `sgl.yaml` or a `sgl.column` directive. |

A **schema source** builds a catalogue without migration replay. **Introspection** is a schema source that reads a database. A **generated header** identifies output that the writer owns and may prune. A **dirty migration** is an interrupted run whose recorded progress needs evaluation before resuming.
