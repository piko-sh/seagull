---
title: What analysis can guarantee
description: Seagull checks the SQL it understands against the schema model and declared
  capabilities. Its output is evidence about structure and types, not proof that every
  future execution succeeds.
section: Explanation
subsection: Design
order: 1140
---

# What analysis can guarantee

Seagull checks the SQL it understands against a schema model and the dialect's declared capabilities. The result establishes facts about structure and types; database execution still depends on runtime conditions.

## Failures that remain at runtime

A correctly typed query can violate a constraint, lack permission, time out, lose its connection or race another transaction. A deployment may have a different schema because a migration was not applied. A function declaration may describe an extension incorrectly even when the generator accepts it.

Statement recognition, type inference, emitter support and execution on a particular server version answer different questions. Compilation checks that the generated API and its callers fit together. A database integration test checks the driver's binding and scanning behaviour against representative values and the actual engine.

## Diagnostics describe the available evidence

Diagnostics preserve source locations, severities and stable codes so a tool can show where analysis found a problem. An override supplies a declaration where inference needs help; it does not demonstrate that the server behaves as declared. A warning therefore remains useful even when generation succeeds.

Operational failures, such as an exceeded input limit or cancellation, stop the analysis itself. SQL diagnostics describe what analysis found. Keeping those channels separate lets a caller distinguish incomplete work from a completed check that found problems.

Analysis is bounded, so hostile or runaway input cannot hold up a run. A dialect refuses a statement nested past its parse depth or longer than its token budget. The analyser follows nested queries to 256 levels in all; past that it reports [Q054](../reference/diagnostics.md#q054), leaves the rest untyped and treats the query as one that may change data, so it is never routed to a replica on evidence it did not read. One query reports at most 1,000 diagnostics and summarises the rest in [Q066](../reference/diagnostics.md#q066), at the highest severity it left out, so a query whose errors were cut off still fails. A panic in Seagull, a dialect or code the host supplies is reported as [Q070](../reference/diagnostics.md#q070) on the statement it was handling, with its stack logged once, rather than ending the run.

Analysis, compilation and database tests complement one another. None establishes that a query implements the application's intended policy; that requires application tests.

See [errors and diagnostics](../reference/errors-observability.md), [troubleshooting](../how-to/troubleshoot.md) and [testing queries](../how-to/test-queries.md).
