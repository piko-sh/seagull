---
title: Dialects, drivers and emitters
description: "A dialect understands SQL."
section: Explanation
subsection: Design
order: 1150
---

# Dialects, drivers and emitters

A dialect understands SQL. A driver connects to the database. An emitter writes Go that
calls a driver API. Seagull keeps these roles separate so a PostgreSQL query can generate
calls to database/sql or native pgx from the same analysis.

The library mirrors that split. A dialect is a `dialect.Dialect` from `sdk/dialect`, an
opaque handle to one of the built-in engines, which an engine provider or the registry
builds, such as `dialect_engine_postgres.New()`; dialects written outside Seagull are not
supported yet.
An emitter is an `EmitterPort` from `sdk/emit`, such as `emit_driver_pgx.New()`, and a
program can implement its own. The driver is neither: it stays the application's own
dependency, which the generated code calls.

## Compatibility has several dimensions

Accepting SQL establishes its analysis shape, not every driver's ability to read its
values or execute its command. A pgx `copyfrom` method uses PostgreSQL COPY and returns
a count. The database/sql version sends multi-row INSERTs and returns only an error.
ClickHouse's native API supplies no affected-row count, so `execrows` is refused.
These differences belong to the emitter and driver contracts.

The CLI makes a separate runtime choice when it opens a connection for migrations or
introspection. Setting `go.driver` chooses generated code; it does not link another
driver into the CLI. A [custom binary](../how-to/custom-cli.md) registers its runtime
drivers explicitly.

## Pure-Go generation versus execution

The core generator needs neither cgo nor a database server. DuckDB's parser can therefore
run in the stock CLI even though its runtime driver requires cgo. Database operations
need a binary built with that driver.

Related dialects can share a parser while retaining different contracts. CockroachDB and
TimescaleDB build on PostgreSQL; MariaDB builds on MySQL. Their own type, function and
migration rules still matter, so projects select the exact dialect.

The [driver matrix](../reference/driver-matrix.md) compares generated APIs, and the
[dialect references](../reference/dialects/index.md) describe each engine's capabilities.
