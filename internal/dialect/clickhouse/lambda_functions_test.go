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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type expiringContext struct {
	context.Context
	checks  *int
	allowed int
}

func (c expiringContext) Err() error {
	*c.checks++
	if *c.checks > c.allowed {
		return context.Canceled
	}
	return nil
}

func (expiringContext) Value(any) any {
	return nil
}

func lambdaCatalogue(t *testing.T, definitions map[string]string) *sqlmodel.Catalogue {
	t.Helper()
	return lambdaCatalogueWith(t, New(), definitions)
}

func lambdaCatalogueWith(t *testing.T, engine *Dialect, definitions map[string]string) *sqlmodel.Catalogue {
	t.Helper()
	schema := &sqlmodel.Schema{Functions: map[string][]*sqlmodel.Function{}}
	identifiers := engine.Info().Identifiers
	for name, source := range definitions {
		mutation, err := applyDDLWith(t, engine, "CREATE FUNCTION "+name+" AS "+source)
		require.NoError(t, err)
		require.NotNil(t, mutation)
		schema.Functions[identifiers.Key(name)] = []*sqlmodel.Function{mutation.Function.Function}
	}
	return &sqlmodel.Catalogue{
		Schemas:       map[string]*sqlmodel.Schema{"default": schema},
		DefaultSchema: "default",
		SearchPath:    []string{"default"},
		Identifiers:   identifiers,
	}
}

func addRawLambda(catalogue *sqlmodel.Catalogue, name string, parameters []string, body string) {
	signature := sqlmodel.NewFunction(lambdaArguments(parameters), unknownType(), sqlmodel.FunctionNullableReturnsNullOnNull,
		sqlmodel.WithFunctionName(name))
	signature.Language = lambdaFunctionLanguage
	signature.BodySQL = body
	catalogue.Schemas["default"].Functions[catalogue.Key(name)] = []*sqlmodel.Function{signature}
}

func TestCreateFunctionRecordsAReadOnlyLambda(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION add_xy ON CLUSTER main AS (x, y) -> x + y")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	signature := mutation.Function.Function
	assert.Equal(t, sqlmodel.DataAccessReadOnly, signature.DataAccess)
	assert.Equal(t, lambdaFunctionLanguage, signature.Language)
	assert.Equal(t, "( x , y ) -> x + y", signature.BodySQL)
	require.Len(t, signature.Arguments, 2)
	assert.Equal(t, "x", signature.Arguments[0].Name)
	assert.Equal(t, []string{"x", "y"}, mutation.Function.BodyParameters)
}

func TestLambdaCallsAreTypedFromTheirArguments(t *testing.T) {
	t.Parallel()

	catalogue := lambdaCatalogue(t, map[string]string{
		"times_two":  "x -> x * 2",
		"add_xy":     "(x, y) -> x + y",
		"shout":      "s -> upper(s)",
		"quadruple":  "x -> times_two(times_two(x))",
		"mystery":    "x -> somethingUnknown(x)",
		"comparison": "x -> x > 1",
		"fallback":   "(x, y) -> coalesce(x, y)",
		"negated":    "x -> -x",
		"converted":  "x -> CAST(x AS Float32)",
		"qualified":  "x -> t.x",
	})
	addRawLambda(catalogue, "recurse", []string{"x"}, "x -> recurse(x)")
	resolver := newCatalogueFunctionResolver(New().BuiltinFunctions())
	testCases := []struct {
		name      string
		function  string
		want      string
		arguments []sqlmodel.SQLType
	}{
		{name: "UInt8 times UInt8 widens", function: "times_two", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "UInt16"},
		{name: "a column argument", function: "times_two", arguments: []sqlmodel.SQLType{primitiveType("UInt32")}, want: "UInt64"},
		{name: "two parameters", function: "add_xy", arguments: []sqlmodel.SQLType{primitiveType("Int32"), primitiveType("Float64")}, want: "Float64"},
		{name: "a built-in call", function: "shout", arguments: []sqlmodel.SQLType{primitiveType("String")}, want: "String"},
		{name: "a lambda calling a lambda", function: "quadruple", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "UInt32"},
		{name: "a comparison is boolean", function: "comparison", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "Bool"},
		{name: "coalesce unifies", function: "fallback", arguments: []sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt32")}, want: "UInt32"},
		{name: "negation", function: "negated", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "Int16"},
		{name: "a cast", function: "converted", arguments: []sqlmodel.SQLType{primitiveType("UInt8")}, want: "Float32"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			resolution, err := resolver.ResolveFunctionCall(t.Context(), catalogue, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			assert.Equal(t, sqlmodel.DataAccessReadOnly, resolution.DataAccess)
		})
	}

	for _, function := range []string{"recurse", "mystery", "qualified"} {
		_, err := resolver.ResolveFunctionCall(t.Context(), catalogue, function, "", []sqlmodel.SQLType{primitiveType("UInt8")})
		assert.ErrorIs(t, err, errResolverNoOpinion, function)
	}
	_, err := resolver.ResolveFunctionCall(t.Context(), catalogue, "times_two", "", []sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt8")})
	assert.ErrorIs(t, err, errResolverNoOpinion, "a call with the wrong number of arguments is not the lambda")
	_, err = resolver.ResolveFunctionCall(t.Context(), nil, "times_two", "", []sqlmodel.SQLType{primitiveType("UInt8")})
	assert.ErrorIs(t, err, errResolverNoOpinion, "without a catalogue no lambda is known")
}

func TestLambdaCallsReachTheFunctionOfTheirCase(t *testing.T) {
	t.Parallel()

	catalogue := lambdaCatalogue(t, map[string]string{
		"Doubled": "x -> x * 2",
		"doubled": "x -> toString(x)",
	})
	resolver := newCatalogueFunctionResolver(New().BuiltinFunctions())

	testCases := []struct {
		name     string
		function string
		want     string
	}{
		{name: "the mixed-case function", function: "Doubled", want: "UInt16"},
		{name: "the lower-case function", function: "doubled", want: "String"},
		{name: "a name in another case reaches neither", function: "DOUBLED", want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolver.ResolveFunctionCall(t.Context(), catalogue, testCase.function, "",
				[]sqlmodel.SQLType{primitiveType("UInt8")})

			if testCase.want == "" {
				require.ErrorIs(t, err, errResolverNoOpinion)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, resolution.ReturnType.EngineName)
		})
	}
}

func TestParseLambdaSourceRejectsWhatIsNotALambda(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()
	for _, source := range []string{"", "x", "x -> ", "x -> x +", "'unterminated"} {
		_, _, ok := resolver.parseLambdaSource(t.Context(), source)
		assert.False(t, ok, source)
	}
	parameters, body, ok := resolver.parseLambdaSource(t.Context(), "(a, b) -> a")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, parameters)
	assert.NotNil(t, body)
}

func TestCreateFunctionRefusesALambdaThatCallsItself(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		position int
		refused  bool
	}{
		{name: "a direct call", sql: "CREATE FUNCTION f AS x -> f(x) + 1", position: 26, refused: true},
		{name: "a call inside a nested lambda", sql: "CREATE FUNCTION f AS a -> arrayMap(x -> f(x), a)", position: 40, refused: true},
		{name: "a call of another function", sql: "CREATE FUNCTION f AS x -> g(x) + 1", position: 0, refused: false},
		{name: "a call of a name differing in case", sql: "CREATE FUNCTION f AS x -> F(x)", position: 0, refused: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			if !testCase.refused {
				require.NoError(t, err)
				require.NotNil(t, mutation)
				return
			}
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, "f", parseError.Token)
			assert.Equal(t, testCase.position, parseError.Position)
			assert.Contains(t, parseError.Message, "cannot call itself")
		})
	}
}

func TestLambdaTypingStopsRecursionAndTypesEachCallOnce(t *testing.T) {
	t.Parallel()

	const chainLength = 10
	fanOut := map[string]string{fmt.Sprintf("f%d", chainLength): "x -> x * 2"}
	for index := range chainLength {
		next := fmt.Sprintf("f%d(x)", index+1)
		fanOut[fmt.Sprintf("f%d", index)] = "x -> " + strings.Join([]string{next, next, next, next}, " + ")
	}
	testCases := []struct {
		build       func(t *testing.T) *sqlmodel.Catalogue
		name        string
		function    string
		evaluations int
		typed       bool
	}{
		{
			name: "a function calling itself four times",
			build: func(t *testing.T) *sqlmodel.Catalogue {
				t.Helper()
				catalogue := lambdaCatalogue(t, map[string]string{})
				addRawLambda(catalogue, "f", []string{"x"}, "x -> f(x) + f(x) + f(x) + f(x)")
				return catalogue
			},
			function:    "f",
			evaluations: 1,
			typed:       false,
		},
		{
			name: "two functions calling each other",
			build: func(t *testing.T) *sqlmodel.Catalogue {
				t.Helper()
				return lambdaCatalogue(t, map[string]string{"f": "x -> g(x) + g(x)", "g": "x -> f(x) * f(x)"})
			},
			function:    "f",
			evaluations: 2,
			typed:       false,
		},
		{
			name: "a chain of functions each calling the next four times",
			build: func(t *testing.T) *sqlmodel.Catalogue {
				t.Helper()
				return lambdaCatalogue(t, fanOut)
			},
			function:    "f0",
			evaluations: chainLength + 1,
			typed:       true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := testCase.build(t)
			engine := New()
			arguments := []sqlmodel.SQLType{primitiveType("UInt8")}
			scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
				resolution, err := engine.ResolveFunctionCall(ctx, catalogue, testCase.function, "", arguments)
				if !testCase.typed {
					assert.ErrorIs(t, err, errResolverNoOpinion)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
			})

			typing := newLambdaTyping()
			_, _ = engine.functionResolver.resolveCall(t.Context(), typing, catalogue, testCase.function, arguments)
			assert.Equal(t, testCase.evaluations, typing.evaluations, "each function is typed once for its argument types")
		})
	}
}

func TestLambdaTypingGivesUpWithNoOpinionPastItsBudget(t *testing.T) {
	t.Parallel()

	catalogue := lambdaCatalogue(t, map[string]string{"times_two": "x -> x * 2"})
	testCases := []struct {
		exhaust func(typing *lambdaTyping)
		name    string
	}{
		{name: "every body evaluation spent", exhaust: func(typing *lambdaTyping) { typing.evaluations = maxLambdaEvaluations }},
		{name: "every typed node spent", exhaust: func(typing *lambdaTyping) { typing.typedNodes = maxLambdaTypedNodes }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			typing := newLambdaTyping()
			testCase.exhaust(typing)
			resolution, err := New().functionResolver.resolveCall(t.Context(), typing, catalogue, "times_two",
				[]sqlmodel.SQLType{primitiveType("UInt8")})
			require.ErrorIs(t, err, errResolverNoOpinion)
			assert.Nil(t, resolution)
			assert.True(t, typing.exhausted)
		})
	}
}

func TestLambdaTypingStopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	catalogue := lambdaCatalogue(t, map[string]string{
		"first":  "x -> second(x) + 1",
		"second": "x -> third(x) + 1",
		"third":  "x -> x + 1",
	})
	cause := errors.New("generation abandoned")
	testCases := []struct {
		contextFor func(t *testing.T) context.Context
		want       error
		name       string
	}{
		{
			name: "a context cancelled before the call",
			contextFor: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancelCause(t.Context())
				cancel(cause)
				return ctx
			},
			want: cause,
		},
		{
			name: "a context cancelled between two bodies",
			contextFor: func(t *testing.T) context.Context {
				t.Helper()
				return expiringContext{Context: t.Context(), checks: new(0), allowed: 2}
			},
			want: context.Canceled,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(testCase.contextFor(t), catalogue, "first", "",
				[]sqlmodel.SQLType{primitiveType("UInt8")})
			require.ErrorIs(t, err, testCase.want)
			assert.Nil(t, resolution)
		})
	}
}

func TestLambdaTypingUsesTheDialectsTypesHooksAndLimits(t *testing.T) {
	t.Parallel()

	money := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Money")
	shadeHook := func(name string, _ []int) *sqlmodel.SQLType {
		if name == "Shade" {
			return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "Shade"))
		}
		return nil
	}
	deepBody := "x -> " + strings.Repeat("(", 8) + "x + 1" + strings.Repeat(")", 8)
	testCases := []struct {
		engine     *Dialect
		name       string
		source     string
		wantEngine string
	}{
		{name: "an extra type", engine: New(WithExtraTypes(map[string]sqlmodel.SQLType{"Money": money})), source: "x -> CAST(x AS Money)", wantEngine: "Money"},
		{name: "a type normaliser hook", engine: New(WithTypeNormaliserHook(shadeHook)), source: "x -> x::Shade", wantEngine: "Shade"},
		{name: "a body within the parse depth", engine: New(WithMaxParseDepth(16)), source: deepBody, wantEngine: "UInt16"},
		{name: "a body nested deeper than the parse depth", engine: New(WithMaxParseDepth(4)), source: deepBody, wantEngine: ""},
		{name: "a body over the token budget", engine: New(WithMaxTokensPerStatement(4)), source: "x -> x + 1 + 1", wantEngine: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := lambdaCatalogue(t, map[string]string{})
			addRawLambda(catalogue, "convert", []string{"x"}, testCase.source)
			resolution, err := testCase.engine.ResolveFunctionCall(t.Context(), catalogue, "convert", "",
				[]sqlmodel.SQLType{primitiveType("UInt8")})
			if testCase.wantEngine == "" {
				require.ErrorIs(t, err, errResolverNoOpinion)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantEngine, resolution.ReturnType.EngineName)

			_, err = New().ResolveFunctionCall(t.Context(), catalogue, "convert", "", []sqlmodel.SQLType{primitiveType("UInt8")})
			if testCase.wantEngine != "UInt16" {
				require.ErrorIs(t, err, errResolverNoOpinion, "a dialect without the type cannot type the call")
			}
		})
	}
}

func TestLambdaCallKeyTellsArgumentTypesApart(t *testing.T) {
	t.Parallel()

	function := &sqlmodel.Function{}
	precision := 10
	decimal := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal")
	decimal.Precision, decimal.Scale = new(precision), new(2)
	widerDecimal := decimal
	widerDecimal.Precision = new(12)
	sameDecimal := decimal
	sameDecimal.Precision = new(precision)
	testCases := []struct {
		name  string
		left  []sqlmodel.SQLType
		right []sqlmodel.SQLType
		same  bool
	}{
		{name: "the same primitive", left: []sqlmodel.SQLType{primitiveType("UInt8")}, right: []sqlmodel.SQLType{primitiveType("UInt8")}, same: true},
		{name: "two primitives", left: []sqlmodel.SQLType{primitiveType("UInt8")}, right: []sqlmodel.SQLType{primitiveType("UInt16")}, same: false},
		{name: "arrays of different elements", left: []sqlmodel.SQLType{arrayOf(primitiveType("UInt8"))}, right: []sqlmodel.SQLType{arrayOf(primitiveType("String"))}, same: false},
		{name: "decimals of different precision", left: []sqlmodel.SQLType{decimal}, right: []sqlmodel.SQLType{widerDecimal}, same: false},
		{name: "decimals of equal precision held apart", left: []sqlmodel.SQLType{decimal}, right: []sqlmodel.SQLType{sameDecimal}, same: true},
		{name: "enum labels that join alike", left: []sqlmodel.SQLType{normaliseBuiltinTypeName("Enum8('a b' = 1)")}, right: []sqlmodel.SQLType{normaliseBuiltinTypeName("Enum8('a' = 1, 'b' = 2)")}, same: false},
		{name: "a different number of arguments", left: []sqlmodel.SQLType{primitiveType("UInt8")}, right: []sqlmodel.SQLType{primitiveType("UInt8"), primitiveType("UInt8")}, same: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			left, leftKeyed := lambdaCallKey(function, testCase.left)
			right, rightKeyed := lambdaCallKey(function, testCase.right)
			require.True(t, leftKeyed)
			require.True(t, rightKeyed)
			assert.Equal(t, testCase.same, left == right)
		})
	}

	deep := primitiveType("UInt8")
	for range sqlmodel.MaxTypeDepth {
		deep = arrayOf(deep)
	}
	_, keyed := lambdaCallKey(function, []sqlmodel.SQLType{deep})
	assert.False(t, keyed, "a type nested past the type depth is typed without being remembered")
}
