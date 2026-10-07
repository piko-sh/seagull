---
title: Configure multiple databases
description: Give each database a unique name, dialect, source directories and non-overlapping
  output directory.
section: How-to guides
subsection: Projects
order: 850
---

# Configure multiple databases

Give each database a unique name and its own SQL inputs and generated package. Output directories must not share or contain one another. A configuration may declare at most 256 databases.

```yaml
version: 1
databases:
  - name: operational
    dialect: postgres
    migrations: db/operational/migrations
    queries: db/operational/queries
    go:
      package: operational
      out: internal/operational
      driver: pgx
  - name: local
    dialect: sqlite
    migrations: db/local/migrations
    queries: db/local/queries
    go:
      package: local
      out: internal/local
```

## Generate one or both packages

Validate the complete configuration, then generate the package you are integrating:

```sh
sgl config validate
sgl --database local generate
```

Validation reports an `out` that several databases share once, naming the databases that write to it, and an `out` inside another with the nearest one that holds it.

Without `--database`, generation processes every entry. Check all committed output with:

```sh
sgl generate --check
```

Construct each generated package with the connection for its database.

## Select a database for runtime commands

Migration, seed and schema-diff commands operate on one database. Choose it explicitly:

```sh
sgl --database local migrate status --dsn file:local.db
sgl --database local migrate up --dsn file:local.db
```

Repeat for the other entry with its own connection string. These are separate runs; a failure in one does not undo changes in the other.

See [configuration](../reference/configuration.md) for per-database settings and [migration commands](../reference/cli.md#migrate-and-seed) for the runtime options.
