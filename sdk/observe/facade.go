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

	"seagull.sh/seagull/internal/observe"
)

const (
	// StageGenerate covers one full generation of a database's code, from building the
	// catalogue to emitting the package. Its attributes name the generated package.
	StageGenerate = observe.StageGenerate

	// StagePlan covers type mapping, package naming and emitter checks for one attempt,
	// within StageGenerate or StageVet.
	StagePlan = observe.StagePlan

	// StageEmit covers emitting the checked package, including formatting, within
	// StageGenerate.
	StageEmit = observe.StageEmit

	// StageFormat covers formatting one generated Go file, within StageEmit.
	StageFormat = observe.StageFormat

	// StageVet covers analysing every query of a database without generating code.
	StageVet = observe.StageVet

	// StageCatalogue covers building the schema catalogue, by replaying the migrations or
	// from a schema source.
	StageCatalogue = observe.StageCatalogue

	// StageAnalyse covers analysing every query file of a database.
	StageAnalyse = observe.StageAnalyse

	// StageAnalyseQuery covers analysing a single query, within StageAnalyse.
	StageAnalyseQuery = observe.StageAnalyseQuery

	// StageResolveColumns covers resolving the output columns of a query, within
	// StageAnalyseQuery.
	StageResolveColumns = observe.StageResolveColumns

	// StageResolveParameters covers resolving the parameters of a query, within
	// StageAnalyseQuery.
	StageResolveParameters = observe.StageResolveParameters

	// StageCatalogueMigration covers applying one migration to the catalogue, within
	// StageCatalogue.
	StageCatalogueMigration = observe.StageCatalogueMigration

	// StageMigrateUp covers applying pending migrations.
	StageMigrateUp = observe.StageMigrateUp

	// StageMigrateDown covers rolling migrations back.
	StageMigrateDown = observe.StageMigrateDown

	// StageMigrateStatus covers reporting the state of every migration.
	StageMigrateStatus = observe.StageMigrateStatus

	// StageMigrateValidate covers checking applied migrations against their files.
	StageMigrateValidate = observe.StageMigrateValidate

	// StageMigrateForce covers recording a migration as applied without running it. Its
	// attributes carry the version.
	StageMigrateForce = observe.StageMigrateForce

	// StageMigrateRepairPlan covers reading what a repair would change, without changing
	// anything.
	StageMigrateRepairPlan = observe.StageMigrateRepairPlan

	// StageMigrateRepair covers applying a reviewed repair plan to the migration history.
	StageMigrateRepair = observe.StageMigrateRepair

	// StageSeedApply covers applying pending seeds.
	StageSeedApply = observe.StageSeedApply

	// StageSeedStatus covers reporting the state of every seed.
	StageSeedStatus = observe.StageSeedStatus

	// StageSeedReseed covers clearing and reapplying seeds.
	StageSeedReseed = observe.StageSeedReseed
)

// Watching the stages Seagull runs, and the clock its runs read.
type (
	// ObserverPort is the port to implement to watch the stages Seagull runs, for tracing
	// spans, metrics or timing output.
	//
	// Its one method, Start(ctx, stage, attrs...), reports that stage has begun, with
	// attributes that describe the work, such as the package name. It returns the context
	// the stage runs under, which may carry a span for nested stages to find, and a function
	// the stage calls exactly once when it ends, with its final error or nil.
	//
	// Attach one with seagull.WithObserver, migrate.WithObserver or [ContextWithObserver].
	// No kit is needed to write one.
	ObserverPort = observe.ObserverPort

	// Clock is the time source Seagull reads, an interface with the one method Now.
	//
	// The generator reads it for trace timings, and the migration executor for migration and
	// seed durations and history timestamps, including entries written without running a
	// migration. [SystemClock] is the default. Pass a fixed clock to seagull.WithClock for
	// repeatable test output.
	Clock = observe.Clock
)

// Stage names a unit of work Seagull reports to an [ObserverPort]. Each is a dotted name,
// such as "generate.plan" or "migrate.up", which a host can use as its span or metric
// name.
type Stage = observe.Stage

// SystemClock returns the Clock that reads the system time, the one the generator and the
// migration executor use when no Clock is set.
//
// Returns Clock which reads time.Now.
func SystemClock() Clock {
	return observe.SystemClock()
}

// ContextWithObserver returns a copy of ctx carrying observer, so every stage run under
// the returned context reports to it. It suits an observer per request or per run; an
// observer set with seagull.WithObserver or migrate.WithObserver takes precedence.
//
// Passing a nil observer returns ctx unchanged.
//
// Takes ctx (context.Context) which is the parent context; nil starts from
// context.Background.
// Takes observer (ObserverPort) which receives the stages run under the returned context.
//
// Returns context.Context which carries the observer.
func ContextWithObserver(ctx context.Context, observer ObserverPort) context.Context {
	return observe.ContextWithObserver(ctx, observer)
}
