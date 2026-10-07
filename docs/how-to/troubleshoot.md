---
title: Troubleshoot generation and execution
description: "Run vet, read the first source-located diagnostic, and ask explain for its Q-code."
section: How-to guides
subsection: Tooling
order: 710
---

# Troubleshoot generation and execution

Start with `sgl vet` and the first diagnostic's source location. Fix the SQL or configuration before investigating errors from executing generated code.

## Read a diagnostic

```sh
sgl vet --strict
sgl explain Q027
```

Replace `Q027` with the code vet reports. For example, a misspelt directive keyword gives a location and a suggested correction:

```text
broken/db/queries/tasks.sql:2:31: error[Q027]: unknown keyword argument "optinal" on sgl.param
   |
 2 | -- :title as sgl.param(title, optinal: true)
   |                               ^^^^^^^
help: did you mean "optional"?
```

Correct `optinal` to `optional` and run vet again. Errors exit 1. Warnings exit 0 unless `--strict` is set; hints do not fail vet.

A tool that embeds the generator reads the same explanation from the root package, `seagull.sh/seagull`: `seagull.LookupCode("Q027")` returns what `sgl explain` prints, and `seagull.Codes()` lists every code.

## Find a missing generated method

Check the generation command's exit status and diagnostics. If a query has an error, the CLI leaves that query out, writes the rest of the package and exits 1. A migration, schema or package-name error stops output for the whole database.

Fix the reported problem, regenerate and compile the caller:

```sh
sgl vet --strict
sgl generate
go test ./...
```

The [diagnostics example](../../examples/tooling/041_vet_and_diagnostics/transcript.txt) shows both a warning and partial generation. The [diagnostics reference](../reference/diagnostics.md) explains every code.

## Investigate a runtime failure

If analysis passes but a query fails when called, check the actual driver, connection and applied schema. Verify the parameter values and inspect the returned driver error. Constraints, permissions and cancellation are checked by the database at execution time.

For a changed schema, use [schema inspection](inspect-schema.md). For a missing row or NULL value, check the [generated query contract](null-and-missing-rows.md). For a custom value that will not scan or bind, check [type overrides](override-types.md).
