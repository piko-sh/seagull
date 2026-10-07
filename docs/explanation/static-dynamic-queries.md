---
title: Static and dynamic queries
description: Static queries fix SQL structure at generation time. Parameters carry
  values at execution time. Dynamic queries let selected parts of the structure vary
  within a generated contract.
section: Explanation
subsection: Design
order: 1160
---

# Static and dynamic queries

A static query fixes SQL structure during generation and supplies values at execution. Dynamic queries allow selected parts of that structure to vary inside a generated contract. The distinction matters because a value and a piece of SQL require different trust boundaries.

## Binding values is not choosing identifiers

A placeholder can carry a value, but it does not stand for an arbitrary table name, operator or SQL fragment. A runtime builder keeps these choices within allowed projected columns, operators and directions, and sends the values separately as bound parameters.

The projection is part of the application's input boundary. Joining a table does not expose all its columns to dynamic filters, and selecting a computed expression does not automatically give it a supported underlying identifier. The query author decides which data the generated contract exposes.

## Different helpers have different failure contracts

A runtime builder reports invalid structural choices at the terminal operation. A sortable parameter instead drops an unrecognised choice. The former supports caller-composed searches with visible validation failures; the latter limits one ordering choice while retaining its established fallback behaviour. An application deciding whether bad sort input is an error needs to account for that difference.

Optional parameters express nullable input, not the intended meaning of NULL. The SQL still defines whether a missing value disables a filter or has another meaning. A slice input similarly changes binding and list shape within the query's declared contract rather than authorising arbitrary SQL.

## Preserve base conditions when composing filters

A caller's added predicate must narrow the base query, including when the original WHERE contains OR. The runtime builder wraps that condition and joins new predicates with AND, so the application's filter cannot accidentally escape a condition such as excluding claimed items.

Counting also depends on query structure. For grouped or distinct results, counting underlying table rows would answer a different question. A count rewrite can therefore wrap the original result shape, and some queries have no supported rewrite at all.

The [runtime builder reference](../reference/runtime-builder.md) gives method availability and errors. The [runtime-query guide](../how-to/runtime-queries.md) and [optional-filter guide](../how-to/optional-filters.md) show how to apply those contracts.
