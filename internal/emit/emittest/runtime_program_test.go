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
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

const samplerSource = `package store

import (
	"context"
	"time"
)

type OrderBy string

const (
	OrderByName OrderBy = "name"
	OrderByID   OrderBy = "id"
)

type Params struct {
	Kind   string
	IDs    []int64
	Label  *string
	Flag   bool
	Small  int8
	At     time.Time
	Data   []byte
	Sort   OrderBy
	hidden int
}

type Row struct {
	ID int64
}

type Builder struct{}

func (builder *Builder) Where(column string, operator string, value any) *Builder { return builder }

func (builder *Builder) OrderBy(column string, direction string) *Builder { return builder }

func (builder *Builder) All(ctx context.Context) ([]Row, error) { return nil, nil }

func (builder *Builder) Count(ctx context.Context) (int64, error) { return 0, nil }

type Queries struct{}

func (queries *Queries) Exec(ctx context.Context, id int64) error { return nil }

func (queries *Queries) One(ctx context.Context) (Row, error) { return Row{}, nil }

func (queries *Queries) Maybe(ctx context.Context) (Row, bool, error) { return Row{}, false, nil }

func (queries *Queries) Many(ctx context.Context) ([]Row, error) { return nil, nil }

func (queries *Queries) Stream(ctx context.Context) func(func(Row, error) bool) { return nil }

func (queries *Queries) Search() *Builder { return nil }
`

func newTestSampler(checked *checkedPackage, length int) *sampler {
	counter := 0
	return &sampler{
		generated: checked.types,
		qualifier: func(imported *types.Package) string {
			if imported == checked.types {
				return "p0"
			}
			return importAliasOf(imported.Path())
		},
		importAlias: importAliasOf,
		counter:     &counter,
		length:      length,
	}
}

func TestSamplerWritesADistinctValueForEveryField(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, samplerSource)
	require.Empty(t, checked.problems)
	current := newTestSampler(checked, manyElements)

	expression := current.value(checked.types.Scope().Lookup("Params").Type(), "params")

	require.Equal(t, `p0.Params{Kind: string("emittest0001"), IDs: []int64{int64(7002), int64(7003), int64(7004)}, `+
		`Label: new(string("emittest0005")), Flag: bool(true), Small: int8(16), `+
		`At: emittestImporttime.Date(2001, emittestImporttime.February, 3, 4, 5, 7, 0, emittestImporttime.UTC), `+
		`Data: []byte("emittest0008"), Sort: p0.OrderByID}`, expression)
	require.Equal(t, []expectedValue{
		{path: "params.Kind", text: "emittest0001"},
		{path: "params.IDs[0]", text: "7002"},
		{path: "params.IDs[1]", text: "7003"},
		{path: "params.IDs[2]", text: "7004"},
		{path: "params.Label", text: "emittest0005"},
		{path: "params.Small", text: "16"},
		{path: "params.At", text: "2001-02-03 04:05:07"},
		{path: "params.Data", text: "emittest0008"},
	}, current.expected)
}

func TestSamplerWritesNilAndEmptySlices(t *testing.T) {
	t.Parallel()

	slice := types.NewSlice(types.Typ[types.Int64])
	testCases := []struct {
		length   int
		expected string
	}{
		{length: nilLength, expected: "[]int64(nil)"},
		{length: 0, expected: "[]int64{}"},
		{length: 1, expected: "[]int64{int64(7001)}"},
	}
	for _, testCase := range testCases {
		counter := 0
		current := &sampler{qualifier: importAliasQualifier, importAlias: importAliasOf, counter: &counter, length: testCase.length}
		require.Equal(t, testCase.expected, current.value(slice, "ids"))
	}
}

func importAliasQualifier(imported *types.Package) string {
	return importAliasOf(imported.Path())
}

func TestCallSourceFollowsTheMethodsResults(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, samplerSource)
	queries := types.NewMethodSet(types.NewPointer(checked.types.Scope().Lookup("Queries").Type()))
	method := func(name string) *types.Func {
		for selection := range queries.Methods() {
			if selection.Obj().Name() == name {
				function, ok := selection.Obj().(*types.Func)
				require.True(t, ok)
				return function
			}
		}
		require.FailNow(t, "no method "+name)
		return nil
	}
	identity := func(column string) string { return column }
	testCases := []struct {
		name     string
		values   []string
		label    string
		expected string
	}{
		{name: "Exec", values: []string{"int64(7001)"}, label: labelPlain, expected: "err := queries.Exec(ctx, int64(7001))\nreturn err"},
		{name: "One", label: labelPlain, expected: "_, err := queries.One(ctx)\nreturn err"},
		{name: "Maybe", label: labelPlain, expected: "_, _, err := queries.Maybe(ctx)\nreturn err"},
		{name: "Many", label: labelPlain, expected: "rows, err := queries.Many(ctx)\nemittestNilSlice = rows == nil\nreturn err"},
		{name: "Stream", label: labelPlain,
			expected: "for _, iterationError := range queries.Stream(ctx) {\nif iterationError != nil {\nreturn iterationError\n}\n}\nreturn nil"},
		{name: "Search", label: labelAll, expected: "builder := queries.Search()\nbuilder = builder.Where(\"kind\", \"=\", \"emittest0001\")\n" +
			"builder = builder.OrderBy(\"id\", \"DESC\")\nrows, err := builder.All(ctx)\nemittestNilSlice = rows == nil\nreturn err"},
		{name: "Search", label: labelCount, expected: "builder := queries.Search()\nbuilder = builder.Where(\"kind\", \"=\", \"emittest0001\")\n" +
			"_, err := builder.Count(ctx)\nreturn err"},
	}
	for _, testCase := range testCases {
		current := newTestSampler(checked, 1)
		require.Equal(t, testCase.expected, callSource(method(testCase.name), testCase.values, testCase.label, current, identity), testCase.name)
	}
}

func TestLabelsOfListTheCallsOfEachScenario(t *testing.T) {
	t.Parallel()

	require.Nil(t, labelsOf(scenarioNone))
	require.Equal(t, []string{labelPlain}, labelsOf(scenarioPlain))
	require.Equal(t, []string{labelNil, labelEmpty, labelOne, labelMany}, labelsOf(scenarioSlices))
	require.Equal(t, []string{labelAll, labelCount}, labelsOf(scenarioBuilder))
	require.Equal(t, nilLength, sliceLength(labelNil))
	require.Equal(t, manyElements, sliceLength(labelBatch))
	require.Equal(t, 1, sliceLength(labelPlain))
}

func TestReturnsRowsMarksTheCallsThatReturnASliceOfRows(t *testing.T) {
	t.Parallel()

	checked := checkedSource(t, samplerSource)
	queries := types.NewMethodSet(types.NewPointer(checked.types.Scope().Lookup("Queries").Type()))
	returns := make(map[string]bool)
	for selection := range queries.Methods() {
		function, ok := selection.Obj().(*types.Func)
		require.True(t, ok)
		returns[function.Name()+"/"+labelPlain] = returnsRows(function, labelPlain)
		returns[function.Name()+"/"+labelAll] = returnsRows(function, labelAll)
	}

	require.True(t, returns["Many/"+labelPlain])
	require.True(t, returns["Search/"+labelAll])
	require.False(t, returns["Search/"+labelPlain])
	require.False(t, returns["One/"+labelPlain])
	require.False(t, returns["Exec/"+labelPlain])
	require.False(t, returns["Stream/"+labelPlain])
}
