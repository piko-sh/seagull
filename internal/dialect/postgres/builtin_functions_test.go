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

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBuiltinFunctionSpecDecodes(t *testing.T) {
	t.Parallel()

	_, err := builtinFunctionSpec()

	require.NoError(t, err)
}

func TestBuiltinFunctionSpecNamesOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	specification, err := builtinFunctionSpec()
	require.NoError(t, err)

	var unknown []string
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			element := strings.TrimSuffix(spelling, arraySubscriptSuffix)
			if _, known := builtinTypeMap[strings.ToLower(element)]; !known && spelling != anyTypeSpelling {
				unknown = append(unknown, spelling)
			}
			return sqlmodel.SQLType{}
		},
		DefaultNullability: nil,
		Add: func(_ string, signature *sqlmodel.Function) *sqlmodel.Function {
			return signature
		},
	})

	require.Empty(t, unknown)
}

func TestBuiltinFunctionSpecResolvesArraySpellingsToArrays(t *testing.T) {
	t.Parallel()

	overloads := New().BuiltinFunctions().Functions["regexp_split_to_array"]

	require.NotEmpty(t, overloads)
	returned := overloads[0].ReturnType
	require.Equal(t, sqlmodel.TypeCategoryArray, returned.Category)
	require.Equal(t, "text[]", returned.EngineName)
	require.NotNil(t, returned.ElementType)
	require.Equal(t, sqlmodel.TypeCategoryText, returned.ElementType.Category)
}

func TestBuiltinFunctionCatalogueKeepsEverySignature(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	signatures := 0
	for _, overloads := range functions {
		signatures += len(overloads)
	}

	require.Len(t, functions, 331)
	require.Equal(t, 448, signatures)
}

func TestArrayToJSONRendersAnArrayAsJSON(t *testing.T) {
	t.Parallel()

	overloads := New().BuiltinFunctions().Functions["array_to_json"]

	require.Len(t, overloads, 2)
	for _, overload := range overloads {
		require.Equal(t, sqlmodel.TypeCategoryJSON, overload.ReturnType.Category)
		require.Equal(t, "json", overload.ReturnType.EngineName)
		require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, overload.NullableBehaviour)
	}
}

func TestBuiltinFunctionsResolveThroughExtraTypes(t *testing.T) {
	t.Parallel()

	override := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "custom_text")
	functions := New(WithExtraTypes(map[string]sqlmodel.SQLType{"text": override})).BuiltinFunctions().Functions

	require.Equal(t, override, functions["upper"][0].ReturnType)
	require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, functions["upper"][0].NullableBehaviour)
	require.Equal(t, sqlmodel.DataAccessReadOnly, functions["upper"][0].DataAccess)
}

func TestBuiltinSequenceFunctionsModifyData(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions

	require.Equal(t, sqlmodel.DataAccessModifiesData, functions["nextval"][0].DataAccess)
	require.Equal(t, sqlmodel.DataAccessReadOnly, functions["currval"][0].DataAccess)
}

func TestEveryFunctionWrittenWithoutParenthesesIsABuiltin(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	for identifier := range implicitFunctionIdentifiers {
		require.NotEmpty(t, functions[strings.ToLower(identifier)], identifier)
	}
}
