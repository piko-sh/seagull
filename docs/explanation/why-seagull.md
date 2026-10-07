---
title: Why Seagull
description: "Seagull keeps SQL as the description of database work and generates the repetitive Go needed to bind inputs and scan results."
section: Explanation
subsection: Design
order: 1100
---

# Why Seagull

Seagull keeps SQL as the description of database work and generates the repetitive Go needed to bind inputs and scan results. This fits applications whose authors want to review query plans and database-specific SQL without maintaining matching row structs and scan lists by hand.

## SQL remains the source

Directives are comments. Tables, joins, projections and writes remain SQL that can be investigated with database tools. A query change can therefore be reviewed alongside the resulting Go API: adding a nullable projection changes the result type, while renaming a parameter can change its caller-facing name.

Generation moves some mistakes earlier. Unknown tables and columns, invalid directives and incompatible result shapes can be reported before an application connects to a server. It cannot establish whether a query meets a business requirement or whether a deployment has applied the expected migrations.

## A tool and a library

The CLI is useful for application development. The same analysis is available to editors and frameworks through the root package. Presets let an integrating framework retain its own vocabulary without duplicating the analyser. Piko uses this boundary; standalone users do not need Piko.

Generated query packages contain their runtime helpers directly. This makes their behaviour inspectable and avoids linking the generator merely to execute queries. Embedding migrations is a separate choice and adds the migration runtime deliberately.

See [the pipeline](from-sql-to-go.md), [generated runtime behaviour](generated-runtime.md) and [installation](../get-started/install.md).
