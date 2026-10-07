---
title: Choose and configure a driver
description: Choose the database first, then the Go API the application will call.
  Set go.driver to database/sql, pgx or clickhouse in that database's configuration.
section: How-to guides
subsection: Projects
order: 860
---

# Choose and configure a driver

Choose the database first, then the Go API the application will use. The generated query package and the migration CLI can use different driver integrations, so configure each for its own job.

## Choose the generated API

Use database/sql when the application benefits from its common interfaces, pgx for PostgreSQL-family native APIs, or clickhouse for the native ClickHouse API. Check the [driver matrix](../reference/driver-matrix.md) if you need prepared wrappers, replica routing, COPY or transaction helpers.

Set the choice in that database's configuration. For example, a PostgreSQL query package using pgx has:

```yaml
go:
  driver: pgx
  package: store
  out: internal/store
```

Use `database/sql` or `clickhouse` instead when that is your chosen API. The selected dialect must support the emitter.

## Connect the application with the matching driver

Add the driver dependency. With database/sql, import its registration package and open the registered driver name; with pgx or native ClickHouse, construct the matching connection or pool. Pass that connection to the generated `store.New`.

Run `sgl generate` after changing the emitter, then update the constructor and any changed method signatures. Compile the caller and test a nullable value, an empty result and a failed query against the actual driver. The [generated API reference](../reference/generated-go.md#queriergo) lists the accepted connection types.

The stock CLI's linked drivers are independent of this choice. A custom CLI must register and link the drivers it uses for migration or introspection; see [custom CLI composition](custom-cli.md).
