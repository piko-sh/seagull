---
title: Reproducible generation and upgrades
description: "Generation is easiest to review when the source SQL, configuration, tool version and emitter choices are explicit."
section: Explanation
subsection: Design
order: 1220
---

# Reproducible generation and upgrades

Generated output is easier to review when SQL, configuration, tool version and emitter choices are explicit. Pinning the generator prevents developers from silently producing different APIs from the same checkout.

## Output ownership

The result's filenames determine which existing files the writer replaces. Generated headers determine which additional, stale files it may delete. A dedicated generated directory therefore gives the clearest boundary; a hand-written file whose name collides with output can be overwritten even without a generated header.

Renaming or deleting a query can leave an old generated file that pruning removes. Changing the generated header also changes that pruning boundary, so adopting an earlier header is an explicit output migration.

`generate --check` compares output without publishing it. This supports CI because a stale result can fail the build while leaving regeneration to a reviewed change. Matching bytes still need compilation and relevant database tests.

## More than one version

The CLI, generated code, configuration schema and browser protocol have distinct versioned contracts. A `CodegenVersion` change signals that identical input may produce different Go. Driver upgrades can change binding and scanning independently of generation, while persisted migration names affect coordination between deployed versions.

Live introspection adds database state as an input. A pinned generator alone cannot reproduce output if that schema changes. A recorded or controlled schema is needed to make the result reproducible.

Read [compatibility](../reference/compatibility.md) before an upgrade and use the [upgrade procedure](../how-to/upgrade.md) to review the result.
