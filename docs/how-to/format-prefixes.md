---
title: Format directives and change prefixes
description: Run fmt --check first, review fmt output, then use fmt -w to canonicalise
  comment directives. Configure accepted prefixes and choose the canonical prefix
  first in the list.
section: How-to guides
subsection: Tooling
order: 740
---

# Format directives and change prefixes

Use `sgl fmt` to normalise directive comments while preserving the SQL and argument order. The first configured prefix is the canonical spelling.

## Preview and apply formatting

Start with this directive from the [formatting example](../../examples/tooling/044_fmt_and_prefixes):

```sql
--   seagull.query( GetCustomer ,one )
SELECT id, name, email FROM customers WHERE id = ?;
```

Preview that file, then format the project's query and seed files:

```sh
sgl fmt db/queries/customers.sql
sgl fmt -w
sgl fmt --check
```

The directive becomes `-- sgl.query(GetCustomer, one)`. The final check exits 0 when every scanned directive is canonical, or 1 with the files still needing formatting. To list the changed files and rewrite them in one run, combine `-w` with `-l`, or with `--check` to exit 1 as well.

## Adopt another prefix

Accept the source prefix as an alias after your canonical prefix:

```yaml
directives:
  prefixes: [sgl, seagull, piko]
```

Run `sgl fmt -w` and inspect the diff. Without the alias, directives using `piko.` are left unread and draw [Q051](../reference/diagnostics.md#q051). With it, formatting rewrites them to `sgl.`.

Piko projects keep their framework's `piko.` vocabulary. This adoption step is for SQL you are moving into a standalone Seagull project.

To choose a different output spelling for every accepted directive instead, use:

```sh
sgl fmt --rewrite-prefix seagull db/queries/orders.sql
```

## Leave applied migrations stable

Formatting omits migrations unless `--include-migrations` is supplied. Changing an applied migration changes its checksum. Format new migrations before they are applied; see [checksum mismatches](checksum-mismatches.md) if an applied file has already changed.

The [tested transcript](../../examples/tooling/044_fmt_and_prefixes/transcript.txt) shows the complete adoption workflow. See [CLI formatting flags](../reference/cli.md#fmt) for path selection and preview options.
