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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

type wantedRow struct {
	query    string
	row      string
	fields   []string
	shared   bool
	problems []string
}

func rolesCatalogue() *sqlmodel.Catalogue {
	return catalogueOf(
		tableOf("roles", sqlmodel.NewColumn("id", integerType(), false), sqlmodel.NewColumn("name", textType(), false)),
		tableOf("grants", sqlmodel.NewColumn("id", integerType(), false), sqlmodel.NewColumn("role_id", integerType(), false)),
	)
}

func readColumn(table, name string, sqlType sqlmodel.SQLType) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{Name: name, SQLType: sqlType, SourceSchema: "public", SourceTable: table, SourceColumn: name}
}

func roleColumns() []sqlmodel.OutputColumn {
	return []sqlmodel.OutputColumn{readColumn("roles", "id", integerType()), readColumn("roles", "name", textType())}
}

func modelQuery(name string, columns ...sqlmodel.OutputColumn) *sqlmodel.AnalysedQuery {
	query := queryOf(name, sqlmodel.QueryCommandMany, nil, columns)
	query.RowModel = sqlmodel.TableName{Schema: "public", Name: "roles"}
	return query
}

func sharedQuery(name, row string, columns ...sqlmodel.OutputColumn) *sqlmodel.AnalysedQuery {
	query := queryOf(name, sqlmodel.QueryCommandMany, nil, columns)
	query.RowType = row
	return query
}

func TestPlanPackageReadsRowsIntoTheStructTheQueryNames(t *testing.T) {
	t.Parallel()

	nullableName := readColumn("roles", "name", textType())
	nullableName.Nullable = true
	taggedName := readColumn("roles", "name", textType())
	taggedName.StructTags = map[string]string{"validate": "required"}

	tests := []struct {
		name    string
		queries []*sqlmodel.AnalysedQuery
		reuse   bool
		want    []wantedRow
	}{
		{
			name:    "a query naming a table's model reads into it",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles", roleColumns()...)},
			want:    []wantedRow{{query: "ListRoles", row: "Roles", fields: []string{"ID", "Name"}, shared: true}},
		},
		{
			name: "its columns may come in another order",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles",
				readColumn("roles", "name", textType()), readColumn("roles", "id", integerType()))},
			want: []wantedRow{{query: "ListRoles", row: "Roles", fields: []string{"Name", "ID"}, shared: true}},
		},
		{
			name:    "a column the model has no field for is reported",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles", append(roleColumns(), readColumn("roles", "extra", textType()))...)},
			want: []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name", "Extra"},
				problems: []string{"model: the rows do not fit the model Roles of roles: the column extra has no field Extra in the model Roles"}}},
		},
		{
			name:    "a field no column gives is reported",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles", readColumn("roles", "id", integerType()))},
			want: []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID"},
				problems: []string{"model: the rows do not fit the model Roles of roles: no column gives the field Name of the model Roles"}}},
		},
		{
			name:    "a column of another Go type is reported",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles", readColumn("roles", "id", integerType()), nullableName)},
			want: []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name"},
				problems: []string{"model: the rows do not fit the model Roles of roles: the column name is *string, and the field Name of the model Roles is string"}}},
		},
		{
			name:    "a column of other struct tags is reported",
			queries: []*sqlmodel.AnalysedQuery{modelQuery("ListRoles", readColumn("roles", "id", integerType()), taggedName)},
			want: []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name"},
				problems: []string{"model: the rows do not fit the model Roles of roles: the column name is tagged " +
					"`json:\"name\" validate:\"required\"`, and the field Name of the model Roles `json:\"name\"`"}}},
		},
		{
			name: "queries naming one row type share it, the first declaring it",
			queries: []*sqlmodel.AnalysedQuery{
				sharedQuery("ListRoles", "role_summary", roleColumns()...),
				sharedQuery("GetRole", "role_summary", roleColumns()...),
			},
			want: []wantedRow{
				{query: "ListRoles", row: "RoleSummary", fields: []string{"ID", "Name"}, shared: true},
				{query: "GetRole", row: "RoleSummary", fields: []string{"ID", "Name"}, shared: false},
			},
		},
		{
			name: "a query whose columns do not fit the shared row type is reported",
			queries: []*sqlmodel.AnalysedQuery{
				sharedQuery("GetRole", "RoleSummary", roleColumns()...),
				sharedQuery("ListRoles", "RoleSummary", readColumn("roles", "id", integerType())),
			},
			want: []wantedRow{
				{query: "GetRole", row: "RoleSummary", fields: []string{"ID", "Name"}, shared: false},
				{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID"},
					problems: []string{"row: the rows do not fit RoleSummary, which GetRole declares: no column gives the field Name of RoleSummary as GetRole declares it"}},
			},
		},
		{
			name:    "a row type named like a model is reported",
			queries: []*sqlmodel.AnalysedQuery{sharedQuery("ListRoles", "Roles", roleColumns()...)},
			want: []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name"},
				problems: []string{"row: Roles is a name the generated package already declares or imports, such as a table's model; " +
					"choose another name, or read the rows into a table's model with model:"}}},
		},
		{
			name:    "reused models read the rows of a query reading one table's fields",
			queries: []*sqlmodel.AnalysedQuery{queryOf("ListRoles", sqlmodel.QueryCommandMany, nil, roleColumns())},
			reuse:   true,
			want:    []wantedRow{{query: "ListRoles", row: "Roles", fields: []string{"ID", "Name"}, shared: true}},
		},
		{
			name: "reused models leave a query reading two tables its own row",
			queries: []*sqlmodel.AnalysedQuery{queryOf("ListRoles", sqlmodel.QueryCommandMany, nil,
				[]sqlmodel.OutputColumn{readColumn("roles", "id", integerType()), readColumn("grants", "name", textType())})},
			reuse: true,
			want:  []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name"}}},
		},
		{
			name:    "reused models leave a query reading part of a table its own row, unreported",
			queries: []*sqlmodel.AnalysedQuery{queryOf("ListRoles", sqlmodel.QueryCommandMany, nil, []sqlmodel.OutputColumn{readColumn("roles", "id", integerType())})},
			reuse:   true,
			want:    []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID"}}},
		},
		{
			name:    "models are not reused unless the style asks",
			queries: []*sqlmodel.AnalysedQuery{queryOf("ListRoles", sqlmodel.QueryCommandMany, nil, roleColumns())},
			want:    []wantedRow{{query: "ListRoles", row: "ListRolesRow", fields: []string{"ID", "Name"}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := requestOf(rolesCatalogue(), test.queries...)
			request.Style.ReuseModels = test.reuse

			naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

			var reported []string
			for _, problem := range problems {
				require.Equal(t, diag.CodeRowType, problem.Code)
				require.Equal(t, diag.SeverityError, problem.Severity)
				reported = append(reported, problem.Query.Name+": "+problem.Message)
			}
			var wantedProblems []string
			for _, want := range test.want {
				naming := plannedNaming(t, naming, request.Queries, want.query)
				require.Equal(t, want.row, naming.Row)
				require.Equal(t, want.shared, naming.SharedRow)
				require.Equal(t, want.fields, naming.ColumnFields)
				for _, problem := range want.problems {
					wantedProblems = append(wantedProblems, want.query+": "+problem)
				}
			}
			require.ElementsMatch(t, wantedProblems, reported)
		})
	}
}

func plannedNaming(t *testing.T, naming emit.Naming, queries []*sqlmodel.AnalysedQuery, name string) emit.QueryNaming {
	t.Helper()

	for index, query := range queries {
		if query.Name == name {
			return naming.Queries[index]
		}
	}
	require.FailNow(t, "no query "+name)
	return emit.QueryNaming{}
}
