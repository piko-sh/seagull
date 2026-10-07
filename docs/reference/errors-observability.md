---
title: Errors and observability
description: Generator failures, SQL diagnostics and database execution errors are
  separate channels. Always inspect the returned error before reading a result, then
  inspect result.Err() before writing files.
section: Reference
subsection: Core
order: 250
---

# Errors and observability

Generator failures, SQL diagnostics and application database errors have separate contracts. The generator returns an operational error when it cannot complete work; a successful call can still return a result containing SQL problems.

## Operational errors and SQL diagnostics

Check the returned error before reading a `Result` or `Report`. Then check `result.Err()` or `report.Err()` for error-severity diagnostics. Warnings and hints remain available in `Diagnostics` without making `Err()` fail.

```go
result, err := generator.Generate(ctx, input)
if err != nil {
    return err
}
if err := result.Err(); err != nil {
    return err
}
return seagull.WriteFiles(output, result)
```

The [generator API](generator-api.md#errors-and-diagnostics) lists sentinels for `errors.Is`; use `errors.As` to inspect typed errors. Prefer those contracts over matching message text.

A run's `Diagnostics` hold at most 10,000 entries, or the cap `WithMaxDiagnostics` sets, then one [Q066](diagnostics.md#q066) summarising the rest at the highest severity among them, so `Err()` still fails when an error was left out.

## Recovered panics

A panic in a dialect, or in an `EmitterPort`, `CheckerPort`, `TypeDefaulterPort`, `NullElementBinderPort`, `ReserverPort` or `SchemaSourcePort` the host supplies, does not crash the process. Seagull recovers it and logs one warning record, `recovered a panic`, with `operation`, `panic` and `stack` attributes, through the logger described below. The stack never travels on an error or a diagnostic. A panic in `Check` or `DefaultTypes`, or while a migration statement or a query is handled, becomes a [Q070](diagnostics.md#q070) error diagnostic; a panic in `Emit`, `BindsNullElements`, `Reserve`, a schema source or any other dialect call is returned as an error matching `seagull.ErrPanic`, which also matches the panic value when that value is an error.

`WithSkipFailingQueries()` can produce files for a partial package while `Result.Err()` still reports rejected queries. A host that publishes partial results should do so deliberately and preserve the diagnostics. [Output writing](output-writing.md#failed-and-partial-results) describes that policy.

## Logging

A generator uses the logger supplied by `WithLogger`, then a context logger from `seagull.ContextWithLogger`, then `slog.Default()`. Records about Seagull's own work use two levels below Debug: `slog.Level(-8)` for per-statement tracing and `slog.Level(-6)` for internal detail, such as a run that error diagnostics stopped, which is not logged as a warning because the diagnostics already report it. Warnings, such as a recovered panic, and errors use their usual levels.

## Stage observation

An `observe.ObserverPort`, from `seagull.sh/seagull/sdk/observe`, starts a span for each stage:

```go
type ObserverPort interface {
    Start(ctx context.Context, stage Stage, attrs ...slog.Attr) (context.Context, func(err error))
}
```

The returned context carries stage state into nested work. The end callback receives the stage's operational error, which is separate from diagnostics in a completed report. Observer panics and nil end callbacks do not fail the observed run: a panic in `Start` or in the end callback is logged once at warning level, with its stack, through the run's logger (`WithLogger`, then a context logger from `seagull.ContextWithLogger`, then `slog.Default()`), and the stage runs on unobserved.

`generate` contains catalogue and analysis stages, including individual migrations and queries.
`generate.plan` covers package naming and emitter checks; `generate.emit` covers each emission attempt.
Within built-in emitters, `generate.format` spans cover query, model and query-interface formatting.
`GenerateDetailed` uses the same stages once while collecting its additional snapshots. `Vet` uses `vet` as its outer stage; migration and seed operations have `migrate.*` and `seed.*` names. An explicit `WithObserver` wins over a context observer; otherwise `observe.ContextWithObserver` can supply request-specific observation.

See the [observe API](observe-api.md) for exported declarations and [observe generator work](../how-to/observe-generator.md) for a complete example.
