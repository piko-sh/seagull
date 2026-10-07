---
title: Writing an emitter
description: "Implement an emitter that turns analysed queries into generated source files, with the same SQL, names and checks as the built-in ones."
section: How-to guides
subsection: Extensions
order: 70
---

# Write an emitter

Build an emitter when generated query code must use a driver API the built-in emitters do not cover. To add one file to an existing emitter, follow the shorter [wrapper guide](custom-emitter-example.md).

## What you import

An emitter is written against the emit area of the `sdk` kit, `seagull.sh/seagull/sdk/emit`, and its provider packages:

| Package | Import path | What it gives an emitter |
|---|---|---|
| `emit` | `seagull.sh/seagull/sdk/emit` | `EmitterPort` and the optional ports (`ReserverPort`, `TypeDefaulterPort`, `CheckerPort` and `NullElementBinderPort`), the `Request`, the naming plan, and the query and Go type model |
| `dialect` | `seagull.sh/seagull/sdk/dialect` | `Facts`, the dialect's facts, which `Request.Dialect` holds and the optional ports receive as a `*dialect.Facts`; the placeholder forms; `Catalogue` and `SQLType` |
| `emit_kit` | `seagull.sh/seagull/sdk/emit/emit_kit` | SQL rendering and argument binding, the placeholder form a query executes in, batch and type-depth checks, row templates for batches, the names an emitter reserves, method names and comment lines |
| `emit_testkit` | `seagull.sh/seagull/sdk/emit/emit_testkit` | `Run`, the conformance suite, which also runs every emitter over an invented dialect, and the `DatabaseSQL` harness |
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite` and the others | The dialects the suite runs your emitter over |
| `seagull` | `seagull.sh/seagull` | `seagull.WithEmitter`, which installs the emitter, and `seagull.ErrPanic` |

Seagull's own emitters are written the same way, as provider packages in the emit area; their implementation sits in `internal/` packages, which other modules cannot import, so the packages above are the whole contract.

## Check the built-in emitters first

Each built-in emitter's `New` returns an `emit.EmitterPort` for `seagull.WithEmitter`, and `go.driver` selects the same emitters in `sgl.yaml`:

| Provider | Generated code calls | Dialects |
|---|---|---|
| `emit_driver_databasesql` | The `database/sql` interfaces, with any registered driver; `WithDriver` pins the driver's binding profile and `WithPrepared` controls `prepared.go`. It is the default when no emitter is given. | Every dialect |
| `emit_driver_pgx` | pgx v5's native interfaces | PostgreSQL family |
| `emit_driver_clickhouse` | clickhouse-go v2's native interface | ClickHouse |

If one of them generates code for your driver, select it, or wrap it as the [wrapper guide](custom-emitter-example.md) shows, rather than writing a new emitter. The [driver matrix](../reference/driver-matrix.md) compares what each one generates.

## Implement the emission interface

Implement `emit.EmitterPort`. It receives one package's analysed queries, resolved types and naming plan, and returns formatted Go files:

```go
package emit // import "seagull.sh/seagull/sdk/emit"

type EmitterPort = emit.EmitterPort
    EmitterPort is the port to implement for a new driver API: it turns analysed
    queries into Go source written against that API.

    It has a single method, Emit, so the request can grow without breaking
    implementations: new inputs are added as fields of Request. Emit must not
    change the request, and its files must depend only on the request, not on
    the order of its queries. Each file has a plain name, unique in the package,
    starts with the style's header line and is formatted as gofmt would.

    emit_kit holds the building blocks the built-in emitters share, and
    emit_testkit.Run checks an implementation against the conformance suite.
    The built-in implementations are emit_driver_databasesql, emit_driver_pgx
    and emit_driver_clickhouse.
```

Its one method, written with the `emit` qualifier your package uses, is:

```go
Emit(ctx context.Context, request *emit.Request) ([]emit.GeneratedFile, error)
```

Return an error only when emission cannot proceed, which stops the run; a query the driver cannot serve is better refused through `CheckerPort`, as [refusing requests](#refuse-unsupported-requests-before-generation) describes. Assert the contract at compile time with `var _ emit.EmitterPort = (*Emitter)(nil)`.

Do not mutate the request. Make output independent of its query order by sorting wherever order appears in a file. Each returned name must be unique and a plain filename, and each file must start with the style's header.

## Consume the analysed request

Read queries from `request.Queries`, engine capabilities from `request.Dialect`, names from `request.Naming` and types from `request.Types`. The schema is in `request.Catalogue`; style settings come from `request.Style`.

The SQL text in `AnalysedQuery.SQL` is for messages and tools. Render `Template` for execution instead. A runtime query also has a `CountTemplate` when counting is available. The [emit API reference](../reference/emit-api.md#the-request) describes the complete request.

## Use the planned names

Read method, Params, Row, field and SQL-constant names from each `QueryNaming`. When `SharedRow` is true, refer to the planned model or shared row type rather than declaring another struct. Use `Naming.Models`, `Naming.Types` and `Naming.Imports` for package declarations and qualifiers.

Implement `emit.ReserverPort` for every fixed declaration, method, field and output filename your own code introduces. The plan then keeps query and model names clear of them. The [custom wrapper](custom-emitter-example.md#reserve-the-declarations-and-file-name) shows forwarding and extending a reservation.

## Render executable SQL and bind arguments

Configure the renderer from engine capabilities and the driver, then render the template:

```go
renderer := &emit_kit.Renderer{}
renderer.Query = query
renderer.Form = emit_kit.ExecutionForm(&request.Dialect, query)
renderer.Placeholders = request.Dialect.Placeholders
renderer.Quote = request.Dialect.IdentifierQuote
renderer.Lists = request.Dialect.Lists
renderer.ListMode = emit_kit.ListExpand
renderer.KeepStars = request.Style.KeepStars
rendered, err := emit_kit.Render(query.Template, renderer)
```

Handle `err`, embed `rendered.SQL` and bind `rendered.Arguments` in their returned order. Do not reconstruct parameter numbering from SQL text. Choose `emit_kit.ListArray` for a driver binding an array, or `emit_kit.ListAsWritten` for one that expands the written list itself, when the engine supports that form.

Use `Rendered.Marks` for runtime edits such as slice expansion, filtering, sorting and pagination. Offsets refer to the rendered SQL. Conditions must join a base WHERE inside parentheses; pagination must use the engine's syntax and precede locking or trailing clauses. A sortable query omits its original ordering with `Renderer.Omit` before the generated caller inserts a choice.

Configure `WrapColumn` when the driver needs a column cast or JSON conversion before scanning. Use the facts in `request.Dialect` rather than selecting behaviour by a dialect's name or family; the test suite's invented dialect checks this separation.

## Resolve and write the Go types

Use an analysed column or parameter's `GoTypeOverride` when present; otherwise resolve its SQL type and nullability:

```go
goType := request.Types.Resolve(column.SQLType, column.Nullable)
```

Write qualified types with the qualifiers the naming plan gives in `Naming.Imports`, and import only the packages a file uses. Declare named enum and structured types from `Naming.Types`, and the inline JSON type when the resolved mapping refers to `emit.InlineJSONTypeName`.

Implement `emit.TypeDefaulterPort` when the driver has natural scan types that differ from the built-in defaults. User overrides and presets still take precedence. Bind JSON documents as text rather than bytes, which drivers can interpret as binary data; see [type mapping](../reference/type-mapping.md).

## Refuse unsupported requests before generation

The optional ports are found by a type assertion on your emitter value, so implement only those the driver needs. Implement `emit.CheckerPort` and return `emit.Problem` values associated with the affected query. The pipeline turns them into diagnostics and does not emit a package after an error. Refuse the same requests in `Emit` for callers that invoke it directly, with an error naming each problem's query, code and message, as the built-in emitters do. The built-in emitters also refuse, before anything else, a request they cannot emit as it is, such as a nil query or a naming plan that does not fit the queries; check the same in your `Emit` rather than panicking on it.

Start `Check` with `emit_kit.CheckTypeDepth(request)`. It reports, as Q059 errors, every parameter, output column and modelled table column whose type nests 64 or more levels deep, past which every walk through a type stops, so your emitter never writes code that reads only part of a value. Check driver constraints such as unsupported composite bindings and result commands. Convert nested enums to the forms a driver accepts or refuse them before runtime. Implement `emit.NullElementBinderPort` if the driver cannot bind NULL array/list elements or map values; without it, support is assumed.

Return problems and errors rather than panicking. The generator does recover a panic in any of these methods, logging its stack once at warning level: one in `Check` or `DefaultTypes` becomes a Q070 error diagnostic, and one in `Emit`, `BindsNullElements` or `Reserve` an error matching `seagull.ErrPanic`. Either way the package is not generated.

## Implement the selected execution patterns

For repeatable INSERT batches, call `emit_kit.CheckBatch`, render the row with `RowTemplate` and chunk inputs under the bind limit. When the emitter repeats the row within one statement, pass the limit as well, `emit_kit.CheckBatch(query, emit_kit.BatchRows, emit_kit.BindLimit{Form: form, Limit: limit})`, so a row that alone binds more variables than one statement allows is refused as Q060 rather than generated into a method that always fails. Keep the SQL around VALUES and its expressions. A statement-level clause such as an upsert may require one item per statement. A queue API such as pgx.Batch can instead execute ordinary statements per item; it has no general INSERT-only requirement.

For a column-oriented copy protocol, check with `emit_kit.BatchColumns` and take the input order from `RowColumns`. Refuse clauses the protocol would drop. The [query command reference](../reference/query-commands.md) supplies the built-in contracts.

For expanded slices, splice at `Marks.Lists` and bind elements where `Arguments` marks `Expand`. Use the engine's empty membership or exclusion form for an empty slice. Test both empty and multiple-element inputs rather than relying on one ordinary case.

## Emit the connection and transaction helpers

Write the querier the generated methods hang off: the `DBTX` interface of the driver's connection methods, the `Queries` type and its constructor, and replica and transaction helpers only when that API supports them. The [generated API reference](../reference/generated-go.md) describes the built-in emitters' querier, which a new emitter can follow; a database/sql wrapper can retain its behaviour inside transactions through `WrapDBTX`, as [wrapping a transaction](../reference/generated-go.md#wrapping-a-transaction-wrapdbtx) describes.

When the style requests a query-method interface, `Naming.Interface` names it: declare it with every query method the package's files hold. Reserve its filename and every fixed name the querier declares.

## Apply the user's style consistently

Read the header, helper/error prefixes, struct-tag settings and optional shapes from `request.Style`: write each field's tags as `JSONTags` and `StructTags` say, and a description or schema comment with a NUL, a byte order mark or invalid UTF-8 written as U+FFFD, which Go source can hold, as the built-in emitters do. Honour `EmptySlices`, `DocComments`, `Interface`, `QueryNames` and the naming plan's row-reuse decision where your emitter supports them.

`ModernIdioms`, which the default preset sets, asks for a stream method returning `iter.Seq2[Row, error]` and a blank line between every two top-level declarations; `emit_kit.DefaultReservation` reserves the `iter` import when it is set. `WrapErrors` asks for every error a method returns to be labelled with the error prefix and the method's name, wrapping the error with `%w` so `errors.Is` still finds it; see [labelled errors](../reference/generated-go.md#labelled-errors-wraperrors) for the built-in emitters' form.

Generated headers identify stale files for pruning. They do not protect handwritten files at output filenames from replacement, so reserve an output namespace the caller can keep for generated code. See [output writing](../reference/output-writing.md) for the writer's contract.

## Testing

Compile the returned files and run `emit_testkit.Run` with the driver harness and dialects your emitter supports. The standalone example uses this suite:

```go
func TestTheThirdPartyEmitterConforms(t *testing.T) {
	t.Parallel()

	emit_testkit.Run(t, emit_testkit.Suite{
		Name:          "thirdparty",
		New:           func(*dialect.Facts) emit.EmitterPort { return thirdparty.New() },
		Dialects:      []dialect.Dialect{dialect_engine_sqlite.New()},
		Harness:       emit_testkit.DatabaseSQL(),
		Corpora:       sqliteCorpora(t),
		KnownFailures: map[string]string{},
	})
}
```

Choose `emit_testkit.DatabaseSQL` for code written against database/sql, or provide a `Harness` of your own for another driver API, as its doc comment describes. Add your dialect's corpus. `Run` also runs every suite over the martian dialect, an invented engine with upper-case folding, bracket quoting, `@p1` placeholders and `OFFSET`-first pagination, to exercise quoting, placeholders and pagination without assuming a familiar engine; a known failure may name it.

The suite checks immutability, determinism, compilation and runtime behaviour through a recording driver. Record a current failure in `KnownFailures` only with its reason, keyed by the check's name, or by a dialect's name, a slash and the check's name for that dialect alone; it must remain a failing check until fixed. `Run` checks the suite before any check runs, and stops the test on an entry that gives no reason or names no check or no dialect of the suite. Do not use `-short` for the complete suite, since it skips checks needing the Go command. The [emit_testkit inventory](../reference/emit-api.md#emit_testkit) lists the test API.

When a case's generated package does not build with the harness, the runtime checks of that case fail with the compiler's report rather than being skipped: fix the generated code, or record those checks with the reason. When a check, the emitter or its constructor panics, that check fails rather than the whole test binary, and the stack is logged once in the subtest's output. The runtime lane builds with the `go` command on `PATH`, keeping the `GOFLAGS` of your environment and adding `-buildvcs=false` and `-mod=mod` to them.

## Use it

Pass the emitter to the generator with `seagull.WithEmitter(myemitter.New())`. To select it by name with `go.driver` in `sgl.yaml`, register it with `cli.WithEmitter` in a custom `cmd/sgl/cli` binary; see [custom binaries](../reference/cli.md#custom-binaries).

## Worked example

The [third-party emitter](../../tests/facade/thirdparty) implements database/sql generation using only the `sdk` packages. Its tests compare emitted SQL constants with the built-in emitter and exercise the conformance suite. Read it as the complete implementation behind the steps above; it lives in the `tests/facade` test module, so applications should copy the approach rather than import that package.
