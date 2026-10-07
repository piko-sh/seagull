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

package methods

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestDisambiguatedJSONTagNamesSuffixesRepeats(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"id", "name", "id2", "title", "id3"},
		goast.JSONTagNames([]string{"id", "name", "id", "title", "id"}),
	)
	assert.Equal(t,
		[]string{"a_b", "a_b2"},
		goast.JSONTagNames([]string{"a,b", "a\"b"}),
	)
}

func TestJSONStructTagSanitisesColumnNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		fieldName string
		expected  string
	}{
		{name: "plain name is untouched", fieldName: "user_id", expected: "`json:\"user_id\"`"},
		{name: "tolerated punctuation is untouched", fieldName: "COUNT(*)", expected: "`json:\"COUNT(*)\"`"},
		{name: "backtick cannot close the raw literal", fieldName: "we`ird", expected: "`json:\"we_ird\"`"},
		{name: "double quote cannot close the tag", fieldName: `we"ird`, expected: "`json:\"we_ird\"`"},
		{name: "comma cannot fake a tag option", fieldName: "id,omitempty", expected: "`json:\"id_omitempty\"`"},
		{name: "empty name falls back", fieldName: "", expected: "`json:\"field\"`"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tag := goast.TagLiteral(emitkit.FieldTags(&emit.Style{}, []string{testCase.fieldName}, nil)[0])

			require.NotNil(t, tag)
			assert.Equal(t, testCase.expected, tag.Value)

			fileSet := token.NewFileSet()
			source := "package p\n\ntype T struct {\n\tField string " + tag.Value + "\n}\n"
			_, err := parser.ParseFile(fileSet, "structs.go", source, parser.AllErrors)
			require.NoError(t, err, "a struct carrying the tag must still parse")
		})
	}
}

func TestOutputStructsTakeTheirNamesFromThePlan(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:     "ListOrders",
		Filename: "orders.sql",
		Command:  sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("foo_bar", false),
			textColumn("foo__bar", true),
			namedEmbeddedColumn("name", "users", "buyer", true),
		},
		Template: template(text("SELECT foo_bar, foo__bar, buyer.name FROM orders")),
	})

	declarations := BuildOutputStructs(emission, query)

	require.Len(t, declarations, 2)
	require.Equal(t, "type ListOrdersBuyer struct {\n\tName *string `json:\"name\"`\n}", renderDecl(t, declarations[0]))
	require.Equal(t, "type ListOrdersRow struct {\n\tFooBar\tstring\t`json:\"foo_bar\"`\n\tFooBar2\t*string\t`json:\"foo__bar\"`\n\tBuyer\t*ListOrdersBuyer\n}",
		renderDecl(t, declarations[1]))
	require.Equal(t, []string{"&row.FooBar", "&row.FooBar2", "&row.Buyer.Name"}, renderExpressions(t, BuildScan(emission, query).Arguments))
}

func TestStructHelpersDeclareWhatAQueryNeeds(t *testing.T) {
	t.Parallel()

	first := &sqlmodel.AnalysedQuery{Name: "GetBoat", Filename: "boats.sql", Command: sqlmodel.QueryCommandOne}
	second := &sqlmodel.AnalysedQuery{Name: "ListBoats", Filename: "boats.sql", Command: sqlmodel.QueryCommandMany}
	third := &sqlmodel.AnalysedQuery{Name: "GetPort", Filename: "ports.sql", Command: sqlmodel.QueryCommandExec}

	require.Equal(t, map[string][]*sqlmodel.AnalysedQuery{"boats.sql": {first, second}, "ports.sql": {third}},
		GroupQueriesByFilename([]*sqlmodel.AnalysedQuery{first, second, third}))
	require.Equal(t, "const getBoatSQL = `SELECT 1`", renderDecl(t, BuildConstant("getBoatSQL", "SELECT 1")))
	require.Equal(t, "type GetBoatRow struct {\n}", renderDecl(t, EmptyRowStruct("GetBoatRow")))
}

func TestHasOutputColumnsAndRequiresRowTypeFollowTheCommand(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		command     sqlmodel.QueryCommand
		columns     bool
		hasColumns  bool
		requiresRow bool
	}{
		{command: sqlmodel.QueryCommandOne, columns: true, hasColumns: true, requiresRow: true},
		{command: sqlmodel.QueryCommandMany, columns: true, hasColumns: true, requiresRow: true},
		{command: sqlmodel.QueryCommandStream, columns: true, hasColumns: true, requiresRow: true},
		{command: sqlmodel.QueryCommandBatch, columns: true, hasColumns: true, requiresRow: false},
		{command: sqlmodel.QueryCommandExec, columns: true, hasColumns: false, requiresRow: false},
		{command: sqlmodel.QueryCommandOne, columns: false, hasColumns: false, requiresRow: true},
	}

	for _, testCase := range testCases {
		t.Run(string(testCase.command), func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "GetBoat", Command: testCase.command}
			if testCase.columns {
				query.OutputColumns = []sqlmodel.OutputColumn{textColumn("id", false)}
			}

			require.Equal(t, testCase.hasColumns, HasOutputColumns(query))
			require.Equal(t, testCase.requiresRow, RequiresRowType(query))
		})
	}
}

func TestBuildParamsStructNamesEachFieldAsThePlanDoes(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	analysed := &sqlmodel.AnalysedQuery{
		Name: "MoveBoat", Filename: "boats.sql", Command: sqlmodel.QueryCommandExec,
		Parameters: []sqlmodel.QueryParameter{textParam("name", 1), textParam("berth", 2)},
		Template:   template(text("UPDATE boats SET berth = "), parameter(1), text(" WHERE name = "), parameter(0)),
	}
	query := queryFor(t, emission, analysed)

	source := renderDecl(t, BuildParamsStruct(query, analysed.Parameters))

	require.Equal(t, "type MoveBoatParams struct {\n\tName\tstring\n\tBerth\tstring\n}", source)
}
