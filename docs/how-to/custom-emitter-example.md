---
title: Build and test a custom emitter
description: "Wrap an emitter to add a generated file of your own, passing on its optional interfaces and reserving your names, and select it with WithEmitter."
section: How-to guides
subsection: Extensions
order: 820
---

# Build and test a custom emitter

Wrap an existing emitter when you need an extra generated Go file without replacing its query methods. The [working example](../../examples/embedding/052_custom_emitter) adds a query index to database/sql output.

## What you import

A wrapper is written against the emit area of the `sdk` kit, `seagull.sh/seagull/sdk/emit`, and wraps one of its provider packages:

| Package | Import path | What it gives a wrapper |
|---|---|---|
| `emit` | `seagull.sh/seagull/sdk/emit` | `EmitterPort`, the optional ports the wrapper passes on (`ReserverPort`, `TypeDefaulterPort`, `CheckerPort` and `NullElementBinderPort`), `Request`, `Reservation` and `GeneratedFile` |
| `emit_kit` | `seagull.sh/seagull/sdk/emit/emit_kit` | `ReservationFor`, which reads the wrapped emitter's reservation |
| `emit_driver_databasesql` | `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` | The emitter being wrapped; `emit_driver_pgx` and `emit_driver_clickhouse` wrap the same way |
| `emit_testkit` | `seagull.sh/seagull/sdk/emit/emit_testkit` | `Run`, the conformance suite, for the wrapper's tests |
| `dialect` | `seagull.sh/seagull/sdk/dialect` | `Facts`, which the optional ports receive as a `*dialect.Facts` |
| `dialect_engine_*` | `seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite` and the others | The dialect the generator runs with |
| `seagull` | `seagull.sh/seagull` | `seagull.New` and `WithEmitter`, which installs the wrapper |

## Append the extra file

Implement `Emit` by calling the wrapped emitter and adding your file to its result:

```go
func (e indexEmitter) Emit(ctx context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
	files, err := e.base.Emit(ctx, request)
	if err != nil {
		return nil, err
	}
	index, err := queryIndex(request)
	if err != nil {
		return nil, err
	}
	return append(files, index), nil
}
```

The example's `queryIndex` reads each method name from `request.Naming`, starts the file with `request.Style.Header` and formats the source with `go/format`. Use the naming plan rather than deriving Go names from SQL query names. Return the wrapped emitter's error as it is, or wrap it with `%w`, so a caller can still match what the wrapped emitter returned with `errors.Is` and `errors.As`. The generator wraps whatever `Emit` returns as `emitting package <name>: ...`, keeping it matchable the same way.

## Reserve the declarations and file name

Forward the wrapped emitter's reservation and add the names your file declares:

```go
func (e indexEmitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	reservation := emit_kit.ReservationFor(e.base, info, style)
	reservation.Declarations = append(reservation.Declarations, infoType, indexVariable)
	reservation.Files = append(reservation.Files, indexFile)
	return reservation
}
```

The plan can then keep table models and query files clear of `QueryInfo`, `QueryIndex` and `query_index.go`. Generation owns returned output filenames, so choose a name that does not overlap a handwritten file. The generated header governs stale-file pruning; it does not protect an existing file at a returned name from replacement.

## Forward the driver's optional interfaces

The generator finds an emitter's optional ports with a type assertion on the emitter it was given, so a wrapper hides any it does not implement itself. Pass on `DefaultTypes` (`emit.TypeDefaulterPort`), `Check` (`emit.CheckerPort`) and `BindsNullElements` (`emit.NullElementBinderPort`) when the wrapped emitter implements them, as well as its reservation (`emit.ReserverPort`). Otherwise the generator sees the wrapper without the driver's type defaults, diagnostic refusals or nullable-element constraints.

Each method must match its port exactly, with `*dialect.Facts` where the port takes the dialect's facts, or the port silently stops applying: the method is then only a method, and the type assertion fails. Assert every port the wrapper implements, so the compiler checks the signatures:

```go
var (
	_ emit.EmitterPort           = (*indexEmitter)(nil)
	_ emit.ReserverPort          = (*indexEmitter)(nil)
	_ emit.TypeDefaulterPort     = (*indexEmitter)(nil)
	_ emit.CheckerPort           = (*indexEmitter)(nil)
	_ emit.NullElementBinderPort = (*indexEmitter)(nil)
)
```

The [complete wrapper](../../examples/embedding/052_custom_emitter/example.go) shows each forwarding method.

## Select and test the wrapper

Pass it to the generator with `WithEmitter`:

```go
	emitter := indexEmitter{base: emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false))}
	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithEmitter(emitter))
	if err != nil {
		return nil, err
	}
```

Generate and inspect the files, then check the generation result's diagnostics before writing them. Compile the generated caller and run the [emitter conformance suite](write-an-emitter.md#testing), `emit_testkit.Run`. The example test also verifies that its committed store matches the in-memory generation; running the test does not update that store.

The [working example](../../examples/embedding/052_custom_emitter) includes connection setup and tests. From the repository root, run:

```sh
cd examples/embedding/052_custom_emitter
go test .
```

For an emitter built from scratch, follow [write an emitter](write-an-emitter.md). The [emit API reference](../reference/emit-api.md) supplies the field and port contracts.
