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

package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type foreignStatement struct{}

type panickingContext struct {
	context.Context
}

func (*foreignStatement) IsParsedStatement() {}

func (panickingContext) Err() error {
	panic("context failed")
}

func TestEngine_SatisfiesTheDialectContract(t *testing.T) {
	t.Parallel()

	var _ dialect.DialectPort = New()
}

func TestEngine_Dialect(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Equal(t, "clickhouse", engine.Info().Name)
}

func TestEngine_DialectNameOverride(t *testing.T) {
	t.Parallel()

	engine := New(WithName("clickhouse-cloud"))
	assert.Equal(t, "clickhouse-cloud", engine.Info().Name)
}

func TestEngine_ParameterStyleIsCurly(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}, engine.Info().Placeholders.Execution)
}

func TestEngine_SupportedDirectivePrefixIsBrace(t *testing.T) {
	t.Parallel()

	engine := New()
	prefixes := engine.Info().Placeholders.Sigils
	require.Len(t, prefixes, 1)
	assert.Equal(t, "{", prefixes[0].Prefix)
	assert.Equal(t, "}", prefixes[0].Suffix)
	assert.Equal(t, ":", prefixes[0].TypeSeparator)
	assert.Equal(t, dialect.PlaceholderNamed, prefixes[0].Kind)
	assert.Equal(t, "p_", engine.Info().Placeholders.NamePrefix)
}

func TestEngine_DoesNotSupportReturning(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Zero(t, engine.Info().Returning)
}

func TestEngine_DefaultSchemaIsDefault(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Equal(t, []string{"default"}, engine.Info().SchemaSearchPath)
}

func TestEngine_CommentStyleIsStandardSQL(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Equal(t, "--", engine.Info().Comments.LinePrefix)
}

func TestEngine_NormaliseTypeNameResolvesPrimitive(t *testing.T) {
	t.Parallel()

	engine := New()
	result := engine.NormaliseTypeName("UInt32")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.Category)
	assert.Equal(t, "UInt32", result.EngineName)
}

func TestEngine_NormaliseTypeNameUnknownFallsBack(t *testing.T) {
	t.Parallel()

	engine := New()
	result := engine.NormaliseTypeName("CompletelyMadeUpType")
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, result.Category)
	assert.Equal(t, "CompletelyMadeUpType", result.EngineName)
}

func TestEngine_NormaliseTypeNameHonoursHook(t *testing.T) {
	t.Parallel()

	hook := func(name string, modifiers []int) *sqlmodel.SQLType {
		_ = modifiers
		if name == "UInt32" {
			return &sqlmodel.SQLType{
				Category:   sqlmodel.TypeCategoryInteger,
				EngineName: "UInt32",
			}
		}
		return nil
	}
	engine := New(WithTypeNormaliserHook(hook))
	result := engine.NormaliseTypeName("UInt32")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, result.Category)
}

func TestEngine_BuiltinCataloguesAreNonNil(t *testing.T) {
	t.Parallel()

	engine := New()
	require.NotNil(t, engine.BuiltinFunctions())
	require.NotNil(t, engine.BuiltinTypes())
}

func TestEngine_WithExtraTypes(t *testing.T) {
	t.Parallel()

	extra := map[string]sqlmodel.SQLType{
		"MyCustomType": {
			Category:   sqlmodel.TypeCategoryText,
			EngineName: "MyCustomType",
		},
	}
	engine := New(WithExtraTypes(extra))
	types := engine.BuiltinTypes()
	require.NotNil(t, types)
	entry, ok := types.Types["MyCustomType"]
	if !ok {
		entry, ok = types.Types["mycustomtype"]
	}
	require.True(t, ok, "extra type not registered in Types catalogue")
	assert.Equal(t, sqlmodel.TypeCategoryText, entry.Category)
}

func TestEngine_WithExtraFunctions(t *testing.T) {
	t.Parallel()

	registerExtras := func(b *FunctionCatalogueBuilder) {
		b.Register("my_extra_func", b.Type("UInt64"), b.Type("UInt64"))
	}
	engine := New(WithExtraFunctions(registerExtras))
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)
	overloads, ok := functions.Functions["my_extra_func"]
	require.True(t, ok, "extra function not registered (catalogue is case-insensitive via lowercase key)")
	require.Len(t, overloads, 1)
	assert.Equal(t, "my_extra_func", overloads[0].Name)
}

func TestEngine_WithImplicitCastHook(t *testing.T) {
	t.Parallel()

	hookCalled := false
	engine := New(WithImplicitCastHook(func(from, to sqlmodel.SQLType) *bool {
		hookCalled = true
		_ = from
		_ = to
		return new(true)
	}))
	result := engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""))
	assert.True(t, hookCalled, "implicit cast hook should be invoked")
	assert.True(t, result, "hook return must override default")
}

func TestEngine_WithPromoteTypeHook(t *testing.T) {
	t.Parallel()

	hookCalled := false
	engine := New(WithPromoteTypeHook(func(left, right sqlmodel.SQLType) *sqlmodel.SQLType {
		hookCalled = true
		_ = left
		_ = right
		return new(sqlmodel.NewFloatType("Float64", 64))
	}))
	leftType := sqlmodel.NewIntegerType("UInt32", 32, true)
	rightType := sqlmodel.NewFloatType("Float32", 32)
	result := engine.PromoteType(leftType, rightType)
	assert.True(t, hookCalled, "promote type hook should be invoked")
	assert.Equal(t, "Float64", result.EngineName, "hook return must override default")
}

func TestEngine_PromoteTypeMixedSignEqualWidthWidensToSigned(t *testing.T) {
	t.Parallel()

	engine := New()
	cases := []struct {
		left     string
		right    string
		expected string
	}{
		{left: "Int32", right: "UInt32", expected: "Int64"},
		{left: "UInt32", right: "Int32", expected: "Int64"},
		{left: "Int8", right: "UInt8", expected: "Int16"},
		{left: "UInt16", right: "Int16", expected: "Int32"},
	}
	for _, testCase := range cases {
		t.Run(testCase.left+"_"+testCase.right, func(subtest *testing.T) {
			subtest.Parallel()
			left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: testCase.left}
			right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: testCase.right}
			result := engine.PromoteType(left, right)
			assert.Equal(t, testCase.expected, result.EngineName,
				"mixed-sign equal-width integers must widen to the next wider signed type")
			assert.Equal(t, sqlmodel.TypeCategoryInteger, result.Category)
		})
	}
}

func TestEngine_PromoteTypeMixedSign64BitKeepsLeftOperand(t *testing.T) {
	t.Parallel()

	engine := New()
	left := sqlmodel.NewIntegerType("Int64", 64, false)
	right := sqlmodel.NewIntegerType("UInt64", 64, true)
	result := engine.PromoteType(left, right)
	assert.Equal(t, "Int64", result.EngineName,
		"Int64 vs UInt64 is out of scope (stock ClickHouse throws); the left operand is preserved")
}

func TestEngine_PromoteTypeSameSignWiderRankWins(t *testing.T) {
	t.Parallel()

	engine := New()
	left := sqlmodel.NewIntegerType("Int16", 16, false)
	right := sqlmodel.NewIntegerType("Int64", 64, false)
	result := engine.PromoteType(left, right)
	assert.Equal(t, "Int64", result.EngineName,
		"same-sign integers promote to the wider rank")
}

func TestBuiltinFunctions_NewlyRegisteredFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	expected := []struct {
		key             string
		minSignatures   int
		expectedAggrTag bool
	}{
		{key: "datetrunc", minSignatures: 2},
		{key: "lengthutf8", minSignatures: 1},
		{key: "hastoken", minSignatures: 1},
		{key: "hastokencaseinsensitive", minSignatures: 1},
		{key: "hastokenornull", minSignatures: 1},
		{key: "leftpad", minSignatures: 2},
		{key: "rightpad", minSignatures: 2},
		{key: "initcap", minSignatures: 1},
		{key: "ascii", minSignatures: 1},
		{key: "regexpextract", minSignatures: 2},
		{key: "tostartofquarter", minSignatures: 1},
		{key: "toisoweek", minSignatures: 1},
		{key: "monthname", minSignatures: 1},
		{key: "fromunixtimestamp64milli", minSignatures: 1},
		{key: "makedate", minSignatures: 1},
		{key: "makedatetime64", minSignatures: 1},
		{key: "tofloat64ornull", minSignatures: 1},
		{key: "toint8ornull", minSignatures: 1},
		{key: "toint64orzero", minSignatures: 1},
		{key: "touint64ordefault", minSignatures: 1},
		{key: "todecimal32", minSignatures: 1},
		{key: "todecimal256ornull", minSignatures: 1},
		{key: "todate32", minSignatures: 1},
		{key: "todateornull", minSignatures: 1},
		{key: "avgweighted", minSignatures: 1, expectedAggrTag: true},
		{key: "sumkahan", minSignatures: 1, expectedAggrTag: true},
		{key: "groupconcat", minSignatures: 2, expectedAggrTag: true},
		{key: "quantiletiming", minSignatures: 1, expectedAggrTag: true},
		{key: "histogram", minSignatures: 1, expectedAggrTag: true},
		{key: "intervallengthsum", minSignatures: 1, expectedAggrTag: true},
		{key: "approx_top_k", minSignatures: 1, expectedAggrTag: true},
		{key: "simplejsonextractstring", minSignatures: 1},
		{key: "simplejsonhas", minSignatures: 1},
		{key: "json_value", minSignatures: 1},
		{key: "clamp", minSignatures: 1},
		{key: "pi", minSignatures: 1},
		{key: "e", minSignatures: 1},
		{key: "sign", minSignatures: 1},
		{key: "factorial", minSignatures: 1},
		{key: "cbrt", minSignatures: 1},
		{key: "widthbucket", minSignatures: 1},
		{key: "generateuuidv7", minSignatures: 2},
		{key: "generatesnowflakeid", minSignatures: 2},
		{key: "snowflakeidtodatetime", minSignatures: 1},
	}

	for index := range expected {
		testCase := expected[index]
		t.Run(testCase.key, func(testRunner *testing.T) {
			testRunner.Parallel()
			signatures, found := functions.Functions[testCase.key]
			require.True(testRunner, found, "function %q is missing from the catalogue", testCase.key)
			require.GreaterOrEqual(testRunner, len(signatures), testCase.minSignatures, "function %q has too few signatures", testCase.key)
			if testCase.expectedAggrTag {
				assert.True(testRunner, signatures[0].IsAggregate, "function %q should be tagged as an aggregate", testCase.key)
			}
		})
	}
}

func TestBuiltinFunctions_NoDuplicateArrayDistinctOrResize(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)
	arrayDistinctSignatures := functions.Functions["arraydistinct"]
	arrayResizeSignatures := functions.Functions["arrayresize"]
	assert.Len(t, arrayDistinctSignatures, 1, "arrayDistinct should have a single registration")
	assert.Len(t, arrayResizeSignatures, 1, "arrayResize should have a single registration")
}

func TestEngine_ParameterStyleIsClickHouseCurly(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}, engine.Info().Placeholders.Execution)
}

func TestEngine_SupportsReturningIsFalse(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.Zero(t, engine.Info().Returning)
}

func TestEngine_SupportedExpressionsAdvertisesAllExpectedFlags(t *testing.T) {
	t.Parallel()

	engine := New()
	flags := engine.Info().Expressions
	required := []ast.ExpressionFeature{
		ast.SQLFeatureScalarSubquery,
		ast.SQLFeatureWindowFunction,
		ast.SQLFeatureArraySubscript,
		ast.SQLFeatureJSONOp,
		ast.SQLFeatureBitwiseOp,
		ast.SQLFeatureLambda,
		ast.SQLFeatureStructFieldAccess,
	}
	for _, flag := range required {
		assert.NotZero(t, flags&flag, "missing expected ExpressionFeature flag")
	}
}

func TestEngine_ParseStatementsRecordsPerStatementByteLength(t *testing.T) {
	t.Parallel()

	const first = "SELECT 1"
	const second = "SELECT 22"
	sql := first + "; " + second

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	assert.Equal(t, 0, statements[0].Location)
	assert.Equal(t, len(first), statements[0].Length,
		"first statement Length should span only its own tokens, not the whole source")

	secondLocation := len(first) + len("; ")
	assert.Equal(t, secondLocation, statements[1].Location)
	assert.Equal(t, len(second), statements[1].Length,
		"second statement Length should span only its own tokens")
}

func TestBuiltinFunctions_NoCaseVariantDuplicateRegistrations(t *testing.T) {
	t.Parallel()

	engine := New()
	functions := engine.BuiltinFunctions()
	require.NotNil(t, functions)

	assert.Len(t, functions.Functions["cast"], 1,
		"CAST should have a single registration despite case-insensitive keys")
	assert.Len(t, functions.Functions["hostname"], 1,
		"hostName should have a single registration despite case-insensitive keys")
}

func TestWithMaxTokensPerStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit is applied", limit: 42, want: 42},
		{name: "zero keeps the default", limit: 0, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative keeps the default", limit: -7, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "a limit above the ceiling is held at the ceiling", limit: dialectkit.MaxTokensPerStatementCeiling + 1, want: dialectkit.MaxTokensPerStatementCeiling},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			applied := settings{}
			WithMaxTokensPerStatement(testCase.limit)(&applied)
			assert.Equal(t, testCase.want, applied.Limits.TokensPerStatement())
		})
	}
}

func TestWithMaxTypeParseDepth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		depth int
		want  int
	}{
		{name: "positive depth is applied", depth: 5, want: 5},
		{name: "zero keeps the default", depth: 0, want: defaultMaxTypeParseDepth},
		{name: "negative keeps the default", depth: -1, want: defaultMaxTypeParseDepth},
		{name: "a depth above the checked type depth is held at it", depth: sqlmodel.MaxTypeDepth + 1, want: sqlmodel.MaxTypeDepth},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			applied := settings{}
			WithMaxTypeParseDepth(testCase.depth)(&applied)
			assert.Equal(t, testCase.want, applied.resolvedMaxTypeParseDepth())
		})
	}
}

func TestTokenBudgetRejectsLongStatements(t *testing.T) {
	t.Parallel()

	const limit = 20
	longSelect := "SELECT " + strings.TrimSuffix(strings.Repeat("1, ", limit), ", ")
	testCases := []struct {
		name      string
		sql       string
		isDDL     bool
		wantError bool
	}{
		{name: "query over the budget", sql: longSelect, isDDL: false, wantError: true},
		{name: "query within the budget", sql: "SELECT 1, 2", isDDL: false, wantError: false},
		{name: "DDL over the budget", sql: "CREATE TABLE t (" + strings.TrimSuffix(strings.Repeat("c UInt8, ", limit), ", ") + ") ENGINE = Memory", isDDL: true, wantError: true},
		{name: "statement without a DDL handler is not walked", sql: "INSERT INTO t VALUES " + strings.TrimSuffix(strings.Repeat("(1), ", limit), ", "), isDDL: true, wantError: false},
		{name: "statement kind AnalyseQuery does not walk, whose unread tokens may hold a parameter", sql: "SHOW TABLES " + strings.Repeat("x ", limit), isDDL: false, wantError: true},
		{name: "statement kind AnalyseQuery does not walk within the budget", sql: "SHOW TABLES x", isDDL: false, wantError: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(limit))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			if testCase.isDDL {
				_, err = engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			}
			if !testCase.wantError {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			assert.Contains(t, err.Error(), "more than 20 tokens")
		})
	}
}

func TestDefaultTokenBudgetAdmitsLargeInLists(t *testing.T) {
	t.Parallel()

	engine := New()
	sql := "SELECT 1 WHERE x IN (" + strings.TrimSuffix(strings.Repeat("1, ", 20_000), ", ") + ")"
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
}

func TestTypeParseDepthOptionAppliesToStatementsAndNormalisation(t *testing.T) {
	t.Parallel()

	const nesting = 6
	deepType := strings.Repeat("Array(", nesting) + "UInt8" + strings.Repeat(")", nesting)

	t.Run("default depth accepts the nesting", func(t *testing.T) {
		t.Parallel()

		engine := New()
		assert.Equal(t, sqlmodel.TypeCategoryArray, engine.NormaliseTypeName(deepType).Category)
		mutation, err := applyDDLWith(t, engine, "CREATE TABLE t (c "+deepType+") ENGINE = Memory")
		require.NoError(t, err)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, sqlmodel.TypeCategoryArray, mutation.Columns[0].SQLType.Category)
	})

	t.Run("lowered depth rejects the nesting", func(t *testing.T) {
		t.Parallel()

		engine := New(WithMaxTypeParseDepth(3))
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, engine.NormaliseTypeName(deepType).Category)
		_, err := applyDDLWith(t, engine, "CREATE TABLE t (c "+deepType+") ENGINE = Memory")
		require.ErrorIs(t, err, dialectkit.ErrParseDepth)
	})
}

func applyDDLWith(t *testing.T, engine *Dialect, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return engine.ApplyDDL(t.Context(), statements[0])
}

func TestApplyDDLAndAnalyseQueryRejectStatementsAnotherDialectParsed(t *testing.T) {
	t.Parallel()

	engine := New()
	cases := []struct {
		raw     ast.ParsedStatementRaw
		name    string
		message string
	}{
		{name: "no payload", raw: nil, message: "the statement was parsed by another dialect: <nil>"},
		{
			name:    "another dialect's payload",
			raw:     &foreignStatement{},
			message: "the statement was parsed by another dialect: *clickhouse.foreignStatement",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statement := ast.ParsedStatement{Raw: testCase.raw, Location: 0, Length: 0}

			mutation, err := engine.ApplyDDL(t.Context(), statement)
			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			require.EqualError(t, err, testCase.message)
			assert.Nil(t, mutation)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statement)
			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			require.EqualError(t, err, testCase.message)
			assert.Nil(t, analysis)
		})
	}
}

func TestApplyDDLHonoursCancellation(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id UInt64) ENGINE = Memory")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	cause := errors.New("catalogue build abandoned")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)

	mutation, err := engine.ApplyDDL(ctx, statements[0])
	require.ErrorIs(t, err, cause)
	assert.Nil(t, mutation)
}

func explodingTypeHook(name string, _ []int) *sqlmodel.SQLType {
	if name == "Exploding" {
		panic("type hook failed")
	}
	return nil
}

func newLoggingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func decodeLogRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func parseOneStatement(t *testing.T, engine *Dialect, sql string) ast.ParsedStatement {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return statements[0]
}

func TestEntryPointsReturnAPanicAsAPanicErrorAndLogItOnce(t *testing.T) {
	t.Parallel()

	engine := New(WithTypeNormaliserHook(explodingTypeHook))
	createTable := parseOneStatement(t, engine, "CREATE TABLE t (c Exploding) ENGINE = Memory")
	selectParameter := parseOneStatement(t, engine, "SELECT {p:Exploding}")
	missing := ast.ParsedStatement{Raw: (*parsedStatement)(nil), Location: 0, Length: 0}
	cases := []struct {
		run       func(ctx context.Context) (any, error)
		name      string
		operation string
	}{
		{
			name:      "parse statements with a context that panics",
			operation: "clickhouse: parse statements",
			run: func(ctx context.Context) (any, error) {
				return engine.ParseStatements(panickingContext{Context: ctx}, "SELECT 1")
			},
		},
		{
			name:      "apply DDL with a type normaliser hook that panics",
			operation: "clickhouse: apply DDL",
			run: func(ctx context.Context) (any, error) {
				return engine.ApplyDDL(ctx, createTable)
			},
		},
		{
			name:      "apply DDL with a parser bug",
			operation: "clickhouse: apply DDL",
			run: func(ctx context.Context) (any, error) {
				return engine.ApplyDDL(ctx, missing)
			},
		},
		{
			name:      "analyse query with a type normaliser hook that panics",
			operation: "clickhouse: analyse query",
			run: func(ctx context.Context) (any, error) {
				return engine.AnalyseQuery(ctx, nil, selectParameter)
			},
		},
		{
			name:      "analyse query with a parser bug",
			operation: "clickhouse: analyse query",
			run: func(ctx context.Context) (any, error) {
				return engine.AnalyseQuery(ctx, nil, missing)
			},
		},
		{
			name:      "resolve function call with a context that panics",
			operation: "clickhouse: resolve function call",
			run: func(ctx context.Context) (any, error) {
				return engine.ResolveFunctionCall(panickingContext{Context: ctx}, nil, "plus", "",
					[]sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt8")})
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)
			var (
				result any
				err    error
			)
			require.NotPanics(t, func() { result, err = testCase.run(ctx) })

			assert.Nil(t, result)
			var panicError *dialectkit.PanicError
			require.ErrorAs(t, err, &panicError)
			require.ErrorIs(t, err, fault.ErrPanic)
			assert.Equal(t, testCase.operation, panicError.Operation)
			assert.True(t, strings.HasPrefix(err.Error(), testCase.operation+": panic: "), err.Error())
			assert.NotContains(t, err.Error(), "goroutine")

			records := decodeLogRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, testCase.operation, records[0]["operation"])
			assert.Contains(t, records[0]["stack"], "goroutine")
		})
	}
}

func TestEntryPointsStopWithTheContextsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("analysis abandoned")
	cancelled, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	engine := New()
	createTable := parseOneStatement(t, engine, "CREATE TABLE t (c UInt8) ENGINE = Memory")
	selectOne := parseOneStatement(t, engine, "SELECT 1")
	cases := []struct {
		run  func(ctx context.Context) (any, error)
		name string
	}{
		{
			name: "parse statements",
			run: func(ctx context.Context) (any, error) {
				return engine.ParseStatements(ctx, "SELECT 1")
			},
		},
		{
			name: "apply DDL",
			run: func(ctx context.Context) (any, error) {
				return engine.ApplyDDL(ctx, createTable)
			},
		},
		{
			name: "analyse query",
			run: func(ctx context.Context) (any, error) {
				return engine.AnalyseQuery(ctx, nil, selectOne)
			},
		},
		{
			name: "resolve function call",
			run: func(ctx context.Context) (any, error) {
				return engine.ResolveFunctionCall(ctx, nil, "plus", "",
					[]sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt8")})
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := testCase.run(cancelled)
			require.ErrorIs(t, err, cause)
			assert.Nil(t, result)
		})
	}
}

func cancellingTypeHook(cancel context.CancelCauseFunc, cause error) func(string, []int) *sqlmodel.SQLType {
	return func(name string, _ []int) *sqlmodel.SQLType {
		if name == "Stop" {
			cancel(cause)
		}
		return nil
	}
}

func TestACancelledContextStopsAWalkWithItsCause(t *testing.T) {
	t.Parallel()

	const columns = 5_000
	cases := []struct {
		run  func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error)
		name string
		sql  string
	}{
		{
			name: "analyse query",
			sql:  "SELECT {p:Stop}" + strings.Repeat(", a + 1", columns) + " FROM t",
			run: func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error) {
				return engine.AnalyseQuery(ctx, nil, statement)
			},
		},
		{
			name: "apply DDL",
			sql:  "CREATE TABLE t (c Stop" + strings.Repeat(", d Nullable(UInt8)", columns) + ") ENGINE = Memory",
			run: func(ctx context.Context, engine *Dialect, statement ast.ParsedStatement) (any, error) {
				return engine.ApplyDDL(ctx, statement)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("analysis abandoned")
			ctx, logs := newLoggingContext(t)
			ctx, cancel := context.WithCancelCause(ctx)
			defer cancel(nil)
			engine := New(WithTypeNormaliserHook(cancellingTypeHook(cancel, cause)))
			statement := parseOneStatement(t, engine, testCase.sql)

			result, err := testCase.run(ctx, engine, statement)
			require.ErrorIs(t, err, cause)
			assert.Nil(t, result)
			assert.Empty(t, decodeLogRecords(t, logs))
		})
	}
}

func TestStatementParsersShareTheInterruptTheirEntryPointBuilt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		read func(statementParser *parser)
		name string
	}{
		{name: "the statement parser", read: func(statementParser *parser) { statementParser.Current() }},
		{name: "a child parser", read: func(statementParser *parser) { statementParser.newChildParser(statementParser.Tokens[1:]).Current() }},
		{
			name: "the probe for an asynchronous mutation's body",
			read: func(statementParser *parser) { alterTableAsyncBody(statementParser.Tokens, statementParser.Interrupt) },
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reads := 0
			interrupt := func() error {
				reads++
				return nil
			}
			tokens := tokeniseStatement(t, "ALTER TABLE t UPDATE a = 1 WHERE b = 2")
			statementParser, err := New().newStatementParser(interrupt, &parsedStatement{source: "", tokens: tokens, kind: statementKindAlterTable, overBudget: false})
			require.NoError(t, err)

			testCase.read(statementParser)
			assert.Positive(t, reads)
		})
	}
}

func TestParseStatementsKeepsNoMoreTokensThanTheBudget(t *testing.T) {
	t.Parallel()

	const limit = 20
	rows := strings.TrimSuffix(strings.Repeat("(1), ", 4*limit), ", ")
	testCases := []struct {
		apply      func(t *testing.T, engine *Dialect, statement ast.ParsedStatement) error
		name       string
		sql        string
		overBudget bool
		refused    bool
	}{
		{
			name:       "an INSERT analysed",
			sql:        "INSERT INTO t VALUES " + rows,
			overBudget: true,
			refused:    true,
			apply: func(t *testing.T, engine *Dialect, statement ast.ParsedStatement) error {
				t.Helper()
				_, err := engine.AnalyseQuery(t.Context(), nil, statement)
				return err
			},
		},
		{
			name:       "an INSERT applied as DDL stays a no-op",
			sql:        "INSERT INTO t VALUES " + rows,
			overBudget: true,
			refused:    false,
			apply: func(t *testing.T, engine *Dialect, statement ast.ParsedStatement) error {
				t.Helper()
				mutation, err := engine.ApplyDDL(t.Context(), statement)
				assert.Nil(t, mutation)
				return err
			},
		},
		{
			name:       "a CREATE TABLE applied",
			sql:        "CREATE TABLE t (" + strings.TrimSuffix(strings.Repeat("c UInt8, ", 4*limit), ", ") + ") ENGINE = Memory",
			overBudget: true,
			refused:    true,
			apply: func(t *testing.T, engine *Dialect, statement ast.ParsedStatement) error {
				t.Helper()
				_, err := engine.ApplyDDL(t.Context(), statement)
				return err
			},
		},
		{
			name:       "a statement within the budget",
			sql:        "INSERT INTO t VALUES (1)",
			overBudget: false,
			refused:    false,
			apply: func(t *testing.T, engine *Dialect, statement ast.ParsedStatement) error {
				t.Helper()
				_, err := engine.AnalyseQuery(t.Context(), nil, statement)
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(limit))
			statement := parseOneStatement(t, engine, testCase.sql)
			parsed, ok := statement.Raw.(*parsedStatement)
			require.True(t, ok)
			assert.Equal(t, testCase.overBudget, parsed.overBudget)
			assert.LessOrEqual(t, len(parsed.tokens), limit)
			assert.Equal(t, len(testCase.sql), statement.Length, "the statement keeps its whole extent")

			err := testCase.apply(t, engine, statement)
			if !testCase.refused {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			assert.Contains(t, err.Error(), "more than 20 tokens")
		})
	}
}

func TestParseStatementsOfAFourMebibyteInsertStaysWithinItsBudget(t *testing.T) {
	const scriptBytes = 4 << 20
	row := "(1, 'abc', [1, 2, 3]), "
	sql := "INSERT INTO t VALUES " + strings.Repeat(row, scriptBytes/len(row))

	engine := New()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
		statements, err := engine.ParseStatements(ctx, sql)
		require.NoError(t, err)
		require.Len(t, statements, 1)
		parsed, ok := statements[0].Raw.(*parsedStatement)
		require.True(t, ok)
		assert.True(t, parsed.overBudget)
		assert.Len(t, parsed.tokens, dialectkit.DefaultMaxTokensPerStatement)
	})
	runtime.ReadMemStats(&after)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(200<<20), "tokenising stops at the token budget")
}

func TestParseStatementsChecksTheContextBeforeEachStatement(t *testing.T) {
	t.Parallel()

	var script strings.Builder
	for index := range 50_000 {
		_, _ = fmt.Fprintf(&script, "INSERT INTO t VALUES (%d);\n", index)
	}
	testCases := []struct {
		name    string
		allowed int
	}{
		{name: "done before the first statement", allowed: 1},
		{name: "done after a few statements", allowed: 4},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			checks := 0
			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				checks = 0
				ctx := expiringContext{Context: t.Context(), checks: &checks, allowed: testCase.allowed}
				statements, err := New().ParseStatements(ctx, script.String())
				require.ErrorIs(t, err, context.Canceled)
				assert.Nil(t, statements)
			})
			assert.Equal(t, testCase.allowed+1, checks, "the context is checked once more and no statement after it is read")
		})
	}
}

func TestHooksAreNotRecoveredInsideTheHookChain(t *testing.T) {
	t.Parallel()

	panicking := func() { panic("hook failed") }
	engine := New(
		WithPromoteTypeHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *sqlmodel.SQLType { panicking(); return nil }),
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { panicking(); return nil }),
		WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { panicking(); return nil }),
	)
	testCases := []struct {
		call func()
		name string
	}{
		{name: "PromoteType", call: func() { engine.PromoteType(primitiveType("UInt8"), primitiveType("UInt16")) }},
		{name: "CanImplicitCast", call: func() { engine.CanImplicitCast(primitiveType("UInt8"), primitiveType("UInt16")) }},
		{name: "NormaliseTypeName", call: func() { engine.NormaliseTypeName("UInt8") }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.PanicsWithValue(t, "hook failed", testCase.call, "the panic goes on to the guard where Seagull called the dialect")
		})
	}
}

func TestAPanickingExtraFunctionRegistrationFailsEveryStatement(t *testing.T) {
	t.Parallel()

	registrationErr := errors.New("registration failed")
	testCases := []struct {
		value any
		name  string
	}{
		{name: "a panic with a string", value: "boom"},
		{name: "a panic with an error", value: registrationErr},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var engine *Dialect
			require.NotPanics(t, func() {
				engine = New(
					WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
						builder.Register("before_panic", primitiveType("UInt8"))
					}),
					WithExtraFunctions(func(*FunctionCatalogueBuilder) { panic(testCase.value) }),
					WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
						builder.Register("after_panic", primitiveType("UInt8"))
					}),
				)
			})
			assert.Contains(t, engine.BuiltinFunctions().Functions, "before_panic")
			assert.Contains(t, engine.BuiltinFunctions().Functions, "after_panic", "the registrations after a panic still run")

			_, applyErr := engine.ApplyDDL(t.Context(), parseOneStatement(t, engine, "CREATE TABLE t (c UInt8) ENGINE = Memory"))
			_, analyseErr := engine.AnalyseQuery(t.Context(), nil, parseOneStatement(t, engine, "SELECT 1"))
			for _, err := range []error{applyErr, analyseErr} {
				require.ErrorIs(t, err, fault.ErrPanic)
				var panicError *dialectkit.PanicError
				require.ErrorAs(t, err, &panicError)
				assert.Equal(t, testCase.value, panicError.Value)
				assert.Equal(t, "clickhouse: register extra functions", panicError.Operation)
			}
			if wrapped, isError := testCase.value.(error); isError {
				assert.ErrorIs(t, applyErr, wrapped)
			}
		})
	}
}

func TestInfoReturnsValuesTheCallerOwns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(info *dialect.Info)
		read   func(info dialect.Info) any
		name   string
	}{
		{name: "system columns", change: func(info *dialect.Info) { info.SystemColumns[0] = "changed" }, read: func(info dialect.Info) any { return info.SystemColumns[0] }},
		{name: "filter operators", change: func(info *dialect.Info) { info.Filters.Operators[0] = "changed" }, read: func(info dialect.Info) any { return info.Filters.Operators[0] }},
		{name: "cast forms", change: func(info *dialect.Info) { info.Filters.Casts[0].Before = "changed" }, read: func(info dialect.Info) any { return info.Filters.Casts[0].Before }},
		{name: "cast types", change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" }, read: func(info dialect.Info) any { return info.Filters.CastTypes[0] }},
		{name: "operator features", change: func(info *dialect.Info) { info.OperatorFeatures["changed"] = 0 }, read: func(info dialect.Info) any { _, found := info.OperatorFeatures["changed"]; return found }},
		{name: "placeholder sigils", change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "changed" }, read: func(info dialect.Info) any { return info.Placeholders.Sigils[0].Prefix }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			before := testCase.read(engine.Info())
			testCase.change(new(engine.Info()))
			assert.Equal(t, before, testCase.read(engine.Info()), "changing one Info value changes no other")
			assert.Equal(t, before, testCase.read(New().Info()), "changing one Info value changes no other dialect")
		})
	}
}

func textType(name string) sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, name)
}

func TestExtraTypesReachEveryTypeName(t *testing.T) {
	t.Parallel()

	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"Citext": textType("citext")}),
		WithExtraTypes(map[string]sqlmodel.SQLType{"Gull": textType("gull")}),
	)
	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("citext").Category, "an earlier call survives a later one")
	assert.Equal(t, "gull", engine.NormaliseTypeName("GULL").EngineName, "names match ignoring case")

	nested := engine.NormaliseTypeName("Array(Gull)")
	require.Equal(t, sqlmodel.TypeCategoryArray, nested.Category)
	assert.Equal(t, "gull", nested.ElementType.EngineName)

	mutation, err := applyDDLWith(t, engine, "CREATE TABLE t (id UInt64, name Nullable(Citext)) ENGINE = Memory")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 2)
	assert.Equal(t, "citext", mutation.Columns[1].SQLType.EngineName)
	assert.True(t, mutation.Columns[1].Nullable)

	statements, err := engine.ParseStatements(t.Context(), "SELECT id FROM t WHERE name = {name:Citext}")
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.Len(t, analysis.ParameterReferences, 1)
	assert.Equal(t, "citext", analysis.ParameterReferences[0].CastType.EngineName)

	assert.Equal(t, "citext", engine.BuiltinTypes().Types["citext"].EngineName)
}

func TestTypeNormaliserHooksChainMostRecentFirst(t *testing.T) {
	t.Parallel()

	named := func(match, engineName string) func(string, []int) *sqlmodel.SQLType {
		return func(name string, _ []int) *sqlmodel.SQLType {
			if name != match {
				return nil
			}
			return new(textType(engineName))
		}
	}
	engine := New(
		WithTypeNormaliserHook(named("Point", "first")),
		WithTypeNormaliserHook(named("Point", "second")),
		WithTypeNormaliserHook(named("Ring", "ring")),
		WithTypeNormaliserHook(nil),
	)
	assert.Equal(t, "second", engine.NormaliseTypeName("Point").EngineName)
	assert.Equal(t, "ring", engine.NormaliseTypeName("Ring").EngineName, "a declining hook falls through to the earlier ones")
	assert.Equal(t, "UInt64", engine.NormaliseTypeName("UInt64").EngineName, "the built-ins follow the hooks")
	assert.Equal(t, "ring", engine.NormaliseTypeName("Array(Ring)").ElementType.EngineName, "hooks reach nested names")
}

func TestCastAndPromoteHooksChainMostRecentFirst(t *testing.T) {
	t.Parallel()

	engine := New(
		WithImplicitCastHook(func(sqlmodel.SQLType, sqlmodel.SQLType) *bool { return new(true) }),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryText {
				return new(false)
			}
			return nil
		}),
		WithImplicitCastHook(nil),
		WithPromoteTypeHook(func(left, _ sqlmodel.SQLType) *sqlmodel.SQLType { return &left }),
		WithPromoteTypeHook(func(_, right sqlmodel.SQLType) *sqlmodel.SQLType { return &right }),
		WithPromoteTypeHook(nil),
	)
	assert.False(t, engine.CanImplicitCast(textType("String"), primitiveType("UInt8")))
	assert.True(t, engine.CanImplicitCast(primitiveType("Float64"), primitiveType("UInt8")), "a declining hook falls through")
	assert.Equal(t, "UInt8", engine.PromoteType(primitiveType("UInt64"), primitiveType("UInt8")).EngineName)
}

func TestExtraFunctionsRunInOrderWithTheKitBuilder(t *testing.T) {
	t.Parallel()

	var order []string
	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"Gull": textType("gull")}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			order = append(order, "first")
			builder.NeverNull("ring_gull", builder.Args(dialectkit.Arg{Name: "name", Type: builder.Type("String")}), builder.Type("Gull"))
		}),
		WithExtraFunctions(nil),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			order = append(order, "second")
			builder.Register("ClassifyGull", builder.Type("UInt8"), builder.Type("Gull"))
		}),
	)
	assert.Equal(t, []string{"first", "second"}, order)

	ringGull := engine.BuiltinFunctions().Functions["ring_gull"]
	require.Len(t, ringGull, 1)
	assert.Equal(t, "gull", ringGull[0].ReturnType.EngineName, "the builder's Type resolves extra types")
	assert.Equal(t, sqlmodel.DataAccessReadOnly, ringGull[0].DataAccess)

	classify := engine.BuiltinFunctions().Functions["classifygull"]
	require.Len(t, classify, 1, "Register files a name under its lower-cased form")
	assert.Equal(t, "ClassifyGull", classify[0].Name)
	assert.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, classify[0].NullableBehaviour)
}

func TestParseStatements_EndToEnd(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id UInt64) ENGINE = MergeTree() ORDER BY id; SELECT * FROM t;")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	first, firstOk := statements[0].Raw.(*parsedStatement)
	require.True(t, firstOk)
	assert.Equal(t, statementKindCreateTable, first.kind)

	second, secondOk := statements[1].Raw.(*parsedStatement)
	require.True(t, secondOk)
	assert.Equal(t, statementKindSelect, second.kind)
}

func TestResolvedMaxParseDepthDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialectkit.DefaultMaxParseDepth, settings{}.Limits.ParseDepth())
	require.Equal(t, 32, settings{Limits: dialectkit.Limits{MaxParseDepth: 32, MaxTokensPerStatement: 0}}.Limits.ParseDepth())
}

func TestWithMaxParseDepthIgnoresNonPositive(t *testing.T) {
	t.Parallel()

	configured := settings{}
	WithMaxParseDepth(0)(&configured)
	WithMaxParseDepth(-5)(&configured)
	require.Equal(t, dialectkit.DefaultMaxParseDepth, configured.Limits.ParseDepth())

	WithMaxParseDepth(16)(&configured)
	require.Equal(t, 16, configured.Limits.ParseDepth())

	WithMaxParseDepth(dialectkit.MaxParseDepthCeiling + 1)(&configured)
	require.Equal(t, dialectkit.MaxParseDepthCeiling, configured.Limits.ParseDepth())
}

func TestTableValuedFunctionColumnsGivesTheFixedShapes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		function     string
		wantColumns  []string
		wantEngine   string
		wantNullable bool
	}{
		{name: "numbers", function: "numbers", wantColumns: []string{"number"}, wantEngine: "UInt64"},
		{name: "numbers_mt", function: "numbers_mt", wantColumns: []string{"number"}, wantEngine: "UInt64"},
		{name: "generateSeries", function: "generateSeries", wantColumns: []string{"value"}, wantEngine: "Int64"},
		{name: "generate_series", function: "generate_series", wantColumns: []string{"value"}, wantEngine: "Int64"},
		{name: "input, whose shape the INSERT gives", function: "input"},
		{name: "remote, whose shape the source gives", function: "remote"},
		{name: "an unknown function", function: "no_such_function"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := New().TableValuedFunctionColumns(testCase.function)
			if testCase.wantColumns == nil {
				assert.Empty(t, columns)
				return
			}
			names := make([]string, len(columns))
			for index := range columns {
				names[index] = columns[index].Name
				assert.Equal(t, testCase.wantEngine, columns[index].SQLType.EngineName)
				assert.Equal(t, testCase.wantNullable, columns[index].Nullable)
			}
			assert.Equal(t, testCase.wantColumns, names)
		})
	}
}

func TestTableValuedFunctionColumnsReturnsColumnsTheCallerOwns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(columns []ast.ScopedColumn)
		name   string
	}{
		{name: "renaming a column", change: func(columns []ast.ScopedColumn) { columns[0].Name = "changed" }},
		{name: "retyping a column", change: func(columns []ast.ScopedColumn) { columns[0].SQLType = primitiveType("String") }},
		{name: "making a column nullable", change: func(columns []ast.ScopedColumn) { columns[0].Nullable = true }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			before := engine.TableValuedFunctionColumns("numbers")
			changed := engine.TableValuedFunctionColumns("numbers")
			testCase.change(changed)
			assert.Equal(t, before, engine.TableValuedFunctionColumns("numbers"))
			assert.Equal(t, before, New().TableValuedFunctionColumns("numbers"))
		})
	}
}

func TestPromoteTypeAcrossNumericCategories(t *testing.T) {
	t.Parallel()

	float32Type, float64Type := primitiveType("Float32"), primitiveType("Float64")
	integerType, smallInteger := primitiveType("Int64"), primitiveType("UInt8")
	decimalType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal(18, 4)")
	textType := primitiveType("String")
	testCases := []struct {
		left  sqlmodel.SQLType
		right sqlmodel.SQLType
		want  sqlmodel.SQLType
		name  string
	}{
		{name: "a Float32 beside an integer too wide for it gives a Float64", left: float32Type, right: integerType, want: float64Type},
		{name: "a Float32 beside an integer it holds exactly stays a Float32", left: smallInteger, right: float32Type, want: float32Type},
		{name: "a float absorbs an integer on the left", left: integerType, right: float64Type, want: float64Type},
		{name: "a float absorbs a decimal on the right", left: float64Type, right: decimalType, want: float64Type},
		{name: "a decimal beside a Float32 gives a Float64", left: decimalType, right: float32Type, want: float64Type},
		{name: "a decimal absorbs an integer on the right", left: decimalType, right: smallInteger, want: decimalType},
		{name: "a decimal absorbs an integer on the left", left: integerType, right: decimalType, want: decimalType},
		{name: "a value that is not a number keeps the left operand", left: textType, right: integerType, want: textType},
		{name: "a number with a value that is not keeps the left operand", left: integerType, right: textType, want: integerType},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, New().PromoteType(testCase.left, testCase.right))
		})
	}
}

func TestApplyDDLFindsAMissingSemicolonPastTheTokenBudget(t *testing.T) {
	t.Parallel()

	const limit = 20
	rows := strings.TrimSuffix(strings.Repeat("(1), ", 4*limit), ", ")
	const next = "\nCREATE TABLE b (x UInt8) ENGINE = Memory"
	testCases := []struct {
		name       string
		sql        string
		wantToken  string
		overBudget bool
	}{
		{name: "a seed INSERT that runs on past the budget", sql: "INSERT INTO t VALUES " + rows + next, wantToken: "CREATE", overBudget: true},
		{name: "a seed INSERT that ends where it should", sql: "INSERT INTO t VALUES " + rows, wantToken: "", overBudget: true},
		{name: "a seed INSERT that runs on within the budget", sql: "INSERT INTO t VALUES (1)" + next, wantToken: "CREATE", overBudget: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(limit))
			statement := parseOneStatement(t, engine, testCase.sql)
			parsed, ok := statement.Raw.(*parsedStatement)
			require.True(t, ok)
			require.Equal(t, testCase.overBudget, parsed.overBudget)

			mutation, err := engine.ApplyDDL(t.Context(), statement)
			assert.Nil(t, mutation)
			if testCase.wantToken == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrParse)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, strings.LastIndex(testCase.sql, "\n")+1, parseError.Position)
		})
	}
}

func TestApplyDDLReadsAnOverBudgetStatementInLinearTime(t *testing.T) {
	engine := New(WithMaxTokensPerStatement(20))
	applyRows := func(count int) func() {
		statement := parseOneStatement(t, engine, "INSERT INTO t VALUES "+strings.TrimSuffix(strings.Repeat("(1, 'a'), ", count), ", "))
		return func() {
			_, err := engine.ApplyDDL(t.Context(), statement)
			assert.NoError(t, err)
		}
	}
	scaling.Linear(t, applyRows(20_000), applyRows(80_000))
}
