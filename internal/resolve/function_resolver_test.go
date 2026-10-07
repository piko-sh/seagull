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

var (
	intType     = sqlType("integer", sqlmodel.TypeCategoryInteger)
	textType    = sqlType("text", sqlmodel.TypeCategoryText)
	floatType   = sqlType("float8", sqlmodel.TypeCategoryFloat)
	boolType    = sqlType("boolean", sqlmodel.TypeCategoryBoolean)
	unknownType = sqlType("", sqlmodel.TypeCategoryUnknown)
)

type coercingEngine struct {
	*fakedialect.FunctionResolverEngine
	coerce func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool)
}

func (engine *coercingEngine) CoerceArgument(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	return engine.coerce(argument, parameter)
}

func sqlType(engineName string, category sqlmodel.SQLTypeCategory) sqlmodel.SQLType {
	return sqlmodel.SQLType{
		EngineName: engineName,
		Category:   category,
	}
}

func functionArgument(name string, sqlType sqlmodel.SQLType) sqlmodel.FunctionArgument {
	return sqlmodel.FunctionArgument{
		Name: name,
		Type: sqlType,
	}
}

func functionSignature(name string, returnType sqlmodel.SQLType, arguments ...sqlmodel.FunctionArgument) *sqlmodel.Function {
	return &sqlmodel.Function{
		Name:       name,
		ReturnType: returnType,
		Arguments:  arguments,
	}
}

func TestFunctionResolver_ArgumentType(t *testing.T) {
	t.Parallel()

	uuidV4 := sqlType("uuid_v4", sqlmodel.TypeCategoryText)
	uuidV7 := sqlType("uuid_v7", sqlmodel.TypeCategoryText)

	newResolver := func(signatures ...*sqlmodel.Function) *FunctionResolver {
		catalogue := fakedialect.NewCatalogue("content")
		for _, signature := range signatures {
			catalogue.Schemas["content"].Functions[signature.Name] = append(catalogue.Schemas["content"].Functions[signature.Name], signature)
		}
		return NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})
	}

	t.Run("single overload returns the declared argument type by ordinal", func(t *testing.T) {
		t.Parallel()
		signature := functionSignature("get_pages_with_latest_version", intType,
			functionArgument("_environment_id", uuidV4), functionArgument("_time_boundary", uuidV7), functionArgument("_published", boolType))
		signature.Schema = "content"
		resolver := newResolver(signature)

		firstType, firstFound := resolver.ArgumentType("content.get_pages_with_latest_version", 0)
		require.True(t, firstFound)
		assert.Equal(t, "uuid_v4", firstType.EngineName)
		secondType, secondFound := resolver.ArgumentType("content.get_pages_with_latest_version", 1)
		require.True(t, secondFound)
		assert.Equal(t, "uuid_v7", secondType.EngineName)
		thirdType, thirdFound := resolver.ArgumentType("content.get_pages_with_latest_version", 2)
		require.True(t, thirdFound)
		assert.Equal(t, sqlmodel.TypeCategoryBoolean, thirdType.Category)
	})

	t.Run("bare (unqualified) name resolves", func(t *testing.T) {
		t.Parallel()
		resolver := newResolver(functionSignature("string_to_array", intType, functionArgument("input", textType), functionArgument("delim", textType)))
		sqlType, ok := resolver.ArgumentType("string_to_array", 0)
		require.True(t, ok)
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)
	})

	t.Run("ordinal beyond arity for non-variadic declines", func(t *testing.T) {
		t.Parallel()
		resolver := newResolver(functionSignature("f", intType, functionArgument("a", intType)))
		_, ok := resolver.ArgumentType("f", 1)
		assert.False(t, ok)
	})

	t.Run("variadic clamps ordinal to the last declared slot", func(t *testing.T) {
		t.Parallel()
		signature := functionSignature("greatest", intType, functionArgument("first", intType))
		signature.IsVariadic = true
		resolver := newResolver(signature)
		sqlType, ok := resolver.ArgumentType("greatest", 5)
		require.True(t, ok)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
	})

	t.Run("ambiguous overloads disagreeing at the ordinal decline", func(t *testing.T) {
		t.Parallel()
		resolver := newResolver(
			functionSignature("f", intType, functionArgument("a", intType)),
			functionSignature("f", textType, functionArgument("a", textType)),
		)
		_, ok := resolver.ArgumentType("f", 0)
		assert.False(t, ok, "competing overloads with different types at ordinal 0 must decline")
	})

	t.Run("unknown function declines", func(t *testing.T) {
		t.Parallel()
		resolver := newResolver()
		_, ok := resolver.ArgumentType("nope", 0)
		assert.False(t, ok)
	})
}

func TestNewFunctionResolver(t *testing.T) {
	t.Parallel()

	t.Run("merges builtin functions from engine", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"lower": {
					functionSignature("lower", textType, functionArgument("input", textType)),
				},
				"abs": {
					functionSignature("abs", intType, functionArgument("n", intType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		assert.Len(t, resolver.functions["lower"], 1, "expected one overload for lower")
		assert.Len(t, resolver.functions["abs"], 1, "expected one overload for abs")
	})

	t.Run("case-insensitive builtin keys", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"UPPER": {
					functionSignature("UPPER", textType, functionArgument("input", textType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		assert.Len(t, resolver.functions["upper"], 1, "builtin key should be lowercased")
		assert.Empty(t, resolver.functions["UPPER"], "original-cased key should not exist")
	})

	t.Run("catalogue function appends new overload", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"myfunc": {
					functionSignature("myfunc", intType, functionArgument("a", intType)),
				},
			},
		}
		catalogue := fakedialect.NewCatalogue("public")

		catalogue.Schemas["public"].Functions["myfunc"] = []*sqlmodel.Function{
			functionSignature("myfunc", textType, functionArgument("a", textType)),
		}
		engine := &fakedialect.Engine{}

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		require.Len(t, resolver.functions["myfunc"], 2, "expected two overloads for myfunc")
	})

	t.Run("catalogue function overrides builtin with same argument types", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"myfunc": {
					functionSignature("myfunc", intType, functionArgument("a", intType)),
				},
			},
		}

		catalogueSignature := functionSignature("myfunc", textType, functionArgument("a", intType))
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Functions["myfunc"] = []*sqlmodel.Function{catalogueSignature}
		engine := &fakedialect.Engine{}

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "myfunc", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)
		require.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.returnType.Category,
			"catalogue signature should override the builtin")
	})

	t.Run("nil builtins handled gracefully", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Functions["myfunc"] = []*sqlmodel.Function{
			functionSignature("myfunc", intType),
		}
		engine := &fakedialect.Engine{}

		resolver := NewFunctionResolver(nil, catalogue, engine)

		assert.Len(t, resolver.functions["myfunc"], 1, "catalogue functions should still be present")
	})

	t.Run("nil catalogue handled gracefully", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"abs": {
					functionSignature("abs", intType, functionArgument("n", intType)),
				},
			},
		}
		engine := &fakedialect.Engine{}

		resolver := NewFunctionResolver(builtins, nil, engine)

		assert.Len(t, resolver.functions["abs"], 1, "builtin functions should still be present")
	})
}

func TestFunctionResolver_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("unknown function returns Q005 error", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "nonexistent", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, match, "unknown function should not produce a match")
		require.NotNil(t, resolveError, "unknown function should produce an error")
		assert.Equal(t, "Q005", resolveError.Code)
		assert.Contains(t, resolveError.Message, "nonexistent")
	})

	t.Run("variadic function declared with a zero minimum resolves with no arguments", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"json_build_object": {
					sqlmodel.NewFunction(
						[]sqlmodel.FunctionArgument{functionArgument("key", unknownType), functionArgument("value", unknownType)},
						sqlType("json", sqlmodel.TypeCategoryJSON),
						sqlmodel.FunctionNullableNeverNull,
						sqlmodel.WithVariadic(0),
					),
				},
			},
		}
		resolver := NewFunctionResolver(builtins, fakedialect.NewCatalogue("public"), &fakedialect.Engine{})

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "json_build_object", "", nil)
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryJSON, match.returnType.Category)
	})

	t.Run("exact arity match with single overload", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"abs": {
					functionSignature("abs", intType, functionArgument("n", intType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "abs", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError, "should not produce an error")
		require.NotNil(t, match, "should produce a match")
		assert.Equal(t, sqlmodel.TypeCategoryInteger, match.returnType.Category)
		assert.Equal(t, "integer", match.returnType.EngineName)
	})

	t.Run("case-insensitive function name lookup", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"lower": {
					functionSignature("lower", textType, functionArgument("input", textType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "LOWER", "", []sqlmodel.SQLType{textType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.returnType.Category)
	})

	t.Run("best overload selected by score", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"myfunc": {
					functionSignature("myfunc", textType, functionArgument("a", textType)),
					functionSignature("myfunc", intType, functionArgument("a", intType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "myfunc", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, match.returnType.Category,
			"should pick the integer overload as an exact match")
	})

	t.Run("variadic function accepts extra arguments", func(t *testing.T) {
		t.Parallel()

		variadicSignature := functionSignature("concat", textType, functionArgument("a", textType))
		variadicSignature.IsVariadic = true

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"concat": {variadicSignature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "concat", "", []sqlmodel.SQLType{textType, textType, textType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.returnType.Category)
	})

	t.Run("schema filtering selects matching schema", func(t *testing.T) {
		t.Parallel()

		publicSignature := functionSignature("myfunc", intType, functionArgument("a", intType))
		publicSignature.Schema = "public"

		customSignature := functionSignature("myfunc", textType, functionArgument("a", intType))
		customSignature.Schema = "custom"

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"myfunc": {publicSignature, customSignature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "myfunc", "custom", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.returnType.Category,
			"should pick the custom-schema overload")
	})

	t.Run("no arity match returns Q005 error", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"abs": {
					functionSignature("abs", intType, functionArgument("n", intType)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "abs", "", []sqlmodel.SQLType{intType, intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, match, "wrong arity should not produce a match")
		require.NotNil(t, resolveError)
		assert.Equal(t, "Q005", resolveError.Code)
		assert.Contains(t, resolveError.Message, "no matching overload")
	})

	t.Run("zero-argument function resolves correctly", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"now": {
					functionSignature("now", sqlType("timestamptz", sqlmodel.TypeCategoryTemporal)),
				},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "now", "", nil)
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryTemporal, match.returnType.Category)
	})

	t.Run("preserves aggregate and returnsSet flags", func(t *testing.T) {
		t.Parallel()

		aggregateSignature := functionSignature("count", intType, functionArgument("col", intType))
		aggregateSignature.IsAggregate = true
		aggregateSignature.ReturnsSet = false
		aggregateSignature.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
		aggregateSignature.DataAccess = sqlmodel.DataAccessReadOnly

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"count": {aggregateSignature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "count", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.True(t, match.isAggregate, "isAggregate should be propagated")
		assert.False(t, match.returnsSet, "returnsSet should be propagated")
		assert.Equal(t, sqlmodel.FunctionNullableNeverNull, match.nullableBehaviour)
		assert.Equal(t, sqlmodel.DataAccessReadOnly, match.dataAccess)
	})

	t.Run("implicit cast selects compatible overload", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"sqrt": {
					functionSignature("sqrt", floatType, functionArgument("n", floatType)),
				},
			},
		}
		engine := &fakedialect.Engine{
			CanImplicitCastFunc: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat
			},
		}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "sqrt", "", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, match.returnType.Category)
	})

	t.Run("no implicit cast returns Q005 error", func(t *testing.T) {
		t.Parallel()

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"boolcheck": {
					functionSignature("boolcheck", boolType, functionArgument("b", boolType)),
				},
			},
		}
		engine := &fakedialect.Engine{
			CanImplicitCastFunc: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return false
			},
		}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "boolcheck", "", []sqlmodel.SQLType{textType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, match, "incompatible type should not produce a match")
		require.NotNil(t, resolveError)
		assert.Equal(t, "Q005", resolveError.Code)
	})

	t.Run("optional arguments allow fewer args than declared", func(t *testing.T) {
		t.Parallel()

		signature := functionSignature("substr", textType,
			functionArgument("str", textType),
			functionArgument("start", intType),
			functionArgument("length", intType),
		)

		signature.MinArguments = 2

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"substr": {signature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "substr", "", []sqlmodel.SQLType{textType, intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.returnType.Category)
	})

	t.Run("schema-qualified skips mismatched schema candidates", func(t *testing.T) {
		t.Parallel()

		signature := functionSignature("revenue", floatType, functionArgument("amount", floatType))
		signature.Schema = "analytics"

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"revenue": {signature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "revenue", "sales", []sqlmodel.SQLType{floatType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, match)
		require.NotNil(t, resolveError)
		assert.Equal(t, "Q005", resolveError.Code)
	})

	t.Run("empty schema on candidate matches any requested schema", func(t *testing.T) {
		t.Parallel()

		signature := functionSignature("unscoped", intType, functionArgument("n", intType))
		signature.Schema = ""

		builtins := &ast.FunctionCatalogue{
			Functions: map[string][]*sqlmodel.Function{
				"unscoped": {signature},
			},
		}
		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")

		resolver := NewFunctionResolver(builtins, catalogue, engine)

		match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "unscoped", "anyschema", []sqlmodel.SQLType{intType})
		require.NoError(t, interruption)

		require.NoError(t, interruption)

		assert.Nil(t, resolveError)
		require.NotNil(t, match)
	})
}

func TestScoreCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		candidate       *sqlmodel.Function
		argumentTypes   []sqlmodel.SQLType
		canCastFunction func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool
		wantScore       int
		wantExactCount  int
		wantViable      bool
	}{
		{
			name:           "exact match scores 3 per argument",
			candidate:      functionSignature("f", intType, functionArgument("a", intType), functionArgument("b", intType)),
			argumentTypes:  []sqlmodel.SQLType{intType, intType},
			wantScore:      6,
			wantExactCount: 2,
			wantViable:     true,
		},
		{
			name: "same category different engine name scores 2 per argument",
			candidate: functionSignature("f", intType,
				functionArgument("a", sqlType("int4", sqlmodel.TypeCategoryInteger)),
			),
			argumentTypes:  []sqlmodel.SQLType{sqlType("int8", sqlmodel.TypeCategoryInteger)},
			wantScore:      2,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name:          "implicit cast scores 1 per argument",
			candidate:     functionSignature("f", floatType, functionArgument("n", floatType)),
			argumentTypes: []sqlmodel.SQLType{intType},
			canCastFunction: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat
			},
			wantScore:      1,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name:          "no match makes candidate not viable",
			candidate:     functionSignature("f", boolType, functionArgument("b", boolType)),
			argumentTypes: []sqlmodel.SQLType{textType},
			canCastFunction: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return false
			},
			wantScore:      0,
			wantExactCount: 0,
			wantViable:     false,
		},
		{
			name:           "unknown actual type scores 2",
			candidate:      functionSignature("f", intType, functionArgument("a", intType)),
			argumentTypes:  []sqlmodel.SQLType{unknownType},
			wantScore:      2,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name: "unknown expected type scores 2",
			candidate: functionSignature("f", intType,
				functionArgument("a", unknownType),
			),
			argumentTypes:  []sqlmodel.SQLType{intType},
			wantScore:      2,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name: "variadic function with extra args applies penalty",
			candidate: func() *sqlmodel.Function {
				signature := functionSignature("f", textType, functionArgument("a", textType))
				signature.IsVariadic = true
				return signature
			}(),
			argumentTypes:  []sqlmodel.SQLType{textType, textType, textType},
			wantScore:      8,
			wantExactCount: 3,
			wantViable:     true,
		},
		{
			name: "variadic function matching exactly has no penalty",
			candidate: func() *sqlmodel.Function {
				signature := functionSignature("f", textType, functionArgument("a", textType))
				signature.IsVariadic = true
				return signature
			}(),
			argumentTypes:  []sqlmodel.SQLType{textType},
			wantScore:      3,
			wantExactCount: 1,
			wantViable:     true,
		},
		{
			name: "variadic function with a zero minimum accepts no arguments",
			candidate: sqlmodel.NewFunction(
				[]sqlmodel.FunctionArgument{functionArgument("value", unknownType)},
				textType,
				sqlmodel.FunctionNullableNeverNull,
				sqlmodel.WithVariadic(0),
			),
			argumentTypes:  nil,
			wantScore:      1,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name: "variadic function with a minimum of one rejects no arguments",
			candidate: sqlmodel.NewFunction(
				[]sqlmodel.FunctionArgument{functionArgument("value", textType)},
				textType,
				sqlmodel.FunctionNullableNeverNull,
				sqlmodel.WithVariadic(1),
			),
			argumentTypes:  nil,
			wantScore:      0,
			wantExactCount: 0,
			wantViable:     false,
		},
		{
			name: "non-variadic zero minimum derives the required count from the arguments",
			candidate: functionSignature("f", intType,
				functionArgument("a", intType),
				sqlmodel.FunctionArgument{Name: "b", Type: intType, IsOptional: true},
			),
			argumentTypes:  nil,
			wantScore:      0,
			wantExactCount: 0,
			wantViable:     false,
		},
		{
			name:           "zero-argument function scores 1",
			candidate:      functionSignature("f", intType),
			argumentTypes:  nil,
			wantScore:      1,
			wantExactCount: 0,
			wantViable:     true,
		},
		{
			name:           "too few arguments makes candidate not viable",
			candidate:      functionSignature("f", intType, functionArgument("a", intType), functionArgument("b", intType)),
			argumentTypes:  []sqlmodel.SQLType{intType},
			wantScore:      0,
			wantExactCount: 0,
			wantViable:     false,
		},
		{
			name:           "too many arguments for non-variadic makes candidate not viable",
			candidate:      functionSignature("f", intType, functionArgument("a", intType)),
			argumentTypes:  []sqlmodel.SQLType{intType, intType},
			wantScore:      0,
			wantExactCount: 0,
			wantViable:     false,
		},
		{
			name: "mixed scores across multiple arguments",
			candidate: functionSignature("f", intType,
				functionArgument("a", intType),
				functionArgument("b", sqlType("int4", sqlmodel.TypeCategoryInteger)),
			),
			argumentTypes: []sqlmodel.SQLType{
				intType,
				sqlType("int8", sqlmodel.TypeCategoryInteger),
			},
			wantScore:      5,
			wantExactCount: 1,
			wantViable:     true,
		},
		{
			name: "MinArguments allows fewer args than declared",
			candidate: func() *sqlmodel.Function {
				signature := functionSignature("f", textType,
					functionArgument("a", textType),
					functionArgument("b", intType),
					functionArgument("c", intType),
				)
				signature.MinArguments = 1
				return signature
			}(),
			argumentTypes:  []sqlmodel.SQLType{textType, intType},
			wantScore:      6,
			wantExactCount: 2,
			wantViable:     true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				CanImplicitCastFunc: testCase.canCastFunction,
			}
			resolver := &FunctionResolver{
				functions: make(map[string][]functionCandidate),
				engine:    engine,
			}

			arguments := make([]callArgument, len(testCase.argumentTypes))
			for index, argumentType := range testCase.argumentTypes {
				arguments[index] = callArgument{name: "", sqlType: argumentType, nullable: false, literal: false}
			}
			totalScore, exactCount, viable := resolver.scoreCandidate(testCase.candidate, arguments)

			assert.Equal(t, testCase.wantViable, viable, "viable mismatch")
			if viable {
				assert.Equal(t, testCase.wantScore, totalScore, "totalScore mismatch")
				assert.Equal(t, testCase.wantExactCount, exactCount, "exactCount mismatch")
			}
		})
	}
}

func TestMergeOrAppendSignature(t *testing.T) {
	t.Parallel()

	t.Run("appends new signature with different argument types", func(t *testing.T) {
		t.Parallel()

		existing := []functionCandidate{
			{function: functionSignature("myfunc", intType, functionArgument("a", intType)), schema: "public"},
		}
		newSignature := functionCandidate{function: functionSignature("myfunc", textType, functionArgument("a", textType)), schema: "public"}

		result := mergeOrAppendSignature(existing, newSignature)

		require.Len(t, result, 2, "new signature with different args should be appended")
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result[0].function.ReturnType.Category)
		assert.Equal(t, sqlmodel.TypeCategoryText, result[1].function.ReturnType.Category)
	})

	t.Run("replaces existing signature with matching argument types", func(t *testing.T) {
		t.Parallel()

		existing := []functionCandidate{
			{function: functionSignature("myfunc", intType, functionArgument("a", intType)), schema: "public"},
		}

		replacement := functionCandidate{function: functionSignature("myfunc", textType, functionArgument("a", intType)), schema: "public"}

		result := mergeOrAppendSignature(existing, replacement)

		require.Len(t, result, 1, "matching signature should be replaced, not appended")
		assert.Equal(t, sqlmodel.TypeCategoryText, result[0].function.ReturnType.Category,
			"return type should reflect the replacement")
	})

	t.Run("appends to empty list", func(t *testing.T) {
		t.Parallel()

		var existing []functionCandidate
		newSignature := functionCandidate{function: functionSignature("myfunc", intType, functionArgument("a", intType)), schema: "public"}

		result := mergeOrAppendSignature(existing, newSignature)

		require.Len(t, result, 1)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result[0].function.ReturnType.Category)
	})

	t.Run("replaces correct overload among multiple", func(t *testing.T) {
		t.Parallel()

		existing := []functionCandidate{
			{function: functionSignature("myfunc", intType, functionArgument("a", intType)), schema: "public"},
			{function: functionSignature("myfunc", textType, functionArgument("a", textType)), schema: "public"},
			{function: functionSignature("myfunc", floatType, functionArgument("a", floatType)), schema: "public"},
		}

		replacement := functionCandidate{function: functionSignature("myfunc", boolType, functionArgument("a", textType)), schema: "public"}

		result := mergeOrAppendSignature(existing, replacement)

		require.Len(t, result, 3, "should replace, not append")
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result[0].function.ReturnType.Category, "first overload unchanged")
		assert.Equal(t, sqlmodel.TypeCategoryBoolean, result[1].function.ReturnType.Category, "second overload replaced")
		assert.Equal(t, sqlmodel.TypeCategoryFloat, result[2].function.ReturnType.Category, "third overload unchanged")
	})

	t.Run("different arity is treated as different signature", func(t *testing.T) {
		t.Parallel()

		existing := []functionCandidate{
			{function: functionSignature("myfunc", intType, functionArgument("a", intType)), schema: "public"},
		}

		twoArgumentSignature := functionCandidate{function: functionSignature("myfunc", intType, functionArgument("a", intType), functionArgument("b", intType)), schema: "public"}

		result := mergeOrAppendSignature(existing, twoArgumentSignature)

		require.Len(t, result, 2, "different arity should be treated as a distinct signature")
	})

	t.Run("a matching signature in another schema is kept apart", func(t *testing.T) {
		t.Parallel()

		existing := []functionCandidate{{function: functionSignature("label", intType, functionArgument("a", intType)), schema: "public"}}
		other := functionCandidate{function: functionSignature("label", textType, functionArgument("a", intType)), schema: "audit"}

		result := mergeOrAppendSignature(existing, other)

		require.Len(t, result, 2, "same-named functions in two schemas must not replace each other")
		assert.Equal(t, "public", result[0].schema)
		assert.Equal(t, "audit", result[1].schema)
	})
}

func TestScoreArgument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		expected        sqlmodel.SQLType
		actual          sqlmodel.SQLType
		canCastFunction func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool
		wantScore       int
	}{
		{
			name:      "exact match by category and engine name",
			expected:  intType,
			actual:    intType,
			wantScore: 3,
		},
		{
			name:      "same category different engine name",
			expected:  sqlType("int4", sqlmodel.TypeCategoryInteger),
			actual:    sqlType("int8", sqlmodel.TypeCategoryInteger),
			wantScore: 2,
		},
		{
			name:     "implicit cast available",
			expected: floatType,
			actual:   intType,
			canCastFunction: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat
			},
			wantScore: 1,
		},
		{
			name:     "no match returns zero",
			expected: boolType,
			actual:   textType,
			canCastFunction: func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
				return false
			},
			wantScore: 0,
		},
		{
			name:      "unknown actual type scores 2",
			expected:  intType,
			actual:    unknownType,
			wantScore: 2,
		},
		{
			name:      "unknown expected type scores 2",
			expected:  unknownType,
			actual:    intType,
			wantScore: 2,
		},
		{
			name:      "named opaque expected type rejects a known actual type",
			expected:  sqlType("statssummary1d", sqlmodel.TypeCategoryUnknown),
			actual:    sqlType("int8", sqlmodel.TypeCategoryInteger),
			wantScore: 0,
		},
		{
			name:      "named opaque expected type accepts an unknown actual type",
			expected:  sqlType("statssummary1d", sqlmodel.TypeCategoryUnknown),
			actual:    unknownType,
			wantScore: 2,
		},
		{
			name:      "both unknown scores 2 via actual-unknown branch",
			expected:  unknownType,
			actual:    unknownType,
			wantScore: 2,
		},
		{
			name:      "case-insensitive engine name comparison for exact match",
			expected:  sqlType("INTEGER", sqlmodel.TypeCategoryInteger),
			actual:    sqlType("integer", sqlmodel.TypeCategoryInteger),
			wantScore: 3,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				CanImplicitCastFunc: testCase.canCastFunction,
			}
			resolver := &FunctionResolver{
				functions: make(map[string][]functionCandidate),
				engine:    engine,
			}

			score := resolver.scoreArgument(testCase.expected, callArgument{name: "", sqlType: testCase.actual, nullable: false, literal: false})

			assert.Equal(t, testCase.wantScore, score)
		})
	}
}

func TestFunctionResolver_EngineResolverFallback(t *testing.T) {
	t.Parallel()

	jsonType := sqlType("json", sqlmodel.TypeCategoryJSON)

	testCases := []struct {
		resolveFunction func(ctx context.Context, name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error)
		name            string
		wantDiagnostic  string
		wantDataAccess  sqlmodel.FunctionDataAccess
		wantMatch       bool
	}{
		{
			name: "undeclared data access defaults to read-only with a warning",
			resolveFunction: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return &ast.FunctionResolution{ReturnType: jsonType, DataAccess: sqlmodel.DataAccessUnknown}, nil
			},
			wantMatch:      true,
			wantDataAccess: sqlmodel.DataAccessReadOnly,
			wantDiagnostic: diag.CodeFunctionDataAccessUndeclared,
		},
		{
			name: "declared data access is kept without a warning",
			resolveFunction: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return &ast.FunctionResolution{ReturnType: jsonType, DataAccess: sqlmodel.DataAccessModifiesData}, nil
			},
			wantMatch:      true,
			wantDataAccess: sqlmodel.DataAccessModifiesData,
		},
		{
			name: "engine resolver failure falls through to the unknown function diagnostic",
			resolveFunction: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return nil, errors.New("not resolvable")
			},
			wantMatch:      false,
			wantDiagnostic: diag.CodeUnknownFunction,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: testCase.resolveFunction}
			builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{}}
			resolver := NewFunctionResolver(builtins, fakedialect.NewCatalogue("public"), engine)

			match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "engine_only", "", []sqlmodel.SQLType{intType})
			require.NoError(t, interruption)

			require.NoError(t, interruption)

			diagnostic := resolveError
			if testCase.wantMatch {
				require.NotNil(t, match)
				require.Nil(t, resolveError)
				assert.Equal(t, testCase.wantDataAccess, match.dataAccess)
				assert.Equal(t, jsonType, match.returnType)
				diagnostic = match.notice
			} else {
				assert.Nil(t, match)
			}
			if testCase.wantDiagnostic == "" {
				assert.Nil(t, diagnostic)
				return
			}
			require.NotNil(t, diagnostic)
			assert.Equal(t, testCase.wantDiagnostic, diagnostic.Code)
			assert.Equal(t, diag.SeverityWarning, diagnostic.Severity)
		})
	}
}

func twoSchemaFunctionCatalogue(searchPath ...string) *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.SearchPath = searchPath
	publicLabel := functionSignature("label", intType, functionArgument("value", intType))
	publicLabel.Schema = "public"
	catalogue.Schemas["public"].Functions["label"] = []*sqlmodel.Function{publicLabel}
	auditLabel := functionSignature("label", textType, functionArgument("value", intType))
	auditLabel.Schema = "audit"
	catalogue.Schemas["audit"] = &sqlmodel.Schema{
		Name:      "audit",
		Functions: map[string][]*sqlmodel.Function{"label": {auditLabel}},
	}
	return catalogue
}

func TestFunctionResolverKeysFunctionsBySchema(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		searchPath []string
		schema     string
		wantType   sqlmodel.SQLType
	}{
		{name: "an unqualified call follows the search path", searchPath: []string{"public"}, schema: "", wantType: intType},
		{name: "the first schema on the path wins", searchPath: []string{"audit", "public"}, schema: "", wantType: textType},
		{name: "a qualified call reaches its own schema", searchPath: []string{"public"}, schema: "audit", wantType: textType},
		{name: "a qualified call reaches the path's schema", searchPath: []string{"audit", "public"}, schema: "public", wantType: intType},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, twoSchemaFunctionCatalogue(testCase.searchPath...), &fakedialect.Engine{})
			match, resolveError, interruption := resolveTypedCall(t.Context(), resolver, "label", testCase.schema, []sqlmodel.SQLType{intType})
			require.NoError(t, interruption)
			require.Nil(t, resolveError)
			require.NotNil(t, match)
			require.Equal(t, testCase.wantType, match.returnType)
		})
	}
}

func TestFunctionResolverArgumentTypeHonoursTheSchema(t *testing.T) {
	t.Parallel()

	catalogue := twoSchemaFunctionCatalogue("public")
	auditLabel := catalogue.Schemas["audit"].Functions["label"][0]
	auditLabel.Arguments[0].Type = textType
	resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})

	argumentType, ok := resolver.ArgumentType("label", 0)
	require.True(t, ok)
	require.Equal(t, intType, argumentType)

	argumentType, ok = resolver.ArgumentType("audit.label", 0)
	require.True(t, ok)
	require.Equal(t, textType, argumentType)
}

func TestFunctionResolverArgumentAcceptsNull(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	refresh := functionSignature("refresh", unknownType, functionArgument("view", textType), functionArgument("start", intType))
	refresh.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	strict := functionSignature("strict_square", intType, functionArgument("value", intType))
	strict.NullableBehaviour = sqlmodel.FunctionNullableReturnsNullOnNull
	called := functionSignature("called", intType, functionArgument("value", intType))
	called.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	joined := functionSignature("join_with", textType, functionArgument("separator", textType), functionArgument("part", textType))
	joined.NullableBehaviour = sqlmodel.FunctionNullableNullWhenArgumentNull
	joined.NullingArgument = 0
	joined.IsVariadic = true
	for _, function := range []*sqlmodel.Function{refresh, strict, called, joined} {
		catalogue.Schemas["public"].Functions[function.Name] = []*sqlmodel.Function{function}
	}
	resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})

	require.True(t, resolver.ArgumentAcceptsNull("refresh", 1), "a routine that returns a value whatever its inputs takes NULL as a value")
	require.False(t, resolver.ArgumentAcceptsNull("strict_square", 0), "a strict routine turns NULL into NULL")
	require.False(t, resolver.ArgumentAcceptsNull("called", 0), "a routine that does not say keeps the conservative non-null")
	require.False(t, resolver.ArgumentAcceptsNull("join_with", 0), "the argument whose NULL nulls the result")
	require.True(t, resolver.ArgumentAcceptsNull("join_with", 3), "any other argument of such a routine")
	require.False(t, resolver.ArgumentAcceptsNull("missing", 0))
}

func TestFunctionResolverMatchesNamedArguments(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	note := functionArgument("note", textType)
	note.IsOptional = true
	scale := functionSignature("scale_price", intType, functionArgument("amount", intType), functionArgument("factor", floatType), note)
	byValue := functionSignature("describe", intType, functionArgument("value", intType))
	byLabel := functionSignature("describe", textType, functionArgument("label", textType))
	catalogue.Schemas["public"].Functions["scale_price"] = []*sqlmodel.Function{scale}
	catalogue.Schemas["public"].Functions["describe"] = []*sqlmodel.Function{byValue, byLabel}
	resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})

	testCases := []struct {
		name      string
		function  string
		arguments []callArgument
		wantType  *sqlmodel.SQLType
	}{
		{
			name:      "named arguments in another order",
			function:  "scale_price",
			arguments: []callArgument{{name: "factor", sqlType: floatType}, {name: "amount", sqlType: intType}},
			wantType:  &intType,
		},
		{
			name:      "positional then named",
			function:  "scale_price",
			arguments: []callArgument{{name: "", sqlType: intType}, {name: "FACTOR", sqlType: floatType}},
			wantType:  &intType,
		},
		{
			name:      "the name picks the overload",
			function:  "describe",
			arguments: []callArgument{{name: "label", sqlType: unknownType}},
			wantType:  &textType,
		},
		{
			name:      "an unknown name matches nothing",
			function:  "scale_price",
			arguments: []callArgument{{name: "amount", sqlType: intType}, {name: "colour", sqlType: textType}},
			wantType:  nil,
		},
		{
			name:      "a missing required argument matches nothing",
			function:  "scale_price",
			arguments: []callArgument{{name: "factor", sqlType: floatType}},
			wantType:  nil,
		},
		{
			name:      "an argument passed twice matches nothing",
			function:  "scale_price",
			arguments: []callArgument{{name: "", sqlType: intType}, {name: "amount", sqlType: intType}},
			wantType:  nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			match, resolveError, interruption := resolver.resolveCall(t.Context(), testCase.function, "", testCase.arguments)
			require.NoError(t, interruption)
			if testCase.wantType == nil {
				require.Nil(t, match)
				require.NotNil(t, resolveError)
				require.Equal(t, diag.CodeUnknownFunction, resolveError.Code)
				return
			}
			require.Nil(t, resolveError)
			require.Equal(t, *testCase.wantType, match.returnType)
		})
	}

	argumentType, ok := resolver.NamedArgumentType("scale_price", "factor")
	require.True(t, ok)
	require.Equal(t, floatType, argumentType)
	_, ok = resolver.NamedArgumentType("describe", "missing")
	require.False(t, ok)
}

func TestFunctionResolverReturnTypeFromArgument(t *testing.T) {
	t.Parallel()

	wildcard := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	timestamp := sqlmodel.NewSQLType(sqlmodel.TypeCategoryTemporal, "timestamptz")
	first := sqlmodel.NewFunction(
		[]sqlmodel.FunctionArgument{{Name: "value", Type: wildcard}, {Name: "time", Type: timestamp}},
		wildcard,
		sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithFunctionName("first"),
		sqlmodel.WithReturnTypeFromArgument(1),
	)
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{"first": {first}}}
	resolver := NewFunctionResolver(builtins, fakedialect.NewCatalogue("public"), &fakedialect.Engine{})

	float := sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "float8")
	match, err, interruption := resolver.resolveCall(t.Context(), "first", "", []callArgument{{sqlType: float}, {sqlType: timestamp}})
	require.NoError(t, interruption)
	require.Nil(t, err)
	assert.Equal(t, float, match.returnType)

	unknown, err, interruption := resolver.resolveCall(t.Context(), "first", "", []callArgument{{sqlType: wildcard}, {sqlType: timestamp}})
	require.NoError(t, interruption)
	require.Nil(t, err)
	assert.Equal(t, wildcard, unknown.returnType, "an argument of unknown type leaves the declared return type")
}

func TestResolveCallTellsTheDialectWhichArgumentsMayBeNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		arguments []callArgument
		want      []bool
	}{
		{
			name:      "an argument that is never NULL",
			arguments: []callArgument{{name: "", sqlType: intType, nullable: false}},
			want:      []bool{false},
		},
		{
			name:      "an argument that may be NULL",
			arguments: []callArgument{{name: "", sqlType: intType, nullable: true}},
			want:      []bool{true},
		},
		{
			name: "a type that already holds NULL stays so",
			arguments: []callArgument{
				{name: "", sqlType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger, Nullable: true}, nullable: false},
				{name: "", sqlType: intType, nullable: false},
			},
			want: []bool{true, false},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var seen []bool
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(_ context.Context, _ string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				for _, argumentType := range argumentTypes {
					seen = append(seen, argumentType.Nullable)
				}
				return &ast.FunctionResolution{ReturnType: intType, DataAccess: sqlmodel.DataAccessReadOnly}, nil
			}}
			resolver := NewFunctionResolver(&ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{}}, fakedialect.NewCatalogue("public"), engine)

			match, resolveError, interruption := resolver.resolveCall(t.Context(), "engine_only", "", testCase.arguments)
			require.NoError(t, interruption)

			require.Nil(t, resolveError)
			require.NotNil(t, match)
			require.Equal(t, testCase.want, seen)
			require.False(t, match.returnType.Nullable)
		})
	}
}

func TestFunctionResolverConsultsTheArgumentCoercer(t *testing.T) {
	t.Parallel()

	dateType := sqlType("date", sqlmodel.TypeCategoryTemporal)
	textToDate := func(mayBeNull bool) func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
		return func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
			if argument.Type.Category == sqlmodel.TypeCategoryText && parameter.Category == sqlmodel.TypeCategoryTemporal {
				return dialect.ArgumentCoercion{MayBeNull: mayBeNull}, true
			}
			return dialect.ArgumentCoercion{}, false
		}
	}
	literalToDate := func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
		return dialect.ArgumentCoercion{MayBeNull: false}, argument.Literal && parameter.Category == sqlmodel.TypeCategoryTemporal
	}
	declines := func(dialect.CallArgument, sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
		return dialect.ArgumentCoercion{}, false
	}
	year := functionSignature("year", intType, functionArgument("value", dateType))
	year.NullableBehaviour = sqlmodel.FunctionNullableReturnsNullOnNull
	shift := functionSignature("shift", unknownType, functionArgument("value", dateType))
	stamp := functionSignature("stamp", intType, functionArgument("at", dateType))
	stamp.NullableBehaviour = sqlmodel.FunctionNullableReturnsNullOnNull

	testCases := []struct {
		coerce       func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool)
		implicitCast func(from, to sqlmodel.SQLType) bool
		name         string
		function     string
		arguments    []callArgument
		wantCode     string
		wantReturn   sqlmodel.SQLType
		wantNullable sqlmodel.FunctionNullableBehaviour
	}{
		{
			name:      "a string passed to a date parameter matches nothing when the engine does not convert it",
			coerce:    declines,
			function:  "year",
			arguments: []callArgument{{name: "", sqlType: textType, nullable: false}},
			wantCode:  diag.CodeUnknownFunction,
		},
		{
			name:         "a string the engine converts to a date matches the date parameter",
			coerce:       textToDate(false),
			function:     "year",
			arguments:    []callArgument{{name: "", sqlType: textType, nullable: false}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:         "a conversion that gives NULL for a string holding no date makes the call nullable",
			coerce:       textToDate(true),
			function:     "year",
			arguments:    []callArgument{{name: "", sqlType: textType, nullable: false}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name:         "an argument of the parameter's own category is never converted",
			coerce:       textToDate(true),
			function:     "year",
			arguments:    []callArgument{{name: "", sqlType: dateType, nullable: false}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:         "an argument the engine casts implicitly is not converted",
			coerce:       textToDate(true),
			implicitCast: func(from, to sqlmodel.SQLType) bool { return from.Category == sqlmodel.TypeCategoryText },
			function:     "year",
			arguments:    []callArgument{{name: "", sqlType: textType, nullable: false}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:         "the dialect's own typing of a call with a converted argument may be NULL too",
			coerce:       textToDate(true),
			function:     "shift",
			arguments:    []callArgument{{name: "", sqlType: textType, nullable: false}},
			wantReturn:   textType,
			wantNullable: sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name:         "a named argument the engine converts makes the call nullable",
			coerce:       textToDate(true),
			function:     "stamp",
			arguments:    []callArgument{{name: "at", sqlType: textType, nullable: false}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name:         "a literal the engine reads as the parameter's type matches the parameter",
			coerce:       literalToDate,
			function:     "year",
			arguments:    []callArgument{{name: "", sqlType: textType, nullable: false, literal: true}},
			wantReturn:   intType,
			wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name:      "a computed value of the literal's type is not read as the parameter's type",
			coerce:    literalToDate,
			function:  "year",
			arguments: []callArgument{{name: "", sqlType: textType, nullable: false, literal: false}},
			wantCode:  diag.CodeUnknownFunction,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &coercingEngine{
				FunctionResolverEngine: &fakedialect.FunctionResolverEngine{
					Engine: &fakedialect.Engine{CanImplicitCastFunc: testCase.implicitCast},
					ResolveFunc: func(_ context.Context, name string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
						if name != "shift" {
							return nil, nil
						}
						return &ast.FunctionResolution{
							ReturnType:        textType,
							NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
							DataAccess:        sqlmodel.DataAccessReadOnly,
						}, nil
					},
				},
				coerce: testCase.coerce,
			}
			builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
				"year": {year}, "shift": {shift}, "stamp": {stamp},
			}}
			resolver := NewFunctionResolver(builtins, fakedialect.NewCatalogue("public"), engine)

			match, resolveError, interruption := resolver.resolveCall(t.Context(), testCase.function, "", testCase.arguments)
			require.NoError(t, interruption)

			if testCase.wantCode != "" {
				require.Nil(t, match)
				require.NotNil(t, resolveError)
				require.Equal(t, testCase.wantCode, resolveError.Code)
				return
			}
			require.Nil(t, resolveError)
			require.NotNil(t, match)
			require.Equal(t, testCase.wantReturn, match.returnType)
			require.Equal(t, testCase.wantNullable, match.nullableBehaviour)
		})
	}
}

func TestTheArgumentCountOfACallPicksTheOverloadThatTypesAnArgument(t *testing.T) {
	t.Parallel()

	single := functionSignature("advisory_lock", boolType, functionArgument("key", sqlType("int8", sqlmodel.TypeCategoryInteger)))
	pair := functionSignature("advisory_lock", boolType, functionArgument("key1", intType), functionArgument("key2", intType))
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Functions["advisory_lock"] = []*sqlmodel.Function{single, pair}
	resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})

	testCases := []struct {
		name     string
		ordinal  int
		arity    int
		wantType string
		wantName string
		resolved bool
	}{
		{name: "one argument picks the overload taking one", ordinal: 0, arity: 1, wantType: "int8", wantName: "key", resolved: true},
		{name: "two arguments pick the overload taking two", ordinal: 1, arity: 2, wantType: intType.EngineName, wantName: "key2", resolved: true},
		{name: "three arguments pick none", ordinal: 0, arity: 3, wantType: "", wantName: "", resolved: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			argumentType, typed := resolver.ArgumentTypeAtArity("advisory_lock", testCase.ordinal, testCase.arity)
			argumentName, named := resolver.ArgumentNameAtArity("advisory_lock", testCase.ordinal, testCase.arity)

			require.Equal(t, testCase.resolved, typed)
			require.Equal(t, testCase.resolved, named)
			require.Equal(t, testCase.wantType, argumentType.EngineName)
			require.Equal(t, testCase.wantName, argumentName)
		})
	}

	_, typed := resolver.ArgumentType("advisory_lock", 0)
	require.False(t, typed, "without the argument count the two overloads disagree")
}

func TestEngineResolverFailuresAreSortedIntoProblemsAndInterruptions(t *testing.T) {
	t.Parallel()

	panicked := fmt.Errorf("resolve function call: %w: index out of range", fault.ErrPanic)
	testCases := []struct {
		failure         error
		wantInterrupted error
		name            string
		wantProblem     string
		wantSeverity    diag.ErrorSeverity
		declared        bool
		cancelled       bool
		wantMatch       bool
	}{
		{
			name:         "a panic the dialect recovered is the call's problem",
			failure:      panicked,
			wantProblem:  diag.CodeRecoveredPanic,
			wantSeverity: diag.SeverityError,
		},
		{
			name:         "a panic typing a declared overload keeps the overload's typing and notes the panic",
			failure:      panicked,
			declared:     true,
			wantMatch:    true,
			wantProblem:  diag.CodeRecoveredPanic,
			wantSeverity: diag.SeverityError,
		},
		{
			name:            "a cancelled context the dialect reports stops the resolution",
			failure:         fmt.Errorf("walk: %w", context.Canceled),
			wantInterrupted: context.Canceled,
		},
		{
			name:            "a deadline the dialect ran out of stops the resolution",
			failure:         fmt.Errorf("walk: %w", context.DeadlineExceeded),
			wantInterrupted: context.DeadlineExceeded,
		},
		{
			name:            "any failure once the context is cancelled stops the resolution",
			failure:         errors.New("not resolvable"),
			cancelled:       true,
			wantInterrupted: context.Canceled,
		},
		{
			name:         "any other failure leaves the function unknown",
			failure:      errors.New("not resolvable"),
			wantProblem:  diag.CodeUnknownFunction,
			wantSeverity: diag.SeverityWarning,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if testCase.cancelled {
				cancel()
			}
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return nil, testCase.failure
			}}
			builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{}}
			if testCase.declared {
				builtins.Functions["engine_only"] = []*sqlmodel.Function{functionSignature("engine_only", unknownType, functionArgument("value", intType))}
			}
			resolver := NewFunctionResolver(builtins, fakedialect.NewCatalogue("public"), engine)

			match, problem, interruption := resolveTypedCall(ctx, resolver, "engine_only", "", []sqlmodel.SQLType{intType})

			if testCase.wantInterrupted != nil {
				require.ErrorIs(t, interruption, testCase.wantInterrupted)
				assert.Nil(t, match)
				assert.Nil(t, problem)
				return
			}
			require.NoError(t, interruption)
			if testCase.wantMatch {
				require.NotNil(t, match)
				require.Nil(t, problem)
				assert.Equal(t, unknownType, match.returnType)
				problem = match.notice
			} else {
				assert.Nil(t, match)
			}
			require.NotNil(t, problem)
			assert.Equal(t, testCase.wantProblem, problem.Code)
			assert.Equal(t, testCase.wantSeverity, problem.Severity)
			assert.Contains(t, problem.Message, "engine_only")
		})
	}
}

func TestEngineResolverReceivesTheCallersContext(t *testing.T) {
	t.Parallel()

	type marker struct{}
	ctx := context.WithValue(t.Context(), marker{}, "caller")
	var seen []any
	engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(ctx context.Context, _ string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		seen = append(seen, ctx.Value(marker{}))
		return nil, nil
	}}
	resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, fakedialect.NewCatalogue("public"), engine)

	_, _, interruption := resolveTypedCall(ctx, resolver, "engine_only", "", nil)

	require.NoError(t, interruption)
	require.Equal(t, []any{"caller"}, seen)
}

func resolveTypedCall(
	ctx context.Context,
	resolver *FunctionResolver,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (*FunctionMatch, *Error, error) {
	arguments := make([]callArgument, len(argumentTypes))
	for index := range argumentTypes {
		arguments[index] = callArgument{name: "", sqlType: argumentTypes[index], nullable: false, literal: false}
	}
	return resolver.resolveCall(ctx, name, schema, arguments)
}

func TestFunctionResolverReachesACreatedFunctionOnlyByItsKey(t *testing.T) {
	t.Parallel()

	foldsToLower := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}
	keepsCase := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false}
	ignoresCase := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
		"lower": {functionSignature("lower", textType, functionArgument("input", textType))},
	}}

	testCases := []struct {
		identifiers sqlmodel.IdentifierCase
		wantType    sqlmodel.SQLType
		name        string
		called      string
		functions   []*sqlmodel.Function
		argument    sqlmodel.SQLType
		wantUnknown bool
	}{
		{
			name: "an unquoted call reaches the unquoted function beside a quoted one", identifiers: foldsToLower,
			functions: []*sqlmodel.Function{functionSignature("Foo", intType, functionArgument("a", intType)), functionSignature("foo", textType, functionArgument("a", intType))},
			called:    "foo", argument: intType, wantType: textType,
		},
		{
			name: "a quoted call reaches the quoted function beside an unquoted one", identifiers: foldsToLower,
			functions: []*sqlmodel.Function{functionSignature("Foo", intType, functionArgument("a", intType)), functionSignature("foo", textType, functionArgument("a", intType))},
			called:    "Foo", argument: intType, wantType: intType,
		},
		{
			name: "a quoted call in another case reaches neither", identifiers: foldsToLower,
			functions: []*sqlmodel.Function{functionSignature("Foo", intType, functionArgument("a", intType)), functionSignature("foo", textType, functionArgument("a", intType))},
			called:    "FOO", argument: intType, wantUnknown: true,
		},
		{
			name: "a name kept as written reaches the function of that case", identifiers: keepsCase,
			functions: []*sqlmodel.Function{functionSignature("MyFunc", intType, functionArgument("a", intType))},
			called:    "MyFunc", argument: intType, wantType: intType,
		},
		{
			name: "a name kept as written does not reach a function of another case", identifiers: keepsCase,
			functions: []*sqlmodel.Function{functionSignature("MyFunc", intType, functionArgument("a", intType))},
			called:    "myfunc", argument: intType, wantUnknown: true,
		},
		{
			name: "a case-insensitive dialect reaches the function in any case", identifiers: ignoresCase,
			functions: []*sqlmodel.Function{functionSignature("MyFunc", intType, functionArgument("a", intType))},
			called:    "MYFUNC", argument: intType, wantType: intType,
		},
		{
			name: "a built-in is reached in any case in a case-sensitive dialect", identifiers: foldsToLower,
			functions: nil, called: "LOWER", argument: textType, wantType: textType,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Identifiers = testCase.identifiers
			functions := catalogue.Schemas["public"].Functions
			for _, function := range testCase.functions {
				key := testCase.identifiers.Key(function.Name)
				functions[key] = append(functions[key], function)
			}
			resolver := NewFunctionResolver(builtins, catalogue, &fakedialect.Engine{})

			match, problem, interruption := resolveTypedCall(t.Context(), resolver, testCase.called, "", []sqlmodel.SQLType{testCase.argument})

			require.NoError(t, interruption)
			if testCase.wantUnknown {
				require.NotNil(t, problem)
				assert.Equal(t, diag.CodeUnknownFunction, problem.Code)
				return
			}
			require.Nil(t, problem)
			require.NotNil(t, match)
			assert.Equal(t, testCase.wantType.EngineName, match.returnType.EngineName)
		})
	}
}

func TestFunctionResolverReachesAQualifiedFunctionOnlyThroughItsSchemaKey(t *testing.T) {
	t.Parallel()

	foldsToLower := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}
	ignoresCase := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}

	testCases := []struct {
		identifiers sqlmodel.IdentifierCase
		name        string
		schemaName  string
		qualifier   string
		wantUnknown bool
	}{
		{
			name: "a qualifier of the schema's own case reaches it", identifiers: foldsToLower,
			schemaName: "Audit", qualifier: "Audit", wantUnknown: false,
		},
		{
			name: "a qualifier differing only in case does not reach a case-sensitive schema", identifiers: foldsToLower,
			schemaName: "Audit", qualifier: "audit", wantUnknown: true,
		},
		{
			name: "a qualifier in upper case does not reach a lower-case schema", identifiers: foldsToLower,
			schemaName: "audit", qualifier: "AUDIT", wantUnknown: true,
		},
		{
			name: "a case-insensitive dialect reaches the schema in any case", identifiers: ignoresCase,
			schemaName: "Audit", qualifier: "AUDIT", wantUnknown: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Identifiers = testCase.identifiers
			label := functionSignature("label", textType, functionArgument("value", intType))
			label.Schema = testCase.schemaName
			catalogue.Schemas[testCase.identifiers.Key(testCase.schemaName)] = &sqlmodel.Schema{
				Name:      testCase.schemaName,
				Functions: map[string][]*sqlmodel.Function{"label": {label}},
			}
			resolver := NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, &fakedialect.Engine{})

			match, problem, interruption := resolveTypedCall(t.Context(), resolver, "label", testCase.qualifier, []sqlmodel.SQLType{intType})

			require.NoError(t, interruption)
			if testCase.wantUnknown {
				require.NotNil(t, problem)
				assert.Equal(t, diag.CodeUnknownFunction, problem.Code)
				return
			}
			require.Nil(t, problem)
			require.NotNil(t, match)
			assert.Equal(t, textType.EngineName, match.returnType.EngineName)
		})
	}
}

func TestFunctionResolverTypesAnArgumentFromTheFunctionItsCallReaches(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Identifiers = sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}
	catalogue.Schemas["public"].Functions = map[string][]*sqlmodel.Function{
		"Scale": {functionSignature("Scale", floatType, functionArgument("factor", floatType))},
		"scale": {functionSignature("scale", intType, functionArgument("factor", intType))},
	}
	resolver := NewFunctionResolver(nil, catalogue, &fakedialect.Engine{})

	testCases := []struct {
		name     string
		called   string
		wantType sqlmodel.SQLType
	}{
		{name: "the quoted function types its argument", called: "Scale", wantType: floatType},
		{name: "the unquoted function types its argument", called: "scale", wantType: intType},
		{name: "a qualified call types its argument from the function of its key", called: "public.Scale", wantType: floatType},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			argumentType, resolved := resolver.ArgumentType(testCase.called, 0)

			require.True(t, resolved)
			assert.Equal(t, testCase.wantType.EngineName, argumentType.EngineName)
		})
	}
}
