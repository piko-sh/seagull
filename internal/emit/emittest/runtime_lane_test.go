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

package emittest

import (
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const noHarness = "the suite has no Harness, so the runtime lane does not run"

func withRecordedCalls(current *subject, calls map[string][]*laneCall) *subject {
	lane := &runtimeLane{harness: DatabaseSQL()}
	for _, name := range slices.Sorted(maps.Keys(calls)) {
		lane.packages = append(lane.packages, &lanePackage{subject: current, caseName: name,
			name: numberedName("p", len(lane.packages)), calls: calls[name], imports: map[string]bool{}})
	}
	lane.once.Do(func() {})
	current.lane = lane
	return current
}

func sent(query, sql string, arguments ...laneArgument) laneStatement {
	return laneStatement{SQL: sql, QueryName: query, Arguments: arguments}
}

func called(query, label string, expected []expectedValue, statements ...laneStatement) *laneCall {
	return &laneCall{query: query, label: label, expected: expected, outcome: &laneOutcome{Statements: statements, QueryNames: true}}
}

func failing(call *laneCall, text string) *laneCall {
	call.outcome.Error = text
	return call
}

func withoutHarness(cases int) string {
	return strings.Join(slices.Repeat([]string{noHarness}, cases), "\n")
}

func returningRows(call *laneCall, nilSlice bool) *laneCall {
	call.slice = true
	call.outcome.NilSlice = nilSlice
	return call
}

func TestMissingModulesListsImportsNoHarnessModuleProvides(t *testing.T) {
	t.Parallel()

	lane := &runtimeLane{harness: &Harness{Modules: map[string]fs.FS{"github.com/ClickHouse/clickhouse-go/v2": fstest.MapFS{}}}}
	checked := &checkedPackage{external: map[string]bool{
		"github.com/ClickHouse/clickhouse-go/v2":            true,
		"github.com/ClickHouse/clickhouse-go/v2/lib/driver": true,
		"github.com/jackc/pgx/v5":                           true,
		"github.com/ClickHouse/clickhouse-go/v22":           true,
	}}

	require.Equal(t, []string{"github.com/ClickHouse/clickhouse-go/v22", "github.com/jackc/pgx/v5"}, lane.missingModules(checked))
}

func TestCalledQueriesLeaveOutDroppedAndUncalledQueries(t *testing.T) {
	t.Parallel()

	definition := corpusCase{files: []corpusFile{{queries: []corpusQuery{
		query("Called", commandMany, "SELECT id FROM items;", scenarioPlain),
		query("Uncalled", commandMany, "SELECT id FROM items;", scenarioNone),
		query("Dropped", commandMany, "SELECT id FROM items;", scenarioPlain),
	}}}}

	called := calledQueries(definition, []string{"Dropped"})

	require.Len(t, called, 1)
	require.Equal(t, "Called", called[0].name)
}

func TestImportAliasOfIsAnIdentifierPerPath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "emittestImporttime", importAliasOf("time"))
	require.Equal(t, "emittestImportgithub_com_ClickHouse_clickhouse_go_v2", importAliasOf("github.com/ClickHouse/clickhouse-go/v2"))
}

func TestRuntimeLaneWithoutHarnessHoldsNothing(t *testing.T) {
	t.Parallel()

	lane := &runtimeLane{}

	result := lane.result(t.Context(), &subject{}, caseCommands)

	require.Equal(t, noHarness, result.unavailable)
	require.Empty(t, result.calls)
}

func TestPackagesThatDoNotBuildFailTheirChecksRatherThanSkipThem(t *testing.T) {
	t.Parallel()

	const compilerReport = buildFailurePrefix + "p0/queries.sql.go:3:1: undefined: pgx.Batch\n"
	testCases := []struct {
		name              string
		caseName          string
		expectedProblems  []string
		expectedSkip      string
		expectedLaneEntry laneResult
	}{
		{
			name:              "a package the compiler rejected fails",
			caseName:          caseCommands,
			expectedProblems:  []string{"case " + caseCommands + ": " + compilerReport},
			expectedSkip:      "",
			expectedLaneEntry: laneResult{unavailable: "", failure: compilerReport, calls: nil},
		},
		{
			name:              "a package whose module the harness lacks is skipped",
			caseName:          caseSlices,
			expectedProblems:  nil,
			expectedSkip:      "case slices imports example.com/driver, which the harness does not provide",
			expectedLaneEntry: laneResult{unavailable: "case slices imports example.com/driver, which the harness does not provide", failure: "", calls: nil},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			current := &subject{}
			lane := &runtimeLane{harness: DatabaseSQL()}
			current.lane = lane
			lane.packages = []*lanePackage{
				{subject: current, caseName: caseCommands, name: "p0", failure: compilerReport},
				{subject: current, caseName: caseSlices, name: "p1", unavailable: "case slices imports example.com/driver, which the harness does not provide"},
			}
			lane.once.Do(func() {})

			require.Equal(t, testCase.expectedLaneEntry, lane.result(t.Context(), current, testCase.caseName))
			result := current.overCalls(t.Context(), []string{testCase.caseName}, nil, func(string, *laneCall) []string { return nil })
			require.Equal(t, testCase.expectedProblems, result.Problems)
			require.Equal(t, testCase.expectedSkip, result.NotApplicable)
		})
	}
}
