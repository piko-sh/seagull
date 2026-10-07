---
title: The schema catalogue
description: "The catalogue is Seagull's model of the schema, not a database connection."
section: Explanation
subsection: Design
order: 1120
---

# The schema catalogue

The catalogue is the schema model against which Seagull analyses queries. Migration replay applies supported DDL to that model, providing the tables, columns, views, types and functions needed for name and type resolution.

## Replay is not execution

Generation reads a migration's schema effects without creating tables or running application writes on a server. Analysing a function definition does not call the function. This separation makes ordinary generation possible without a database connection.

Replay still refuses what the engine would refuse, so the model never holds a schema the migrations could not produce. Creating a table under a name a table already holds is reported as [Q067](../reference/diagnostics.md#q067), and altering or dropping an object that does not exist, without `IF EXISTS`, as [Q068](../reference/diagnostics.md#q068); neither statement changes the catalogue. A part of a statement the dialect cannot analyse, such as a view body nested too deeply, is reported as [Q069](../reference/diagnostics.md#q069) while the rest of the statement applies, and a dialect that panics fails only its statement ([Q070](../reference/diagnostics.md#q070)). On SQLite and DuckDB, where a view follows the tables it reads, replay derives a view's columns again when a table or view it reads changes, and stops at a round limit with [Q071](../reference/diagnostics.md#q071) when views keep changing each other.

The migration runtime has a different responsibility: execute ordered changes, maintain history and coordinate concurrent runners. The paths share lexical rules so they agree on statement boundaries, while retaining different outputs and failure modes.

## Another source of schema truth

PostgreSQL-family and SQLite introspection, through `schema_source_postgres` and `schema_source_sqlite` under `sdk/schema`, build a catalogue from a live database. That supports projects whose database is the schema authority, but makes generation depend on connection permissions and the schema at the time of inspection. A clean source checkout alone no longer determines the result.

A custom source implements `SchemaSourcePort` from `sdk/schema` to adapt another schema representation. The source retains ownership of its catalogue; the generator clones it before preparing it for analysis. This permits shared caches without allowing overrides from one run to alter later runs. The source must keep a returned catalogue stable while it may be read.

See [live schema generation](../how-to/live-schema.md), [custom schema sources](../how-to/custom-schema-source.md) and [migration safety](migration-safety.md).
