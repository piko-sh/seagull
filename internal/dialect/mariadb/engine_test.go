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

package mariadb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNewMariaDBEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	require.NotNil(t, engine, "engine should not be nil")
	assert.Equal(t, "mariadb", engine.Info().Name, "dialect should be mariadb")
}

func TestMariaDB_SupportsReturningOnInsertAndDelete(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, ast.DataChangeInsert|ast.DataChangeDelete, engine.Info().Returning,
		"MariaDB accepts RETURNING on INSERT and DELETE but not on UPDATE")
}

func TestMariaDB_ParameterStyle(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}, engine.Info().Placeholders.Execution,
		"MariaDB should use question-mark parameter style")
}

func TestMariaDB_DefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{""}, engine.Info().SchemaSearchPath,
		"MariaDB should have an empty default schema")
}

func TestMariaDB_BuiltinFunctions_ExtraFunctions(t *testing.T) {
	t.Parallel()

	engine := New()

	catalogue := engine.BuiltinFunctions()
	require.NotNil(t, catalogue, "function catalogue should not be nil")

	expectedFunctions := []string{
		"uuid",
		"sys_guid",
		"inet_aton",
		"inet_ntoa",
		"inet6_aton",
		"inet6_ntoa",
	}

	for _, functionName := range expectedFunctions {
		t.Run(functionName, func(t *testing.T) {
			t.Parallel()

			signatures, exists := catalogue.Functions[functionName]
			assert.True(t, exists, "function %q should exist in the catalogue", functionName)
			assert.NotEmpty(t, signatures, "function %q should have at least one signature", functionName)
		})
	}
}

func TestNewMariaDBEngineAppliesCallerOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		options   []mysql.Option
		wantError bool
	}{
		{
			name:      "default token budget accepts an ordinary query",
			sql:       "SELECT a, b, c FROM t WHERE a = ?",
			options:   nil,
			wantError: false,
		},
		{
			name:      "caller token budget is honoured",
			sql:       "SELECT a, b, c FROM t WHERE a = ?",
			options:   []mysql.Option{mysql.WithMaxTokensPerStatement(5)},
			wantError: true,
		},
		{
			name:      "caller parse depth is honoured",
			sql:       "SELECT ((((((((((1)))))))))) FROM t",
			options:   []mysql.Option{mysql.WithMaxParseDepth(4)},
			wantError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

			assert.Equal(t, "mariadb", engine.Info().Name, "caller options must not displace the MariaDB defaults")
			if testCase.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMariaDBKeepsItsOwnSettingsUnderCallerOptions(t *testing.T) {
	t.Parallel()

	engine := New(
		mysql.WithExtraFunctions(func(builder *mysql.FunctionCatalogueBuilder) {
			builder.NeverNull("caller_function", nil, builder.Text())
		}),
		mysql.WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { return nil }),
	)

	functions := engine.BuiltinFunctions().Functions
	require.Len(t, functions["sys_guid"], 1, "the flavour's own function survives a caller's registration")
	require.Len(t, functions["caller_function"], 1)
	require.Equal(t, "varchar", functions["sys_guid"][0].ReturnType.EngineName)
	require.Equal(t, ast.DataChangeInsert|ast.DataChangeDelete, engine.Info().Returning)
	require.Equal(t, "mariadb", engine.Info().Name)
}

func TestMariaDBRejectsMySQLOnlySyntax(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "json extract arrow", sql: "SELECT doc -> '$.a' AS a FROM t", token: "->"},
		{name: "json unquote arrow", sql: "SELECT id FROM t WHERE doc ->> '$.a' = ?", token: "->>"},
		{
			name:  "insert row alias",
			sql:   "INSERT INTO stock (item, quantity) VALUES (?, ?) AS new ON DUPLICATE KEY UPDATE quantity = new.quantity",
			token: "AS",
		},
		{name: "json value returning", sql: "SELECT JSON_VALUE(doc, '$.a' RETURNING SIGNED) AS a FROM t", token: "RETURNING"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, engine := range []*mysql.Dialect{New(), New(mysql.WithMaxParseDepth(64))} {
				statements, err := engine.ParseStatements(t.Context(), testCase.sql)
				require.NoError(t, err)

				analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

				require.Nil(t, analysis)
				parseError, ok := errors.AsType[*dialectkit.ParseError](err)
				require.True(t, ok, "got %v", err)
				require.Equal(t, testCase.token, parseError.Token)
			}

			statements, err := mysql.New().ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			_, err = mysql.New().AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err, "MySQL itself accepts the syntax")
		})
	}
}

func TestMariaDBAcceptsTheJSONFunctionsInPlaceOfArrows(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(),
		"SELECT JSON_UNQUOTE(JSON_EXTRACT(doc, '$.a')) AS a, JSON_VALUE(doc, '$.b') AS b FROM t "+
			"WHERE JSON_VALUE(doc, '$.c') = ?")
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	require.Len(t, analysis.ParameterReferences, 1)
	require.Equal(t, "text", analysis.ParameterReferences[0].CastType.EngineName)
}

func TestMariaDBOffersNoJSONArrowOperators(t *testing.T) {
	t.Parallel()

	mariaDBInfo := New().Info()
	assert.NotContains(t, mariaDBInfo.OperatorFeatures, "->")
	assert.NotContains(t, mariaDBInfo.OperatorFeatures, "->>")
	assert.Empty(t, dialect.JSONOperators(mariaDBInfo.OperatorFeatures), "the runtime builder offers no JSON arrow operators")
	assert.Contains(t, mariaDBInfo.OperatorFeatures, "&", "the other operators remain")

	mySQLInfo := mysql.New().Info()
	assert.Equal(t, []string{"->", "->>"}, dialect.JSONOperators(mySQLInfo.OperatorFeatures), "MySQL keeps its arrows")
}

func TestMariaDBRegistersTheJSONFunctionsMySQLLacks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		nullable  sqlmodel.FunctionNullableBehaviour
		category  sqlmodel.SQLTypeCategory
		arguments int
	}{
		{name: "json_query", nullable: sqlmodel.FunctionNullableCalledOnNull, category: sqlmodel.TypeCategoryJSON, arguments: 2},
		{name: "json_exists", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, category: sqlmodel.TypeCategoryBoolean, arguments: 2},
		{name: "json_equals", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, category: sqlmodel.TypeCategoryBoolean, arguments: 2},
		{name: "json_detailed", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, category: sqlmodel.TypeCategoryJSON, arguments: 1},
		{name: "json_normalize", nullable: sqlmodel.FunctionNullableReturnsNullOnNull, category: sqlmodel.TypeCategoryJSON, arguments: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signatures := New().BuiltinFunctions().Functions[testCase.name]

			require.Len(t, signatures, 1)
			require.Equal(t, testCase.nullable, signatures[0].NullableBehaviour)
			require.Equal(t, testCase.category, signatures[0].ReturnType.Category)
			require.Len(t, signatures[0].Arguments, testCase.arguments)
			require.NotContains(t, mysql.New().BuiltinFunctions().Functions, testCase.name, "MySQL has no %s", testCase.name)
		})
	}
}

func TestMariaDBResolvesItsOracleCompatibleFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	nullableInteger := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32, Nullable: true}
	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int", Width: 32}
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		nullable  bool
	}{
		{name: "NVL is NULL only when both arguments may be", function: "nvl", arguments: []sqlmodel.SQLType{nullableInteger, integer}, nullable: false},
		{name: "NVL2 may be NULL when its test may be", function: "nvl2", arguments: []sqlmodel.SQLType{nullableInteger, integer, integer}, nullable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.nullable, resolution.ReturnType.Nullable)
		})
	}
}

func TestMariaDBLacksTheMySQLOnlyFunctions(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	for _, name := range []string{
		"any_value", "regexp_like", "uuid_to_bin", "bin_to_uuid", "is_uuid", "grouping",
		"json_storage_size", "json_schema_validation_report",
	} {
		require.NotContains(t, functions, name)
	}
	require.Contains(t, mysql.New().BuiltinFunctions().Functions, "any_value")
}

func TestMariaDBTakesFewerArgumentsWhereMySQLTakesMore(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	testCases := []struct {
		name    string
		fewest  int
		longest int
	}{
		{name: "regexp_instr", fewest: 2, longest: 2},
		{name: "regexp_substr", fewest: 2, longest: 2},
		{name: "regexp_replace", fewest: 3, longest: 3},
		{name: "lag", fewest: 1, longest: 2},
		{name: "lead", fewest: 1, longest: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			overloads := functions[testCase.name]
			require.NotEmpty(t, overloads)
			for _, overload := range overloads {
				require.GreaterOrEqual(t, overload.RequiredArguments(), testCase.fewest)
				require.LessOrEqual(t, len(overload.Arguments), testCase.longest)
				require.False(t, overload.IsVariadic)
			}
		})
	}
}

func TestMariaDBResolvesNoCallWithMoreArgumentsThanItTakes(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int", 32, false)
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		resolves  bool
	}{
		{name: "LAG with an offset", function: "lag", arguments: []sqlmodel.SQLType{integer, integer}, resolves: true},
		{name: "LAG with a default", function: "lag", arguments: []sqlmodel.SQLType{integer, integer, integer}, resolves: false},
		{name: "LEAD with a default", function: "lead", arguments: []sqlmodel.SQLType{integer, integer, integer}, resolves: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.Equal(t, testCase.resolves, resolution != nil)
		})
	}
}

func TestMariaDBShiftsAParameterIntoAString(t *testing.T) {
	t.Parallel()

	parameter := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	integer := sqlmodel.NewIntegerType("int", 32, false)
	unit := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	datetime := sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false)
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		mariaDB   string
		mySQL     string
	}{
		{name: "DATE_ADD of a parameter", function: "date_add", arguments: []sqlmodel.SQLType{parameter, parameter}, mariaDB: "varchar", mySQL: "datetime"},
		{name: "ADDTIME of a parameter", function: "addtime", arguments: []sqlmodel.SQLType{parameter, unit}, mariaDB: "varchar", mySQL: "datetime"},
		{name: "TIMESTAMPADD of a parameter", function: "timestampadd", arguments: []sqlmodel.SQLType{unit, integer, parameter}, mariaDB: "varchar", mySQL: "datetime"},
		{name: "DATE_ADD of a date and time", function: "date_add", arguments: []sqlmodel.SQLType{datetime, parameter}, mariaDB: "datetime", mySQL: "datetime"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fromMariaDB, err := New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, fromMariaDB)
			require.Equal(t, testCase.mariaDB, fromMariaDB.ReturnType.EngineName)

			fromMySQL, err := mysql.New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, fromMySQL)
			require.Equal(t, testCase.mySQL, fromMySQL.ReturnType.EngineName)
		})
	}
}

func TestMariaDBTypesItsOwnColumnTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		name     string
		spelling string
		category sqlmodel.SQLTypeCategory
	}{
		{name: "UUID is a UUID", spelling: "UUID", category: sqlmodel.TypeCategoryUUID},
		{name: "INET4 is the address's text", spelling: "INET4", category: sqlmodel.TypeCategoryText},
		{name: "INET6 is the address's text", spelling: "inet6", category: sqlmodel.TypeCategoryText},
		{name: "VECTOR is its packed bytes", spelling: "VECTOR", category: sqlmodel.TypeCategoryBinary},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.category, engine.NormaliseTypeName(testCase.spelling, 3).Category)
		})
	}
}

func TestMariaDBMakesAFloatOfMoreThanTwentyFourBitsADouble(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		columnType     string
		wantEngineName string
		wantWidth      int
	}{
		{name: "a float with no precision", columnType: "FLOAT", wantEngineName: "float", wantWidth: 32},
		{name: "a float of twenty-four bits", columnType: "FLOAT(24)", wantEngineName: "float", wantWidth: 32},
		{name: "a float of twenty-five bits", columnType: "FLOAT(25)", wantEngineName: "double", wantWidth: 64},
		{name: "a float of fifty-three bits", columnType: "FLOAT(53) UNSIGNED", wantEngineName: "double", wantWidth: 64},
		{name: "a float with a scale", columnType: "FLOAT(30, 4)", wantEngineName: "float", wantWidth: 32},
		{name: "FLOAT4", columnType: "FLOAT4", wantEngineName: "float", wantWidth: 32},
		{name: "FLOAT4 of twenty-five bits", columnType: "FLOAT4(25)", wantEngineName: "double", wantWidth: 64},
		{name: "FLOAT8", columnType: "FLOAT8", wantEngineName: "double", wantWidth: 64},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (c "+testCase.columnType+")")
			require.NoError(t, err)
			require.Len(t, statements, 1)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.Len(t, mutation.Columns, 1)

			assert.Equal(t, testCase.wantEngineName, mutation.Columns[0].SQLType.EngineName)
			assert.Equal(t, testCase.wantWidth, mutation.Columns[0].SQLType.Width)
		})
	}
}

func TestMariaDBRefusesAFloatOfMoreThanFiftyThreeBits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		columnType string
	}{
		{name: "a float column", columnType: "FLOAT(54)"},
		{name: "a FLOAT4 column", columnType: "FLOAT4(60)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (c "+testCase.columnType+")")
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.Nil(t, mutation)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, "FLOAT takes a precision of at most 53 bits", parseError.Message)
		})
	}
}

func TestMariaDBAddressTestsAreIntegersThatAreNeverNull(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	for _, name := range []string{"is_ipv4", "is_ipv6", "is_ipv4_mapped", "is_ipv4_compat"} {
		require.Len(t, functions[name], 1, name)
		require.Equal(t, "int", functions[name][0].ReturnType.EngineName, name)
		require.Equal(t, sqlmodel.FunctionNullableNeverNull, functions[name][0].NullableBehaviour, name)
	}
}

func TestMariaDBReturnsTheRowOfAQueryReadingUngroupedColumns(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialect.UngroupedColumnsNullable, New().Info().UngroupedColumns)
}

func TestMariaDBReadsSequencesByName(t *testing.T) {
	t.Parallel()

	engine := New()
	sql := "SELECT NEXT VALUE FOR orders_seq AS n, PREVIOUS VALUE FOR shop.orders_seq AS p, NEXTVAL(orders_seq) AS n2, " +
		"LASTVAL(shop.orders_seq) AS l, SETVAL(orders_seq, 10, 1) AS s, ? AS chosen"
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	calls := make([]string, 0, 5)
	for _, column := range analysis.OutputColumns[:5] {
		call, isCall := column.Expression.(*ast.FunctionCallExpression)
		require.True(t, isCall, column.Name)
		require.Equal(t, &ast.LiteralExpression{TypeName: "text"}, call.Arguments[0], column.Name)
		calls = append(calls, call.FunctionName)
	}
	require.Equal(t, []string{"nextval", "lastval", "nextval", "lastval", "setval"}, calls[:5])
	require.Len(t, analysis.ParameterReferences, 1)
}

func TestMariaDBRejectsAParameterSetvalCannotBind(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(), "SELECT SETVAL(orders_seq, ?) AS moved")
	require.NoError(t, err)

	analysis, err := New().AnalyseQuery(t.Context(), nil, statements[0])

	require.Nil(t, analysis)
	parseError, ok := errors.AsType[*dialectkit.ParseError](err)
	require.True(t, ok, "got %v", err)
	require.Equal(t, "?", parseError.Token)
}

func TestMariaDBTypesItsSequenceFunctions(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	testCases := []struct {
		name       string
		nullable   sqlmodel.FunctionNullableBehaviour
		dataAccess sqlmodel.FunctionDataAccess
	}{
		{name: "nextval", nullable: sqlmodel.FunctionNullableNeverNull, dataAccess: sqlmodel.DataAccessModifiesData},
		{name: "lastval", nullable: sqlmodel.FunctionNullableCalledOnNull, dataAccess: sqlmodel.DataAccessReadOnly},
		{name: "setval", nullable: sqlmodel.FunctionNullableCalledOnNull, dataAccess: sqlmodel.DataAccessModifiesData},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, functions[testCase.name], 1)
			signature := functions[testCase.name][0]
			require.Equal(t, sqlmodel.TypeCategoryInteger, signature.ReturnType.Category)
			require.Equal(t, testCase.nullable, signature.NullableBehaviour)
			require.Equal(t, testCase.dataAccess, signature.DataAccess)
		})
	}
	require.Empty(t, mysql.New().BuiltinFunctions().Functions["nextval"], "MySQL has no sequences")
}

func TestMySQLHasNoSequenceSyntax(t *testing.T) {
	t.Parallel()

	engine := mysql.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT NEXT VALUE FOR orders_seq AS n")
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.Nil(t, analysis)
	require.Error(t, err)
}

func TestMariaDBTakesTheStandardRowLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		contexts []sqlmodel.ParameterContext
	}{
		{name: "an offset and a fetch", sql: "SELECT id FROM t ORDER BY id OFFSET ? ROWS FETCH NEXT ? ROWS ONLY",
			contexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextOffset, sqlmodel.ParameterContextLimit}},
		{name: "a fetch with ties", sql: "SELECT id FROM t ORDER BY id FETCH FIRST ? ROWS WITH TIES",
			contexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextLimit}},
		{name: "one row", sql: "SELECT id FROM t ORDER BY id OFFSET 1 ROW FETCH FIRST ROW ONLY", contexts: nil},
		{name: "an offset alone", sql: "SELECT id FROM t ORDER BY id OFFSET ? ROWS",
			contexts: []sqlmodel.ParameterContext{sqlmodel.ParameterContextOffset}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := New().ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			analysis, err := New().AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			var contexts []sqlmodel.ParameterContext
			for _, reference := range analysis.ParameterReferences {
				contexts = append(contexts, reference.Context)
			}
			require.Equal(t, testCase.contexts, contexts)

			statements, err = mysql.New().ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			_, err = mysql.New().AnalyseQuery(t.Context(), nil, statements[0])
			require.Error(t, err, "MySQL takes LIMIT alone")
		})
	}
}

func TestMariaDBReadsThePercentileSortedValueAsAnArgument(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(),
		"SELECT PERCENTILE_DISC(0.5) WITHIN GROUP (ORDER BY price DESC) OVER (PARTITION BY shop) AS p FROM t")
	require.NoError(t, err)

	analysis, err := New().AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	window, isWindow := analysis.OutputColumns[0].Expression.(*ast.WindowFunctionExpression)
	require.True(t, isWindow)
	require.Equal(t, "percentile_disc", window.Function.FunctionName)
	require.Len(t, window.Function.Arguments, 2)
	sorted, isColumn := window.Function.Arguments[1].(*ast.ColumnRefExpression)
	require.True(t, isColumn)
	require.Equal(t, "price", sorted.ColumnName)
}

func TestMariaDBReplacesWhatCreateOrReplaceNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantKinds []ast.MutationKind
		wantNames []string
	}{
		{
			name:      "a table",
			sql:       "CREATE OR REPLACE TABLE items (id INT PRIMARY KEY, label TEXT)",
			wantKinds: []ast.MutationKind{ast.MutationDropTable, ast.MutationCreateTable},
			wantNames: []string{"items", "items"},
		},
		{
			name:      "a temporary table",
			sql:       "CREATE OR REPLACE TEMPORARY TABLE scratch (id INT)",
			wantKinds: []ast.MutationKind{ast.MutationDropTable, ast.MutationCreateTable},
			wantNames: []string{"scratch", "scratch"},
		},
		{
			name:      "a copy of another table",
			sql:       "CREATE OR REPLACE TABLE shop.copied LIKE items",
			wantKinds: []ast.MutationKind{ast.MutationDropTable, ast.MutationCreateTable},
			wantNames: []string{"copied", "copied"},
		},
		{
			name:      "a table made by a query",
			sql:       "CREATE OR REPLACE TABLE totals AS SELECT 1 AS total",
			wantKinds: []ast.MutationKind{ast.MutationDropTable, ast.MutationCreateTable},
			wantNames: []string{"totals", "totals"},
		},
		{
			name:      "an index",
			sql:       "CREATE OR REPLACE INDEX items_label ON items (label(10))",
			wantKinds: []ast.MutationKind{ast.MutationCreateIndex},
			wantNames: []string{"items"},
		},
		{
			name:      "a database",
			sql:       "CREATE OR REPLACE DATABASE archive",
			wantKinds: []ast.MutationKind{ast.MutationDropSchema, ast.MutationCreateSchema},
			wantNames: []string{"", ""},
		},
		{
			name:      "a trigger",
			sql:       "CREATE OR REPLACE TRIGGER items_touch BEFORE INSERT ON items FOR EACH ROW SET NEW.label = 'x'",
			wantKinds: []ast.MutationKind{ast.MutationCreateTrigger},
			wantNames: []string{"items"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.NotNil(t, mutation)

			kinds := []ast.MutationKind{mutation.Kind}
			names := []string{mutation.TableName}
			for _, additional := range mutation.AdditionalMutations {
				kinds = append(kinds, additional.Kind)
				names = append(names, additional.TableName)
			}
			require.Equal(t, testCase.wantKinds, kinds)
			require.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestMySQLRefusesCreateOrReplaceTable(t *testing.T) {
	t.Parallel()

	engine := mysql.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE OR REPLACE TABLE items (id INT PRIMARY KEY)")
	require.NoError(t, err)

	_, err = engine.ApplyDDL(t.Context(), statements[0])

	require.Error(t, err)
}

func TestMariaDBInfoReturnsACopyTheCallerMayChange(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		change func(info *dialect.Info)
	}{
		{name: "the operator features", change: func(info *dialect.Info) { clear(info.OperatorFeatures) }},
		{name: "an operator MariaDB lacks", change: func(info *dialect.Info) { info.OperatorFeatures["->"] = ast.SQLFeatureJSONOp }},
		{name: "the system columns", change: func(info *dialect.Info) { info.SystemColumns[0] = "changed" }},
		{name: "the drivers", change: func(info *dialect.Info) { info.Drivers[0] = "changed" }},
		{name: "the cast types", change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" }},
		{name: "the placeholder forms", change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "$" }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := withoutLexicalCallbacks(engine.Info())

			testCase.change(new(engine.Info()))

			assert.Equal(t, want, withoutLexicalCallbacks(engine.Info()))
			assert.Equal(t, want, withoutLexicalCallbacks(New().Info()))
		})
	}
}

func withoutLexicalCallbacks(info dialect.Info) dialect.Info {
	info.Lexical.ProceduralBlock = nil
	info.Lexical.RequiresNoTransaction = nil
	return info
}
