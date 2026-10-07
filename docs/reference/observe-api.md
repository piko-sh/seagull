---
title: Observe API
description: "sdk/observe reports the stages of generator, migration and seed work and supplies their clock; the root package carries the diagnostics API."
section: Reference
subsection: Extensions
order: 1090
---

# Observe API

The observe area of the `sdk` kit, `seagull.sh/seagull/sdk/observe`, lets a host watch the
stages of a generation, migration or seed run, for tracing spans, metrics or timing
output, and supplies the clock those runs read. Seagull carries no telemetry dependency of
its own: observer callbacks describe stages of generator or migration work, and they do
not observe execution of generated query methods. Diagnostics, which describe SQL problems
with stable codes, severities and source locations, come from the root package
`seagull.sh/seagull`.

## Providers

`sdk/observe` has no provider packages. A host supplies its own `ObserverPort`, such as
an adapter onto its tracing library; [observe generator work](../how-to/observe-generator.md)
writes one. A run given no observer reports its stages to none, and `SystemClock` is the
default `Clock`.

Source: [`sdk/observe/facade.go`](../../sdk/observe/facade.go), and for the diagnostics
[`diagnostics.go`](../../diagnostics.go) in the root package.

## Exported package inventory

These declarations are the current `go doc -short` output of each package. Most are
aliases of the implementation under `internal/`, which a program never imports; run
`go doc` on a name for its doc comment.

### sdk/observe

[Package source](../../sdk/observe) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/observe)

`go doc -short` folds the stage constants into their first, `StageGenerate`; running
`go doc` on the package lists every stage with what it covers.

```go
const StageGenerate = observe.StageGenerate ...
func ContextWithObserver(ctx context.Context, observer ObserverPort) context.Context
type Clock = observe.Clock
    func SystemClock() Clock
type ObserverPort = observe.ObserverPort
type Stage = observe.Stage
```

### Diagnostics in the root package

[Package source](../../diagnostics.go) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull)

`go doc -short` folds the diagnostic codes into their first, `CodeUnknownColumn`; the
[diagnostic codes](diagnostics.md) page lists every one.

```go
const CodeUnknownColumn = diag.CodeUnknownColumn ...
func DidYouMean(suggestion string) string
func WriteText(w io.Writer, items []Presentation) error
type CodeInfo = diag.CodeInfo
    func Codes() []CodeInfo
    func LookupCode(code string) (CodeInfo, bool)
type ErrorSeverity = diag.ErrorSeverity
type Presentation = diag.Presentation
    func Present(diagnostic SourceError, display string, sourceLine string) Presentation
type SourceError = diag.SourceError
```

## Diagnostics

`seagull.SourceError` carries the code, severity, message, filename, position and optional suggestion. `Line` and `EndLine` are one-based. `Column` and `EndColumn` are one-based byte offsets within their line's UTF-8 text, `EndColumn` just past the span's last byte, not counts of runes or UTF-16 units: an editor that counts characters converts them on a line holding non-ASCII text.

`seagull.Present` produces a presentation for a source line. A line longer than 160 runes is quoted as a window of at most 160 runes around the span, cut on rune boundaries with `...` where it was cut, and the carets follow the window, so a minified single-line file never fills the output. `seagull.WriteText` writes those presentations, each block as soon as it is laid out, and stops at the first write error. `seagull.DidYouMean(suggestion)` phrases a suggestion as diagnostics and the help line do, `did you mean "title"?`, so a renderer of your own words it alike. `seagull.Codes` and `seagull.LookupCode` describe the registered diagnostics.

A `Result` or `Report` retains every diagnostic up to the run's cap (`WithMaxDiagnostics`, 10,000 by default), then one Q066 summarising the rest. Its `Err()` method reports error-severity items; warnings and hints do not make that method fail. A host that promotes warnings must inspect their severities explicitly, as `sgl vet --strict` does.

## Observer callbacks

`observe.ObserverPort.Start(ctx, stage, attrs...)` returns the context for the stage and an end callback receiving its final operational error or nil. A derived context can carry a parent tracing span. Seagull calls the end callback once; observer panics and nil callbacks are contained. A panic in `Start` or the end callback is logged once at warning level, with its stack, through the logger the context carries (`WithLogger`, `seagull.ContextWithLogger` or `slog.Default()`), and the stage runs on unobserved.

`seagull.WithObserver` supplies an observer for a generator, and `migrate.WithObserver` one for a migrator or seeder. Otherwise a run uses the observer `observe.ContextWithObserver` attached to the context, if any, and observes nothing when there is none.

## Clock

`observe.Clock.Now` supplies time for generator logging. `seagull.WithClock` accepts it; `observe.SystemClock()` is the default, and the one a migrator or seeder measures its recorded durations with. A fixed clock makes the generator's timing-related output predictable in tests.

The browser session also measures its own request and stage durations; a generator clock does not control those measurements.

See [errors and observability](errors-observability.md) for failure channels and [observe generator work](../how-to/observe-generator.md) for an implementation.
