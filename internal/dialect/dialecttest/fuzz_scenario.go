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

package dialecttest

import (
	"fmt"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// checkScenarioGeneration requires every positive query to reach generation.
//
// Takes t (*testing.T) which reports incomplete generation.
// Takes newDialect (func() dialect.DialectPort) which creates the engine.
// Takes scenario (fuzzcase.Scenario) which supplies independent SQL and output widths.
func checkScenarioGeneration(t *testing.T, newDialect func() dialect.DialectPort, scenario fuzzcase.Scenario) {
	t.Helper()
	ctx, cancel := fuzzContext(t)
	defer cancel()
	engine := newDialect()
	generator, err := app.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		path.Join(migrationDirectory, fuzzMigrationFile): &fstest.MapFile{Data: []byte(strings.Join(scenario.Schema, ";\n"))},
	}
	sources := make([]string, 0, len(scenario.Queries))
	for index := range scenario.Queries {
		sources = append(sources, scenario.Queries[index].SQL)
	}
	files[path.Join(queryDirectory, "scenario.sql")] = &fstest.MapFile{Data: []byte(strings.Join(sources, "\n"))}
	details, err := generator.GenerateDetailed(ctx, fuzzInput(files))
	if err != nil {
		t.Fatal(err)
	}
	if err := details.Result.Err(); err != nil {
		t.Fatalf("positive scenario %v failed: %v\n%s", scenario.Data, err, strings.Join(sources, "\n"))
	}
	if len(details.Report.Queries) != len(scenario.Queries) || len(details.Result.Files) == 0 {
		t.Fatal("positive scenario did not reach generation")
	}
	info := engine.Info()
	for index, query := range details.Report.Queries {
		if len(query.OutputColumns) != scenario.Queries[index].Columns {
			t.Errorf("scenario changed output width for %s", query.Name)
		}
		for _, problem := range templateProblems(&info, "scenario", query) {
			t.Error(problem)
		}
	}
}

// auditScenarioFeatures requires each admitted scenario seed to reach generation and
// every admitted query grammar feature to be reached by a generated seed.
//
// Takes t (*testing.T) which reports missing coverage.
// Takes newDialect (func() dialect.DialectPort) which creates the engine.
func auditScenarioFeatures(t *testing.T, newDialect func() dialect.DialectPort) {
	t.Helper()
	engine := newDialect().Info().Name
	covered := make(map[string]int)
	for index, data := range fuzzcase.ScenarioSeeds(engine) {
		scenario, ok := fuzzcase.DecodeScenario(engine, data)
		if !ok {
			t.Fatalf("scenario seed was rejected: %v", data)
		}
		if t.Run(fmt.Sprintf("scenario_%03d", index), func(t *testing.T) { checkScenarioGeneration(t, newDialect, scenario) }) {
			for _, feature := range scenario.Features {
				covered[feature]++
			}
		}
	}
	for _, feature := range fuzzcase.QueryGrammarFeatures(engine) {
		if covered[feature] == 0 {
			t.Errorf("query grammar version %d feature %s reached no generated seed", fuzzcase.QueryGrammarVersion, feature)
		}
	}
}
