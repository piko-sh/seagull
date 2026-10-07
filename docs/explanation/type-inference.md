---
title: How inference works
description: Seagull infers types from schema objects and the expressions in each
  query. It resolves columns through aliases and scopes before mapping SQL types into
  Go.
section: Explanation
subsection: Design
order: 1130
---

# How inference works

A query's Go types come from its SQL expressions, not just the tables it reads. Seagull
first resolves names within their scopes, computes SQL types and nullability, then maps
those results into Go types. This separation lets the same analysed query target more
than one driver API.

## A result is not a table row

Consider a book's optional author:

```sql
SELECT b.title, a.name
FROM books b
LEFT JOIN authors a ON a.id = b.author_id;
```

Even if `authors.name` is `NOT NULL`, the join can find no author. The query's name field
therefore needs to represent NULL, usually as `*string`, while the table model keeps a
plain `string`. CTEs and derived tables carry the same distinction: their projections
are resolved in the scope where they are used.

Expressions can introduce NULL too. A scalar subquery may find no row; most aggregates
over no rows and a `CASE` without `ELSE` are nullable. Ordinary comparisons follow SQL's
three-valued logic, so comparing a nullable value can produce NULL. `IS NULL` always
produces a non-null test, and `COALESCE` can give a non-null alternative. These rules
explain why a result struct may differ from a table model even without an outer join.

Set operations combine the types of their branches. A cast changes the SQL type before
Go mapping, while a dialect supplies engine-specific rules for operators and functions.
For the precise nullability and collection rules, see [type mapping](../reference/type-mapping.md).

## Overrides are assertions

An SQL type override changes what the analyser believes the expression returns. A Go
type override changes how that SQL value is represented. Neither converts stored data
nor implements scanning for a custom type. Declaring a value non-null is an assertion
that must hold when the query executes.

A direct column projection retains its catalogue identity and its overrides. Computing
`lower(status)` produces a new expression instead, so its type is inferred from the
function rather than the `status` column's Go override. CTEs and derived tables can lose
that identity too. This is why [override scope](../reference/type-mapping.md#where-each-override-applies)
matters alongside precedence.

[Schema inspection](../how-to/inspect-schema.md) exposes the inferred types before you
choose an override. [Analysis guarantees](analysis-guarantees.md) explains the limits of
inference without a database.
