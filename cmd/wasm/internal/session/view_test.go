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

package session_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

const (
	failingQueries = "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = ?1;\n\n" +
		"-- sgl.query(AddBroken, exec)\nINSERT INTO authors (name, bio) VALUES (?1);\n"
	rowTypeQueries = "-- GetAuthor reads one author.\n" +
		"-- It reads the author by id.\n" +
		"-- sgl.query(GetAuthor, one)\n" +
		"SELECT id, name, bio FROM authors WHERE id = ?1;\n\n" +
		"-- sgl.query(GetAuthorModel, one, model: authors)\n" +
		"SELECT bio, name, id FROM authors WHERE id = ?1;\n\n" +
		"-- sgl.query(ListAuthorNames, many, row: AuthorName)\n" +
		"SELECT id, name FROM authors ORDER BY name;\n\n" +
		"-- sgl.query(AuthorNamed, one, row: AuthorName)\n" +
		"SELECT name, id FROM authors WHERE name = ?1;\n\n" +
		"-- sgl.query(StreamAuthors, stream, model: authors)\n" +
		"SELECT id, name, bio FROM authors;\n\n" +
		"-- sgl.query(DeleteAuthor, exec)\n" +
		"DELETE FROM authors WHERE id = ?1;\n"
)

var rowTypes = []struct {
	query       string
	rowType     string
	rowKind     string
	description string
	columnTypes []string
}{
	{query: "GetAuthor", rowType: "GetAuthorRow", rowKind: "own", description: "GetAuthor reads one author.\nIt reads the author by id.", columnTypes: []string{"int64", "string", "*string"}},
	{query: "GetAuthorModel", rowType: "Authors", rowKind: "model", columnTypes: []string{"*string", "string", "int64"}},
	{query: "ListAuthorNames", rowType: "AuthorName", rowKind: "shared", columnTypes: []string{"int64", "string"}},
	{query: "AuthorNamed", rowType: "AuthorName", rowKind: "shared", columnTypes: []string{"string", "int64"}},
	{query: "StreamAuthors", rowType: "Authors", rowKind: "model", columnTypes: []string{"int64", "string", "*string"}},
	{query: "DeleteAuthor", rowType: "", rowKind: "", columnTypes: []string{}},
}

func TestRunSkipsFailingQueriesWhenAsked(t *testing.T) {
	t.Parallel()

	files := starter(t, "sqlite")
	files[starterQueries] = failingQueries

	stopped := run(t, files, session.Override{})
	require.True(t, stopped.OK, stopped.Error)
	require.False(t, stopped.Partial)
	require.Empty(t, stopped.Files, "without the skip, an error stops generation")
	require.False(t, findQuery(t, stopped, "AddBroken").Skipped)
	require.False(t, stopped.Config.SkipFailingQueries)

	skipped := run(t, files, session.Override{SkipFailingQueries: new(true)})
	require.True(t, skipped.OK, skipped.Error)
	require.True(t, skipped.Partial)
	require.True(t, skipped.Config.SkipFailingQueries)
	require.Positive(t, skipped.Counts.Errors, "the skipped query's errors are still reported")
	require.Equal(t, stopped.Diagnostics, skipped.Diagnostics)
	generated := findFile(t, skipped, "example.sql.go").Content
	require.Contains(t, generated, "func (queries *Queries) GetAuthor(")
	require.NotContains(t, generated, "AddBroken")
	require.False(t, findQuery(t, skipped, "GetAuthor").Skipped)
	broken := findQuery(t, skipped, "AddBroken")
	require.True(t, broken.Skipped)
	require.Empty(t, broken.Signature)
}

func TestRunDescribesTheStructRowsAreReadInto(t *testing.T) {
	t.Parallel()

	files := starter(t, "sqlite")
	files[starterQueries] = rowTypeQueries
	response := run(t, files, session.Override{})
	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors, response.Diagnostics)

	for _, expected := range rowTypes {
		t.Run(expected.query, func(t *testing.T) {
			t.Parallel()

			query := findQuery(t, response, expected.query)
			require.Equal(t, expected.rowType, query.RowType)
			require.Equal(t, expected.rowKind, query.RowKind)
			require.Equal(t, expected.description, query.Description)
			columnTypes := make([]string, 0, len(query.Columns))
			for _, column := range query.Columns {
				columnTypes = append(columnTypes, column.GoType)
			}
			require.Equal(t, expected.columnTypes, columnTypes)
			if expected.rowType != "" {
				require.Contains(t, query.Types, "type "+expected.rowType+" struct")
			}
		})
	}
}

func TestRunFindsColumnGoTypesWhateverTheJSONTags(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		override session.Override
	}{
		{name: "camel case", override: session.Override{JSONCase: "camel"}},
		{name: "pascal case with omitempty", override: session.Override{JSONCase: "pascal", JSONOmitEmpty: new(true)}},
		{name: "no json tags", override: session.Override{JSONOmit: new(true)}},
		{name: "no json tags, rows read into the model", override: session.Override{JSONOmit: new(true), ReuseModels: new(true)}},
		{name: "camel case, rows read into the model", override: session.Override{JSONCase: "camel", ReuseModels: new(true)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := starter(t, "sqlite")
			files[starterQueries] = "-- sgl.query(GetAuthor, one)\nSELECT id, name, bio FROM authors WHERE id = ?1;\n"
			response := run(t, files, testCase.override)
			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)

			query := findQuery(t, response, "GetAuthor")
			columnTypes := make([]string, 0, len(query.Columns))
			for _, column := range query.Columns {
				columnTypes = append(columnTypes, column.GoType)
			}
			require.Equal(t, []string{"int64", "string", "*string"}, columnTypes)
		})
	}
}

func TestRunFindsColumnGoTypesOfColumnsTheCaseStylesRename(t *testing.T) {
	t.Parallel()

	for _, jsonCase := range []string{"", "snake", "camel", "pascal"} {
		t.Run("case "+jsonCase, func(t *testing.T) {
			t.Parallel()

			files := starter(t, "sqlite")
			files[starterQueries] = "-- sgl.query(GetAuthor, one)\n" +
				"SELECT id AS \"HTTPStatus\", name AS \"author-name\", bio AS \"bioText2\", id AS \"-\" FROM authors WHERE id = ?1;\n"
			response := run(t, files, session.Override{JSONCase: jsonCase})
			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)

			query := findQuery(t, response, "GetAuthor")
			columnTypes := make([]string, 0, len(query.Columns))
			for _, column := range query.Columns {
				columnTypes = append(columnTypes, column.GoType)
			}
			require.Equal(t, []string{"int64", "string", "*string", "int64"}, columnTypes)
		})
	}
}

func TestRunShowsTheQueryMethodWhenABuilderMethodSharesItsName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		query   string
		command string
	}{
		{name: "Count", query: "Count", command: "one"},
		{name: "All", query: "All", command: "many"},
		{name: "Where", query: "Where", command: "many"},
		{name: "OrderBy", query: "OrderBy", command: "many"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := starter(t, "sqlite")
			files[starterQueries] = "-- sgl.query(SearchAuthors, many, dynamic: runtime)\nSELECT id, name FROM authors\n\n" +
				"-- sgl.query(" + testCase.query + ", " + testCase.command + ")\nSELECT id, name FROM authors WHERE bio IS NULL;\n"

			response := run(t, files, session.Override{})

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			query := findQuery(t, response, testCase.query)
			require.True(t, strings.HasPrefix(query.Signature, "func (queries *Queries) "+testCase.query+"(ctx context.Context)"), query.Signature)
			require.Equal(t, "generated", query.SQLSentFrom)
			require.Equal(t, "SELECT id, name FROM authors WHERE bio IS NULL;", query.SQLSent)
			require.Empty(t, query.CountSQL)
			require.Equal(t, testCase.query+"Row", query.RowType)
		})
	}
}
