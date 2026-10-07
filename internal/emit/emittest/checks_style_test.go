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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const styledQueries = `package store

import "context"

type Queries struct{}

// ListItems lists the items.
func (queries *Queries) ListItems(ctx context.Context) ([]ListItemsRow, error) {
	return []ListItemsRow{}, nil
}

func (queries *Queries) DeleteItem(ctx context.Context, id int64) error {
	return nil
}

type ListItemsRow struct {
	ID    int64        ` + "`json:\"id,omitempty\" db:\"id\"`" + `
	Kind  string       ` + "`db:\"kind\" json:\"kind,omitempty\"`" + `
	Name  string       ` + "`json:\"name\" db:\"name\"`" + `
	Label string       ` + "`json:\"label,omitempty\" db:\"title\"`" + `
	Owner *ListItemsOwner
}

type ListItemsOwner struct {
	Rank int32 ` + "`json:\"rank,omitempty\"`" + `
}
`

const styledInterface = `package store

import "context"

// Querier lists the methods.
type Querier interface {
	// ListItems lists the items.
	ListItems(ctx context.Context) ([]ListItemsRow, error)
}

var _ Querier = (*Queries)(nil)
`

const rowTypeQueries = `package store

import (
	"context"
	"iter"
)

type Queries struct{}

type Items struct {
	ID int64
}

type ItemKind struct {
	Kind string
}

type FirstKindRow struct {
	Kind string
}

func (queries *Queries) ListItemModels(ctx context.Context) ([]Items, error) {
	return nil, nil
}

func (queries *Queries) StreamItemModels(ctx context.Context) iter.Seq2[Items, error] {
	return nil
}

func (queries *Queries) FirstItemModel(ctx context.Context) (*ItemKind, error) {
	return nil, nil
}

func (queries *Queries) ListKinds(ctx context.Context) func(func(ItemKind, error) bool) {
	return nil
}

func (queries *Queries) FirstKind(ctx context.Context) (FirstKindRow, error) {
	return FirstKindRow{}, nil
}
`

const describedQueries = `package store

import "context"

const listDescribedSQL = "SELECT id, kind FROM items WHERE kind = $1"

const deleteDescribedSQL = "DELETE FROM items WHERE id = $1"

const countDescribedSQL = "SELECT count(*) AS total FROM items"

type Queries struct{}

// ListDescribed lists the items of one kind.
//
// It reads them in no particular order.
func (queries *Queries) ListDescribed(ctx context.Context, kind string) error {
	return queries.run(ctx, listDescribedSQL, kind)
}

// DeleteDescribed deletes one item.
func (queries *Queries) DeleteDescribed(ctx context.Context, id int64) error {
	return queries.run(ctx, deleteDescribedSQL, id)
}

func (queries *Queries) CountDescribed(ctx context.Context) error {
	return queries.run(ctx, countDescribedSQL)
}

func (queries *Queries) run(ctx context.Context, sql string, arguments ...any) error {
	return nil
}
`

const describedInterface = `package store

import "context"

type Querier interface {
	// ListDescribed lists the items of one kind.
	//
	// It reads them in no particular order.
	ListDescribed(ctx context.Context, kind string) error

	// DeleteDescribed deletes one item.
	DeleteDescribed(ctx context.Context, id int64) error

	CountDescribed(ctx context.Context) error
}
`

const rowTypesQueries = `package store

import (
	"context"
	"iter"
)

type Queries struct{}

type Items struct {
	ID   int64
	Kind string
}

type ItemKind struct {
	ID   int64
	Kind string
}

func (queries *Queries) ListItemModels(ctx context.Context, kind string) ([]Items, error) {
	return []Items{}, nil
}

func (queries *Queries) FirstItemModel(ctx context.Context, id int64) (*Items, error) {
	return &Items{}, nil
}

func (queries *Queries) StreamItemModels(ctx context.Context, rank int64) iter.Seq2[Items, error] {
	return nil
}

func (queries *Queries) ListKinds(ctx context.Context, rank int64) func(func(ItemKind, error) bool) {
	return nil
}

func (queries *Queries) FirstKind(ctx context.Context, id int64) (ItemKind, error) {
	return ItemKind{}, nil
}
`

func styledPackage(t *testing.T) (*app.Result, *checkedPackage) {
	t.Helper()

	files := []sqlmodel.GeneratedFile{
		{Name: "items.sql.go", Content: []byte(styledQueries)},
		{Name: interfaceFile, Content: []byte(styledInterface)},
	}
	return &app.Result{Files: files}, checkedWithoutToolchain(t, files...)
}

func TestInterfaceProblemsFindAMethodTheInterfaceLeavesOut(t *testing.T) {
	t.Parallel()

	generated, checked := styledPackage(t)

	require.Equal(t, []string{"Querier lists [ListItems]; the query files declare [DeleteItem ListItems] on Queries"}, interfaceProblems(generated, checked))
	require.Equal(t, []string{"the generation wrote no querier_interface.go though the preset asks for the interface"},
		interfaceProblems(&app.Result{Files: generated.Files[:1]}, checked))
	require.Equal(t, []string{"the output does not type-check"}, interfaceProblems(generated, nil))
}

func TestTagProblemsFindFieldsWithoutTheTagsTheStyleAsks(t *testing.T) {
	t.Parallel()

	_, checked := styledPackage(t)

	require.Equal(t, []string{
		"items.sql.go:18:2: ListItemsRow.Kind has the tag \"db:\\\"kind\\\" json:\\\"kind,omitempty\\\"\", which does not start with json",
		"items.sql.go:19:2: ListItemsRow.Name has the json tag \"name\", without the omitempty option",
		"items.sql.go:20:2: ListItemsRow.Label has the tag \"json:\\\"label,omitempty\\\" db:\\\"title\\\"\", whose db tag does not name the column the json tag does",
		"items.sql.go:25:2: ListItemsOwner.Rank has the tag \"json:\\\"rank,omitempty\\\"\", whose db tag does not name the column the json tag does",
	}, tagProblems(checked))
}

func TestDocumentationProblemsCompareEachMethodWithItsDescription(t *testing.T) {
	t.Parallel()

	_, checked := styledPackage(t)
	listed := query("ListItems", commandMany, "SELECT id FROM items;", scenarioNone)
	listed.description = "ListItems lists the items."
	deleted := query("DeleteItem", commandExec, "DELETE FROM items;", scenarioNone)
	deleted.description = "DeleteItem deletes one item."
	definition := corpusCase{name: caseDescribed, files: singleFile(listed, deleted)}

	require.Equal(t, []string{
		"method DeleteItem is documented \"\", not \"DeleteItem deletes one item.\\n\"",
		"Querier.DeleteItem is documented \"\", not \"DeleteItem deletes one item.\\n\"",
	}, documentationProblems(definition, checked))
}

func TestNilRowsProblemsReportACallThatReturnedNil(t *testing.T) {
	t.Parallel()

	empty := &laneCall{outcome: &laneOutcome{NilSlice: false}, query: "ListItems", label: labelPlain, slice: true}
	nilRows := &laneCall{outcome: &laneOutcome{NilSlice: true}, query: "ListItems", label: labelPlain, slice: true}
	failed := &laneCall{outcome: &laneOutcome{NilSlice: true, Error: "boom"}, query: "ListItems", label: labelPlain, slice: true}
	single := &laneCall{outcome: &laneOutcome{NilSlice: true}, query: "GetItem", label: labelPlain, slice: false}

	require.Empty(t, nilRowsProblems(caseCommands, empty))
	require.Equal(t, []string{describeCall(caseCommands, nilRows) + " returned nil for no rows rather than an empty slice"}, nilRowsProblems(caseCommands, nilRows))
	require.Empty(t, nilRowsProblems(caseCommands, failed))
	require.Empty(t, nilRowsProblems(caseCommands, single))
}

func TestRowTypeProblemsFindAQueryReadingRowsIntoAStructOfItsOwn(t *testing.T) {
	t.Parallel()

	checked := checkedWithoutToolchain(t, sqlmodel.GeneratedFile{Name: "items.sql.go", Content: []byte(rowTypeQueries)})

	require.Equal(t, []string{
		"method FirstItemModel reads its rows into [ItemKind], not Items",
		"method FirstKind reads its rows into [FirstKindRow], not ItemKind",
		"the package declares FirstKindRow, a Row struct of the query's own",
	}, rowTypeProblems(checked))
	require.Equal(t, []string{"the output does not type-check"}, rowTypeProblems(nil))
}

func TestEachStyleCheckPassesGoodOutputAndReportsBrokenOutput(t *testing.T) {
	t.Parallel()

	interfaceOf := func(contract string) *caseResult {
		return generatedFiles(storeFiles(storeQueries, contract, storeModels)...)
	}
	untaggedModel := replaced(t, storeModels, "\tKind string `json:\"kind,omitempty\" db:\"kind\"`", "\tKind string")
	keptEmpty := replaced(t, storeQueries, "`json:\"name,omitempty\" db:\"name\"`", "`json:\"name\" db:\"name\"`")
	undocumented := replaced(t, describedQueries, "// DeleteDescribed deletes one item.\n", "")
	overDocumented := replaced(t, describedInterface, "\tCountDescribed(", "\t// CountDescribed counts the items.\n\tCountDescribed(")
	ownRows := "package store\n\ntype ItemKind struct {\n\tID int64\n}\n\ntype SearchItemModelsRow struct {\n\tID int64\n}\n"
	testCases := []struct {
		generations map[string]*caseResult
		name        string
		check       string
		expected    knownfailure.Outcome
	}{
		{
			name:        "the interface lists every query method",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "an interface that leaves out a method fails",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: interfaceOf("package store\n\ntype Querier interface{}\n")},
			expected:    knownfailure.OutcomeOf([]string{"case commands: Querier lists []; the query files declare [GetItem] on Queries"}),
		},
		{
			name:        "an interface whose method *Queries does not have fails",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: interfaceOf(replaced(t, storeInterface, "id int64", "id string"))},
			expected:    knownfailure.OutcomeOf([]string{"case commands: *Queries does not implement Querier"}),
		},
		{
			name:        "a generation without the interface's file fails",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: generatedFiles(goFile("queries.sql.go", storeQueries), goFile(modelsFile, storeModels))},
			expected:    knownfailure.OutcomeOf([]string{"case commands: the generation wrote no querier_interface.go though the preset asks for the interface"}),
		},
		{
			name:        "an interface's file that declares a struct in its place fails",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: interfaceOf("package store\n\ntype Querier struct{}\n")},
			expected:    knownfailure.OutcomeOf([]string{"case commands: Querier is not an interface"}),
		},
		{
			name:        "an interface's file that declares no interface fails",
			check:       "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{caseCommands: interfaceOf("package store\n")},
			expected:    knownfailure.OutcomeOf([]string{"case commands: the package declares no Querier type and Queries struct"}),
		},
		{
			name:  "a described case the emitter refused fails, and a general case is left to other checks",
			check: "QuerierInterfaceListsEveryQueryMethod",
			generations: map[string]*caseResult{
				caseCommands:  refusedGeneration("Q100", "refused"),
				caseDescribed: refusedGeneration("Q100", "refused"),
			},
			expected: knownfailure.OutcomeOf([]string{"case described: the emitter refused queries every emitter must generate: [Q100 :0 refused]"}),
		},
		{
			name:        "every column field carries the tags the style asks for",
			check:       "FieldsCarryTheTagsTheStyleAsks",
			generations: map[string]*caseResult{caseCommands: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a case the emitter refused is left to GeneratesWithoutError when reading tags",
			check:       "FieldsCarryTheTagsTheStyleAsks",
			generations: map[string]*caseResult{caseCommands: refusedGeneration("Q100", "refused"), caseNamed: storeGeneration(storeQueries)},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "a model field without tags and a row field without omitempty fail",
			check:       "FieldsCarryTheTagsTheStyleAsks",
			generations: map[string]*caseResult{caseCommands: generatedFiles(storeFiles(keptEmpty, storeInterface, untaggedModel)...)},
			expected: knownfailure.OutcomeOf([]string{
				`case commands: models.go:7:2: Items.Kind has the tag "", which does not start with json`,
				`case commands: queries.sql.go:13:2: GetItemRow.Name has the json tag "name", without the omitempty option`,
			}),
		},
		{
			name:        "each description is its method's doc comment",
			check:       "DescriptionsDocumentQueryMethods",
			generations: map[string]*caseResult{caseDescribed: generatedFiles(goFile("queries.sql.go", describedQueries), goFile(interfaceFile, describedInterface))},
			expected:    knownfailure.Passed(),
		},
		{
			name:  "a method without its description and an undescribed method with a doc comment fail",
			check: "DescriptionsDocumentQueryMethods",
			generations: map[string]*caseResult{
				caseDescribed: generatedFiles(goFile("queries.sql.go", undocumented), goFile(interfaceFile, overDocumented)),
			},
			expected: knownfailure.OutcomeOf([]string{
				`case described: method DeleteDescribed is documented "", not "DeleteDescribed deletes one item.\n"`,
				`case described: Querier.CountDescribed is documented "CountDescribed counts the items.\n", not ""`,
			}),
		},
		{
			name:        "a described case that generated nothing fails",
			check:       "DescriptionsDocumentQueryMethods",
			generations: map[string]*caseResult{caseDescribed: generatedFiles()},
			expected:    knownfailure.OutcomeOf([]string{"case described: the generation produced no files"}),
		},
		{
			name:        "each query reads its rows into the struct it names",
			check:       "QueriesReadRowsIntoTheStructTheyName",
			generations: map[string]*caseResult{caseRowTypes: generatedFiles(goFile("queries.sql.go", rowTypesQueries))},
			expected:    knownfailure.Passed(),
		},
		{
			name:        "queries reading rows into other structs fail",
			check:       "QueriesReadRowsIntoTheStructTheyName",
			generations: map[string]*caseResult{caseRowTypes: generatedFiles(goFile("queries.sql.go", rowTypeQueries))},
			expected: knownfailure.OutcomeOf([]string{
				"case rowtypes: method FirstItemModel reads its rows into [ItemKind], not Items",
				"case rowtypes: method FirstKind reads its rows into [FirstKindRow], not ItemKind",
				"case rowtypes: the package declares FirstKindRow, a Row struct of the query's own",
			}),
		},
		{
			name:        "a shared row declared twice and a builder's row of its own fail",
			check:       "QueriesReadRowsIntoTheStructTheyName",
			generations: map[string]*caseResult{caseRowTypes: generatedFiles(goFile("queries.sql.go", rowTypesQueries), goFile("rows.go", ownRows))},
			expected: knownfailure.OutcomeOf([]string{
				"case rowtypes: the package declares ItemKind 2 times, not once",
				"case rowtypes: the package declares SearchItemModelsRow, a Row struct of the runtime builder's own",
			}),
		},
		{
			name:        "a row types case that generated nothing fails",
			check:       "QueriesReadRowsIntoTheStructTheyName",
			generations: map[string]*caseResult{caseRowTypes: generatedFiles()},
			expected:    knownfailure.OutcomeOf([]string{"case rowtypes: the generation produced no files"}),
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

func TestManyReturnsAnEmptySliceReadsWhatEachCallReturned(t *testing.T) {
	t.Parallel()

	listed := func(nilSlice bool) *laneCall {
		return returningRows(called("ListItems", labelPlain, nil, sent("ListItems", "SELECT id FROM items WHERE kind = $1", positional("emittest0001"))), nilSlice)
	}
	testCases := []struct {
		calls    map[string][]*laneCall
		name     string
		expected knownfailure.Outcome
	}{
		{
			name:     "an empty slice for no rows",
			calls:    map[string][]*laneCall{caseCommands: {listed(false)}},
			expected: knownfailure.Passed(),
		},
		{
			name:     "a nil slice for no rows fails",
			calls:    map[string][]*laneCall{caseCommands: {listed(true), failing(listed(true), "refused")}},
			expected: knownfailure.OutcomeOf([]string{"case commands, ListItems (plain call) returned nil for no rows rather than an empty slice"}),
		},
		{
			name:     "the check applies to nothing without a harness",
			calls:    nil,
			expected: knownfailure.NotApplicable(withoutHarness(len(generalCaseNames()))),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			current := recordedSubject(t, postgres.New(), nil)
			if testCase.calls != nil {
				withRecordedCalls(current, testCase.calls)
			}

			require.Equal(t, testCase.expected, checkNamed(t, "ManyReturnsAnEmptySlice").run(t.Context(), current))
		})
	}
}
