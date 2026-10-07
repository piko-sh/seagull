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

package observe

import (
	"context"
	"log/slog"

	"seagull.sh/seagull/internal/logging"
)

// Stage names a unit of work Seagull reports to an ObserverPort.
type Stage string

const (
	// StageGenerate covers one full generation of a database's code.
	StageGenerate Stage = "generate"

	// StagePlan covers type mapping, package naming and emitter checks for one attempt.
	StagePlan Stage = "generate.plan"

	// StageEmit covers emitting the checked package, including formatting.
	StageEmit Stage = "generate.emit"

	// StageFormat covers formatting generated Go source.
	StageFormat Stage = "generate.format"

	// StageVet covers analysing every query of a database without generating code.
	StageVet Stage = "vet"

	// StageCatalogue covers building the schema catalogue from the migrations.
	StageCatalogue Stage = "generate.catalogue"

	// StageAnalyse covers analysing every query file of a database.
	StageAnalyse Stage = "generate.analyse"

	// StageAnalyseQuery covers analysing a single query.
	StageAnalyseQuery Stage = "generate.analyse.query"

	// StageResolveColumns covers resolving the output columns of a query.
	StageResolveColumns Stage = "generate.resolve.columns"

	// StageResolveParameters covers resolving the parameters of a query.
	StageResolveParameters Stage = "generate.resolve.params"

	// StageCatalogueMigration covers applying one migration to the catalogue.
	StageCatalogueMigration Stage = "generate.catalogue.migration"

	// StageMigrateUp covers applying pending migrations.
	StageMigrateUp Stage = "migrate.up"

	// StageMigrateDown covers rolling migrations back.
	StageMigrateDown Stage = "migrate.down"

	// StageMigrateStatus covers reporting the state of every migration.
	StageMigrateStatus Stage = "migrate.status"

	// StageMigrateValidate covers checking applied migrations against their files.
	StageMigrateValidate Stage = "migrate.validate"

	// StageMigrateForce covers recording a migration as applied without running it.
	StageMigrateForce Stage = "migrate.force"

	// StageMigrateRepairPlan covers reading what a repair would change, without changing
	// anything.
	StageMigrateRepairPlan Stage = "migrate.repair.plan"

	// StageMigrateRepair covers applying a reviewed repair plan to the migration history.
	StageMigrateRepair Stage = "migrate.repair"

	// StageSeedApply covers applying pending seeds.
	StageSeedApply Stage = "seed.apply"

	// StageSeedStatus covers reporting the state of every seed.
	StageSeedStatus Stage = "seed.status"

	// StageSeedReseed covers clearing and reapplying seeds.
	StageSeedReseed Stage = "seed.reseed"
)

// nopObserver is the ObserverPort that ignores every stage.
type nopObserver struct{}

// Start returns ctx unchanged and an end function that does nothing.
//
// Takes ctx (context.Context) which is returned as is.
//
// Returns context.Context which is ctx.
// Returns func(err error) which ignores its argument.
func (nopObserver) Start(ctx context.Context, _ Stage, _ ...slog.Attr) (context.Context, func(err error)) {
	return ctx, endNothing
}

// guardedObserver forwards to a host's ObserverPort and keeps a faulty one from failing
// the stage it observes.
type guardedObserver struct {
	// observer is the ObserverPort the host attached.
	observer ObserverPort
}

// Start forwards to the host's ObserverPort. A panic in its Start is recovered and
// logged, and the stage runs unobserved; a nil context or end function falls back to ctx
// and a no-op; a panic in its end function is recovered and logged.
//
// Takes ctx (context.Context) which is the context the stage runs under.
// Takes stage (Stage) which names the work.
// Takes attrs (...slog.Attr) which describe the work.
//
// Returns context.Context which the stage runs under: the observer's, or ctx.
// Returns func(err error) which ends the stage and never panics.
func (g guardedObserver) Start(ctx context.Context, stage Stage, attrs ...slog.Attr) (context.Context, func(err error)) {
	stageContext, end := g.startRecovered(ctx, stage, attrs)
	if stageContext == nil {
		stageContext = ctx
	}
	if end == nil {
		return stageContext, endNothing
	}
	return stageContext, endRecovered(ctx, stage, end)
}

// startRecovered calls the host's Start, turning a panic into an unobserved stage. The
// panic is logged once at warning level, with its stack, through the logger ctx carries.
//
// Takes ctx (context.Context) which is the context the stage runs under.
// Takes stage (Stage) which names the work.
// Takes attrs ([]slog.Attr) which describe the work.
//
// Returns context.Context which is the observer's context, or nil after a panic.
// Returns func(err error) which is the observer's end function, or nil after a panic.
func (g guardedObserver) startRecovered(
	ctx context.Context,
	stage Stage,
	attrs []slog.Attr,
) (stageContext context.Context, end func(err error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.LogRecoveredPanic(ctx, "observer start of stage "+string(stage), recovered)
			stageContext, end = nil, nil
		}
	}()
	return g.observer.Start(ctx, stage, attrs...)
}

// observerContextKey keys the ObserverPort a host attaches to a context.
type observerContextKey struct{}

// Discard returns the ObserverPort used when a context carries none. It ignores every
// stage.
//
// Returns ObserverPort which does nothing.
func Discard() ObserverPort {
	return nopObserver{}
}

// ContextWithObserver returns a copy of ctx carrying observer.
//
// Passing a nil observer returns ctx unchanged.
//
// Takes ctx (context.Context) which is the parent context; nil starts from
// context.Background.
// Takes observer (ObserverPort) which receives the stages run under the returned context.
//
// Returns context.Context which carries the observer.
func ContextWithObserver(ctx context.Context, observer ObserverPort) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if observer == nil {
		return ctx
	}

	return context.WithValue(ctx, observerContextKey{}, observer)
}

// From returns the ObserverPort to report a stage run under ctx to: Discard() when ctx
// carries none, and otherwise a guard around the attached ObserverPort.
//
// The guard keeps a faulty ObserverPort from failing the stage it observes. A panic in
// its Start or in the end function it returns is recovered and logged once at warning
// level, with its stack, through the logger attached to the context, and the stage
// carries on. A nil context or end function from its Start falls back to the stage's own
// context and a no-op, so callers may always run `ctx, end :=
// observe.From(ctx).Start(...)` and call end.
//
// Takes ctx (context.Context) which may be nil.
//
// Returns ObserverPort which is never nil.
func From(ctx context.Context) ObserverPort {
	if ctx != nil {
		if observer, ok := ctx.Value(observerContextKey{}).(ObserverPort); ok && observer != nil {
			return guardedObserver{observer: observer}
		}
	}

	return Discard()
}

// endRecovered wraps an observer's end function so a panic in it is recovered. The panic
// is logged once at warning level, with its stack, through the logger ctx carries.
//
// Takes stage (Stage) which names the work.
// Takes end (func(err error)) which is the observer's end function.
//
// Returns func(err error) which calls end and never panics.
func endRecovered(ctx context.Context, stage Stage, end func(err error)) func(err error) {
	return func(err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logging.LogRecoveredPanic(ctx, "observer end of stage "+string(stage), recovered)
			}
		}()
		end(err)
	}
}

// endNothing is the end function of a stage nobody observes.
func endNothing(_ error) {}
