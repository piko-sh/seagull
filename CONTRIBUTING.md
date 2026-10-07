# Contributing to Seagull

## Requirements

Go 1.27+. `make help` lists every target, grouped by area. `golangci-lint`
2.14.0 for `make lint-go`; `shellcheck` for `make lint-scripts`; `node`
for `make test-wasm`; Docker for the server-database integration suites.
`piko.sh/safeconv` and `piko.sh/safedisk` are ordinary module
dependencies resolved from the proxy, so no sibling checkout is needed.

---

## Quick start

```bash
git clone https://github.com/piko-sh/seagull.git
cd seagull
make check       # vet, lint, validators
make test        # the library and CLI suites
```

---

## Code organisation

The library lives under `internal/`, and the root package
`seagull.sh/seagull` and the kit under `sdk/` are the only ways out of
it. Layers import downwards: the tooling over `app` over the core stages
over the adapters (engines, emitters, schema sources) over the kits over
the contracts over the leaves; nothing under `internal/` imports `sdk/`
or the root. The `depguard` rules in `.golangci.yml` enforce that, so
`make lint-go` is what fails on a bad import. The
[architecture page](docs/explanation/architecture.md) has the pipeline
diagram, the layer table and the boundary rules.

`sdk/` has one area per extension point (`dialect`, `emit`, `migrate`,
`schema`, `observe`); each is a facade over `internal/`, with providers
named `<area>_<role>_<impl>` that forward to it and hold no logic. The
facades expose a deliberately small surface: every exported name is a
compatibility promise, so a facade names only what a caller uses or
implements, and a value a caller only reads stays reachable through the
field that holds it. `facade_closure_test.go` at the root checks that
every type a caller must write has a name. The rule for a new provider:
the implementation goes under `internal/` and its provider under `sdk/`,
and the facade gains only the names a caller needs. `sdk/dialect` names
no dialect port: a `Dialect` is an opaque handle to a built-in engine.

---

## Make targets

`make help` is the authoritative list; this table may lag it. Every target
is one line that delegates to a script under `hack/`, and the reasoning for
what a target does lives in that script's header comment.

| Target                                                       | What it does                                                                               |
|--------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `make check`                                                 | vet, both lint passes, module verification and every generated-file validator              |
| `make check-all`                                             | `check` plus `test` and `test-compile`                                                     |
| `make test`, `test-short`, `test-race`                       | the library and CLI suites, in the default, short and race lanes                           |
| `make test-golden`, `test-golden-update`                     | the golden suites; update with `-update`                                                   |
| `make test-compile`                                          | generate every case for every emitter, then `go vet` and `go build` the output             |
| `make test-integration`                                      | every suite under `tests/integration`; `ARGS=postgres` runs one                            |
| `make test-examples`, `test-tutorials`                       | every examples module and the tutorial checkpoints; the server engines skip without a DSN  |
| `make test-wasm`                                             | test the WASM runtime natively and under Node, then self-test it                           |
| `make build-wasm`, `build-wasm-clean`                        | build, optimise and compress the WASM runtime into `bin/wasm`, or remove it                |
| `make lint-go`, `lint-scripts`, `lint-vet`, `lint-vet-tags`  | golangci-lint (layering included), shellcheck, and vet on one or every build configuration |
| `make generate-all` and `generate-*` (each with `-validate`) | regenerate or validate each generated artefact                                             |
| `make go-mod-tidy`, `go-mod-verify`                          | tidy the go.mod files that can be tidied; checksums and tidiness where it is meaningful    |

### tests/

Unit tests and the dialect and pipeline goldens live beside their code.
The database suites live under `tests/integration`, one module per
database, behind the `integration` build tag, plus `compile`, which needs
no database; `tests/tutorials` holds the tutorial checkpoints. The root
package's behaviour tests live in `tests/facade`, a module of its own
that reaches the library only through the root and `sdk/`, so a plain
`go test ./...` misses them; `make test` runs them.

The API snapshots, the frozen compatibility corpus and the persisted-name
contract have flags of their own, so `make test-golden-update` never
re-records them. A diff in one is a breaking change.

### CI

CI runs every gate on every pull request and push to master: build and
unit tests, both lint passes, module verification, the generated-file
validators, the compile suite, the examples and tutorials, the
WebAssembly runtime and every integration suite against its real
database. Its unit tests run without `-short`, so the goldens and the
frozen compatibility corpus run before every merge. The race lane is
local: `make test-race`.

### hack/

Every Make target delegates to a script under `hack/`, organised by area:
`build/`, `generate/`, `go/`, `lib/`, `lint/`, `test/` and `wasm/`. Each
script sources `hack/lib/init.sh`, which sets the shell options, exports
`SEAGULL_ROOT`, changes to the repository root and loads the
`seagull::log::`, `seagull::util::` and `seagull::go::` helpers. Scripts
are found rather than listed by `make lint-scripts`, so a new one cannot
be added without being linted.

---

## Working on the analyser

Every change runs against the same nets. Before calling a step done:

1. `make test-golden`. State whether a golden diff is expected ("golden
   diff expected: none" or the list of cases), inspect each listed case,
   then re-record with `make test-golden-update`.
2. `make test-compile`, so every changed output still builds.
3. `make test-integration ARGS=sqlite` and `ARGS=postgres` at least, and
   the suite for every dialect the change touches.
4. `make lint-go`. The limits new code must meet: functions 90 lines or 60
   statements, cognitive complexity 15, cyclomatic 20, nesting depth 3,
   7 arguments, files 1000 lines, lines 200 characters.

---

## Conventions

- Minimise depth of nesting, prefer guard clauses.
- Prefer verbose code, no complicated boolean expressions.
- Seagull is exclusively written in British English.
- Always use field initialisation for structs (`Foo{bar: 1}`, not `Foo{1}`).
- Table-driven tests, with field initialisation for all test cases.
- No `goto`.

---

## Licence header

Every source code file needs this header.

```go
// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.
```

The anti-fascism statement is mandatory and non-negotiable.
