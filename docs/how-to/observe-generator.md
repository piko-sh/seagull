---
title: Observe and limit generator work
description: "Supply a logger and observer through options or context."
section: How-to guides
subsection: Embedding
order: 810
---

# Observe and limit generator work

Attach an observer when your tool needs stage spans or metrics, and a logger when it needs log records. Set input limits before accepting projects from users or remote systems.

## What you import

| Package | Import path | What it gives you |
|---|---|---|
| `seagull` | `seagull.sh/seagull` | `WithObserver`, `WithLogger`, `ContextWithLogger`, `WithClock` and the input limit options |
| `observe` | `seagull.sh/seagull/sdk/observe` | `ObserverPort`, the `Stage` values, `ContextWithObserver`, and `Clock` with `SystemClock` |
| A dialect provider | `seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite`, or the one for your engine | The dialect the generator runs |

The observe area has no provider packages: an observer is a type of your own with the one method of `observe.ObserverPort`, `Start`. The same observer value also watches migration and seed runs through `migrate.WithObserver` from `seagull.sh/seagull/sdk/migrate`.

## Attach a stage observer

This complete command logs each stage while vetting a SQLite project. Its `stageLogger` implements `observe.ObserverPort`; replace `stageLogger.Start` with your tracing system's span creation and return its end callback.

<!-- example: observe -->
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/observe"
)

type stageLogger struct{ logger *slog.Logger }

func (observer stageLogger) Start(ctx context.Context, stage observe.Stage, attrs ...slog.Attr) (context.Context, func(error)) {
	observer.logger.LogAttrs(ctx, slog.LevelInfo, "stage started", append(attrs, slog.String("stage", string(stage)))...)
	return ctx, func(err error) {
		if err != nil {
			observer.logger.ErrorContext(ctx, "stage failed", "stage", stage, "error", err)
		}
	}
}

func main() {
	project := flag.String("project", ".", "project directory")
	flag.Parse()
	if err := inspect(context.Background(), *project); err != nil {
		log.Fatal(err)
	}
}

func inspect(ctx context.Context, directory string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	generator, err := seagull.New(dialect_engine_sqlite.New(dialect_engine_sqlite.WithMaxParseDepth(128)),
		seagull.WithLogger(logger), seagull.WithObserver(stageLogger{logger: logger}),
		seagull.WithMaxFiles(64), seagull.WithMaxFileBytes(256<<10),
		seagull.WithMaxTotalBytes(1<<20), seagull.WithMaxQueries(1000),
		seagull.WithMaxDiagnostics(500))
	if err != nil {
		return err
	}
	report, err := generator.Vet(ctx, seagull.Input{Migrations: os.DirFS(directory),
		Package: "store", MigrationDir: "db/migrations", QueryDir: "db/queries"})
	if err != nil {
		return err
	}
	if err := report.Err(); err != nil {
		return err
	}
	fmt.Printf("checked %d queries\n", len(report.Queries))
	return nil
}
```

Save it as `cmd/inspect/main.go`, then run:

```sh
go run ./cmd/inspect -project .
```

The output includes `vet`, catalogue and per-query stages, followed by the query count. These spans describe generation work; they do not trace the generated application's query execution.

## Choose limits for your input

The example caps file count, file size, total bytes, query count, reported diagnostics and parser depth. Choose values for the projects your tool accepts. A limit failure or cancellation is an operational error, so handle the returned error before reading a report. The query cap counts every query block, one with a malformed header included. The diagnostics cap is not an error: the diagnostics past it are summarised in one [Q066](../reference/diagnostics.md#q066) at the highest severity among them, so `report.Err()` still fails when an error was left out. SQL diagnostics remain in `report.Diagnostics` and are checked with `report.Err()`.

Pass the caller's cancellable context instead of `context.Background()` in a service. An observer can return a derived context so nested stages see their parent span. A nil end callback or a panic in the observer is contained by Seagull: the panic is logged once at warning level, with its stack, through the run's logger, and the stage runs on unobserved. A panic in a dialect or a host-supplied emitter or schema source is recovered and logged the same way, then reported as a Q070 diagnostic or an error matching `seagull.ErrPanic`; read the stack from the logger you pass to `WithLogger`.

For request-specific logging, use `seagull.ContextWithLogger`; for request-specific observation, use `observe.ContextWithObserver`. Explicit `WithLogger` and `WithObserver` options take precedence over their context values.

The [observability reference](../reference/errors-observability.md) describes log levels and stage behaviour, and the [observe API](../reference/observe-api.md) lists every stage. The [working example](../../examples/embedding/051_generator_observability/example.go) also observes migration work and fixes the clock for repeatable output.
