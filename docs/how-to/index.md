---
title: How-to guides
description: Choose a guide by the task you need to complete. These guides assume
  you already have a configured project.
section: How-to guides
order: 0
---

# How-to guides

Choose a guide by the task you need to complete. These guides assume you already have a configured project.

## Connections

- [Run queries in a transaction](transactions.md)
- [Route reads to a replica](read-replicas.md)
- [Use prepared statements](prepared-statements.md)
- [Name and trace generated queries](trace-queries.md)

## Dialects

- [Use PostgreSQL-specific queries](postgres.md)
- [Use TimescaleDB hypertables](timescaledb.md)
- [Use continuous aggregates](continuous-aggregates.md)
- [Use ClickHouse MergeTree tables](clickhouse.md)
- [Read ClickHouse arrays, maps and tuples](clickhouse-complex-types.md)
- [Run asynchronous ClickHouse mutations](async-mutations.md)
- [Read nested data with DuckDB](duckdb.md)
- [Write analytical DuckDB queries](duckdb-analytics.md)
- [Use MySQL and MariaDB](mysql-mariadb.md)
- [Use SQLite databases and extensions](sqlite.md)
- [Use CockroachDB](cockroachdb.md)

## Embedding

- [Generate SQL wrappers from Go](generate-from-go.md)
- [Vet and inspect a catalogue from Go](vet-catalogue.md)
- [Register custom SQL functions](custom-functions.md)
- [Customise generation with a preset](presets.md)
- [Generate from an existing database](live-schema.md)
- [Observe and limit generator work](observe-generator.md)
- [Provide a custom schema source](custom-schema-source.md)

## Extensions

- [Writing an emitter](write-an-emitter.md)
- [Build and test a custom emitter](custom-emitter-example.md)
- [Build a custom sgl binary](custom-cli.md)
- [Configure a built-in dialect](configure-a-dialect.md)
- [Implement a migration backend or executor](migration-backend.md)

## Interactive tools

- [Use the playground and open an example](playground.md)
- [Run an example locally](run-examples.md)

## Migrations

- [Run migrations at application start-up](startup-migrations.md)
- [Create, apply and roll back migrations](migration-lifecycle.md)
- [Apply seeds, namespaces and hooks](seeds-namespaces-hooks.md)
- [Run migrations from the command line](migrate-cli.md)
- [Configure migration locks and namespaces](configure-locks.md)
- [Recover an interrupted migration](recover-migration.md)
- [Investigate checksum mismatches](checksum-mismatches.md)
- [Roll back or target a migration version](rollback-target.md)
- [Use migration hooks and observability](migration-observability.md)

## Operations

- [Verifying releases](../VERIFYING.md)

## Projects

- [Add Seagull to an existing project](existing-project.md)
- [Configure multiple databases](multiple-databases.md)
- [Choose and configure a driver](choose-driver.md)

## Queries

- [Choose a query command](choose-query-command.md)
- [Name and type parameters](parameters.md)
- [Pass a slice to a query](slice-parameters.md)
- [Add optional filters](optional-filters.md)
- [Validate sorting and pagination](sorting-pagination.md)
- [Add bounded pagination](pagination.md)
- [Build queries at runtime](runtime-queries.md)
- [Query through CTEs, views and functions](ctes-views-functions.md)
- [Declare SQL functions in migrations](sql-functions.md)

## Results

- [Handle NULL and missing rows](null-and-missing-rows.md)
- [Embed joined results](embed-results.md)
- [Collect one-to-many results](one-to-many.md)
- [Stream rows and stop early](stream-rows.md)
- [Handle an optional single row](optional-one.md)
- [Tag, document and mock generated code](shape-generated-code.md)

## Tooling

- [Troubleshoot generation and execution](troubleshoot.md)
- [Check SQL and generated code in CI](ci.md)
- [Inspect queries and compare schemas](inspect-schema.md)
- [Format directives and change prefixes](format-prefixes.md)
- [Upgrade Seagull and regenerate](upgrade.md)
- [Test generated queries](test-queries.md)

## Types

- [Inspect SQL-to-Go types](inspect-type-mapping.md)
- [Use JSON and custom Go types](json-custom-types.md)
- [Work with enums, arrays and nested values](enums-arrays-nested.md)
- [Read arrays and lists](arrays.md)
- [Override SQL-to-Go types](override-types.md)

## Writes

- [Return inserted or updated rows](return-written-rows.md)
- [Write an upsert](upsert.md)
- [Batch writes and use COPY](bulk-writes.md)
