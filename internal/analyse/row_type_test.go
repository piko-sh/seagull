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

package analyse

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func rowTypeCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	idColumn := sqlmodel.NewColumn("id", sqlmodel.NewIntegerType("int4", 32, false), false)
	roles := fakedialect.NewTable("roles", idColumn)
	roles.Schema = "public"
	catalogue.Schemas["public"].Tables["roles"] = roles
	partition := fakedialect.NewTable("roles_2026", idColumn)
	partition.Schema = "public"
	partition.PartitionOf = &sqlmodel.TableName{Schema: "public", Name: "roles"}
	catalogue.Schemas["public"].Tables["roles_2026"] = partition
	catalogue.Schemas["public"].Views["role_names"] = &sqlmodel.View{Name: "role_names", Schema: "public", Columns: []sqlmodel.Column{idColumn}}
	audit := fakedialect.NewCatalogue("audit").Schemas["audit"]
	auditRoles := fakedialect.NewTable("roles", idColumn)
	auditRoles.Schema = "audit"
	audit.Tables["roles"] = auditRoles
	catalogue.Schemas["audit"] = audit
	return catalogue
}

func TestApplyRowTypeRecordsWhatTheHeaderAsks(t *testing.T) {
	t.Parallel()

	plain := []sqlmodel.OutputColumn{{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false)}}
	embedded := []sqlmodel.OutputColumn{{Name: "id", IsEmbedded: true, EmbedTable: "roles"}}

	tests := []struct {
		name       string
		directives directive.QueryDirectives
		command    sqlmodel.QueryCommand
		runtime    bool
		columns    []sqlmodel.OutputColumn
		wantModel  sqlmodel.TableName
		wantRow    string
		wantError  string
	}{
		{name: "a table's model", directives: directive.QueryDirectives{Model: "roles"}, command: sqlmodel.QueryCommandMany,
			columns: plain, wantModel: sqlmodel.TableName{Schema: "public", Name: "roles"}},
		{name: "a schema-qualified table's model", directives: directive.QueryDirectives{Model: "audit.roles"}, command: sqlmodel.QueryCommandOne,
			columns: plain, wantModel: sqlmodel.TableName{Schema: "audit", Name: "roles"}},
		{name: "a partition's model is its table's", directives: directive.QueryDirectives{Model: "roles_2026"}, command: sqlmodel.QueryCommandStream,
			columns: plain, wantModel: sqlmodel.TableName{Schema: "public", Name: "roles"}},
		{name: "a runtime builder's model", directives: directive.QueryDirectives{Model: "roles"}, command: sqlmodel.QueryCommandMany, runtime: true,
			columns: plain, wantModel: sqlmodel.TableName{Schema: "public", Name: "roles"}},
		{name: "a shared row type", directives: directive.QueryDirectives{Row: "RoleSummary"}, command: sqlmodel.QueryCommandMany,
			columns: plain, wantRow: "RoleSummary"},
		{name: "a view, which has no model", directives: directive.QueryDirectives{Model: "role_names"}, command: sqlmodel.QueryCommandMany,
			columns: plain, wantError: "model: role_names is a view, and only a table has a model; name a table, or name a shared row type with row:"},
		{name: "a table the schema does not hold", directives: directive.QueryDirectives{Model: "people"}, command: sqlmodel.QueryCommandMany,
			columns: plain, wantError: "model: the schema holds no table people"},
		{name: "a query returning no rows", directives: directive.QueryDirectives{Row: "RoleSummary"}, command: sqlmodel.QueryCommandExecRows,
			columns: plain, wantError: "row: names the struct a query's rows are read into, and the execrows command returns no rows"},
		{name: "a query embedding a table", directives: directive.QueryDirectives{Model: "roles"}, command: sqlmodel.QueryCommandMany,
			columns: embedded, wantError: "model: cannot shape the rows of a query that embeds a table, which hold a struct for each embed; drop model: or the embed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analyser := NewQueryAnalyser(&fakedialect.Engine{}, rowTypeCatalogue(), directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
			query := &sqlmodel.AnalysedQuery{Name: "ListRoles", Command: test.command, DynamicRuntime: test.runtime, OutputColumns: test.columns}

			diagnostics := analyser.applyRowType(query, &test.directives, &directive.DirectiveBlock{}, "queries.sql")

			if test.wantError != "" {
				require.Len(t, diagnostics, 1)
				require.Equal(t, diag.CodeRowType, diagnostics[0].Code)
				require.Equal(t, test.wantError, diagnostics[0].Message)
				return
			}
			require.Empty(t, diagnostics)
			require.Equal(t, test.wantModel, query.RowModel)
			require.Equal(t, test.wantRow, query.RowType)
		})
	}
}
