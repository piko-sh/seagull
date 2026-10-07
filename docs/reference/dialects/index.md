---
title: Dialect reference
description: Supported SQL, drivers and runtime behaviour for each dialect.
section: Reference
subsection: Dialects
order: 0
---

# Dialect reference

Choose the exact database family member used by the application. SQL recognition, inferred types, emitter support, migration execution and live introspection are separate capabilities.

- [PostgreSQL](postgres.md)
- [CockroachDB](cockroachdb.md)
- [TimescaleDB](timescaledb.md)
- [MySQL](mysql.md)
- [MariaDB](mariadb.md)
- [SQLite](sqlite.md)
- [DuckDB](duckdb.md)
- [ClickHouse](clickhouse.md)

The [driver matrix](../driver-matrix.md) compares generated APIs. All eight dialects generate without a running server. Built-in live introspection covers the PostgreSQL family (PostgreSQL, CockroachDB and TimescaleDB) and SQLite; adding a runtime driver does not add introspection. DuckDB database operations require a custom CLI linking its cgo driver.

The [known limitations](../limitations.md) record the places where a dialect reads SQL less precisely than its engine.

Spanner, BigQuery (GoogleSQL) and SQL Server have no built-in dialect, and third-party dialects are not supported yet.

A dialect either reads a query in full or reports [Q010](../diagnostics.md#q010).
Malformed or unsupported syntax is rejected at its source location, rather than
analysing a partial statement. The conformance suite checks this contract.
