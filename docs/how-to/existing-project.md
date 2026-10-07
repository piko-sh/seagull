---
title: Add Seagull to an existing project
description: 'Add a configuration file and SQL source directories alongside the application.
  Do not run init over existing starter files: init refuses the whole operation when
  any target exists.'
section: How-to guides
subsection: Projects
order: 840
---

# Add Seagull to an existing project

Add SQL source directories and a generated package alongside your application. Keep the existing connection setup and handwritten data-layer logic.

## Configure one package

[Pin the CLI](../get-started/install.md), then create `sgl.yaml` with the actual database dialect and a dedicated output directory. Use the [project layout](../get-started/project-structure.md) as a starting point.

Put schema-building SQL in files such as `db/migrations/0001_schema.up.sql`. A reverse migration is optional. If your existing migration tool puts up and down SQL in one file, split them before giving them to Seagull; see [migration file layouts](../reference/migration-files.md).

For the PostgreSQL family or SQLite, you can instead [generate from the live schema](live-schema.md) when the database is the schema authority.

## Generate the first query

Add one query with an `sgl.query` header, then inspect the planned output:

```sh
sgl config validate
sgl vet --strict
sgl generate --diff
```

Review the method and result types. When they fit the caller, write the package:

```sh
sgl generate
go test ./...
```

Import the package and pass your existing connection to its constructor. Run the query against a disposable database with the expected schema, then add [CI checks](ci.md).

## Choose output ownership

Use a directory dedicated to the generated package. Keep handwritten files under names the generator does not produce. Current output filenames are replaced regardless of their old header; stale-file pruning uses recognised generated headers.

Review [output writing](../reference/output-writing.md) before moving files from another generator or changing legacy-header settings.
