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

package resolve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type argumentTypedEngine struct {
	*fakedialect.Engine
}

func (argumentTypedEngine) TypedTableValuedFunctionColumns(_ *sqlmodel.Catalogue, functionName string, argumentTypes []sqlmodel.SQLType) []ast.ScopedColumn {
	if functionName != "series" || len(argumentTypes) == 0 || argumentTypes[0].Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return []ast.ScopedColumn{{Name: "series", SQLType: argumentTypes[0], Nullable: false}}
}

func TestTableValuedFunctionWithOrdinalityAppendsItsColumn(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	integerType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	bigintType := sqlmodel.SQLType{EngineName: "bigint", Category: sqlmodel.TypeCategoryInteger, Width: 64}
	shared := make([]ast.ScopedColumn, 1, 4)
	shared[0] = ast.ScopedColumn{Name: "unnest", SQLType: integerType, Nullable: true}
	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			switch functionName {
			case "json_each":
				return []ast.ScopedColumn{
					{Name: "key", SQLType: textType, Nullable: true},
					{Name: "value", SQLType: textType, Nullable: true},
				}
			case "unnest":
				return shared
			default:
				return nil
			}
		},
	}
	ordinality := &ast.ScopedColumn{Name: "ordinality", SQLType: bigintType, Nullable: false}
	aliases := func(names ...string) []ast.TVFColumnDefinition {
		definitions := make([]ast.TVFColumnDefinition, len(names))
		for index, name := range names {
			definitions[index] = ast.TVFColumnDefinition{Name: name}
		}
		return definitions
	}

	testCases := []struct {
		name           string
		function       string
		definitions    []ast.TVFColumnDefinition
		wantColumns    []string
		wantNil        bool
		wantDiagnostic bool
	}{
		{
			name:        "the ordinality column follows the function's columns",
			function:    "json_each",
			wantColumns: []string{"key", "value", "ordinality"},
		},
		{
			name:        "an alias list names the ordinality column after the function's columns",
			function:    "json_each",
			definitions: aliases("k", "v", "i"),
			wantColumns: []string{"k", "v", "i"},
		},
		{
			name:        "a short alias list leaves the ordinality column its own name",
			function:    "json_each",
			definitions: aliases("k"),
			wantColumns: []string{"k", "value", "ordinality"},
		},
		{
			name:        "a one-column function named after its alias keeps the ordinality column",
			function:    "unnest",
			definitions: aliases("element"),
			wantColumns: []string{"element", "ordinality"},
		},
		{
			name:           "aliases beyond the ordinality column are reported",
			function:       "unnest",
			definitions:    aliases("element", "position", "extra"),
			wantColumns:    []string{"element", "position"},
			wantDiagnostic: true,
		},
		{
			name:     "an unknown function stays unknown",
			function: "missing",
			wantNil:  true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reference := ast.RawTableValuedFunctionReference{
				FunctionName:      testCase.function,
				Alias:             "f",
				ColumnDefinitions: testCase.definitions,
				Ordinality:        ordinality,
			}
			columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), engine, fakedialect.NewCatalogue("public"), reference, callTypers{arguments: untypedArguments, scalarCall: nil})
			require.NoError(t, interruption)

			if testCase.wantNil {
				require.Nil(t, columns)
				require.Nil(t, diagnostic)
				return
			}
			names := make([]string, len(columns))
			for index := range columns {
				names[index] = columns[index].Name
			}
			require.Equal(t, testCase.wantColumns, names)
			require.Equal(t, testCase.wantDiagnostic, diagnostic != nil)
			require.Equal(t, bigintType, columns[len(columns)-1].SQLType)
			require.False(t, columns[len(columns)-1].Nullable)
			require.Equal(t, "ordinality", ordinality.Name)
			require.Equal(t, "unnest", shared[:cap(shared)][0].Name)
			require.Equal(t, ast.ScopedColumn{}, shared[:cap(shared)][1])
		})
	}
}

func TestTableValuedFunctionWithoutOrdinalityKeepsItsColumns(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(string) []ast.ScopedColumn {
			return []ast.ScopedColumn{{Name: "value", SQLType: textType, Nullable: true}}
		},
	}

	columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), engine, fakedialect.NewCatalogue("public"),
		ast.RawTableValuedFunctionReference{FunctionName: "json_array_elements", Alias: "element"}, callTypers{arguments: untypedArguments, scalarCall: nil})
	require.NoError(t, interruption)

	require.Nil(t, diagnostic)
	require.Equal(t, []ast.ScopedColumn{{Name: "value", SQLType: textType, Nullable: true}}, columns)
}

func TestTableValuedFunctionOfSeveralCallsJoinsTheirColumns(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	integerType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	bigintType := sqlmodel.SQLType{EngineName: "bigint", Category: sqlmodel.TypeCategoryInteger, Width: 64}
	series := []ast.ScopedColumn{{Name: "generate_series", SQLType: integerType, Nullable: false}}
	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			switch functionName {
			case "generate_series":
				return series
			case "jsonb_each":
				return []ast.ScopedColumn{
					{Name: "key", SQLType: textType, Nullable: false},
					{Name: "value", SQLType: textType, Nullable: true},
				}
			default:
				return nil
			}
		},
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "int" {
				return integerType
			}
			return textType
		},
	}
	ordinality := &ast.ScopedColumn{Name: "ordinality", SQLType: bigintType, Nullable: false}
	call := func(name string, definitions ...ast.TVFColumnDefinition) ast.TableFunctionCall {
		return ast.TableFunctionCall{FunctionName: name, ColumnDefinitions: definitions}
	}
	aliases := func(names ...string) []ast.TVFColumnDefinition {
		definitions := make([]ast.TVFColumnDefinition, len(names))
		for index, name := range names {
			definitions[index] = ast.TVFColumnDefinition{Name: name}
		}
		return definitions
	}

	testCases := []struct {
		name            string
		functions       []ast.TableFunctionCall
		definitions     []ast.TVFColumnDefinition
		ordinality      *ast.ScopedColumn
		wantColumns     []string
		wantTypes       []sqlmodel.SQLType
		wantNullable    []bool
		wantUnknown     bool
		wantSurplusNote bool
	}{
		{
			name:         "each call's columns follow the previous call's and may be NULL",
			functions:    []ast.TableFunctionCall{call("generate_series"), call("jsonb_each")},
			wantColumns:  []string{"generate_series", "key", "value"},
			wantTypes:    []sqlmodel.SQLType{integerType, textType, textType},
			wantNullable: []bool{true, true, true},
		},
		{
			name:         "the ordinality column comes last and is never NULL",
			functions:    []ast.TableFunctionCall{call("generate_series"), call("generate_series")},
			ordinality:   ordinality,
			wantColumns:  []string{"generate_series", "generate_series", "ordinality"},
			wantTypes:    []sqlmodel.SQLType{integerType, integerType, bigintType},
			wantNullable: []bool{true, true, false},
		},
		{
			name:         "the alias list renames the columns of every call and the ordinality column by position",
			functions:    []ast.TableFunctionCall{call("jsonb_each"), call("generate_series")},
			definitions:  aliases("k", "v", "n", "i"),
			ordinality:   ordinality,
			wantColumns:  []string{"k", "v", "n", "i"},
			wantTypes:    []sqlmodel.SQLType{textType, textType, integerType, bigintType},
			wantNullable: []bool{true, true, true, false},
		},
		{
			name:         "a short alias list leaves the later columns their own names",
			functions:    []ast.TableFunctionCall{call("generate_series"), call("jsonb_each")},
			definitions:  aliases("n"),
			wantColumns:  []string{"n", "key", "value"},
			wantTypes:    []sqlmodel.SQLType{integerType, textType, textType},
			wantNullable: []bool{true, true, true},
		},
		{
			name: "a call's own column definition list gives its columns",
			functions: []ast.TableFunctionCall{
				call("json_to_record", ast.TVFColumnDefinition{Name: "a", TypeName: "int"}, ast.TVFColumnDefinition{Name: "b", TypeName: "text"}),
				call("generate_series"),
			},
			wantColumns:  []string{"a", "b", "generate_series"},
			wantTypes:    []sqlmodel.SQLType{integerType, textType, integerType},
			wantNullable: []bool{true, true, true},
		},
		{
			name:            "aliases beyond the columns are reported",
			functions:       []ast.TableFunctionCall{call("generate_series"), call("generate_series")},
			definitions:     aliases("a", "b", "c"),
			wantColumns:     []string{"a", "b"},
			wantTypes:       []sqlmodel.SQLType{integerType, integerType},
			wantNullable:    []bool{true, true},
			wantSurplusNote: true,
		},
		{
			name:        "an unknown call leaves the reference unknown",
			functions:   []ast.TableFunctionCall{call("generate_series"), call("missing")},
			wantUnknown: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reference := ast.RawTableValuedFunctionReference{
				FunctionName:      testCase.functions[0].FunctionName,
				Alias:             "f",
				ColumnDefinitions: testCase.definitions,
				Ordinality:        testCase.ordinality,
				Functions:         testCase.functions,
			}
			columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), engine, fakedialect.NewCatalogue("public"), reference, callTypers{arguments: untypedArguments, scalarCall: nil})
			require.NoError(t, interruption)

			if testCase.wantUnknown {
				require.Nil(t, columns)
				require.NotNil(t, diagnostic)
				require.Contains(t, diagnostic.Error(), `"missing"`)
				return
			}
			names := make([]string, len(columns))
			types := make([]sqlmodel.SQLType, len(columns))
			nullable := make([]bool, len(columns))
			for index := range columns {
				names[index] = columns[index].Name
				types[index] = columns[index].SQLType
				nullable[index] = columns[index].Nullable
			}
			require.Equal(t, testCase.wantColumns, names)
			require.Equal(t, testCase.wantTypes, types)
			require.Equal(t, testCase.wantNullable, nullable)
			require.Equal(t, testCase.wantSurplusNote, diagnostic != nil)
			require.False(t, series[0].Nullable)
			require.Equal(t, "generate_series", series[0].Name)
		})
	}
}

func TestTableValuedFunctionOfSeveralCallsTakesATypedAliasList(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	decimalType := sqlmodel.SQLType{EngineName: "numeric", Category: sqlmodel.TypeCategoryDecimal}
	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(string) []ast.ScopedColumn {
			return []ast.ScopedColumn{{Name: "unnest", SQLType: sqlmodel.SQLType{}, Nullable: true}}
		},
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "numeric" {
				return decimalType
			}
			return textType
		},
	}

	columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), engine, fakedialect.NewCatalogue("public"),
		ast.RawTableValuedFunctionReference{
			FunctionName: "unnest",
			Alias:        "t",
			ColumnDefinitions: []ast.TVFColumnDefinition{
				{Name: "name", TypeName: "text"},
				{Name: "price", TypeName: "numeric"},
			},
			Functions: []ast.TableFunctionCall{{FunctionName: "unnest"}, {FunctionName: "unnest"}},
		}, callTypers{arguments: untypedArguments, scalarCall: nil})
	require.NoError(t, interruption)

	require.Nil(t, diagnostic)
	require.Equal(t, []ast.ScopedColumn{
		{Name: "name", SQLType: textType, Nullable: true},
		{Name: "price", SQLType: decimalType, Nullable: true},
	}, columns)
}

func untypedArguments(context.Context, []ast.Expression) []sqlmodel.SQLType {
	return nil
}

func TestTableValuedFunctionColumnsFollowTheArgumentTypes(t *testing.T) {
	t.Parallel()

	integerType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	timestampType := sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false)
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["events"] = &sqlmodel.Table{
		Name:    "events",
		Schema:  "public",
		Columns: []sqlmodel.Column{{Name: "at", SQLType: timestampType}},
	}
	engine := argumentTypedEngine{Engine: &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			if functionName != "series" {
				return nil
			}
			return []ast.ScopedColumn{{Name: "series", SQLType: integerType, Nullable: false}}
		},
	}}
	at := &ast.ColumnRefExpression{TableAlias: "events", ColumnName: "at"}

	testCases := []struct {
		name      string
		reference ast.RawTableValuedFunctionReference
		want      sqlmodel.SQLTypeCategory
	}{
		{
			name:      "a call typed by its arguments",
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "s", Arguments: []ast.Expression{at, at}},
			want:      sqlmodel.TypeCategoryTemporal,
		},
		{
			name:      "a call whose arguments the dialect does not type keeps its columns by name",
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "s", Arguments: []ast.Expression{&ast.UnknownExpression{}}},
			want:      sqlmodel.TypeCategoryInteger,
		},
		{
			name: "each call of several typed by its own arguments",
			reference: ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: "s", Functions: []ast.TableFunctionCall{
				{FunctionName: "series", Arguments: []ast.Expression{at}},
			}},
			want: sqlmodel.TypeCategoryTemporal,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine))
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
			raw := &ast.RawQueryAnalysis{
				FromTables:              []ast.TableReference{{Name: "events"}},
				RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{testCase.reference},
			}
			require.Empty(t, builder.BuildQueryScope(t.Context(), raw, scope))

			column, _, err := scope.ResolveColumn("s", "series")
			require.NoError(t, err)
			require.Equal(t, testCase.want, column.SQLType.Category)
		})
	}
}

func scalarFunctionEngine(scalarFunctionsInFrom bool) *fakedialect.Engine {
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	catalogue := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
		"lower": {{Name: "lower", Arguments: []sqlmodel.FunctionArgument{{Name: "value", Type: text}}, ReturnType: text,
			NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull, DataAccess: sqlmodel.DataAccessReadOnly}},
		"total": {{Name: "total", Arguments: []sqlmodel.FunctionArgument{{Name: "value", Type: integer}}, ReturnType: integer,
			IsAggregate: true, DataAccess: sqlmodel.DataAccessReadOnly}},
		"spread": {{Name: "spread", Arguments: []sqlmodel.FunctionArgument{{Name: "value", Type: integer}}, ReturnType: integer,
			ReturnsSet: true, DataAccess: sqlmodel.DataAccessReadOnly}},
	}}
	engine := &fakedialect.Engine{BuiltinFunctionsFunc: func() *ast.FunctionCatalogue { return catalogue }}
	info := engine.Info()
	info.ScalarFunctionsInFrom = scalarFunctionsInFrom
	engine.InfoFunc = func() dialect.Info { return info }
	return engine
}

func TestACallOfAFunctionReturningOneValueInFromFollowsTheDialect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		inFrom       bool
		reference    ast.RawTableValuedFunctionReference
		wantColumns  []ast.ScopedColumn
		wantDiagnose bool
	}{
		{
			name:   "a scalar call gives one column named after the function",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "lower", Alias: "lower",
				Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}},
			wantColumns: []ast.ScopedColumn{{Name: "lower", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				Nullable: false}},
		},
		{
			name:   "the alias names the column",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "lower", Alias: "l",
				Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}},
			wantColumns: []ast.ScopedColumn{{Name: "l", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				Nullable: false}},
		},
		{
			name:   "a call among several is named after the function",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "lower", Alias: "both", Functions: []ast.TableFunctionCall{
				{FunctionName: "lower", Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}},
				{FunctionName: "lower", Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}},
			}},
			wantColumns: []ast.ScopedColumn{
				{Name: "lower", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true},
				{Name: "lower", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true},
			},
		},
		{
			name:   "a schema-qualified call is named after the bare function",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "public.lower", Alias: "lower",
				Arguments: []ast.Expression{&ast.ColumnRefExpression{ColumnName: "missing"}}},
			wantColumns: []ast.ScopedColumn{{Name: "lower", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				Nullable: true}},
		},
		{
			name:   "an aggregate is unknown",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "total", Alias: "total",
				Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "int4"}}},
			wantDiagnose: true,
		},
		{
			name:   "a set-returning function the dialect gives no columns is unknown",
			inFrom: true,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "spread", Alias: "spread",
				Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "int4"}}},
			wantDiagnose: true,
		},
		{
			name:   "a dialect without scalar calls in FROM reports the call",
			inFrom: false,
			reference: ast.RawTableValuedFunctionReference{FunctionName: "lower", Alias: "lower",
				Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}},
			wantDiagnose: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := scalarFunctionEngine(testCase.inFrom)
			builder := NewCatalogueScopeBuilder(engine, fakedialect.NewCatalogue("public"))
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)

			diagnostics := builder.resolveTableValuedFunctions(t.Context(), []ast.RawTableValuedFunctionReference{testCase.reference}, scope)

			if testCase.wantDiagnose {
				require.Len(t, diagnostics, 1)
				require.Equal(t, diag.CodeUnknownTable, diagnostics[0].Code)
				return
			}
			require.Empty(t, diagnostics)
			relation, found := scope.Table(testCase.reference.Alias)
			require.True(t, found)
			require.Equal(t, testCase.wantColumns, relation.Columns)
		})
	}
}

func TestTableValuedFunctionColumnsSortTheDialectsFailures(t *testing.T) {
	t.Parallel()

	panicked := fmt.Errorf("table function columns: %w: index out of range", fault.ErrPanic)
	single := ast.RawTableValuedFunctionReference{FunctionName: "my_udf", Alias: "u"}
	several := ast.RawTableValuedFunctionReference{
		FunctionName: "my_udf",
		Alias:        "u",
		Functions:    []ast.TableFunctionCall{{FunctionName: "my_udf"}, {FunctionName: "other_udf"}},
	}
	testCases := []struct {
		failure         error
		wantInterrupted error
		name            string
		wantProblem     string
		reference       ast.RawTableValuedFunctionReference
	}{
		{name: "a panic resolving a call", failure: panicked, reference: single, wantProblem: diag.CodeRecoveredPanic},
		{name: "a panic resolving one of several calls", failure: panicked, reference: several, wantProblem: diag.CodeRecoveredPanic},
		{name: "an interruption resolving a call", failure: context.Canceled, reference: single, wantInterrupted: context.Canceled},
		{name: "an interruption resolving one of several calls", failure: context.Canceled, reference: several, wantInterrupted: context.Canceled},
		{name: "any other failure resolving a call", failure: errors.New("not resolvable"), reference: single},
		{name: "any other failure resolving one of several calls", failure: errors.New("not resolvable"), reference: several, wantProblem: diag.CodeUnknownTable},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.CatalogueResolverEngine{
				Engine: &fakedialect.Engine{},
				FromCatalogueFunc: func(context.Context, *sqlmodel.Catalogue, string) ([]ast.ScopedColumn, error) {
					return nil, testCase.failure
				},
			}

			columns, problem, interruption := resolveTableValuedFunctionColumns(t.Context(), engine, fakedialect.NewCatalogue("public"),
				testCase.reference, callTypers{arguments: untypedArguments, scalarCall: nil})

			require.Nil(t, columns)
			require.ErrorIs(t, interruption, testCase.wantInterrupted)
			if testCase.wantProblem == "" {
				require.Nil(t, problem)
				return
			}
			require.NotNil(t, problem)
			require.Equal(t, testCase.wantProblem, problem.Code)
		})
	}
}

func TestAScalarCallInFromReportsWhatTheDialectFailedOn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		failure   error
		name      string
		wantCodes []string
	}{
		{
			name:      "a panic the dialect recovered is reported in place of an unknown function",
			failure:   fmt.Errorf("resolve function call: %w: index out of range", fault.ErrPanic),
			wantCodes: []string{diag.CodeRecoveredPanic},
		},
		{name: "an interruption is no problem with the query", failure: fmt.Errorf("walk: %w", context.Canceled), wantCodes: nil},
		{name: "any other failure leaves the function unknown", failure: errors.New("not resolvable"), wantCodes: []string{diag.CodeUnknownTable}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.FunctionResolverEngine{
				Engine: scalarFunctionEngine(true),
				ResolveFunc: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
					return nil, testCase.failure
				},
			}
			builder := NewCatalogueScopeBuilder(engine, fakedialect.NewCatalogue("public"))
			reference := ast.RawTableValuedFunctionReference{FunctionName: "explode", Alias: "e", Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: "text"}}}

			problems := builder.resolveTableValuedFunctions(t.Context(), []ast.RawTableValuedFunctionReference{reference}, builder.TypeResolver.NewRootScope(ScopeKindQuery))

			var codes []string
			for _, problem := range problems {
				codes = append(codes, problem.Code)
			}
			require.Equal(t, testCase.wantCodes, codes)
		})
	}
}

func TestAColumnDefinitionListTypesItsColumnsWithTheNamedTypesOfTheCatalogue(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"calm", "tense"}}
	catalogue.Schemas["public"].CompositeTypes["pair"] = &sqlmodel.CompositeType{Name: "pair", Schema: "public"}
	testCases := []struct {
		name           string
		calls          bool
		wantCategories []sqlmodel.SQLTypeCategory
	}{
		{name: "one call", calls: false, wantCategories: []sqlmodel.SQLTypeCategory{
			sqlmodel.TypeCategoryEnum, sqlmodel.TypeCategoryComposite, sqlmodel.TypeCategoryUnknown,
		}},
		{name: "a call of several", calls: true, wantCategories: []sqlmodel.SQLTypeCategory{
			sqlmodel.TypeCategoryEnum, sqlmodel.TypeCategoryComposite, sqlmodel.TypeCategoryUnknown,
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			definitions := []ast.TVFColumnDefinition{
				{Name: "feeling", TypeName: "mood"}, {Name: "both", TypeName: "pair"}, {Name: "other", TypeName: "unheard"},
			}
			reference := ast.RawTableValuedFunctionReference{FunctionName: "json_to_record", Alias: "x", ColumnDefinitions: definitions}
			if testCase.calls {
				reference = ast.RawTableValuedFunctionReference{FunctionName: "", Alias: "x", Functions: []ast.TableFunctionCall{
					{FunctionName: "json_to_record", ColumnDefinitions: definitions},
				}}
			}

			columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), &fakedialect.Engine{}, catalogue, reference,
				callTypers{arguments: untypedArguments, scalarCall: nil})
			require.NoError(t, interruption)
			require.Nil(t, diagnostic)
			categories := make([]sqlmodel.SQLTypeCategory, len(columns))
			for index, column := range columns {
				categories[index] = column.SQLType.Category
			}
			assert.Equal(t, testCase.wantCategories, categories)
			require.Len(t, columns, len(definitions))
			assert.Equal(t, []string{"calm", "tense"}, columns[0].SQLType.EnumValues)
		})
	}
}

func TestResolveTableValuedFunctionColumns(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	builtinEngine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			if functionName != "json_each" {
				return nil
			}
			return []ast.ScopedColumn{
				{Name: "key", SQLType: textType, Nullable: true},
				{Name: "value", SQLType: textType, Nullable: true},
			}
		},
	}
	catalogue := fakedialect.NewCatalogue("public")

	testCases := []struct {
		name           string
		engine         dialect.DialectPort
		reference      ast.RawTableValuedFunctionReference
		wantColumns    []string
		wantNilColumns bool
		wantDiagnostic bool
	}{
		{
			name:        "builtin lookup resolves json_each",
			engine:      builtinEngine,
			reference:   ast.RawTableValuedFunctionReference{FunctionName: "json_each", Alias: "je"},
			wantColumns: []string{"key", "value"},
		},
		{
			name:   "explicit typed column definitions win over lookup",
			engine: builtinEngine,
			reference: ast.RawTableValuedFunctionReference{
				FunctionName:      "json_each",
				Alias:             "je",
				ColumnDefinitions: []ast.TVFColumnDefinition{{Name: "n", TypeName: "int4"}},
			},
			wantColumns: []string{"n"},
		},
		{
			name:           "unknown function resolves to nil columns without a diagnostic",
			engine:         builtinEngine,
			reference:      ast.RawTableValuedFunctionReference{FunctionName: "does_not_exist", Alias: "x"},
			wantNilColumns: true,
		},
		{
			name:   "surplus alias-only definitions rename what fits and report the mismatch",
			engine: builtinEngine,
			reference: ast.RawTableValuedFunctionReference{
				FunctionName:      "json_each",
				Alias:             "je",
				ColumnDefinitions: []ast.TVFColumnDefinition{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			},
			wantColumns:    []string{"a", "b"},
			wantDiagnostic: true,
		},
		{
			name: "catalogue fallback resolves a user-defined function",
			engine: &fakedialect.CatalogueResolverEngine{
				Engine: &fakedialect.Engine{},
				FromCatalogueFunc: func(_ context.Context, _ *sqlmodel.Catalogue, functionName string) ([]ast.ScopedColumn, error) {
					if functionName != "my_udf" {
						return nil, nil
					}
					return []ast.ScopedColumn{{Name: "out", SQLType: textType}}, nil
				},
			},
			reference:   ast.RawTableValuedFunctionReference{FunctionName: "my_udf", Alias: "u"},
			wantColumns: []string{"out"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, diagnostic, interruption := resolveTableValuedFunctionColumns(t.Context(), testCase.engine, catalogue, testCase.reference, callTypers{arguments: untypedArguments, scalarCall: nil})
			require.NoError(t, interruption)

			if testCase.wantNilColumns {
				assert.Nil(t, columns)
				assert.Nil(t, diagnostic)
				return
			}

			names := make([]string, len(columns))
			for index, column := range columns {
				names[index] = column.Name
			}
			assert.Equal(t, testCase.wantColumns, names)

			if testCase.wantDiagnostic {
				assert.NotNil(t, diagnostic)
			} else {
				assert.Nil(t, diagnostic)
			}
		})
	}
}
