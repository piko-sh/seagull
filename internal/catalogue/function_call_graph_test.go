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

package catalogue

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestPropagateDataAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		calls      map[string][]string
		catalogue  *sqlmodel.Catalogue
		assertions func(t *testing.T, catalogue *sqlmodel.Catalogue)
		name       string
	}{
		{
			name: "no functions results in no changes",
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name:      "public",
						Functions: make(map[string][]*sqlmodel.Function),
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				assert.Empty(t, catalogue.Schemas["public"].Functions)
			},
		},
		{
			name: "read-only calling read-only stays read-only",
			calls: map[string][]string{
				"caller": {"callee"},
			},
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name: "public",
						Functions: map[string][]*sqlmodel.Function{
							"caller": {
								{
									Name:       "caller",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
							"callee": {
								{
									Name:       "callee",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				caller := catalogue.Schemas["public"].Functions["caller"][0]
				callee := catalogue.Schemas["public"].Functions["callee"][0]
				assert.Equal(t, sqlmodel.DataAccessReadOnly, caller.DataAccess)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, callee.DataAccess)
			},
		},
		{
			name: "read-only calling data-modifying becomes data-modifying",
			calls: map[string][]string{
				"caller": {"writer"},
			},
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name: "public",
						Functions: map[string][]*sqlmodel.Function{
							"caller": {
								{
									Name:       "caller",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
							"writer": {
								{
									Name:       "writer",
									DataAccess: sqlmodel.DataAccessModifiesData,
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				caller := catalogue.Schemas["public"].Functions["caller"][0]
				assert.Equal(t, sqlmodel.DataAccessModifiesData, caller.DataAccess)
			},
		},
		{
			name: "transitive propagation through call chain",
			calls: map[string][]string{
				"a": {"b"},
				"b": {"c"},
			},
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name: "public",
						Functions: map[string][]*sqlmodel.Function{
							"a": {
								{
									Name:       "a",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
							"b": {
								{
									Name:       "b",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
							"c": {
								{
									Name:       "c",
									DataAccess: sqlmodel.DataAccessModifiesData,
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				functionA := catalogue.Schemas["public"].Functions["a"][0]
				functionB := catalogue.Schemas["public"].Functions["b"][0]
				functionC := catalogue.Schemas["public"].Functions["c"][0]
				assert.Equal(t, sqlmodel.DataAccessModifiesData, functionA.DataAccess)
				assert.Equal(t, sqlmodel.DataAccessModifiesData, functionB.DataAccess)
				assert.Equal(t, sqlmodel.DataAccessModifiesData, functionC.DataAccess)
			},
		},
		{
			name: "already data-modifying stays unchanged",
			calls: map[string][]string{
				"writer": {"reader"},
			},
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name: "public",
						Functions: map[string][]*sqlmodel.Function{
							"writer": {
								{
									Name:       "writer",
									DataAccess: sqlmodel.DataAccessModifiesData,
								},
							},
							"reader": {
								{
									Name:       "reader",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				writer := catalogue.Schemas["public"].Functions["writer"][0]
				assert.Equal(t, sqlmodel.DataAccessModifiesData, writer.DataAccess)
			},
		},
		{
			name: "fixed-point convergence with circular calls",
			calls: map[string][]string{
				"alpha": {"beta"},
				"beta":  {"alpha"},
			},
			catalogue: &sqlmodel.Catalogue{
				Schemas: map[string]*sqlmodel.Schema{
					"public": {
						Name: "public",
						Functions: map[string][]*sqlmodel.Function{
							"alpha": {
								{
									Name:       "alpha",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
							"beta": {
								{
									Name:       "beta",
									DataAccess: sqlmodel.DataAccessReadOnly,
								},
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, catalogue *sqlmodel.Catalogue) {
				t.Helper()

				alpha := catalogue.Schemas["public"].Functions["alpha"][0]
				beta := catalogue.Schemas["public"].Functions["beta"][0]
				assert.Equal(t, sqlmodel.DataAccessReadOnly, alpha.DataAccess)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, beta.DataAccess)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			propagateDataAccess(testCase.catalogue, calledFunctionsByName(testCase.catalogue, testCase.calls), nil, nil)
			testCase.assertions(t, testCase.catalogue)
		})
	}
}

func calledFunctionsByName(catalogue *sqlmodel.Catalogue, calls map[string][]string) map[*sqlmodel.Function][]string {
	calledFunctions := make(map[*sqlmodel.Function][]string)
	for _, schema := range catalogue.Schemas {
		for _, overloads := range schema.Functions {
			for _, function := range overloads {
				if called, ok := calls[function.Name]; ok {
					calledFunctions[function] = called
				}
			}
		}
	}
	return calledFunctions
}

func TestPropagateDataAccessReachesACalledFunctionByTheKeyOfItsName(t *testing.T) {
	t.Parallel()

	foldsToLower := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}
	ignoresCase := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}

	testCases := []struct {
		name        string
		identifiers sqlmodel.IdentifierCase
		called      string
		table       bool
		want        sqlmodel.FunctionDataAccess
	}{
		{name: "a call of the unquoted reader stays read-only beside a quoted writer", identifiers: foldsToLower,
			called: "purge", want: sqlmodel.DataAccessReadOnly},
		{name: "a call of the quoted writer is promoted", identifiers: foldsToLower,
			called: "Purge", want: sqlmodel.DataAccessModifiesData},
		{name: "a qualified call of the quoted writer is promoted", identifiers: foldsToLower,
			called: "public.Purge", want: sqlmodel.DataAccessModifiesData},
		{name: "a table function read of the unquoted reader stays read-only", identifiers: foldsToLower,
			called: "purge", table: true, want: sqlmodel.DataAccessReadOnly},
		{name: "a table function read of the quoted writer is promoted", identifiers: foldsToLower,
			called: "Purge", table: true, want: sqlmodel.DataAccessModifiesData},
		{name: "a call in another case reaching no created function falls back to the built-ins", identifiers: foldsToLower,
			called: "NEXTVAL", want: sqlmodel.DataAccessModifiesData},
		{name: "a built-in that only reads keeps the caller read-only in any case", identifiers: foldsToLower,
			called: "LOWER", want: sqlmodel.DataAccessReadOnly},
		{name: "a name reaching nothing promotes the caller", identifiers: foldsToLower,
			called: "PURGE", want: sqlmodel.DataAccessModifiesData},
		{name: "a case-insensitive dialect reaches both functions by either spelling", identifiers: ignoresCase,
			called: "PURGE", want: sqlmodel.DataAccessModifiesData},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			writer := &sqlmodel.Function{Name: "Purge", Schema: "public", DataAccess: sqlmodel.DataAccessModifiesData}
			reader := &sqlmodel.Function{Name: "purge", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
			caller := &sqlmodel.Function{Name: "caller", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
			functions := map[string][]*sqlmodel.Function{}
			for _, function := range []*sqlmodel.Function{writer, reader, caller} {
				key := testCase.identifiers.Key(function.Name)
				functions[key] = append(functions[key], function)
			}
			catalogue := &sqlmodel.Catalogue{
				Identifiers: testCase.identifiers,
				Schemas:     map[string]*sqlmodel.Schema{"public": {Name: "public", Functions: functions}},
			}
			builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
				"nextval": {{Name: "nextval", DataAccess: sqlmodel.DataAccessModifiesData}},
				"lower":   {{Name: "lower", DataAccess: sqlmodel.DataAccessReadOnly}},
			}}
			calls := map[*sqlmodel.Function][]string{caller: {testCase.called}}
			var functionCalls, tableCalls map[*sqlmodel.Function][]string
			if testCase.table {
				tableCalls = calls
			} else {
				functionCalls = calls
			}

			propagateDataAccess(catalogue, functionCalls, tableCalls, builtins)

			assert.Equal(t, testCase.want, caller.DataAccess)
		})
	}
}

func TestPropagateDataAccessCarriesAPromotionUpAChainOfQuotedNames(t *testing.T) {
	t.Parallel()

	writer := &sqlmodel.Function{Name: "Purge", Schema: "public", DataAccess: sqlmodel.DataAccessModifiesData}
	middle := &sqlmodel.Function{Name: "Middle", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
	top := &sqlmodel.Function{Name: "top", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
	sibling := &sqlmodel.Function{Name: "middle", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
	bystander := &sqlmodel.Function{Name: "bystander", Schema: "public", DataAccess: sqlmodel.DataAccessReadOnly}
	catalogue := &sqlmodel.Catalogue{
		Identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
		Schemas: map[string]*sqlmodel.Schema{"public": {Name: "public", Functions: map[string][]*sqlmodel.Function{
			"Purge": {writer}, "Middle": {middle}, "middle": {sibling}, "top": {top}, "bystander": {bystander},
		}}},
	}
	calls := map[*sqlmodel.Function][]string{middle: {"Purge"}, top: {"Middle"}, bystander: {"middle"}}

	propagateDataAccess(catalogue, calls, nil, nil)

	testCases := []struct {
		function *sqlmodel.Function
		name     string
		want     sqlmodel.FunctionDataAccess
	}{
		{name: "the quoted caller of the writer", function: middle, want: sqlmodel.DataAccessModifiesData},
		{name: "the caller of the quoted caller", function: top, want: sqlmodel.DataAccessModifiesData},
		{name: "the unquoted function of the same letters", function: sibling, want: sqlmodel.DataAccessReadOnly},
		{name: "the caller of the unquoted function", function: bystander, want: sqlmodel.DataAccessReadOnly},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.function.DataAccess)
		})
	}
}
