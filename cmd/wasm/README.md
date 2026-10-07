# cmd/wasm

This directory contains the WebAssembly runtime for the playground on seagull.sh
and the native `inspect` command that prepares JSON for the site at build time.
Both share the `session` package. Run the shell commands below from this directory.

| Path                | What it is                                                                        |
|---------------------|-----------------------------------------------------------------------------------|
| `internal/session/` | One playground run: in-memory project in, JSON-ready response out                 |
| `internal/bridge/`  | Each bridge function: argument limits, request decoding and JSON, tested natively |
| `main.go`           | The `js && wasm` entry point that sets `self.seagull` in a Web Worker             |
| `inspect/`          | Native command: example responses, starters, diagnostic codes and dialects        |

The generator is assembled from `sgl.yaml` by `cmd/internal/assemble`, the package `sgl`
itself uses. Matching supported projects and settings produce the same generated files;
`TestPlaygroundMatchesTheCommittedExamples` in `internal/session/session_test.go` checks
that output against committed example variants.
Starters come from `cmd/internal/starter`, and diagnostic text uses `seagull.Present` and
`seagull.WriteText` from the root package, as the CLI does. Like `cmd/sgl`, the runtime
is built on the root package and the `sdk/` kit; of `internal/` it uses only the logging
leaf, to log a recovered panic's stack the way the library does.

## Build

```bash
GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o seagull.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
```

Serve the WASM file with HTTP compression. Measure download size and start-up time against
the toolchain and browser versions you deploy before adding a separate optimisation step.

## The JavaScript surface

In the worker, load `wasm_exec.js`, then:

```js
self.seagullReady = () => postMessage({ ready: true });
const go = new Go();
const { instance } = await WebAssembly.instantiateStreaming(fetch("seagull.wasm"), go.importObject);
go.run(instance);
```

Once the Go runtime has started it sets `self.seagull` and calls `self.seagullReady()`.
Every function is synchronous and takes and returns strings (JSON unless noted).
Failures come back as `{"error": "..."}`, with `internal` set for an internal playground failure.
A panic never escapes: its stack is written once to the console (`console.error`), and the
answer says only what panicked.

| Function            | Returns                                                          |
|---------------------|------------------------------------------------------------------|
| `version()`         | `{version, revision, goVersion}`                                 |
| `init()`            | `{protocol, version, dialects, defaultDialect, limits}`          |
| `dialects()`        | `[{name, family, placeholder, drivers, preparedDrivers, options, returning, returningOn}]` |
| `generate(request)` | the response; see `session.Response`                             |
| `explain(code)`     | `{code, title, severity, explanation}`; `code` is plain text     |
| `format(request)`   | `{source, error}` for `{source, dialect, canonical, prefixes}`   |
| `starter(dialect)`  | `{files: {path: content}}`, what `sgl init` writes               |

A generate request is `{"v": 1, "files": {path: content}, "override": {...}}`.
Requests are limited to 64 files, 256 KiB each, 1 MiB in all and 1,000 analysed queries.
The bridge measures each argument before copying it into Go and refuses a generate
request longer than 2 MiB + 64 KiB of JSON, a format request longer than 512 KiB + 64 KiB,
and a code or dialect name longer than 256 bytes, as a failure of the request rather
than an internal one. A format request's source may be at most 256 KiB with at most 16
prefixes, and its dialect, canonical prefix and each prefix at most 64 characters. An
override's `structTags`, `packageNames` and `searchPath` may hold at most 64 entries
each, and each of its strings at most 256 characters. Parser limits that `sgl.yaml`
raises (`dialect_options.max_parse_depth`, `max_tokens_per_statement` and
`max_type_parse_depth`) are lowered to the dialects' defaults of 256, 100,000 and 64,
with a warning in `config.warnings`. `sgl.yaml` is read when present, and only its first
database is generated; without it the project uses `sgl`'s defaults (SQLite,
`db/migrations`, `db/queries`, package `store`, `database/sql`).

The browser builds schemas from migrations. A configured `schema_from` is ignored with
a warning. By default an error-severity query diagnostic stops code generation;
`override.skipFailingQueries: true` enables partial generation, matching the CLI policy.

Non-empty string overrides replace configured strings; an empty string leaves the value
unchanged. Omitted booleans leave settings unchanged, while explicit `false` replaces
them. Supplied maps and lists replace the whole value. The response's `config` shows the
resolved settings:

| Override field | Replaces |
|---|---|
| `dialect`, `driver`, `prepared` | the dialect, `go.driver` and `go.prepared` |
| `skipFailingQueries` | Enables partial generation; there is no matching `sgl.yaml` key. |
| `groupByUseNulls`, `ansiQuotes`, `searchPath` | The matching `dialect_options` fields, when supported by the selected dialect. |
| `nonNullElements`, `keepStars`, `emptySlices`, `docComments`, `reuseModels`, `wrapErrors` | `go.types.non_null_elements`, `go.keep_stars`, `go.empty_slices`, `go.doc_comments`, `go.reuse_models` and `go.wrap_errors` |
| `interface`, `interfaceName` | `go.interface.enabled` and `go.interface.name` |
| `jsonCase`, `jsonOmitEmpty`, `jsonOmit` | `go.json_tags.case`, `omitempty` and `omit` |
| `structTags`, `packageNames` | `go.struct_tags` and `go.types.package_names`, whole |

An unsupported dialect override is omitted with a warning in `config.warnings`.
Invalid settings, such as an unknown `jsonCase`, fail the request with a reason in
`error`. Custom CLI registrations are not available in this runtime.

For partial generation, the response sets `partial` and marks rejected queries `skipped`.

Each query of the response carries its header's `description`, the struct its rows are
read into as `rowType` and which struct that is as `rowKind` (`own`, `model` for a
table's model, `shared` for a row type queries share). The catalogue shows each table's
and column's `comment`, a partition's `partitionOf` and a sequence's `columns`.

## Checks

```bash
go vet ./... && go test ./...
PATH="$(go env GOROOT)/lib/wasm:$PATH" GOOS=js GOARCH=wasm go test ./...
GOOS=js GOARCH=wasm go build -o seagull.wasm . && \
  node "$(go env GOROOT)/lib/wasm/wasm_exec_node.js" seagull.wasm -selftest
```

The native run tests the bridge in `internal/bridge`; the `js/wasm` run also tests
`main.go`, registering the bridge in Node and running the self test there. From the
repository root, `make test-wasm` runs these checks as CI does, and `make build-wasm`
builds the runtime into `bin/wasm`, shrinks it with `wasm-opt` and compresses it with
gzip and brotli, skipping a step whose tool is not installed.

## inspect

```bash
go run ./inspect -batch ../../examples -out site/examples   # one inspect.json per project
go run ./inspect -starters site/starters                    # one DIALECT.json per dialect
go run ./inspect -codes -out site/codes.json                 # every code, with an example where reproduced
go run ./inspect -dialects                                   # the dialects, to stdout
```

Without `-out`, `-batch` writes `inspect.json` beside each project's `sgl.yaml`.
Timings are left out unless `-timings` is given, so the output is stable.

Each reproduction in `inspect/codes.go` is a dialect's `sgl init` starter with its query
file replaced, so `-codes` gives every code the dialect it was reproduced with. A code
no short project can raise carries an `unreproducible` reason instead of an example.
