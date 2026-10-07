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

package catalogue

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func fullyPopulatedQuery() *sqlmodel.AnalysedQuery {
	column := fullyPopulatedColumn()
	template := sqlmodel.SQLTemplate{
		Segments: []sqlmodel.SQLSegment{{
			Text:     "SELECT",
			Children: []sqlmodel.SQLSegment{{Text: "id", Star: []sqlmodel.StarColumn{{Qualifier: "t", Name: "id", Column: 0}}}},
			Star:     []sqlmodel.StarColumn{{Qualifier: "t", Name: "id", Column: 0}},
		}},
		Arguments: []int{0},
	}
	return &sqlmodel.AnalysedQuery{
		Name:          "ListThings",
		Template:      template,
		CountTemplate: new(template),
		Parameters: []sqlmodel.QueryParameter{{
			Name:            "limit",
			DefaultLimit:    new(10),
			MaxLimit:        new(100),
			GoTypeOverride:  column.GoTypeOverride,
			NonNullElements: new(true),
			SortableColumns: []string{"id"},
			SQLType:         fullyPopulatedType(),
		}},
		OutputColumns: []sqlmodel.OutputColumn{{
			Name:           "id",
			SQLType:        fullyPopulatedType(),
			GoTypeOverride: column.GoTypeOverride,
			StructTags:     map[string]string{"validate": "required"},
		}},
		GroupByKey:     []string{"id"},
		InsertColumns:  []string{"id"},
		AllowedColumns: []sqlmodel.AllowedColumn{{Name: "id", SQLType: fullyPopulatedType()}},
	}
}

func TestCloneQueriesSharesNoMutableMemory(t *testing.T) {
	t.Parallel()

	original := []*sqlmodel.AnalysedQuery{fullyPopulatedQuery(), nil}
	copied := CloneQueries(original)
	require.Equal(t, original, copied)

	originalReferences := newReferenceWalker()
	originalReferences.walk(reflect.ValueOf(original[0]), "query")
	for key, populated := range originalReferences.populated {
		require.True(t, populated, "the fixture must populate %s so the clone of it is checked", key)
	}

	copiedReferences := newReferenceWalker()
	copiedReferences.walk(reflect.ValueOf(copied[0]), "query")
	for address, path := range copiedReferences.addresses {
		originalPath, shared := originalReferences.addresses[address]
		require.False(t, shared, "the clone's %s shares memory with the original's %s", path, originalPath)
	}
}

func TestChangingAClonedQueryLeavesTheOriginal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		change func(query *sqlmodel.AnalysedQuery)
	}{
		{name: "a parameter's limit", change: func(query *sqlmodel.AnalysedQuery) { *query.Parameters[0].DefaultLimit = 1 }},
		{name: "a parameter's sortable columns", change: func(query *sqlmodel.AnalysedQuery) { query.Parameters[0].SortableColumns[0] = "x" }},
		{name: "a parameter's Go type", change: func(query *sqlmodel.AnalysedQuery) { query.Parameters[0].GoTypeOverride.Name = "x" }},
		{name: "a parameter's enum values", change: func(query *sqlmodel.AnalysedQuery) { query.Parameters[0].SQLType.EnumValues[0] = "x" }},
		{name: "an output column's struct tags", change: func(query *sqlmodel.AnalysedQuery) {
			query.OutputColumns[0].StructTags["validate"] = "x"
		}},
		{name: "an output column's element type", change: func(query *sqlmodel.AnalysedQuery) {
			query.OutputColumns[0].SQLType.ElementType.EngineName = "x"
		}},
		{name: "a template segment", change: func(query *sqlmodel.AnalysedQuery) { query.Template.Segments[0].Children[0].Text = "x" }},
		{name: "a template's star columns", change: func(query *sqlmodel.AnalysedQuery) { query.Template.Segments[0].Star[0].Name = "x" }},
		{name: "the count template", change: func(query *sqlmodel.AnalysedQuery) { query.CountTemplate.Arguments[0] = 9 }},
		{name: "the group-by key", change: func(query *sqlmodel.AnalysedQuery) { query.GroupByKey[0] = "x" }},
		{name: "the insert columns", change: func(query *sqlmodel.AnalysedQuery) { query.InsertColumns[0] = "x" }},
		{name: "an allowed column's precision", change: func(query *sqlmodel.AnalysedQuery) {
			*query.AllowedColumns[0].SQLType.Precision = 99
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			original := fullyPopulatedQuery()
			copied := CloneQueries([]*sqlmodel.AnalysedQuery{original})

			testCase.change(copied[0])

			require.Equal(t, fullyPopulatedQuery(), original)
		})
	}
}
