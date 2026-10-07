---
title: Check SQL and generated code in CI
description: Pin the module tool, run config validate, vet --strict, fmt --check and
  generate --check, then compile and test the application. Use GitHub or SARIF output
  when the CI system consumes it.
section: How-to guides
subsection: Tooling
order: 720
---

# Check SQL and generated code in CI

Run Seagull's checks before compiling the application so a SQL change cannot leave committed Go output stale. Start with a configured project and a [pinned module tool](../get-started/install.md#install-a-published-version).

## Add the checks to the build

Run these commands from the directory containing `sgl.yaml`:

```sh
go tool sgl config validate
go tool sgl vet --strict
go tool sgl fmt --check
go tool sgl generate --check --diff
go test ./...
```

`generate --check` writes nothing. It exits 1 when files differ, and `--diff` shows the change. Regenerate locally, review the SQL and generated API together, then commit the output.

`vet --strict` also fails on warnings. If an upgrade adds a diagnostic, resolve it or review the new behaviour before updating the pinned version.

## Report findings to the CI service

For GitHub Actions annotations, use:

```sh
go tool sgl vet --strict --format github
```

For a service that consumes SARIF, write the same findings to an artefact:

```sh
go tool sgl vet --strict --format sarif > seagull.sarif
```

Configure that service to collect the file even when vet fails. The [CI example transcript](../../examples/tooling/042_ci_pipeline/transcript.txt) shows annotations and a stale-output diff from real commands.

## Test execution separately

Run the application's database tests against a disposable database with the expected migrations applied. Generation checks SQL and types; the tests check execution through the real driver.

A generation run can write a partial package when one query has an error, then exit 1. Keep the failure visible in CI. When the CI system cancels a job, it sends `sgl` SIGTERM and `sgl` exits 143 rather than the 130 of Ctrl-C, so a script that treats 130 as an interrupted run should accept both. See [troubleshooting](troubleshoot.md) for query errors and [the CLI reference](../reference/cli.md#exit-codes) for exit statuses.
