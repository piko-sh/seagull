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

// Package observe lets a host watch the stages of a generation, migration or seed run,
// for tracing spans, metrics or timing output, and supplies the clock those runs read.
//
// Seagull carries no telemetry dependency of its own. When a stage begins it calls Start
// on an [ObserverPort] with the [Stage], such as [StageGenerate] or [StageMigrateUp], and
// when the stage ends it calls the function Start returned, once, with the stage's error
// or nil. Stages nest: a generation reports [StageGenerate] around [StageCatalogue],
// [StageAnalyse] (with [StageAnalyseQuery] for each query), [StagePlan] and [StageEmit],
// so an observer whose Start returns a context carrying its span gives the nested stages
// their parent. The stages describe Seagull's own work; they do not trace the queries the
// generated code runs.
//
// # Choosing the observer
//
// A run reports to the observer set with seagull.WithObserver on the generator, or with
// migrate.WithObserver on a migrator or seeder. Without one it reports to the observer
// its context carries, attached with [ContextWithObserver], which suits an observer per
// request. Without either, the stages go unobserved.
//
// A panic in an observer is recovered and logged once at warning level, with its stack,
// through the logger the context carries, and the stage runs on unobserved, so telemetry
// never fails the run it observes.
//
// # Clock
//
// [Clock] is the time source the generator and the migration executor read: the generator
// times each run for its trace log, and the executor records migration and seed durations
// and the applied time of history rows written without running a migration. [SystemClock]
// is the default. Pass a fixed Clock with seagull.WithClock to make the generator's
// timings repeatable in tests.
//
// # Usage
//
//	type stageLogger struct{ logger *slog.Logger }
//
//	func (observer stageLogger) Start(
//	    ctx context.Context, stage observe.Stage, attrs ...slog.Attr,
//	) (context.Context, func(error)) {
//	    observer.logger.LogAttrs(ctx, slog.LevelInfo, "stage started",
//	        append(attrs, slog.String("stage", string(stage)))...)
//	    return ctx, func(err error) {
//	        if err != nil {
//	            observer.logger.ErrorContext(ctx, "stage failed", "stage", stage, "error", err)
//	        }
//	    }
//	}
//
//	generator, err := seagull.New(engine, seagull.WithObserver(stageLogger{logger: logger}))
//
// or, for the runs under one context only:
//
//	ctx = observe.ContextWithObserver(ctx, stageLogger{logger: logger})
package observe
