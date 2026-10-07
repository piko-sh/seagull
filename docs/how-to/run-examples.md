---
title: Run an example locally
description: Clone Seagull and run an engine's examples module against the checkout.
  Each numbered directory is a small program with SQL, generated code and tests.
section: How-to guides
subsection: Interactive tools
order: 990
---

# Run an example locally

Start with a [prepared source checkout](../get-started/install.md#work-from-a-checkout). Each engine has a module with its own `go.work`, which builds it against the checkout, and each numbered directory contains SQL, generated Go and a runnable caller.

From the repository root:

```sh
cd examples/sqlite
go run ./001_first_query
go test ./001_first_query
```

SQLite uses an in-process driver. Server examples accept a `-dsn` argument; their tests use `SGL_EXAMPLE_DSN`. Set it to a disposable database for the chosen engine, then run that example's test. A skipped server test is not runtime verification. DuckDB requires its cgo driver and toolchain.

Read `example.yaml` for its entry query and expected diagnostics. The curriculum distinguishes a real variant from an equivalent twin, an intentionally omitted case, an unsupported case and a blocked case. These are not interchangeable support claims.

Use `make generate-examples-validate` to check generated output, equivalent variants and expected diagnostics, and `make test-examples` for the repository example modules. The [examples README](../../examples/README.md) gives the full execution environment.
