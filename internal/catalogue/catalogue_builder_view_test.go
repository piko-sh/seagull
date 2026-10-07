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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestEveryTypeNamingAnEnumTakesItsChangedValues(t *testing.T) {
	t.Parallel()

	const setup = "CREATE TYPE mood AS ENUM ('a', 'b');\n" +
		"CREATE TABLE t (m mood, ms mood[]);\n" +
		"CREATE DOMAIN calm AS mood;\n" +
		"CREATE VIEW v AS SELECT m FROM t;\n"
	testCases := []struct {
		name       string
		statements string
		wantValues []string
	}{
		{name: "a value added", statements: "ALTER TYPE mood ADD VALUE 'c';", wantValues: []string{"a", "b", "c"}},
		{name: "a value renamed", statements: "ALTER TYPE mood RENAME VALUE 'a' TO 'z';", wantValues: []string{"z", "b"}},
		{
			name:       "a value added and another renamed",
			statements: "ALTER TYPE mood ADD VALUE 'c';\nALTER TYPE mood RENAME VALUE 'a' TO 'z';",
			wantValues: []string{"z", "b", "c"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, setup, testCase.statements)

			require.Empty(t, diagnosticMessages(diagnostics))
			public := catalogue.Schemas["public"]
			require.Equal(t, testCase.wantValues, public.Enums["mood"].Values)
			table := public.Tables["t"]
			require.Equal(t, testCase.wantValues, table.Columns[0].SQLType.EnumValues, "the column")
			require.Equal(t, 1, table.Columns[1].ArrayDimensions)
			require.Equal(t, testCase.wantValues, table.Columns[1].SQLType.EnumValues, "the array column")
			require.Equal(t, testCase.wantValues, public.Domains["calm"].BaseType.EnumValues, "the domain")
			require.Equal(t, testCase.wantValues, public.Views["v"].Columns[0].SQLType.EnumValues, "the view column")

			table.Columns[0].SQLType.EnumValues[0] = "changed"
			require.Equal(t, testCase.wantValues, public.Enums["mood"].Values, "the column holds a copy of the values")
		})
	}
}

func TestRenamingAnEnumValueThatIsMissingOrTakenIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statement   string
		wantCode    string
		wantMessage string
	}{
		{name: "a missing value", statement: "ALTER TYPE mood RENAME VALUE 'x' TO 'y';", wantCode: diag.CodeUnknownObject,
			wantMessage: `enum mood has no value "x"`},
		{name: "onto a value it has", statement: "ALTER TYPE mood RENAME VALUE 'a' TO 'b';", wantCode: diag.CodeDuplicateObject,
			wantMessage: `enum mood already has the value "b"`},
		{name: "a value added twice", statement: "ALTER TYPE mood ADD VALUE 'a';", wantCode: diag.CodeDuplicateObject,
			wantMessage: `enum mood already has the value "a"`},
		{name: "a value added before one it lacks", statement: "ALTER TYPE mood ADD VALUE 'c' BEFORE 'x';",
			wantCode: diag.CodeUnknownObject, wantMessage: `enum mood has no value "x"`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, "CREATE TYPE mood AS ENUM ('a', 'b');\n"+testCase.statement)

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantCode, diagnostics[0].Code)
			require.Equal(t, 2, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			require.Equal(t, []string{"a", "b"}, catalogue.Schemas["public"].Enums["mood"].Values)
		})
	}
}

func TestRenamingAnEnumValueLeavesASharedValueListAlone(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	shared := []string{"red", "green"}
	builder.catalogue.Schemas["public"].Enums["colour"] = &sqlmodel.Enum{Name: "colour", Schema: "public", Values: shared}

	require.NoError(t, builder.applyAlterEnumRenameValue(&ast.CatalogueMutation{
		Kind:       ast.MutationAlterEnumRenameValue,
		SchemaName: "public",
		EnumName:   "colour",
		EnumValues: []string{"green", "lime"},
	}))

	require.Equal(t, []string{"red", "lime"}, builder.catalogue.Schemas["public"].Enums["colour"].Values)
	require.Equal(t, []string{"red", "green"}, shared)
}

func TestAddingAnEnumValueReachesEveryTypeThatNamesTheEnum(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	public := builder.catalogue.Schemas["public"]
	mood := func() sqlmodel.SQLType {
		sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
		sqlType.Schema = "public"
		sqlType.EnumValues = []string{"a"}
		return sqlType
	}
	other := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	other.Schema = "archive"
	other.EnumValues = []string{"a"}
	listOfMood := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "list", ElementType: new(mood())}
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "row")
	row.StructFields = []sqlmodel.StructField{{Name: "m", SQLType: mood()}}
	public.Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"a"}}
	public.Tables["t"] = &sqlmodel.Table{Name: "t", Schema: "public", Columns: []sqlmodel.Column{
		sqlmodel.NewColumn("plain", mood(), true),
		sqlmodel.NewColumn("listed", listOfMood, true),
		sqlmodel.NewColumn("nested", row, true),
		sqlmodel.NewColumn("elsewhere", other, true),
	}}
	public.Views["v"] = &sqlmodel.View{Name: "v", Schema: "public", Columns: []sqlmodel.Column{sqlmodel.NewColumn("plain", mood(), true)}}
	public.CompositeTypes["pair"] = &sqlmodel.CompositeType{Name: "pair", Schema: "public",
		Fields: []sqlmodel.Column{sqlmodel.NewColumn("m", mood(), true)}}
	public.Domains["calm"] = &sqlmodel.Domain{Name: "calm", Schema: "public", BaseType: mood()}
	public.Functions["swap"] = []*sqlmodel.Function{{Name: "swap", ReturnType: mood(),
		Arguments: []sqlmodel.FunctionArgument{{Name: "m", Type: mood()}}}}

	require.NoError(t, builder.applyMutation(t.Context(), &ast.CatalogueMutation{
		Kind:       ast.MutationAlterEnumAddValue,
		SchemaName: "public",
		EnumName:   "mood",
		EnumValues: []string{"b"},
	}))

	want := []string{"a", "b"}
	columns := public.Tables["t"].Columns
	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    []string
	}{
		{name: "a column", sqlType: columns[0].SQLType, want: want},
		{name: "a list's elements", sqlType: *columns[1].SQLType.ElementType, want: want},
		{name: "a struct's field", sqlType: columns[2].SQLType.StructFields[0].SQLType, want: want},
		{name: "an enum of the name in another schema", sqlType: columns[3].SQLType, want: []string{"a"}},
		{name: "a view column", sqlType: public.Views["v"].Columns[0].SQLType, want: want},
		{name: "a composite type's field", sqlType: public.CompositeTypes["pair"].Fields[0].SQLType, want: want},
		{name: "a domain's base type", sqlType: public.Domains["calm"].BaseType, want: want},
		{name: "a function's return type", sqlType: public.Functions["swap"][0].ReturnType, want: want},
		{name: "a function's argument", sqlType: public.Functions["swap"][0].Arguments[0].Type, want: want},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.sqlType.EnumValues)
		})
	}
}

func TestARelationWhoseQueryAnalysisHoldsANilNodeIsReportedAndKeepsItsDeclaredColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		kind ast.MutationKind
	}{
		{name: "a view", kind: ast.MutationCreateView},
		{name: "a table created from a query", kind: ast.MutationCreateTable},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilder()
			builder.statement = statementSite{filename: "001.up.sql", line: 4}
			mutation := &ast.CatalogueMutation{
				Kind:       testCase.kind,
				SchemaName: "public",
				TableName:  "totals",
				Columns:    []sqlmodel.Column{{Name: "v"}},
				ViewDefinition: &ast.RawQueryAnalysis{
					OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.FunctionCallExpression)(nil)}},
				},
			}

			require.NotPanics(t, func() { require.NoError(t, builder.applyMutation(t.Context(), mutation)) })

			require.Len(t, builder.statementDiagnostics, 1)
			problem := builder.statementDiagnostics[0]
			require.Equal(t, diag.CodeInternalNilGuard, problem.Code)
			require.Equal(t, diag.SeverityError, problem.Severity)
			require.Equal(t, 4, problem.Line)
			columns := relationColumns(builder.catalogue, "totals")
			require.Len(t, columns, 1)
			require.Equal(t, "v", columns[0].Name)
		})
	}
}

func relationColumns(catalogue *sqlmodel.Catalogue, name string) []sqlmodel.Column {
	if view := catalogue.Schemas["public"].Views[name]; view != nil {
		return view.Columns
	}
	if table := catalogue.Schemas["public"].Tables[name]; table != nil {
		return table.Columns
	}
	return nil
}

func TestCompositeFieldsAndFunctionTypesNamingAnEnumTakeItsValues(t *testing.T) {
	t.Parallel()

	const setup = "CREATE TYPE mood AS ENUM ('happy', 'sad');\n" +
		"CREATE TYPE pair AS (feeling mood, note text);\n" +
		"CREATE FUNCTION cheer(m mood, ms mood[]) RETURNS mood LANGUAGE sql AS $$ SELECT m $$;\n"
	testCases := []struct {
		name       string
		statements string
		wantValues []string
	}{
		{name: "as declared", statements: "", wantValues: []string{"happy", "sad"}},
		{name: "after a value is added", statements: "ALTER TYPE mood ADD VALUE 'calm';", wantValues: []string{"happy", "sad", "calm"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, setup+testCase.statements)
			require.Empty(t, diagnostics)

			schema := catalogue.Schemas["public"]
			field := schema.CompositeTypes["pair"].Fields[0].SQLType
			function := schema.Functions["cheer"][0]
			for name, sqlType := range map[string]sqlmodel.SQLType{
				"composite field": field,
				"argument":        function.Arguments[0].Type,
				"array argument":  *function.Arguments[1].Type.ElementType,
				"return type":     function.ReturnType,
			} {
				require.Equal(t, sqlmodel.TypeCategoryEnum, sqlType.Category, name)
				require.Equal(t, testCase.wantValues, sqlType.EnumValues, name)
			}
		})
	}
}
