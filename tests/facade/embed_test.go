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

package facade_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

const (
	staffMigration = `CREATE TABLE departments (
    id   INTEGER PRIMARY KEY,
    name TEXT    NOT NULL
);
CREATE TABLE employees (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL,
    department_id INTEGER NOT NULL REFERENCES departments (id),
    manager_id    INTEGER REFERENCES employees (id)
);`
	managerEmbedQuery = `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id
ORDER BY e.id;
`
)

func generateStaff(t *testing.T, query string) *seagull.Result {
	t.Helper()
	generator, err := seagull.New(dialect_engine_sqlite.New())
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package: "store",
		Migrations: fstest.MapFS{
			"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(staffMigration)},
			"db/queries/employees.sql":       &fstest.MapFile{Data: []byte(query)},
		},
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	require.NoError(t, err)
	return result
}

func generateStaffSource(t *testing.T, query string) string {
	t.Helper()
	result := generateStaff(t, query)
	require.NoError(t, result.Err())
	return fileContent(t, result.Files, "employees.sql.go")
}

func TestEmbedOnASelfJoinGeneratesTheSameOutputEveryRun(t *testing.T) {
	t.Parallel()

	outputs := make(map[string]struct{})
	for range 30 {
		outputs[generateStaffSource(t, managerEmbedQuery)] = struct{}{}
	}

	assert.Len(t, outputs, 1)
}

func TestEmbedAsNamesTheNestedFieldAndType(t *testing.T) {
	t.Parallel()

	source := generateStaffSource(t, managerEmbedQuery)

	assert.Contains(t, source, "type ListEmployeesManager struct")
	assert.Regexp(t, `\n\tManager\s+\*ListEmployeesManager\n`, source)
	assert.Contains(t, source, "row.Manager = &ListEmployeesManager{ID: embedRows.Manager.ID, Name: embedRows.Manager.Name}")
	assert.Contains(t, source, "if embedRows.Manager.ID != nil {")
	assert.NotContains(t, source, "ListEmployeesEmployees")
}

func TestEmbedFromBindsOnlyTheNamedAlias(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		query     string
		wantScan  string
		wantField string
	}{
		{
			name:      "the left-joined alias embeds as a pointer",
			query:     managerEmbedQuery,
			wantScan:  "scanArgs := []any{&row.ID, &row.Name, &embedRows.Manager.ID, &embedRows.Manager.Name}",
			wantField: `\n\tManager\s+\*ListEmployeesManager\n`,
		},
		{
			name: "the preserved side of a left join embeds as a value",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: e, as: Report)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id
ORDER BY e.id;
`,
			wantScan:  "rows.Scan(&row.Report.ID, &row.Report.Name, &row.ID, &row.Name)",
			wantField: `\n\tReport\s+ListEmployeesReport\n`,
		},
		{
			name: "an inner-joined alias embeds as a value",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
INNER JOIN employees m ON e.manager_id = m.id
ORDER BY e.id;
`,
			wantScan:  "scanArgs := []any{&row.ID, &row.Name, &row.Manager.ID, &row.Manager.Name}",
			wantField: `\n\tManager\s+ListEmployeesManager\n`,
		},
		{
			name: "both sides of a self-join embed under their own names",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: e, as: Report)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id
ORDER BY e.id;
`,
			wantScan:  "scanArgs := []any{&row.Report.ID, &row.Report.Name, &embedRows.Manager.ID, &embedRows.Manager.Name}",
			wantField: `\n\tReport\s+ListEmployeesReport\n\tManager\s+\*ListEmployeesManager\n`,
		},
		{
			name: "without from the alias of the table's only reference is used",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(departments)
SELECT e.id, e.name, d.id, d.name
FROM employees e
JOIN departments d ON d.id = e.department_id
ORDER BY e.id;
`,
			wantScan:  "scanArgs := []any{&row.ID, &row.Name, &row.Departments.ID, &row.Departments.Name}",
			wantField: `\n\tDepartments\s+ListEmployeesDepartments\n`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := generateStaffSource(t, testCase.query)

			assert.Contains(t, source, testCase.wantScan)
			assert.Regexp(t, testCase.wantField, source)
		})
	}
}

func TestEmbedReportsABindingItCannotMake(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		query       string
		wantCode    string
		wantMessage string
	}{
		{
			name: "a self-join without from",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id;
`,
			wantCode:    seagull.CodeMissingRequired,
			wantMessage: `keyword argument "from"`,
		},
		{
			name: "from naming no table reference",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: x, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id;
`,
			wantCode:    seagull.CodeInvalidKeywordArgumentValue,
			wantMessage: `"x"`,
		},
		{
			name: "from naming another table's alias",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(departments, from: m)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id;
`,
			wantCode:    seagull.CodeInvalidKeywordArgumentValue,
			wantMessage: `"employees"`,
		},
		{
			name: "two embeds of one table without as",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: e)
-- sgl.embed(employees, from: m)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id;
`,
			wantCode:    seagull.CodeMissingRequired,
			wantMessage: `keyword argument "as"`,
		},
		{
			name: "two embeds given the same as",
			query: `-- sgl.query(ListEmployees, many)
-- sgl.embed(employees, from: e, as: Person)
-- sgl.embed(employees, from: m, as: Person)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON e.manager_id = m.id;
`,
			wantCode:    seagull.CodeInvalidKeywordArgumentValue,
			wantMessage: `"Person"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := generateStaff(t, testCase.query)

			require.Error(t, result.Err())
			var matched []seagull.SourceError
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == testCase.wantCode {
					matched = append(matched, diagnostic)
				}
			}
			require.Len(t, matched, 1, "diagnostics: %v", result.Diagnostics)
			assert.Equal(t, seagull.SeverityError, matched[0].Severity)
			assert.Contains(t, matched[0].Message, testCase.wantMessage)
			assert.Contains(t, matched[0].Message, "sgl.embed")
			assert.Equal(t, "employees.sql", matched[0].Filename)
			assert.Positive(t, matched[0].Line)
		})
	}
}
