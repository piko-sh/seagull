---
title: Read-only analysis and replicas
description: With the database/sql emitter, NewWithReplica gives generated queries
  a reader and a writer. Analysis determines which connection each query should use.
section: Explanation
subsection: Design
order: 1180
---

# Read-only analysis and replicas

Replica routing moves suitable reads away from the writer, but requires a decision about effects as well as a policy for consistency. With database/sql, `NewWithReplica` supplies the two connections; analysis chooses which each generated query uses.

Read-only is not determined by the first keyword. A SELECT can write through a CTE or function, and a row-locking SELECT needs the writer even when it changes no stored value. A `readonly` directive is an explicit assertion overriding that analysis.

## How a function call is routed

Function data access describes effects; volatility describes whether repeated evaluations can differ. A volatile function whose body only reads can therefore be suitable for a reader, while a sequence operation can require the writer. Treating volatility alone as the routing rule would confuse these separate properties.

The analyser uses declarations, supported function bodies, built-in catalogues and custom-function configuration. Calls through other functions carry their effects too. Analysis does not execute SQL, so its conclusion depends on the declarations and supported information available to it. The [routing reference](../reference/directives.md#reader-and-writer-routing) defines the exact rules and overrides.

A function body is judged by everything it runs, not by the statement that gives its result. A SQL body is analysed statement by statement, and one `DELETE` before its final `SELECT` makes the function writing, as does reading a writing table-valued function in `FROM`. A procedural body is scanned for the statements it starts: `EXECUTE` of dynamic SQL counts as writing because no analysis can see what the string will hold, and so do `CALL`, `COPY`, `REFRESH MATERIALIZED VIEW`, `LOCK`, `NOTIFY`, `GRANT`, `REVOKE` and the maintenance and labelling statements, while a column that merely shares one of those names does not. Each of these errs towards the writer, so a function that only reads but looks otherwise is moved back with `sgl.migration(readonly: true)` above its `CREATE FUNCTION`, or by declaring its data access, such as PostgreSQL's `STABLE` or MySQL's `READS SQL DATA`. A query nested too deeply to check every call it makes goes to the writer for the same reason, with a Q054 warning.

### Why an unknown function goes to the writer

An unknown or unmatched function call supplies no reliable effect contract. Sending a read to the writer costs replica capacity; sending a possible write to a reader can fail or modify a database the primary never sees. Seagull chooses the writer and reports Q005 rather than assuming the call is harmless.

The same uncertainty affects the result type. Declaring or correcting the function resolves both questions; a routing override alone does not supply a missing type. The [replica guide](../how-to/read-replicas.md) and [custom-function guide](../how-to/custom-functions.md) describe those actions.

## Routing does not provide consistency

A replica can lag a committed write. Routing a later SELECT to it therefore does not imply that the read observes that write. Workflows requiring read-after-write consistency need the writer or an application policy appropriate to the database.

A transaction is another consistency boundary: its queries must use the transaction's connection. Replica routing does not spread one transaction across connections. pgx and native ClickHouse do not generate the same replica constructor.

An incorrect override can defeat the analysis. Reviewing it against the SQL's effects and testing actual deployment roles follows from making that assertion explicit, rather than treating the constructor as a safety check on arbitrary SQL. An `sgl.migration(readonly: ...)` override covers only the `CREATE FUNCTION` or `CREATE PROCEDURE` directly below it, so an overload that writes is not routed to a reader because another overload of its name was marked.
