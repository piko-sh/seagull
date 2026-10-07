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

package dialectkit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func registerSpec(t *testing.T, source string) map[string][]*sqlmodel.Function {
	t.Helper()

	spec, err := dialectkit.ParseFunctionSpec(source)
	require.NoError(t, err)
	builder := dialectkit.NewCatalogueBuilder()
	spec.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, spelling)
		},
		DefaultNullability: func(string, bool) sqlmodel.FunctionNullableBehaviour {
			return sqlmodel.FunctionNullableReturnsNullOnNull
		},
		Add: builder.Add,
	})
	return builder.Catalogue.Functions
}

func TestFunctionSpecDecodesSignatures(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want   func(t *testing.T, signature *sqlmodel.Function)
		name   string
		source string
	}{
		{
			name:   "named and unnamed arguments",
			source: "round(x decimal, integer) decimal",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				require.Len(t, signature.Arguments, 2)
				assert.Equal(t, "x", signature.Arguments[0].Name)
				assert.Equal(t, "decimal", signature.Arguments[0].Type.EngineName)
				assert.Empty(t, signature.Arguments[1].Name)
				assert.Equal(t, "integer", signature.Arguments[1].Type.EngineName)
				assert.Equal(t, "decimal", signature.ReturnType.EngineName)
			},
		},
		{
			name:   "no arguments",
			source: "pi() double never-null",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.Empty(t, signature.Arguments)
				assert.Equal(t, sqlmodel.FunctionNullableNeverNull, signature.NullableBehaviour)
			},
		},
		{
			name:   "optional and not-null markers",
			source: "refresh(view relation!, start timestamp?, finish timestamp?!) void",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				require.Len(t, signature.Arguments, 3)
				assert.True(t, signature.Arguments[0].NotNull)
				assert.False(t, signature.Arguments[0].IsOptional)
				assert.True(t, signature.Arguments[1].IsOptional)
				assert.False(t, signature.Arguments[1].NotNull)
				assert.True(t, signature.Arguments[2].IsOptional)
				assert.True(t, signature.Arguments[2].NotNull)
			},
		},
		{
			name:   "spellings holding commas and spaces in brackets",
			source: "pick(m map(text, integer), d decimal(18, 3), l integer[]) pair(low integer, high integer)",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				require.Len(t, signature.Arguments, 3)
				assert.Equal(t, "map(text, integer)", signature.Arguments[0].Type.EngineName)
				assert.Equal(t, "decimal(18, 3)", signature.Arguments[1].Type.EngineName)
				assert.Equal(t, "integer[]", signature.Arguments[2].Type.EngineName)
				assert.Equal(t, "pair(low integer, high integer)", signature.ReturnType.EngineName)
			},
		},
		{
			name:   "quoted spelling",
			source: "label(e enum('a, b' = 1)) text",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				require.Len(t, signature.Arguments, 1)
				assert.Equal(t, "enum('a, b' = 1)", signature.Arguments[0].Type.EngineName)
			},
		},
		{
			name:   "default nullability applies when the line names none",
			source: "abs(x decimal) decimal",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, signature.NullableBehaviour)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, signature.DataAccess)
			},
		},
		{
			name:   "called-on-null",
			source: "coalesce(value any) any called-on-null variadic=1",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, signature.NullableBehaviour)
				assert.True(t, signature.IsVariadic)
				assert.Equal(t, 1, signature.MinArguments)
			},
		},
		{
			name:   "nulling argument",
			source: "concat_ws(separator text, value any) text nulling-argument=0 variadic=2",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.Equal(t, sqlmodel.FunctionNullableNullWhenArgumentNull, signature.NullableBehaviour)
				assert.Zero(t, signature.NullingArgument)
				assert.True(t, signature.IsVariadic)
				assert.Equal(t, 2, signature.MinArguments)
			},
		},
		{
			name:   "aggregate returning its argument type",
			source: "first(value any, time timestamp) any aggregate return-type-from=1",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.True(t, signature.IsAggregate)
				assert.Equal(t, 1, signature.ReturnTypeFromArgument)
			},
		},
		{
			name:   "minimum arguments, sets, data access and schema",
			source: "show_parts(relation relation, older_than any) relation never-null min-arguments=1 returns-set modifies-data schema=extension",
			want: func(t *testing.T, signature *sqlmodel.Function) {
				assert.False(t, signature.IsVariadic)
				assert.Equal(t, 1, signature.MinArguments)
				assert.True(t, signature.ReturnsSet)
				assert.Equal(t, sqlmodel.DataAccessModifiesData, signature.DataAccess)
				assert.Equal(t, "extension", signature.Schema)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			functions := registerSpec(t, testCase.source)
			require.Len(t, functions, 1)
			for name, overloads := range functions {
				require.Len(t, overloads, 1)
				assert.Equal(t, name, overloads[0].Name)
				testCase.want(t, overloads[0])
			}
		})
	}
}

func TestFunctionSpecKeepsOverloadsInLineOrder(t *testing.T) {
	t.Parallel()

	functions := registerSpec(t, `
# Rounding.
round(x decimal) decimal

round(x decimal, places integer) decimal
	# An indented comment.
abs(x decimal) decimal
`)

	require.Len(t, functions["round"], 2)
	assert.Len(t, functions["round"][0].Arguments, 1)
	assert.Len(t, functions["round"][1].Arguments, 2)
	assert.Len(t, functions["abs"], 1)
}

func TestFunctionSpecReportsMalformedLines(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		line    string
		message string
	}{
		{name: "no argument list", line: "abs decimal", message: "expected a function name"},
		{name: "no name", line: "(x decimal) decimal", message: "expected a function name"},
		{name: "name with a space", line: "my func(x decimal) decimal", message: "holds a space"},
		{name: "unbalanced argument list", line: "abs(x decimal decimal", message: "unbalanced"},
		{name: "stray closing bracket", line: "abs(x decimal)) decimal", message: "unbalanced"},
		{name: "unclosed quote", line: "abs(x enum('a = 1)) decimal", message: "unbalanced"},
		{name: "no return type", line: "abs(x decimal)", message: "missing return type"},
		{name: "unknown flag", line: "abs(x decimal) decimal deterministic", message: "unknown flag"},
		{name: "switch with a value", line: "abs(x decimal) decimal aggregate=1", message: "unknown flag"},
		{name: "repeated NULL behaviour", line: "abs(x decimal) decimal never-null called-on-null", message: "repeats"},
		{name: "NULL behaviour with a value", line: "abs(x decimal) decimal never-null=1", message: "takes no value"},
		{name: "negative count", line: "f(x decimal) decimal variadic=-1", message: "non-negative"},
		{name: "count without a value", line: "f(x decimal) decimal min-arguments", message: "non-negative"},
		{name: "schema without a name", line: "f(x decimal) decimal schema=", message: "schema name"},
		{name: "nulling argument out of range", line: "f(x decimal) decimal nulling-argument=1", message: "names no argument"},
		{name: "return type source out of range", line: "f(x decimal) decimal return-type-from=2", message: "names no argument"},
		{name: "argument with three words", line: "f(x double precision) decimal", message: "not a type with an optional name"},
		{name: "empty argument", line: "f(x decimal, ) decimal", message: "not a type with an optional name"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec, err := dialectkit.ParseFunctionSpec("good(x decimal) decimal\n" + testCase.line + "\n")

			require.Error(t, err)
			assert.Contains(t, err.Error(), "function spec line 2")
			assert.Contains(t, err.Error(), testCase.message)
			require.NotNil(t, spec)
			builder := dialectkit.NewCatalogueBuilder()
			spec.Register(dialectkit.FunctionSpecHooks{
				ResolveType:        builder.Type,
				DefaultNullability: nil,
				Add:                builder.Add,
			})
			assert.Len(t, builder.Catalogue.Functions, 1, "the lines that decode are still registered")
		})
	}
}

func TestFunctionSpecRegistersFreshSignaturesEachTime(t *testing.T) {
	t.Parallel()

	spec, err := dialectkit.ParseFunctionSpec("lower(x text) text\nupper(x text) text\n")
	require.NoError(t, err)

	var resolved []string
	register := func() map[string][]*sqlmodel.Function {
		builder := dialectkit.NewCatalogueBuilder()
		spec.Register(dialectkit.FunctionSpecHooks{
			ResolveType: func(spelling string) sqlmodel.SQLType {
				resolved = append(resolved, spelling)
				return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, strings.ToUpper(spelling))
			},
			DefaultNullability: nil,
			Add:                builder.Add,
		})
		return builder.Catalogue.Functions
	}

	first := register()
	first["lower"][0].Arguments[0].Name = "changed"
	second := register()

	assert.Equal(t, "x", second["lower"][0].Arguments[0].Name)
	assert.Equal(t, []string{"text", "text"}, resolved, "each spelling resolves once per registration")
	assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, second["upper"][0].NullableBehaviour,
		"without a default the zero behaviour stands")
}

func TestEmbeddedFunctionSpecDecodesOnceAndReportsTheDecodeError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		source         string
		wantErr        string
		wantSignatures int
	}{
		{name: "a spec that decodes", source: "abs(x decimal) decimal\nlower(x text) text\n", wantErr: "", wantSignatures: 2},
		{name: "a spec with a bad line", source: "abs(x decimal) decimal\nbroken\n",
			wantErr: "decoding embedded function spec: function spec line 2", wantSignatures: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			load := dialectkit.EmbeddedFunctionSpec(testCase.source)
			first, firstErr := load()
			second, secondErr := load()

			require.NotNil(t, first)
			assert.Same(t, first, second, "the spec is decoded once")
			assert.Equal(t, firstErr, secondErr)
			if testCase.wantErr == "" {
				require.NoError(t, firstErr)
			} else {
				require.ErrorContains(t, firstErr, testCase.wantErr)
			}
			builder := dialectkit.NewCatalogueBuilder()
			first.Register(dialectkit.FunctionSpecHooks{ResolveType: builder.Type, DefaultNullability: nil, Add: builder.Add})
			assert.Len(t, builder.Catalogue.Functions, testCase.wantSignatures)
		})
	}
}

func TestFunctionSpecRegisterIsSafeOnANilSpecAndANilSignature(t *testing.T) {
	t.Parallel()

	spec, err := dialectkit.ParseFunctionSpec("f(x decimal) decimal modifies-data schema=extra\n")
	require.NoError(t, err)
	cases := []struct {
		spec *dialectkit.FunctionSpec
		name string
	}{
		{name: "a nil spec", spec: nil},
		{name: "a hook that files nothing", spec: spec},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			added := 0
			require.NotPanics(t, func() {
				testCase.spec.Register(dialectkit.FunctionSpecHooks{
					ResolveType: func(spelling string) sqlmodel.SQLType {
						return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, spelling)
					},
					DefaultNullability: nil,
					Add: func(string, *sqlmodel.Function) *sqlmodel.Function {
						added++
						return nil
					},
				})
			})
			assert.Equal(t, testCase.spec != nil, added == 1)
		})
	}
}
