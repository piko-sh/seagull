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

package observe_test

import (
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/observe"
)

func TestObserveFacadeAPI(t *testing.T) {
	surface := apitest.Surface{
		"ObserverPort": (*observe.ObserverPort)(nil),
		"Clock":        (*observe.Clock)(nil),

		"Stage": observe.Stage(""),

		"StageGenerate":           observe.StageGenerate,
		"StagePlan":               observe.StagePlan,
		"StageEmit":               observe.StageEmit,
		"StageFormat":             observe.StageFormat,
		"StageVet":                observe.StageVet,
		"StageCatalogue":          observe.StageCatalogue,
		"StageAnalyse":            observe.StageAnalyse,
		"StageAnalyseQuery":       observe.StageAnalyseQuery,
		"StageResolveColumns":     observe.StageResolveColumns,
		"StageResolveParameters":  observe.StageResolveParameters,
		"StageCatalogueMigration": observe.StageCatalogueMigration,
		"StageMigrateUp":          observe.StageMigrateUp,
		"StageMigrateDown":        observe.StageMigrateDown,
		"StageMigrateStatus":      observe.StageMigrateStatus,
		"StageMigrateValidate":    observe.StageMigrateValidate,
		"StageMigrateForce":       observe.StageMigrateForce,
		"StageMigrateRepairPlan":  observe.StageMigrateRepairPlan,
		"StageMigrateRepair":      observe.StageMigrateRepair,
		"StageSeedApply":          observe.StageSeedApply,
		"StageSeedStatus":         observe.StageSeedStatus,
		"StageSeedReseed":         observe.StageSeedReseed,

		"SystemClock":         observe.SystemClock,
		"ContextWithObserver": observe.ContextWithObserver,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
