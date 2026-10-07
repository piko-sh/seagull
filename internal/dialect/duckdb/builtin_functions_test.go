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

package duckdb

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
			element := strings.TrimRight(spelling, arraySubscriptSuffix)
			if _, known := builtinTypeMap[strings.ToLower(element)]; !known && element != anyTypeSpelling {
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

func TestBuiltinFunctionCatalogueKeepsEverySignature(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	signatures := 0
	for _, overloads := range functions {
		signatures += len(overloads)
	}

	require.Len(t, functions, 421)
	require.Equal(t, 495, signatures)
}
