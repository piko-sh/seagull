---
title: Project structure
description: Keep SQL inputs and generated output in separate directories. Keep handwritten
  application logic outside generated files.
section: Get started
subsection: Overview
order: 310
---

# Project structure

Keep SQL inputs and generated output in separate directories. Keep handwritten application logic outside generated files.

```text
app/
  go.mod
  go.sum
  sgl.yaml
  db/
    migrations/
      0001_tasks.up.sql
      0001_tasks.down.sql
    queries/
      tasks.sql
    seeds/
      0001_tasks.sql
  internal/store/
    querier.go
    models.go
    tasks.sql.go
  main.go
```

## Configure the paths

```yaml
version: 1
databases:
  - name: tasks
    dialect: sqlite
    migrations: db/migrations
    queries: db/queries
    seeds: db/seeds
    go:
      package: store
      out: internal/store
```

Paths in configuration are resolved against the configuration directory. `sgl` searches the working directory and its parents for configuration; `-c` chooses it explicitly. `init` refuses to overwrite existing starter files.

## Generated files are owned by the generator

`WriteFiles` replaces the filenames in the current result, then prunes stale files carrying a recognised generated header. A current output file is replaced regardless of its old header. Put handwritten helpers in separately named files without a generated header. Review `sgl generate --diff` before a large change. Commit generated code if your build consumes committed output, then enforce `generate --check` in CI.

Each configured database needs its own output directory. Output directories must not overlap. Separate packages also make it clear which connection a query expects.

For embedded files, directories are slash-separated paths inside an `fs.FS`, not operating-system absolute paths. Read [generator API](../reference/generator-api.md) for ownership and default-source rules.
