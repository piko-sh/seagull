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

package harness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzznative"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
)

const (
	// commandMany reads every row into a slice.
	commandMany = "many"

	// commandStream reads every row through an iterator.
	commandStream = "stream"

	// commandOne reads the first row.
	commandOne = "one"

	// queryModeFamilies bounds the seeds kept for each feature set.
	queryModeFamilies = 1

	// modeInputDeadline bounds one input run through every mode.
	modeInputDeadline = 4 * databaseInputDeadline

	// modeFieldSeparator joins a row's canonical fields, the ASCII unit separator.
	modeFieldSeparator = "\x1f"

	// modeEmptyBlob spells an empty or nil []byte once both drivers' forms are merged.
	modeEmptyBlob = "bytes-empty-or-nil"
)

// modeCommands lists the forms each plain many query is run in.
var modeCommands = []string{commandMany, commandStream, commandOne}

// modeWorker owns one compiled mode helper and its cache.
type modeWorker struct {
	// process runs the current helper.
	process *fuzznative.Process

	// cache bounds retained compiled helpers.
	cache *fuzzruntime.ProgramCache

	// temporary owns compiled programs outside managed campaigns.
	temporary string

	// binary names the running helper's executable.
	binary string
}

// start compiles the packages, when not cached, and runs their helper.
//
// Takes t (*testing.T) which reports build and startup failures.
// Takes suite (NativeFuzzSuite) which supplies the driver.
// Takes packages ([]modePackage) which holds each mode's generated files.
//
// Returns *fuzznative.Process which serves every compiled mode.
func (worker *modeWorker) start(t *testing.T, suite NativeFuzzSuite, packages []modePackage) *fuzznative.Process {
	t.Helper()
	if worker.cache == nil {
		worker.cache = openProgramCache(t, worker.temporary)
	}
	binary := buildModeProgram(t, suite, worker.cache, packages)
	if worker.process != nil && worker.binary == binary {
		return worker.process
	}
	if worker.process != nil {
		require.NoError(t, worker.process.Close())
		worker.process = nil
	}
	var err error
	worker.process, err = fuzznative.Start(binary)
	require.NoError(t, err)
	worker.binary = binary
	return worker.process
}

// newModeWorker creates a worker whose helper is closed with the fuzz target.
//
// Takes f (*testing.F) which owns the worker's lifetime.
//
// Returns *modeWorker which starts helpers on demand.
func newModeWorker(f *testing.F) *modeWorker {
	f.Helper()
	worker := &modeWorker{process: nil, cache: nil, temporary: f.TempDir(), binary: ""}
	f.Cleanup(func() {
		var failures []error
		if worker.process != nil {
			failures = append(failures, worker.process.Close())
		}
		if worker.cache != nil {
			failures = append(failures, worker.cache.Close())
		}
		assert.NoError(f, errors.Join(failures...))
	})
	return worker
}

// FuzzQueryModes runs version 8 and 14 read queries through every execution mode of
// generated Go and requires identical values and outcomes. The modes are database/sql
// direct and prepared, pgx or native ClickHouse, and many, stream and one over the same
// SQL.
//
// Takes f (*testing.F) which owns the worker and seed catalogue.
// Takes suite (NativeFuzzSuite) which supplies native connections and the driver.
func FuzzQueryModes(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	for _, seed := range queryModeSeeds(suite.Engine) {
		f.Add(seed)
	}
	worker := newModeWorker(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		scenario, ok := fuzzcase.DecodeScenario(suite.Engine, data)
		if !ok || (data[0] != fuzzcase.TypedVersion && data[0] != fuzzcase.QueryGrammarVersion) {
			t.Skip("the mode oracle reads version 8 and 14 scenarios")
		}
		if slices.ContainsFunc(scenario.Queries, func(query fuzzcase.ScenarioQuery) bool { return query.Writes }) {
			t.Skip("the mode oracle runs read queries")
		}
		runQueryModes(t, suite, worker, scenario)
	})
}

// queryModeSeeds keeps a bounded sample of the scenario seeds. It keeps every bound
// parameter boundary of version 8 and one input per version 14 feature set.
//
// Takes engine (string) which selects the scenario seeds.
//
// Returns [][]byte which reaches each family through every mode.
func queryModeSeeds(engine string) [][]byte {
	var seeds [][]byte
	seen := map[string]int{}
	for _, seed := range fuzzcase.ScenarioSeeds(engine) {
		scenario, ok := fuzzcase.DecodeScenario(engine, seed)
		if !ok || slices.ContainsFunc(scenario.Queries, func(query fuzzcase.ScenarioQuery) bool { return query.Writes }) {
			continue
		}
		key := ""
		switch seed[0] {
		case fuzzcase.TypedVersion:
			if !slices.ContainsFunc(scenario.Queries, func(query fuzzcase.ScenarioQuery) bool { return len(query.Values) > 0 }) {
				continue
			}
			key = string(seed)
		case fuzzcase.QueryGrammarVersion:
			key = strings.Join(scenario.Features, ",")
		default:
			continue
		}
		if seen[key] < queryModeFamilies {
			seeds = append(seeds, seed)
		}
		seen[key]++
	}
	return seeds
}

// runQueryModes generates, runs and compares one scenario in every mode.
//
// Takes t (*testing.T) which reports differences.
// Takes suite (NativeFuzzSuite) which supplies connections and the driver.
// Takes worker (*modeWorker) which owns the helper.
// Takes scenario (fuzzcase.Scenario) which supplies the schema, data and queries.
func runQueryModes(t *testing.T, suite NativeFuzzSuite, worker *modeWorker, scenario fuzzcase.Scenario) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(modeInputDeadline), errFuzzInputDeadline)
	defer cancel()
	catalogue, calls := modeCatalogue(scenario)
	packages := queryModePackages(ctx, t, suite, scenario, catalogue)
	process := worker.start(t, suite, packages)
	database, connection := suite.OpenNative(t)
	database.SetMaxOpenConns(1)
	fuzzApply(ctx, t, database, append(slices.Clone(scenario.Schema), scenario.Inserts...))
	require.NoError(t, database.Close())
	results := map[string][]fuzznative.ModeResult{}
	for _, item := range packages {
		response, err := process.ExchangeModes(ctx, fuzznative.ModeRequest{Connection: connection, Mode: item.mode, Calls: calls, DML: nil, ID: 0, Version: 0})
		require.NoError(t, err)
		require.True(t, response.Error == "" && response.Panic == "", "%s mode failed at %s: %s %s", item.mode, response.Stage, response.Error, response.Panic)
		results[item.mode] = response.Calls
	}
	ordered := map[string]bool{}
	for index := range scenario.Queries {
		ordered[scenario.Queries[index].Name] = scenario.Queries[index].Ordered
	}
	problems := queryModeProblems(packages, results, ordered)
	problems = append(problems, recordAcrossDrivers(t, suite, "query-modes", scenario.Data, blobAgnostic(results[modeSQL]))...)
	require.Empty(t, problems, "execution modes differ for input %v:\n%s\nqueries:\n%s", scenario.Data, strings.Join(problems, "\n"), catalogue)
}

// queryModePackages generates the scenario's catalogue in every mode the emitters accept.
//
// Takes t (*testing.T) which reports a refusal of only the stream or one forms.
// Takes suite (NativeFuzzSuite) which supplies the dialect and driver.
// Takes scenario (fuzzcase.Scenario) which supplies the schema.
// Takes catalogue (string) which holds every form of every query.
//
// Returns []modePackage which starts with the database/sql package.
func queryModePackages(ctx context.Context, t *testing.T, suite NativeFuzzSuite, scenario fuzzcase.Scenario, catalogue string) []modePackage {
	t.Helper()
	packages := make([]modePackage, 0, len(executionModes(suite.Engine)))
	for _, mode := range executionModes(suite.Engine) {
		files, err := generateMode(ctx, suite, mode, scenario.Schema, catalogue)
		if err != nil && mode == modeSQL {
			if _, plain := generateMode(ctx, suite, mode, scenario.Schema, modeCatalogueMany(scenario)); plain == nil {
				require.NoError(t, err, "Seagull accepts the queries as many but refuses their stream or one forms")
			}
			t.Skip("Seagull refuses the scenario. FuzzQueryEquivalence owns that outcome")
		}
		if err != nil {
			t.Logf("the %s emitter refuses the scenario: %v", mode, err)
			continue
		}
		packages = append(packages, modePackage{mode: mode, files: files})
	}
	return packages
}

// modeCatalogue writes each query three times, as many, stream and one, and lists the
// calls that run them.
//
// Takes scenario (fuzzcase.Scenario) which supplies the queries.
//
// Returns string which is the annotated query file.
// Returns []fuzznative.ModeCall which runs every form.
func modeCatalogue(scenario fuzzcase.Scenario) (string, []fuzznative.ModeCall) {
	var catalogue strings.Builder
	calls := make([]fuzznative.ModeCall, 0, len(scenario.Queries)*len(modeCommands))
	for index := range scenario.Queries {
		query := &scenario.Queries[index]
		body, ok := queryBody(query)
		commands := modeCommands
		if !ok {
			body, commands = query.SQL, []string{commandMany}
		}
		for _, command := range commands {
			name := modeMethod(query.Name, command)
			if ok {
				fmt.Fprintf(&catalogue, "-- sgl.query(%s, %s)\n%s\n\n", name, command, body)
			} else {
				fmt.Fprintf(&catalogue, "%s\n\n", body)
			}
			calls = append(calls, fuzznative.ModeCall{Method: name, Command: command, Arguments: query.Values})
		}
	}
	return catalogue.String(), calls
}

// modeCatalogueMany writes each query once as many, to tell a refused stream or one form
// from a refused query.
//
// Takes scenario (fuzzcase.Scenario) which supplies the queries.
//
// Returns string which is the annotated query file.
func modeCatalogueMany(scenario fuzzcase.Scenario) string {
	var catalogue strings.Builder
	for index := range scenario.Queries {
		query := &scenario.Queries[index]
		body, ok := queryBody(query)
		if ok {
			body = "-- sgl.query(" + query.Name + ", many)\n" + body
		} else {
			body = query.SQL
		}
		catalogue.WriteString(body + "\n\n")
	}
	return catalogue.String()
}

// queryBody strips a plain many directive from a scenario query.
//
// Takes query (*fuzzcase.ScenarioQuery) which holds the directive and SQL.
//
// Returns string which is the SQL after the directive.
// Returns bool which is false when the query is not a plain many query.
func queryBody(query *fuzzcase.ScenarioQuery) (string, bool) {
	header, body, found := strings.Cut(query.SQL, "\n")
	if !found || header != "-- sgl.query("+query.Name+", many)" {
		return "", false
	}
	return body, true
}

// modeMethod names the method of one form of a query.
//
// Takes name (string) which is the scenario's method name.
// Takes command (string) which is the form's command.
//
// Returns string which keeps the many form's name.
func modeMethod(name, command string) string {
	switch command {
	case commandStream:
		return name + "Stream"
	case commandOne:
		return name + "One"
	default:
		return name
	}
}

// queryModeProblems compares every mode with database/sql and each form with many.
//
// Takes packages ([]modePackage) which lists the modes run.
// Takes results (map[string][]fuzznative.ModeResult) which holds each mode's calls.
// Takes ordered (map[string]bool) which marks queries with a defined row order.
//
// Returns []string which lists every difference.
func queryModeProblems(packages []modePackage, results map[string][]fuzznative.ModeResult, ordered map[string]bool) []string {
	var problems []string
	baseline := results[modeSQL]
	for _, item := range packages {
		calls := results[item.mode]
		if len(calls) != len(baseline) {
			problems = append(problems, fmt.Sprintf("%s mode made %d calls, database/sql %d", item.mode, len(calls), len(baseline)))
			continue
		}
		many := map[string]fuzznative.ModeResult{}
		for index, result := range calls {
			name := strings.TrimSuffix(strings.TrimSuffix(result.Method, "Stream"), "One")
			if result.Command == commandMany {
				name = result.Method
				many[name] = result
			}
			if item.mode != modeSQL {
				problems = append(problems, compareModeResults(item.mode+" vs database/sql", baseline[index], result, ordered[name])...)
			}
			problems = append(problems, compareForms(item.mode, many[name], result, ordered[name])...)
		}
	}
	return problems
}

// compareModeResults requires the same outcome and rows from two modes.
//
// Takes label (string) which names the comparison.
// Takes want (fuzznative.ModeResult) which is the database/sql result.
// Takes got (fuzznative.ModeResult) which is the other mode's result.
// Takes ordered (bool) which compares rows in order.
//
// Returns []string which lists the differences.
func compareModeResults(label string, want, got fuzznative.ModeResult, ordered bool) []string {
	label += " " + got.Method
	if (want.Error == "") != (got.Error == "") || want.NoRows != got.NoRows {
		return []string{fmt.Sprintf("%s: outcome differs: database/sql error %q, this mode %q", label, want.Error, got.Error)}
	}
	if got.Command == commandOne && !ordered {
		return nil
	}
	if !sameModeRows(representationFree(want.Rows), representationFree(got.Rows), ordered) {
		return []string{fmt.Sprintf("%s: rows differ:\n  database/sql %v\n  this mode    %v", label, want.Rows, got.Rows)}
	}
	return nil
}

// compareForms requires stream to equal many and one to be many's first row.
//
// Takes mode (string) which names the mode.
// Takes many (fuzznative.ModeResult) which is the many form's result.
// Takes form (fuzznative.ModeResult) which is the form's result.
// Takes ordered (bool) which compares rows in order.
//
// Returns []string which lists the differences.
func compareForms(mode string, many, form fuzznative.ModeResult, ordered bool) []string {
	label := mode + " " + form.Method
	switch form.Command {
	case commandStream:
		if (many.Error == "") != (form.Error == "") || !sameModeRows(many.Rows, form.Rows, ordered) {
			return []string{fmt.Sprintf("%s: stream differs from many: many %v (%s), stream %v (%s)", label, many.Rows, many.Error, form.Rows, form.Error)}
		}
	case commandOne:
		if problem := compareOne(many, form, ordered); problem != "" {
			return []string{label + ": " + problem}
		}
	}
	return nil
}

// compareOne requires a one method to return many's first row, or the no-rows error when
// many returns nothing.
//
// Takes many (fuzznative.ModeResult) which is the many form's result.
// Takes form (fuzznative.ModeResult) which is the one form's result.
// Takes ordered (bool) which requires the first row rather than any row.
//
// Returns string which describes the difference, or nothing.
func compareOne(many, form fuzznative.ModeResult, ordered bool) string {
	switch {
	case many.Error != "":
		return ""
	case len(many.Rows) == 0 && !form.NoRows:
		return fmt.Sprintf("one over no rows returned %v (%q), not the no-rows error", form.Rows, form.Error)
	case len(many.Rows) == 0:
		return ""
	case form.Error != "" || len(form.Rows) != 1:
		return fmt.Sprintf("one failed over %d rows: %s", len(many.Rows), form.Error)
	case ordered && !slices.Equal(many.Rows[0], form.Rows[0]):
		return fmt.Sprintf("one returned %v, not the first row of many %v", form.Rows[0], many.Rows)
	case !slices.ContainsFunc(many.Rows, func(row []string) bool { return slices.Equal(row, form.Rows[0]) }):
		return fmt.Sprintf("one returned %v, which many does not return: %v", form.Rows[0], many.Rows)
	}
	return ""
}

// sameModeRows compares canonical rows in order or as multisets.
//
// Takes want ([][]string) which holds one result's rows.
// Takes got ([][]string) which holds the other's.
// Takes ordered (bool) which compares in order.
//
// Returns bool which reports equality.
func sameModeRows(want, got [][]string, ordered bool) bool {
	if len(want) != len(got) {
		return false
	}
	join := func(rows [][]string) []string {
		joined := make([]string, len(rows))
		for index, row := range rows {
			joined[index] = strings.Join(row, modeFieldSeparator)
		}
		if !ordered {
			slices.Sort(joined)
		}
		return joined
	}
	return slices.Equal(join(want), join(got))
}

// blobAgnostic treats nil and empty byte slices alike when comparing SQLite drivers.
// modernc.org/sqlite scans an empty BLOB as nil, which Seagull reads as SQL NULL for a
// nullable byte slice.
//
// Takes results ([]fuzznative.ModeResult) which holds one driver's observations.
//
// Returns []fuzznative.ModeResult which writes both forms alike.
func blobAgnostic(results []fuzznative.ModeResult) []fuzznative.ModeResult {
	merged := slices.Clone(results)
	for index := range merged {
		rows := make([][]string, len(merged[index].Rows))
		for position, row := range merged[index].Rows {
			rows[position] = slices.Clone(row)
			for field, value := range rows[position] {
				if value == "nil-uint8s" || value == "bytes:" {
					rows[position][field] = modeEmptyBlob
				}
			}
		}
		merged[index].Rows = rows
	}
	return merged
}
