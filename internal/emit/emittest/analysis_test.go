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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

const bindingSource = `package store

import "context"

type Queries struct{}

type FindParams struct {
	Kind  string
	Kind2 string
}

type FindRow struct {
	ID   int64
	Name string
}

type FindGroup struct {
	Label string
}

type GroupedRow struct {
	ID    int64
	Group *FindGroup
}

func (queries *Queries) Find(ctx context.Context, params FindParams) ([]FindRow, error) {
	use(ctx, params.Kind, params.Kind)
	var row FindRow
	use(&row.ID)
	return []FindRow{row}, nil
}

func (queries *Queries) Get(ctx context.Context, id int64) (FindRow, error) {
	return FindRow{ID: 1}, nil
}

func (queries *Queries) Grouped(ctx context.Context) func(func(GroupedRow, error) bool) {
	return func(yield func(GroupedRow, error) bool) {
		yield(GroupedRow{ID: 1, Group: &FindGroup{Label: "x"}}, nil)
	}
}

func (queries *Queries) helper(id int64) {}

func use(...any) {}
`

const wholeValueSource = `package store

import "context"

type Queries struct{}

type Berth struct {
	Pontoon string
	Number  int32
}

type BoatRow struct {
	ID    int64
	Berth Berth
}

func (queries *Queries) Moor(ctx context.Context, berth Berth) error {
	use(ctx, berth)
	return nil
}

func (queries *Queries) Boat(ctx context.Context) (BoatRow, error) {
	var row BoatRow
	use(&row.ID, &row.Berth)
	return row, nil
}

func use(...any) {}
`

const contextStandIn = `package context

type Context interface {
	Done() <-chan struct{}
}
`

const stringsStandIn = `package strings

func ToUpper(text string) string { return text }
`

const timeStandIn = `package time

type Time struct{ wall uint64 }
`

const iterStandIn = `package iter

type Seq2[K, V any] func(yield func(K, V) bool)
`

type standInImporter struct {
	fileSet *token.FileSet
	sources map[string]string
}

func (importer standInImporter) Import(importPath string) (*types.Package, error) {
	source, ok := importer.sources[importPath]
	if !ok {
		return nil, fmt.Errorf("no stand-in for %s", importPath)
	}
	parsed, err := parser.ParseFile(importer.fileSet, importPath+".go", source, 0)
	if err != nil {
		return nil, err
	}
	config := types.Config{}
	return config.Check(importPath, importer.fileSet, []*ast.File{parsed}, nil)
}

func checkedWithoutToolchain(t *testing.T, files ...sqlmodel.GeneratedFile) *checkedPackage {
	t.Helper()

	fileSet := token.NewFileSet()
	standard := standInImporter{fileSet: fileSet, sources: map[string]string{
		"context": contextStandIn, "strings": stringsStandIn, "time": timeStandIn, "iter": iterStandIn,
	}}
	return typeCheckWith(fileSet, standard, files)
}

func checkedSource(t *testing.T, source string) *checkedPackage {
	t.Helper()

	return checkedWithoutToolchain(t, sqlmodel.GeneratedFile{Name: "queries.sql.go", Content: []byte(source)})
}

func TestBindingProblemsFindUnreadFieldsAndUnusedArguments(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, bindingSource)

	require.Empty(t, checked.problems)
	require.Equal(t, []string{
		"queries.sql.go:26: Find never reads FindParams.Kind2",
		"queries.sql.go:33: Get never uses its argument id",
	}, bindingProblems(checked))
}

func TestRowProblemsFindFieldsNothingFills(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, bindingSource)

	require.Equal(t, []string{"queries.sql.go:14:2: nothing fills FindRow.Name"}, rowProblems(checked))
}

func TestAnalysisLeavesTheFieldsOfAValueHandledWholeToItsHelper(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, wholeValueSource)

	require.Empty(t, checked.problems)
	require.Empty(t, bindingProblems(checked), "a struct argument bound whole has its fields read by the binding")
	require.Empty(t, rowProblems(checked), "a struct field scanned whole has its fields filled by the scan")
}

func TestPlaceholderProblemsHoldConstantsToTheExecutionForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   dialect.DialectPort
		constant string
		expected []string
	}{
		{name: "numbered placeholders pass", engine: postgres.New(), constant: "SELECT id FROM items WHERE id = $1 AND kind = $2", expected: nil},
		{
			name:     "a source form is not the execution form",
			engine:   postgres.New(),
			constant: "SELECT id FROM items WHERE kind = :kind",
			expected: []string{`writes ":kind", which is not the execution form $1`},
		},
		{name: "anonymous placeholders pass", engine: mysql.New(), constant: "SELECT id FROM items WHERE id = ? AND kind = ?", expected: nil},
		{
			name:     "a numbered anchor form left in the SQL is foreign",
			engine:   mysql.New(),
			constant: "SELECT id FROM items WHERE id IN (?1)",
			expected: []string{`writes "?1", which is not the execution form ?`},
		},
		{name: "prefixed named placeholders pass", engine: clickhouse.New(), constant: "SELECT id FROM items WHERE id = {p_id:UInt64}", expected: nil},
		{
			name:     "a named placeholder needs the name prefix",
			engine:   clickhouse.New(),
			constant: "SELECT id FROM items WHERE id = {id:UInt64}",
			expected: []string{`writes the placeholder id without the name prefix "p_"`},
		},
		{
			name:     "execution and positional forms do not mix",
			engine:   clickhouse.New(),
			constant: "SELECT id FROM items WHERE id = {p_id:UInt64} AND rank = $1",
			expected: []string{"mixes placeholder forms"},
		},
		{name: "text without words is not SQL", engine: mysql.New(), constant: "?", expected: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, sqlPlaceholderProblems(testCase.constant, new(testCase.engine.Info())))
		})
	}
}

func TestPlaceholderProblemsSkipNamedStringConstants(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, "package store\n\ntype Column string\n\nconst sorted Column = \"kind = :kind\"\n\nconst query = `SELECT id FROM items WHERE kind = :kind`\n")
	require.Equal(t, []string{`queries.sql.go:7:7: the SQL constant query writes ":kind", which is not the execution form $1`},
		placeholderProblems(checked, new(postgres.New().Info())))
}

func TestFileNameProblemFindsNamesGoTreatsSpecially(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		expected string
	}{
		{name: "items.sql.go", expected: ""},
		{name: "device_settings.sql.go", expected: ""},
		{name: "_private.sql.go", expected: "starts with _, so the go command ignores it"},
		{name: ".hidden.sql.go", expected: "starts with ., so the go command ignores it"},
		{name: "devices_ios.sql.go", expected: "ends in _ios, so it builds only for that target"},
		{name: "reports_windows_amd64.sql.go", expected: "ends in _amd64, so it builds only for that target"},
		{name: "queries_test.go", expected: "ends in _test.go, so it compiles only into tests"},
		{name: "schema_linux.sql", expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, fileNameProblem(testCase.name))
		})
	}
}

func TestFirstDifferenceLocatesTheFirstChangedLine(t *testing.T) {
	t.Parallel()

	first := []sqlmodel.GeneratedFile{{Name: "a.go", Content: []byte("package a\nvar x = 1\n")}}
	testCases := []struct {
		name     string
		second   []sqlmodel.GeneratedFile
		expected string
	}{
		{name: "the same files", second: []sqlmodel.GeneratedFile{{Name: "a.go", Content: []byte("package a\nvar x = 1\n")}}, expected: ""},
		{name: "a changed line", second: []sqlmodel.GeneratedFile{{Name: "a.go", Content: []byte("package a\nvar x = 2\n")}},
			expected: `a.go:2 "var x = 1" became "var x = 2"`},
		{name: "another file", second: []sqlmodel.GeneratedFile{{Name: "b.go", Content: []byte("package a\n")}},
			expected: "the files [a.go] became [b.go]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, firstDifference(first, testCase.second))
		})
	}
}

func TestQueryMethodsAreExportedMethodsTakingAContextFirst(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

import "context"

type Queries struct{}

type Local interface{ Done() <-chan struct{} }

func (queries *Queries) Find(ctx context.Context, id int64) error { return nil }

func (queries *Queries) Plain(id int64) error { return nil }

func (queries *Queries) Local(ctx Local) error { return nil }

func (queries *Queries) none() {}

func (queries *Queries) Empty() {}

func Free(ctx context.Context) {}

func (queries *Queries) Declared(ctx context.Context)
`)
	require.Empty(t, checked.problems)

	methods := checked.queryMethods()
	names := make([]string, 0, len(methods))
	for _, method := range methods {
		names = append(names, method.function.Name())
	}

	require.Equal(t, []string{"Find"}, names)
}

func TestIsContextKnowsOnlyContextContext(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

import "context"

type Context interface{ Done() <-chan struct{} }

type Alias = context.Context

var standard context.Context
var aliased Alias
var local Context
var number int
`)
	require.Empty(t, checked.problems)
	testCases := []struct {
		name     string
		variable string
		expected bool
	}{
		{name: "context.Context", variable: "standard", expected: true},
		{name: "an alias of context.Context", variable: "aliased", expected: true},
		{name: "a local type named Context", variable: "local", expected: false},
		{name: "a basic type", variable: "number", expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			variable := checked.types.Scope().Lookup(testCase.variable)
			require.NotNil(t, variable)
			require.Equal(t, testCase.expected, isContext(variable.Type()))
		})
	}
}

func TestDereferenceStripsPointersAndSlices(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

type Row struct{ ID int64 }

var rows []*Row
var nested *[]*Row
var row Row
var number int
`)
	require.Empty(t, checked.problems)
	rowType := checked.types.Scope().Lookup("Row").Type()
	testCases := []struct {
		name     string
		variable string
		expected types.Type
	}{
		{name: "a slice of pointers", variable: "rows", expected: rowType},
		{name: "a pointer to a slice of pointers", variable: "nested", expected: rowType},
		{name: "a plain struct", variable: "row", expected: rowType},
		{name: "a basic type", variable: "number", expected: types.Typ[types.Int]},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.True(t, types.Identical(testCase.expected, dereference(checked.types.Scope().Lookup(testCase.variable).Type())))
		})
	}
}

func TestPackageStructAcceptsOnlyStructsTheGeneratedPackageDeclares(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

import "context"

type Row struct{ ID int64 }

type Kind string

var row Row
var kind Kind
var outside context.Context
var number int
`)
	require.Empty(t, checked.problems)
	testCases := []struct {
		name     string
		variable string
		expected string
	}{
		{name: "a struct of the package", variable: "row", expected: "Row"},
		{name: "a named type that is not a struct", variable: "kind", expected: ""},
		{name: "a type of another package", variable: "outside", expected: ""},
		{name: "a basic type", variable: "number", expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			named, structure, ok := checked.packageStruct(checked.types.Scope().Lookup(testCase.variable).Type())
			if testCase.expected == "" {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Equal(t, testCase.expected, named.Obj().Name())
			require.Equal(t, 1, structure.NumFields())
		})
	}
}

func TestStringConstantsAndPlainStringsReadOnlyUntypedOrStringConstants(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

type Column string

const query = "SELECT 1"
const typed string = "SELECT 2"
const sorted Column = "kind"
const limit = 10
`)
	require.Empty(t, checked.problems)

	require.ElementsMatch(t, []string{"SELECT 1", "SELECT 2", "kind"}, stringConstants(checked))
	require.Empty(t, stringConstants(nil))
	require.Empty(t, stringConstants(&checkedPackage{}))
	testCases := []struct {
		name     string
		constant string
		expected bool
	}{
		{name: "an untyped string", constant: "query", expected: true},
		{name: "a string", constant: "typed", expected: true},
		{name: "a named string type", constant: "sorted", expected: false},
		{name: "an untyped integer", constant: "limit", expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, isPlainString(checked.types.Scope().Lookup(testCase.constant).Type()))
		})
	}
}

func TestAnalysisOfAPackageThatWasNotGeneratedFindsNothing(t *testing.T) {
	t.Parallel()

	info := new(postgres.New().Info())

	require.Empty(t, bindingProblems(nil))
	require.Empty(t, rowProblems(nil))
	require.Empty(t, placeholderProblems(nil, info))
	require.Empty(t, placeholderProblems(&checkedPackage{}, info))
}

func TestFilledWholeFindsFieldsTakenByAddressOrAssignedWhole(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

type Berth struct{ Pontoon string }

type Row struct {
	ID      int64
	Berth   Berth
	Spare   Berth
	Ignored Berth
}

func fill(row *Row, value Berth) {
	use(&row.ID)
	row.Spare = value
	local := row.Ignored
	use(local, (&row.Berth))
}

func use(...any) {}
`)
	require.Empty(t, checked.problems)

	fields := checked.filledWhole()
	filled := make([]string, 0, len(fields))
	for field := range fields {
		filled = append(filled, field.Name())
	}

	require.ElementsMatch(t, []string{"ID", "Berth", "Spare"}, filled)
}

func TestCollectRowsFollowsContainersAndSkipsStructsWithMethods(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

type Inner struct{ Label string }

type Decoded struct{ Raw string }

func (Decoded) Decode() {}

type Row struct {
	ID      int64
	Inner   *Inner
	Decoded Decoded
	Whole   Inner
}

type Rows func(func(Row, error) bool)

var many []*Row
var iterate Rows
`)
	require.Empty(t, checked.problems)
	scope := checked.types.Scope()
	whole := map[*types.Var]bool{}
	row, ok := scope.Lookup("Row").Type().Underlying().(*types.Struct)
	require.True(t, ok)
	for field := range row.Fields() {
		if field.Name() == "Whole" {
			whole[field] = true
		}
	}
	testCases := []struct {
		name     string
		variable string
		expected []string
	}{
		{name: "a slice of row pointers reaches nested rows", variable: "many", expected: []string{"Inner", "Row"}},
		{name: "an iterator's yield reaches its row", variable: "iterate", expected: []string{"Inner", "Row"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rows := make(map[*types.Named]*types.Struct)
			candidate := scope.Lookup(testCase.variable).Type()
			if signature, ok := candidate.Underlying().(*types.Signature); ok {
				candidate = signature
			}
			checked.collectRows(candidate, rows, whole)

			names := make([]string, 0, len(rows))
			for named := range rows {
				names = append(names, named.Obj().Name())
			}
			require.ElementsMatch(t, testCase.expected, names)
		})
	}
}

func TestUsedWholeTellsAWholeUseFromAFieldSelection(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, `package store

type Params struct{ Kind string }

func selects(params Params) { use(params.Kind) }

func passes(params Params) { use(params) }

func ignores(params Params) {}

func use(...any) {}
`)
	require.Empty(t, checked.problems)
	testCases := []struct {
		name     string
		function string
		expected bool
	}{
		{name: "a field selection is not a whole use", function: "selects", expected: false},
		{name: "passing the value on is a whole use", function: "passes", expected: true},
		{name: "an unused value is not used whole", function: "ignores", expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var declaration *ast.FuncDecl
			for _, candidate := range checked.files[0].Decls {
				if function, ok := candidate.(*ast.FuncDecl); ok && function.Name.Name == testCase.function {
					declaration = function
				}
			}
			require.NotNil(t, declaration)
			parameter := checked.info.Defs[declaration.Type.Params.List[0].Names[0]]

			require.Equal(t, testCase.expected, checked.usedWhole(declaration.Body, parameter))
		})
	}
}

func TestTypeCheckWithStandsInForPackagesOutsideTheStandardLibrary(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		source           string
		expectedProblems []string
		expectedExternal []string
	}{
		{
			name:             "a selector into a stand-in package is not a problem",
			source:           "package store\n\nimport \"github.com/example/thing/v2\"\n\nvar Value thing.Kind\n",
			expectedProblems: nil,
			expectedExternal: []string{"github.com/example/thing/v2"},
		},
		{
			name:             "a standard library package is imported",
			source:           "package store\n\nimport \"strings\"\n\nvar Value = strings.ToUpper(\"x\")\n",
			expectedProblems: nil,
			expectedExternal: nil,
		},
		{
			name:             "a misuse of the standard library is a problem",
			source:           "package store\n\nimport \"strings\"\n\nvar Value int = strings.ToUpper(\"x\")\n",
			expectedProblems: []string{`value.go:5:17: cannot use strings.ToUpper("x") (value of type string) as int value in variable declaration`},
			expectedExternal: nil,
		},
		{
			name:             "a file that does not parse is a problem",
			source:           "package store\n\nvar = 1\n",
			expectedProblems: []string{"parsing: value.go:3:5: expected 'IDENT', found '='"},
			expectedExternal: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			checked := checkedWithoutToolchain(t,
				sqlmodel.GeneratedFile{Name: "value.go", Content: []byte(testCase.source)},
				sqlmodel.GeneratedFile{Name: "schema.sql", Content: []byte("not go")})

			require.Equal(t, testCase.expectedProblems, checked.problems)
			require.ElementsMatch(t, testCase.expectedExternal, slices.Sorted(maps.Keys(checked.external)))
		})
	}
}
