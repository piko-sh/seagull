---
title: Generated code at runtime
description: Generated methods use a connection supplied by the application. They
  do not choose credentials, create the application's pool or decide its readiness
  policy.
section: Explanation
subsection: Design
order: 1170
---

# Generated code at runtime

Generated queries use a connection supplied by the application. This keeps pool configuration, credentials and readiness decisions with the code that owns the deployment, while generated methods concentrate on binding inputs and scanning results.

## Ownership stays visible

A connection can outlive a query package or be shared by several packages. Giving generated methods that connection avoids a hidden pool with its own credentials and shutdown policy. The connection's owner closes it.

The same boundary applies to prepared wrappers, transactions and streaming rows. The wrapper has a lifetime the application chooses; a transaction-bound Queries value uses the transaction its owner supplied. Streaming methods close rows at the end of iteration, but an error can arrive after earlier rows have been consumed, so the caller still decides whether the result is complete. A runtime builder holds no context of its own: the query's method only gathers its parameters, and each terminal takes the context the query runs with, so cancellation belongs to the call that does the work.

## Preparation is a runtime decision

Emitting `prepared.go` makes preparation available without performing database work during generation. Calling `Prepare` at startup lets an application check its static statements early; using the wrapper later reuses them. When one statement fails to prepare, `Prepare` closes those it had already prepared before returning the failure, so a failed start leaves nothing open on the server for the application to clean up. Runtime-shaped queries cannot use one fixed prepared statement and run through the underlying connection instead.

Transaction-bound prepared statements are still bound per call. Caching those bindings
would change errors after commit or rollback from `sql.ErrTxDone` to a closed-statement
error; preserving that contract takes precedence over removing those allocations.

This distinction explains why the CLI can make prepared-file generation opt-in while the library preserves its earlier default. Both expose the same runtime choice to the application.

## Schema changes meet deployed code

A rolling deployment can run a migration before every old process has stopped. If an old SELECT star suddenly returns another column, a scanner expecting the generated fields can fail. Explicit column lists keep that process's projection stable until its code is regenerated.

Seagull therefore expands stars where the analysed columns can be named safely. Stars over some derived sources remain as written, and `KeepStars` deliberately preserves them, so the protection is bounded by the query and style. Removing or changing a selected column still requires deployment coordination; column expansion is not a general schema-compatibility guarantee.

## Generated helpers become application code

The query package contains the helpers it needs and does not import the Seagull generator. Driver and mapped-type dependencies remain ordinary application dependencies. Complex-value adapters and runtime-filter conversions can use reflection even though ordinary row methods scan into known fields. Those helpers check what reaches them at run time as well as what generation checked: the runtime filter refuses a column expression holding a backslash or a NUL, and a database/sql parameter conversion refuses a value that nests more than 32 levels deep or refers to itself, returning an error rather than writing unsafe SQL or exhausting the stack.

Regeneration can change a parameter struct or result type, and a driver change can change scan behaviour while SQL stays the same. Reviewing generated changes and testing the real driver follows from treating that package as application code. Importing `sdk/migrate` and a `migrate_backend_*` provider is a separate, deliberate step that adds the migration runtime.

See the [generated API](../reference/generated-go.md) for exact file contracts and the [preparation guide](../how-to/prepared-statements.md) for setup.

## Allocation boundaries

Many-row and streaming methods reuse scan destinations for built-in scalar mappings.
Scratch storage is created after the first successful `Next`, so an empty result creates
none. Each successful row is copied into the result or yielded value. Custom scanners,
overrides and converters keep independent storage per row because they may retain their
receiver's address. An absent supported outer embed allocates no embed object; a present
embed receives its own object. Driver allocations, returned slices, strings and nullable
values still have their normal cost.

Grouping uses indexes into the output slice and preserves first-seen order. Native
ClickHouse bindings use descriptors produced during generation. pgx copy operations reuse
the callback's argument slice, while pgx batches keep separate arguments for queued rows.
Runtime builders reserve the final argument buffer once and preserve anonymous placeholder
order. JSON columns use typed decoding for ordinary documents and retain the fallback
needed for null checks and custom decoders.

The reproducible allocation checks generate fresh code: in `tests/facade`, run
`SGL_PERFORMANCE_BENCH=1 go test -run '^TestGeneratedRuntimePerformance$' -v`.
Generation benchmarks use `go test -run '^$' -bench '^BenchmarkGenerate$' -benchmem`
in the same directory.
The runtime fixtures isolate generator overhead with mock rows; real-driver integration
suites cover conversion and ownership semantics. These are separate measurements from
network latency and server execution time.

## Measured costs

The implementation was measured with Go 1.27.0 on linux/amd64, an Intel Core Ultra 9
285K, and the default `GOMAXPROCS=24`. The generation fixture reuses a generator and
includes migration replay, 1,000 queries and emission, with 100 queries per file. It
excludes writing files. Median generation time across three runs fell from 129.2 ms to
99.3 ms; allocated bytes fell from 130.8 MB to 82.0 MB per generation. Those are about
23% and 37% reductions respectively.

| Fixture | Earlier allocations | Current allocations |
|---|---:|---:|
| pgx, collecting 1,000 scalar rows | 2,010 | 12 |
| database/sql, collecting 1,000 scalar rows | 1,016 | 17 |
| JSON column containing 1,000 integers | 3,040 | 14 |
| Native ClickHouse append helper with existing arguments | 10 | 1 |
| Runtime filter, ordering, limit and offset builder | 22 | 11 |

A second sweep removed temporary clause-prefix strings from runtime builders. Against
the first pass, the 100-value `IN` fixture fell from 109 to 108 allocations and from
6,360 to 5,784 allocated bytes. Returning contiguous SQL directly and trimming directive
ranges in place also removed about 20,000 allocations per 1,000-query generation
(1,020,544 to 1,000,546). Alternating before/after runs showed no clear generation-time
change, so these are allocation improvements rather than a further speed claim.

The pgx streaming fixture uses two allocations for both one and 1,000 rows, and none
for zero rows. Allocation ceilings in the tests allow small compiler variations:
20 for pgx collection, 25 for database/sql collection, 20 for the JSON fixture and one
for native ClickHouse helper work. These figures describe the fixtures above; drivers,
custom types and application work can add allocations.

On the same 1,000-query fixture, `GenerateDetailed` took about 104 ms and 84.9 MB,
compared with 137 ms and 124.3 MB for separate generation, vetting and catalogue calls.
Run `go test -run '^$' -bench '^BenchmarkDetailedGeneration$' -benchmem` in `tests/facade` to compare them.

## Standard-library review

The performance work reviewed the ten major releases from Go 1.18 through 1.27. The
repository already requires Go 1.27; no toolchain upgrade is needed for these changes.

| Release | Relevant additions and their application |
|---|---|
| [1.18](https://go.dev/doc/go1.18) | `Cut`, `Clone` and reusable reflection map iterators. Reusing map-entry destinations was measured and rejected here because it increased allocations and changed addressability. |
| [1.19](https://go.dev/doc/go1.19) | `fmt.Append*` allows caller-owned output buffers. Numeric hot paths use the more specific `strconv.Append*` functions. |
| [1.20](https://go.dev/doc/go1.20) | `CutPrefix`, `CutSuffix`, `bytes.Clone` and `token.FileSet.RemoveFile`. The companion cache fix removes parsed file records before pooling file sets. |
| [1.21](https://go.dev/doc/go1.21) | `slices`, `maps`, `clear` and `sync.OnceValues`. Prepared helper sources use `OnceValues`; cleared scratch storage preserves row ownership. |
| [1.22](https://go.dev/doc/go1.22) | `reflect.TypeFor` and `slices.Concat`; the existing typed reflection and package-name planning can use these directly. |
| [1.23](https://go.dev/doc/go1.23) | Iterators and `sync.Map.Clear`. Cache reset still needs synchronisation with insertion and accounting; `unique.Make` does not impose a strict size bound. |
| [1.24](https://go.dev/doc/go1.24) | `SplitSeq`, `FieldsSeq`, append interfaces and `B.Loop`. Directive spans and field documentation use iterator scans without split slices. Benchmarks use `B.Loop`. |
| [1.25](https://go.dev/doc/go1.25) | `reflect.TypeAssert` can avoid interface boxing. ClickHouse time recognition uses it. Compiler escape improvements do not remove the need to measure generated code. |
| [1.26](https://go.dev/doc/go1.26) | `errors.AsType` avoids a separate target variable; the recovering SQL lexer uses it. New reflection iterators were considered where ordering and ownership permit. |
| [1.27](https://go.dev/doc/go1.27) | JSON implementation and small-allocation improvements are included in the benchmark toolchain. The JSON helper retains its established decoding contract. |
