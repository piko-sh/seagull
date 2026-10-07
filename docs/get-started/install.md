---
title: Install Seagull
description: Install sgl as a tool dependency so a project records the generator version
  it uses. The current source targets Go 1.27; check go version before starting.
section: Get started
subsection: Overview
order: 290
---

# Install Seagull

Install `sgl` as a tool dependency so a project records the generator version it uses. The current source targets Go 1.27; check `go version` before starting.

## Install a published version

From an existing Go module:

```sh
go get -tool seagull.sh/seagull/cmd/sgl@latest
go tool sgl version
```

`@latest` selects the release during installation; the resulting `go.mod` and `go.sum` pin it. Commit both files. For a reproducible upgrade, replace `latest` with the release tag selected for the project. Do not assume an alpha tag exists before the release is published.

For a global executable instead:

```sh
go install seagull.sh/seagull/cmd/sgl@latest
sgl version
```

The executable is placed in `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset. Add that directory to your shell's PATH. The rest of these docs use `sgl` for the executable; use `go tool sgl` when installed as a module tool.

`go install seagull.sh/seagull/cmd/seagull@latest` installs the same tool as `seagull`.

## Work from a checkout

The tutorials use a checkout so their scaffolding and generator match. Clone the
repository:

```sh
git clone https://github.com/piko-sh/seagull.git
cd seagull
```

Its dependencies, `safeconv` and `safedisk` included, come from the module proxy. Build
the CLI:

```sh
go build -o /tmp/seagull-sgl ./cmd/sgl
/tmp/seagull-sgl version
```

For the tutorials, [prepare the practice workspace](../tutorials/01-first-query.md#step-1-prepare-the-practice-workspace). The preparation helper supplies the module and connection code and builds an executable from this checkout. The committed checkpoints stay available for comparison.

## Release archives

Select the archive for your operating system and architecture from the project's releases. Follow [verify release downloads](../VERIFYING.md) before unpacking it into a directory on PATH. The version command reports library, generator and configuration versions as well as the CLI build.

## Database requirements

The SQLite tutorials use a pure-Go in-process driver. Server examples require their named server and a disposable database; a MySQL or MariaDB DSN must set `parseTime=true`, so the generated code reads dates and times into `time.Time`. The stock CLI generates DuckDB code but needs a custom binary with its cgo driver for DuckDB database operations. See the [driver matrix](../reference/driver-matrix.md).

Continue with [your first query](../tutorials/01-first-query.md).
