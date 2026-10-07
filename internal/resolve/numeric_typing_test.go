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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

var (
	bigintType  = sqlType("int8", sqlmodel.TypeCategoryInteger)
	numericType = sqlType("numeric", sqlmodel.TypeCategoryDecimal)
)

type numericTestEngine interface {
	dialect.TypeSystemPort
	TypeRules
}

type preferringEngine struct {
	*fakedialect.Engine
}

func (*preferringEngine) UntypedArgumentType(candidates []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	for _, candidate := range candidates {
		if candidate.Category == sqlmodel.TypeCategoryFloat {
			return candidate, true
		}
	}
	return sqlmodel.SQLType{}, false
}

type coalescingEngine struct {
	*fakedialect.Engine
}

func (*coalescingEngine) CoalesceTypedUpToNonNullable() bool {
	return true
}

func newOverloadResolver(engine numericTestEngine, signatures ...*sqlmodel.Function) *FunctionResolver {
	catalogue := fakedialect.NewCatalogue("public")
	for _, signature := range signatures {
		catalogue.Schemas["public"].Functions[signature.Name] = append(catalogue.Schemas["public"].Functions[signature.Name], signature)
	}
	return NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine)
}

func TestFunctionResolverPrefersAnUntypedArgumentOnlyForAKnownArgumentCount(t *testing.T) {
	t.Parallel()

	overloads := []*sqlmodel.Function{
		functionSignature("abs", floatType, functionArgument("x", floatType)),
		functionSignature("abs", intType, functionArgument("x", intType)),
		functionSignature("abs", numericType, functionArgument("x", numericType)),
	}

	preferring := newOverloadResolver(&preferringEngine{Engine: &fakedialect.Engine{}}, overloads...)
	chosen, settled := preferring.ArgumentTypeAtArity("abs", 0, 1)
	require.True(t, settled, "the engine chooses among overloads of the call's arity")
	assert.Equal(t, "float8", chosen.EngineName)

	_, settled = preferring.ArgumentType("abs", 0)
	assert.False(t, settled, "an unknown arity never consults the engine")

	plain := newOverloadResolver(&fakedialect.Engine{}, overloads...)
	_, settled = plain.ArgumentTypeAtArity("abs", 0, 1)
	assert.False(t, settled, "without the port, disagreeing overloads leave the argument untyped")
}

func TestFunctionResolverTypesAnUntypedArgumentByTheOverloadsTheOthersMatch(t *testing.T) {
	t.Parallel()

	resolver := newOverloadResolver(&fakedialect.Engine{},
		functionSignature("mod", bigintType, functionArgument("y", bigintType), functionArgument("x", bigintType)),
		functionSignature("mod", intType, functionArgument("y", intType), functionArgument("x", intType)),
		functionSignature("mod", numericType, functionArgument("y", numericType), functionArgument("x", numericType)),
	)

	cases := []struct {
		name          string
		argumentTypes []sqlmodel.SQLType
		ordinal       int
		want          string
		settled       bool
	}{
		{name: "an integer beside it picks the integer overload", argumentTypes: []sqlmodel.SQLType{unknownType, intType}, ordinal: 0, want: "integer", settled: true},
		{name: "a bigint before it picks the bigint overload", argumentTypes: []sqlmodel.SQLType{bigintType, unknownType}, ordinal: 1, want: "int8", settled: true},
		{name: "a type no overload takes exactly settles nothing", argumentTypes: []sqlmodel.SQLType{unknownType, textType}, ordinal: 0, settled: false},
		{name: "no typed argument settles nothing", argumentTypes: []sqlmodel.SQLType{unknownType, unknownType}, ordinal: 0, settled: false},
		{name: "a position outside the call settles nothing", argumentTypes: []sqlmodel.SQLType{unknownType, intType}, ordinal: 2, settled: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, settled := resolver.ArgumentTypeAmongExactMatches("mod", testCase.ordinal, testCase.argumentTypes)
			require.Equal(t, testCase.settled, settled)
			if settled {
				assert.Equal(t, testCase.want, got.EngineName)
			}
		})
	}
}

func TestCommonSupertypeMeetsNumbersWhereTheEnginePromotesThemInEitherOrder(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		PromoteTypeFunc: func(left, right sqlmodel.SQLType) sqlmodel.SQLType {
			if right.Category == sqlmodel.TypeCategoryFloat {
				return right
			}
			return left
		},
		CanImplicitCastFunc: func(sqlmodel.SQLType, sqlmodel.SQLType) bool { return true },
	}
	catalogue := fakedialect.NewCatalogue("public")
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)

	assert.Equal(t, "float8", resolver.commonSupertype(numericType, floatType).EngineName)
	assert.Equal(t, "float8", resolver.commonSupertype(floatType, numericType).EngineName,
		"an engine casting numbers both ways no longer hands the result to the right operand")
}

func TestResolveCoalesceExpressionStopsTypingAtTheFirstNonNullableArgumentWhenTheEngineDoes(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
		sqlmodel.Column{Name: "email", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true},
	)
	expression := &ast.CoalesceExpression{Arguments: []ast.Expression{
		&ast.ColumnRefExpression{ColumnName: "id"},
		&ast.ColumnRefExpression{ColumnName: "email"},
	}}
	resolve := func(engine numericTestEngine) sqlmodel.SQLType {
		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, catalogue.Schemas["public"].Tables["users"])
		resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))
		require.NoError(t, err)
		assert.False(t, nullable)
		return sqlType
	}

	assert.Equal(t, "int4", resolve(&coalescingEngine{Engine: &fakedialect.Engine{}}).EngineName,
		"the argument after a non-nullable one takes no part")
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, resolve(&fakedialect.Engine{}).Category,
		"an engine typing every argument finds no common type for an integer and text")
}
