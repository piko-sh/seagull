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
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const storeQueries = `package store

import "context"

const getItemSQL = "SELECT id, name FROM items WHERE id = $1"

type Queries struct{}

type GetItemRow struct {
	ID   int64  ` + "`json:\"id,omitempty\" db:\"id\"`" + `
	Name string ` + "`json:\"name,omitempty\" db:\"name\"`" + `
}

func (queries *Queries) GetItem(ctx context.Context, id int64) (GetItemRow, error) {
	var row GetItemRow
	err := queries.scan(ctx, getItemSQL, []any{id}, &row.ID, &row.Name)
	return row, err
}

func (queries *Queries) scan(ctx context.Context, sql string, arguments []any, targets ...any) error {
	return nil
}
`

const storeInterface = `package store

import "context"

type Querier interface {
	GetItem(ctx context.Context, id int64) (GetItemRow, error)
}
`

const storeModels = `package store

type Items struct {
	ID   int64  ` + "`json:\"id,omitempty\" db:\"id\"`" + `
	Kind string ` + "`json:\"kind,omitempty\" db:\"kind\"`" + `
}
`

const sortedQueries = `package store

import "context"

type OrderDirection string

const sortedFirstSQL = "SELECT id, kind FROM items WHERE rank > $1"

type Queries struct{}

func (queries *Queries) SortedFirst(ctx context.Context, rank int64, direction OrderDirection) error {
	return queries.run(ctx, sortedFirstSQL+string(direction), rank)
}

func (queries *Queries) run(ctx context.Context, sql string, arguments ...any) error {
	return nil
}
`

const sortedSecondQueries = `package store

import "context"

const sortedSecondSQL = "SELECT id, kind FROM items WHERE kind = $1"

func (queries *Queries) SortedSecond(ctx context.Context, kind string, direction OrderDirection) error {
	return queries.run(ctx, sortedSecondSQL+string(direction), kind)
}
`

const nulBytesQueries = `package store

const bytesSQL = "SELECT id FROM items WHERE name = 'x\x00y'"
`

func TestGeneratedCleanlyExplainsWhatCannotBeExamined(t *testing.T) {
	t.Parallel()

	refusal := []diag.SourceError{{Code: "Q100", Severity: diag.SeverityError, Message: "refused"}}
	files := []sqlmodel.GeneratedFile{{Name: "queries.sql.go", Content: []byte("package store\n")}}
	testCases := []struct {
		name       string
		result     *caseResult
		refusable  bool
		expectOK   bool
		expectText string
	}{
		{name: "a raw error", result: &caseResult{generateError: errors.New("boom")}, expectText: "Generate failed instead of reporting a diagnostic: boom"},
		{name: "a refusal of a refusable case", result: &caseResult{generated: &app.Result{Diagnostics: refusal}}, refusable: true},
		{name: "a refusal of a case every emitter generates", result: &caseResult{generated: &app.Result{Diagnostics: refusal}},
			expectText: "the emitter refused queries every emitter must generate"},
		{name: "no files", result: &caseResult{generated: &app.Result{}}, expectText: "the generation produced no files"},
		{name: "files", result: &caseResult{generated: &app.Result{Files: files}}, expectOK: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems, ok := generatedCleanly("c", testCase.result, testCase.refusable)
			require.Equal(t, testCase.expectOK, ok)
			if testCase.expectText == "" {
				require.Empty(t, problems)
				return
			}
			require.Len(t, problems, 1)
			require.Contains(t, problems[0], testCase.expectText)
		})
	}
}

func TestSameOutputComparesAFurtherGeneration(t *testing.T) {
	t.Parallel()

	files := []sqlmodel.GeneratedFile{{Name: "a.go", Content: []byte("package a\n")}}
	changed := []sqlmodel.GeneratedFile{{Name: "a.go", Content: []byte("package b\n")}}
	result := &caseResult{generated: &app.Result{Files: files}}

	require.Empty(t, sameOutput("c", "again", result, &app.Result{Files: files}, nil))
	require.Equal(t, []string{`case c: again changed the output: a.go:1 "package a" became "package b"`},
		sameOutput("c", "again", result, &app.Result{Files: changed}, nil))
	require.Equal(t, []string{"case c: again failed: boom"}, sameOutput("c", "again", result, nil, errors.New("boom")))
}

func storeFiles(queries, contract, models string) []sqlmodel.GeneratedFile {
	return []sqlmodel.GeneratedFile{goFile("queries.sql.go", queries), goFile(interfaceFile, contract), goFile(modelsFile, models)}
}

func storeGeneration(queries string) *caseResult {
	return generatedFiles(storeFiles(queries, storeInterface, storeModels)...)
}

func sortedGeneration(second string) *caseResult {
	return generatedFiles(goFile("first.sql.go", sortedQueries), goFile("second.sql.go", second))
}

func fileNamesGeneration(names ...string) *caseResult {
	files := make([]sqlmodel.GeneratedFile, 0, len(names))
	for _, name := range names {
		files = append(files, goFile(name, "package store\n"))
	}
	return generatedFiles(files...)
}

func unrecordedCases(names []string) string {
	reasons := make([]string, 0, len(names))
	for _, name := range names {
		reasons = append(reasons, "case "+name+": "+notRecorded)
	}
	return strings.Join(reasons, "\n")
}

func TestEachStaticCheckPassesGoodOutputAndReportsBrokenOutput(t *testing.T) {
	t.Parallel()

	undefinedSQL := replaced(t, storeQueries, "getItemSQL, []any", "missingSQL, []any")
	unusedArgument := replaced(t, storeQueries, "[]any{id}", "nil")
	unfilledField := replaced(t, storeQueries, ", &row.Name)", ")")
	unboundAndUnfilled := replaced(t, storeQueries, "[]any{id}, &row.ID, &row.Name", "nil, &row.ID")
	duplicateField := replaced(t, storeQueries, "\tName string", "\tID   int64\n\tName string")
	sourceForm := replaced(t, storeQueries, "id = $1", "id = :id")
	changedLiteral := replaced(t, nulBytesQueries, `'x\x00y'`, `'xy'`)
	redeclaredDirection := replaced(t, sortedSecondQueries, "import \"context\"\n", "import \"context\"\n\ntype OrderDirection string\n")
	tableModel := storeModels + "\ntype Querier struct {\n\tID int64 `json:\"id,omitempty\" db:\"id\"`\n}\n"
	testCases := []struct {
		generations map[string]*caseResult
		name        string
		check       string
		expected    knownfailure.Outcome
	}{
		{
			name:        "every general case generates",
			check:       "GeneratesWithoutError",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:  "a raw error, a refusal and an empty generation fail, and a refusable case's refusal does not",
			check: "GeneratesWithoutError",
			generations: map[string]*caseResult{
				caseCommands: {generateError: errors.New("boom")},
				caseResults:  refusedGeneration("Q100", "refused"),
				caseNamed:    refusedGeneration("Q100", "refused"),
				caseSlices:   generatedFiles(),
			},
			expected: knownfailure.OutcomeOf([]string{
				"case commands: Generate failed instead of reporting a diagnostic: boom",
				"case named: the emitter refused queries every emitter must generate: [Q100 :0 refused]",
				"case slices: the generation produced no files",
			}),
		},
		{
			name:        "every file starts with the header under a name of its own",
			check:       "GeneratedFilesAreHeadered",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:  "a file without the header, two names alike but for case and a name Go ignores fail",
			check: "GeneratedFilesAreHeadered",
			generations: map[string]*caseResult{caseCommands: generatedFiles(
				sqlmodel.GeneratedFile{Name: "queries.sql.go", Content: []byte(storeQueries)},
				goFile(modelsFile, storeModels),
				goFile("Models.go", storeModels),
				goFile("_hidden.go", "package store\n"),
			)},
			expected: knownfailure.OutcomeOf([]string{
				`case commands: queries.sql.go does not start with the header "// Code generated by seagull. DO NOT EDIT."`,
				"case commands: two files are named models.go and Models.go",
				"case commands: the generated file _hidden.go starts with _, so the go command ignores it",
			}),
		},
		{
			name:        "query files named like build constraints give names Go compiles",
			check:       "FileNamesAreSanitised",
			generations: map[string]*caseResult{caseFileNames: fileNamesGeneration("devices_ios_sql.sql.go", "reports_windows_amd64_sql.sql.go", "q_private.sql.go")},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "file names Go reads as build constraints or ignores fail",
			check:       "FileNamesAreSanitised",
			generations: map[string]*caseResult{caseFileNames: fileNamesGeneration("devices_ios.sql.go", "reports_windows_amd64.sql.go", "_private.sql.go")},
			expected: knownfailure.OutcomeOf([]string{
				"case filenames: the generated file devices_ios.sql.go ends in _ios, so it builds only for that target",
				"case filenames: the generated file reports_windows_amd64.sql.go ends in _amd64, so it builds only for that target",
				"case filenames: the generated file _private.sql.go starts with _, so the go command ignores it",
			}),
		},
		{
			name:        "a file names case that generated nothing fails",
			check:       "FileNamesAreSanitised",
			generations: map[string]*caseResult{caseFileNames: generatedFiles()},
			expected:    knownfailure.OutcomeOf([]string{"case filenames: the generation produced no files"}),
		},
		{
			name:        "output that type-checks",
			check:       "OutputTypeChecks",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries), caseResults: refusedGeneration("Q100", "refused")},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "output naming an undeclared constant fails",
			check:       "OutputTypeChecks",
			generations: map[string]*caseResult{caseCommands: storeGeneration(undefinedSQL)},
			expected:    knownfailure.OutcomeOf([]string{"case commands: the output does not type-check:\n\tqueries.sql.go:18:27: undefined: missingSQL"}),
		},
		{
			name:        "the check applies to no case the dialect cannot take",
			check:       "OutputTypeChecks",
			generations: nil,
			expected:    knownfailure.NotApplicable(unrecordedCases(generalCaseNames())),
		},
		{
			name:        "every argument is bound",
			check:       "ParametersAreBound",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries), caseFolded: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "an argument the method never uses fails",
			check:       "ParametersAreBound",
			generations: map[string]*caseResult{caseCommands: storeGeneration(unusedArgument)},
			expected:    knownfailure.OutcomeOf([]string{"case commands: queries.sql.go:16: GetItem never uses its argument id"}),
		},
		{
			name:  "a folded case that does not generate or type-check fails, and a general case is left to other checks",
			check: "ParametersAreBound",
			generations: map[string]*caseResult{
				caseCommands:    refusedGeneration("Q100", "refused"),
				caseFolded:      storeGeneration(undefinedSQL),
				caseFoldedSlice: refusedGeneration("Q100", "refused"),
			},
			expected: knownfailure.OutcomeOf([]string{
				"case folded: the output does not type-check:\n\tqueries.sql.go:18:27: undefined: missingSQL",
				"case foldedslice: the emitter refused queries every emitter must generate: [Q100 :0 refused]",
			}),
		},
		{
			name:        "every row field is filled",
			check:       "RowFieldsAreScanned",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a case the emitter refused is left to GeneratesWithoutError when looking for unfilled fields",
			check:       "RowFieldsAreScanned",
			generations: map[string]*caseResult{caseCommands: refusedGeneration("Q100", "refused"), caseNamed: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a row field nothing fills fails",
			check:       "RowFieldsAreScanned",
			generations: map[string]*caseResult{caseCommands: storeGeneration(unfilledField)},
			expected:    knownfailure.OutcomeOf([]string{"case commands: queries.sql.go:13:2: nothing fills GetItemRow.Name"}),
		},
		{
			name:        "parameters named like the generated code's locals generate",
			check:       "ReservedParameterNames",
			generations: map[string]*caseResult{caseReservedParameters: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "parameters named like the generated code's locals that fail to generate fail",
			check:       "ReservedParameterNames",
			generations: map[string]*caseResult{caseReservedParameters: {generateError: errors.New("boom")}},
			expected:    knownfailure.OutcomeOf([]string{"case reservedparameters: Generate failed instead of reporting a diagnostic: boom"}),
		},
		{
			name:        "queries named like the package's declarations generate",
			check:       "ReservedQueryNames",
			generations: map[string]*caseResult{caseReservedQueries: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "queries named like the package's declarations that lose an argument and a row field fail",
			check:       "ReservedQueryNames",
			generations: map[string]*caseResult{caseReservedQueries: storeGeneration(unboundAndUnfilled)},
			expected: knownfailure.OutcomeOf([]string{
				"case reservedqueries: queries.sql.go:16: GetItem never uses its argument id",
				"case reservedqueries: queries.sql.go:13:2: nothing fills GetItemRow.Name",
			}),
		},
		{
			name:        "tables named like the package's types generate",
			check:       "ReservedTableNames",
			generations: map[string]*caseResult{caseReservedTables: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a table model that redeclares the interface fails",
			check:       "ReservedTableNames",
			generations: map[string]*caseResult{caseReservedTables: generatedFiles(storeFiles(storeQueries, storeInterface, tableModel)...)},
			expected: knownfailure.OutcomeOf([]string{"case reservedtables: the output does not type-check:\n\tmodels.go:10:6: Querier redeclared in this block\n" +
				"\tquerier_interface.go:7:6: \tother declaration of Querier"}),
		},
		{
			name:        "a grouped row of distinct fields",
			check:       "DuplicateGroupedColumns",
			generations: map[string]*caseResult{caseGroupedColumns: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a grouped row declaring one field twice fails",
			check:       "DuplicateGroupedColumns",
			generations: map[string]*caseResult{caseGroupedColumns: storeGeneration(duplicateField)},
			expected: knownfailure.OutcomeOf([]string{"case groupedcolumns: the output does not type-check:\n\tqueries.sql.go:13:2: ID redeclared\n" +
				"\tqueries.sql.go:12:2: \tother declaration of ID"}),
		},
		{
			name:        "two sortable files declare their shared types once",
			check:       "DeclarationsAreUnique",
			generations: map[string]*caseResult{caseTwoSortableFiles: sortedGeneration(sortedSecondQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "two sortable files that each declare the order direction fail",
			check:       "DeclarationsAreUnique",
			generations: map[string]*caseResult{caseTwoSortableFiles: sortedGeneration(redeclaredDirection)},
			expected: knownfailure.OutcomeOf([]string{"case twosortablefiles: the output does not type-check:\n\tsecond.sql.go:7:6: OrderDirection redeclared in this block\n" +
				"\tfirst.sql.go:7:6: \tother declaration of OrderDirection"}),
		},
		{
			name:        "SQL constants in the execution form",
			check:       "PlaceholdersUseExecutionForm",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a case the emitter refused is left to GeneratesWithoutError when reading SQL constants",
			check:       "PlaceholdersUseExecutionForm",
			generations: map[string]*caseResult{caseCommands: refusedGeneration("Q100", "refused"), caseNamed: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a SQL constant keeping a source form fails",
			check:       "PlaceholdersUseExecutionForm",
			generations: map[string]*caseResult{caseCommands: storeGeneration(sourceForm)},
			expected: knownfailure.OutcomeOf([]string{
				`case commands: queries.sql.go:7:7: the SQL constant getItemSQL writes ":id", which is not the execution form $1`,
			}),
		},
		{
			name:        "a literal holding a NUL reaches a SQL constant unchanged",
			check:       "SQLBytesRoundTrip",
			generations: map[string]*caseResult{"bytesnul": generatedFiles(goFile("queries.sql.go", nulBytesQueries))},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a literal that loses its NUL fails",
			check:       "SQLBytesRoundTrip",
			generations: map[string]*caseResult{"bytesnul": generatedFiles(goFile("queries.sql.go", changedLiteral))},
			expected:    knownfailure.OutcomeOf([]string{`case bytesnul: no SQL constant holds the literal "'x\x00y'" unchanged`}),
		},
		{
			name:        "a byte case whose output does not type-check fails",
			check:       "SQLBytesRoundTrip",
			generations: map[string]*caseResult{"bytesnul": generatedFiles(goFile("queries.sql.go", nulBytesQueries+"\nvar broken = missing\n"))},
			expected:    knownfailure.OutcomeOf([]string{"case bytesnul: the output does not type-check:\n\tqueries.sql.go:7:14: undefined: missing"}),
		},
		{
			name:        "a byte case that generated nothing fails",
			check:       "SQLBytesRoundTrip",
			generations: map[string]*caseResult{"bytesnul": generatedFiles()},
			expected:    knownfailure.OutcomeOf([]string{"case bytesnul: the generation produced no files"}),
		},
		{
			name:        "a second generation writes the same files",
			check:       "OutputIsDeterministic",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:  "a second generation that changes a line fails",
			check: "OutputIsDeterministic",
			generations: map[string]*caseResult{caseCommands: {
				generated: storeGeneration(storeQueries).generated,
				again:     storeGeneration(unusedArgument).generated,
			}},
			expected: knownfailure.OutcomeOf([]string{
				`case commands: generating a second time changed the output: queries.sql.go:18 "\terr := queries.scan(ctx, getItemSQL, []any{id}, &row.ID, &row.Name)" ` +
					`became "\terr := queries.scan(ctx, getItemSQL, nil, &row.ID, &row.Name)"`,
			}),
		},
		{
			name:        "a second generation that fails fails",
			check:       "OutputIsDeterministic",
			generations: map[string]*caseResult{caseCommands: {generated: storeGeneration(storeQueries).generated, againError: errors.New("boom")}},
			expected:    knownfailure.OutcomeOf([]string{"case commands: generating a second time failed: boom"}),
		},
		{
			name:        "a generation of the queries reversed writes the same files",
			check:       "OutputIgnoresQueryOrder",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:  "a generation of the queries reversed that orders the files otherwise fails",
			check: "OutputIgnoresQueryOrder",
			generations: map[string]*caseResult{caseCommands: {
				generated: storeGeneration(storeQueries).generated,
				reversed:  &app.Result{Header: recordedHeader, Files: []sqlmodel.GeneratedFile{goFile(modelsFile, storeModels), goFile("queries.sql.go", storeQueries)}},
			}},
			expected: knownfailure.OutcomeOf([]string{
				"case commands: generating with the queries reversed changed the output: " +
					"the files [queries.sql.go querier_interface.go models.go] became [models.go queries.sql.go]",
			}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			current := recordedSubject(t, postgres.New(), testCase.generations)

			require.Equal(t, testCase.expected, checkNamed(t, testCase.check).run(t.Context(), current))
		})
	}
}
