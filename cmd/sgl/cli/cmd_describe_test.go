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

package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestDescribe(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	result := project.Run("describe", "GetAuthor")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, `GetAuthor (one) in database "app", db/queries/example.sql:1`)
	assert.Contains(t, result.Stdout, "Parameters:\n    ?1  id  bigint  not null", "a SQLite parameter is shown as SQLite writes it")
	assert.Contains(t, result.Stdout, "bio   text    null      authors.bio")
	assert.Contains(t, result.Stdout, "Go:\n    const getAuthorSQL = `SELECT id, name, bio FROM authors WHERE id = ?1;`\n")
	assert.Contains(t, result.Stdout, "type GetAuthorRow struct {")
	assert.Contains(t, result.Stdout, "func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)")

	result = project.Run("describe", "CreateAuthor", "--format", "json")
	requireCode(t, result, 0)
	var described struct {
		Name       string `json:"name"`
		Command    string `json:"command"`
		Go         string `json:"go"`
		Parameters []struct {
			Name        string `json:"name"`
			Placeholder string `json:"placeholder"`
			Nullable    bool   `json:"nullable"`
		} `json:"parameters"`
		Columns []any `json:"columns"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &described))
	assert.Equal(t, "CreateAuthor", described.Name)
	assert.Equal(t, "exec", described.Command)
	require.Len(t, described.Parameters, 2)
	assert.Equal(t, "name", described.Parameters[0].Name)
	assert.Equal(t, "?2", described.Parameters[1].Placeholder)
	assert.True(t, described.Parameters[1].Nullable)
	assert.Empty(t, described.Columns)
	assert.Contains(t, described.Go, "CreateAuthorParams")
	assert.Contains(t, described.Go, "const createAuthorSQL = `INSERT INTO authors (name, bio) VALUES (?1, ?2);`\n\n")

	result = project.Run("describe", "GetAuthr")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, `did you mean "GetAuthor"?`)

	requireCode(t, project.Run("describe"), 2)
	requireCode(t, project.Run("describe", "GetAuthor", "--format", "yaml"), 2)
}

func TestDescribeWithoutGeneratedCode(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/broken.sql", "-- sgl.query(Broken, one)\nSELECT id FROM authors WHERE name = 'unterminated;\n")
	result := project.Run("describe", "ListAuthors")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "Parameters:\n    none")
	assert.NotContains(t, result.Stdout, "Go:")
}

func TestDescribeShowsParametersInTheDialectsForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dialect string
		want    string
	}{
		{dialect: "postgres", want: "Parameters:\n    $1  id"},
		{dialect: "mysql", want: "Parameters:\n    ?  id"},
		{dialect: "clickhouse", want: "Parameters:\n    {p_id:UInt64}  id  UInt64"},
	}
	for _, testCase := range tests {
		t.Run(testCase.dialect, func(t *testing.T) {
			t.Parallel()

			project := clitest.NewProject(t)
			requireCode(t, project.Run("init", "--dialect", testCase.dialect), 0)
			result := project.Run("describe", "GetAuthor")
			requireCode(t, result, 0)
			assert.Contains(t, result.Stdout, testCase.want)
		})
	}
}

func TestDescribeFindsAQueryByItsHeaderOrMethodName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		asked  string
		header string
	}{
		{name: "a snake_case header by its header name", asked: "get_report_by_id", header: "get_report_by_id"},
		{name: "a snake_case header by its method name", asked: "GetReportByID", header: "get_report_by_id"},
		{name: "a camelCase header by its method name", asked: "GetReportByID", header: "getReportByID"},
		{name: "a PascalCase header by its name", asked: "GetReportByID", header: "GetReportByID"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := sqliteProject(t)
			project.Write("db/queries/reports.sql", "-- sgl.query("+testCase.header+", one)\nSELECT id, name FROM authors WHERE id = ?1;\n")
			result := project.Run("describe", testCase.asked)
			requireCode(t, result, 0)
			require.Contains(t, result.Stdout, testCase.header+" (one) in database \"app\", db/queries/reports.sql:1")
			require.Contains(t, result.Stdout, "Go:\n    const ")
			require.Contains(t, result.Stdout, "func (queries *Queries) GetReportByID(ctx context.Context, id int64) (GetReportByIDRow, error)")
		})
	}
}

func TestDescribeSuggestsAMethodName(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/reports.sql", "-- sgl.query(get_report_by_id, one)\nSELECT id, name FROM authors WHERE id = ?1;\n")
	result := project.Run("describe", "GetReportById")
	requireCode(t, result, 2)
	require.Contains(t, result.Stderr, `did you mean "GetReportByID"?`)
}

func TestDescribeFindsAQueryWhoseMethodAvoidsAGeneratedName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		asked string
	}{
		{name: "by its header name", asked: "WithTx"},
		{name: "by the method the generated code gives it", asked: "WithTx2"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := sqliteProject(t)
			project.Write("db/queries/transactions.sql", "-- sgl.query(WithTx, many)\nSELECT id FROM authors;\n")
			result := project.Run("describe", testCase.asked)
			requireCode(t, result, 0)
			require.Contains(t, result.Stdout, "WithTx (many) in database \"app\", db/queries/transactions.sql:1")
			require.Contains(t, result.Stdout, "func (queries *Queries) WithTx2(ctx context.Context) ([]WithTx2Row, error)")
			require.NotContains(t, result.Stdout, "type Queries struct")
		})
	}
}

func TestDescribePrintsTheSQLWithoutItsDirectiveLines(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/reports.sql", "-- sgl.query(GetReport, one)\n-- Reads one report.\n-- :id as sgl.param(report_id)\n"+
		"SELECT id, name FROM authors WHERE id = :id;\n")

	result := project.Run("describe", "GetReport")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, "SQL:\n    -- Reads one report.\n    SELECT id, name FROM authors WHERE id = ?1;\n\n")

	result = project.Run("describe", "GetReport", "--format", "json")
	requireCode(t, result, 0)
	var described struct {
		SQL string `json:"sql"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &described))
	require.Equal(t, "-- Reads one report.\nSELECT id, name FROM authors WHERE id = ?1;", described.SQL)
}

func TestDescribeShowsTheQueryMethodWhenABuilderMethodSharesItsName(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/browse.sql", "-- sgl.query(Browse, many, dynamic: runtime)\nSELECT id, name FROM authors;\n\n"+
		"-- sgl.query(Count, one)\nSELECT count(*) AS total FROM authors;\n")

	result := project.RunWith(t.Context(), nil, "describe", "Count")

	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "func (queries *Queries) Count(ctx context.Context)")
	assert.Contains(t, result.Stdout, "SELECT count(*) AS total FROM authors")
	assert.NotContains(t, result.Stdout, "BrowseBuilder")
}
