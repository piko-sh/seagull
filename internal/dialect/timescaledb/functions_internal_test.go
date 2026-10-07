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

package timescaledb

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFunctionSpecDecodes(t *testing.T) {
	t.Parallel()

	_, err := functionSpec()

	require.NoError(t, err)
}

func TestFunctionSpecNamesOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	specification, err := functionSpec()
	require.NoError(t, err)

	types := New().BuiltinTypes().Types
	var unknown []string
	signatures := 0
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			base := strings.ToLower(strings.TrimSuffix(spelling, arrayTypeSuffix))
			_, known := types[base]
			if !known && base != anyTypeSpelling && base != regprocTypeSpelling && !strings.HasSuffix(base, "_record") {
				unknown = append(unknown, spelling)
			}
			return sqlmodel.SQLType{}
		},
		DefaultNullability: nil,
		Add: func(_ string, signature *sqlmodel.Function) *sqlmodel.Function {
			signatures++
			return signature
		},
	})

	require.Empty(t, unknown)
	require.Equal(t, 419, signatures)
}

func TestFunctionCatalogueKeepsEverySignature(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	signatures := 0
	for _, overloads := range functions {
		signatures += len(overloads)
	}

	require.Len(t, functions, 512)
	require.Equal(t, 867, signatures)
}

func TestAFunctionSpecThatDidNotDecodeFailsEveryStatement(t *testing.T) {
	t.Parallel()

	decodeErr := errors.New("line 7: unknown NULL behaviour")
	testCases := []struct {
		loadSpecification func() (*dialectkit.FunctionSpec, error)
		name              string
		wantErr           error
	}{
		{name: "a spec that did not decode", loadSpecification: func() (*dialectkit.FunctionSpec, error) { return nil, decodeErr }, wantErr: decodeErr},
		{name: "the embedded spec", loadSpecification: functionSpec, wantErr: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := postgres.New(postgres.WithExtraFunctions(func(builder *postgres.FunctionCatalogueBuilder) {
				registerFunctionSpec(builder, testCase.loadSpecification)
			}))
			statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id int); SELECT 1")
			require.NoError(t, err)

			_, applyErr := engine.ApplyDDL(t.Context(), statements[0])
			_, analyseErr := engine.AnalyseQuery(t.Context(), nil, statements[1])

			if testCase.wantErr == nil {
				require.NoError(t, applyErr)
				require.NoError(t, analyseErr)
				return
			}
			require.ErrorIs(t, applyErr, testCase.wantErr)
			require.ErrorIs(t, analyseErr, testCase.wantErr)
			require.ErrorContains(t, applyErr, "registering the TimescaleDB functions")
		})
	}
}
