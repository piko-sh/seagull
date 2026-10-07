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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
)

func TestCollectFunctionCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		analysis *ast.RawQueryAnalysis
		expected []string
	}{
		{
			name:     "nil analysis returns nil",
			analysis: nil,
			expected: nil,
		},
		{
			name: "nil expressions returns nil",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{Expression: nil},
				},
			},
			expected: nil,
		},
		{
			name: "single function call returns function name",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.FunctionCallExpression{
							FunctionName: "lower",
						},
					},
				},
			},
			expected: []string{"lower"},
		},
		{
			name: "nested function calls collects from inner expressions",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.FunctionCallExpression{
							FunctionName: "upper",
							Arguments: []ast.Expression{
								&ast.FunctionCallExpression{
									FunctionName: "trim",
								},
							},
						},
					},
				},
			},
			expected: []string{"trim", "upper"},
		},
		{
			name: "schema-qualified function includes schema in name",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.FunctionCallExpression{
							FunctionName: "my_func",
							Schema:       "myschema",
						},
					},
				},
			},
			expected: []string{"myschema.my_func"},
		},
		{
			name: "deduplicated same function called twice returns once",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.FunctionCallExpression{
							FunctionName: "now",
						},
					},
					{
						Expression: &ast.FunctionCallExpression{
							FunctionName: "now",
						},
					},
				},
			},
			expected: []string{"now"},
		},
		{
			name: "binary op with function collects from left and right",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.BinaryOpExpression{
							Left: &ast.FunctionCallExpression{
								FunctionName: "length",
							},
							Right: &ast.FunctionCallExpression{
								FunctionName: "abs",
							},
							Operator: "+",
						},
					},
				},
			},
			expected: []string{"abs", "length"},
		},
		{
			name: "coalesce with function collects from inner expressions",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.CoalesceExpression{
							Arguments: []ast.Expression{
								&ast.FunctionCallExpression{
									FunctionName: "max",
								},
								&ast.FunctionCallExpression{
									FunctionName: "min",
								},
							},
						},
					},
				},
			},
			expected: []string{"max", "min"},
		},
		{
			name: "cast with function collects from inner expression",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.CastExpression{
							Inner: &ast.FunctionCallExpression{
								FunctionName: "random",
							},
							TypeName: "integer",
						},
					},
				},
			},
			expected: []string{"random"},
		},
		{
			name: "case when with function collects from branches and else",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.CaseWhenExpression{
							Branches: []ast.CaseWhenBranch{
								{
									Condition: &ast.FunctionCallExpression{
										FunctionName: "is_valid",
									},
									Result: &ast.FunctionCallExpression{
										FunctionName: "format",
									},
								},
							},
							ElseResult: &ast.FunctionCallExpression{
								FunctionName: "coalesce",
							},
						},
					},
				},
			},
			expected: []string{"coalesce", "format", "is_valid"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := collectFunctionCallsWithinLimit(testCase.analysis).functions

			if result != nil {
				slices.Sort(result)
			}
			if testCase.expected != nil {
				slices.Sort(testCase.expected)
			}

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestCollectFunctionCallsReturnsSortedOrder(t *testing.T) {
	t.Parallel()

	analysis := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{
			{Expression: &ast.FunctionCallExpression{FunctionName: "zeta"}},
			{Expression: &ast.FunctionCallExpression{FunctionName: "alpha"}},
			{Expression: &ast.FunctionCallExpression{FunctionName: "mid"}},
		},
	}

	result := collectFunctionCallsWithinLimit(analysis).functions

	assert.Equal(t, []string{"alpha", "mid", "zeta"}, result,
		"collected function names must be returned in deterministic sorted order")
}

func TestCollectFunctionCallsIsDepthBounded(t *testing.T) {
	t.Parallel()

	var nested ast.Expression = &ast.LiteralExpression{}
	for range 100000 {
		nested = &ast.BinaryOpExpression{Left: nested, Right: &ast.LiteralExpression{}}
	}
	top := &ast.FunctionCallExpression{
		FunctionName: "outer",
		Arguments:    []ast.Expression{nested},
	}

	result := collectFunctionCallsWithinLimit(&ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Expression: top}},
	}).functions

	assert.Equal(t, []string{"outer"}, result)
}

func TestCollectFunctionCallsDescendsIntoSubqueries(t *testing.T) {
	t.Parallel()

	scalar := &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{
			{Expression: &ast.FunctionCallExpression{FunctionName: "scalar_fn"}},
		},
	}}
	analysis := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Expression: scalar}},
		PredicateSubqueries: []*ast.RawQueryAnalysis{
			{OutputColumns: []ast.RawOutputColumn{
				{Expression: &ast.FunctionCallExpression{FunctionName: "predicate_fn"}},
			}},
		},
	}

	result := collectFunctionCallsWithinLimit(analysis).functions
	slices.Sort(result)
	assert.Equal(t, []string{"predicate_fn", "scalar_fn"}, result)
}

func TestCollectFunctionCallsReportsTheDepthLimit(t *testing.T) {
	t.Parallel()

	nestedQueries := func(levels int) *ast.RawQueryAnalysis {
		query := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
			{Expression: &ast.FunctionCallExpression{FunctionName: "hidden"}},
		}}
		for range levels {
			query = &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
				{Expression: &ast.ScalarSubqueryExpression{InnerQuery: query}},
			}}
		}
		return query
	}
	var deepExpression ast.Expression = &ast.FunctionCallExpression{FunctionName: "hidden"}
	for range resolve.MaxExpressionResolveDepth + 2 {
		deepExpression = &ast.CastExpression{Inner: deepExpression}
	}

	testCases := []struct {
		name        string
		analysis    *ast.RawQueryAnalysis
		wantNames   []string
		wantLimited bool
	}{
		{
			name:        "queries nested past the limit leave the deepest call out",
			analysis:    nestedQueries(resolve.MaxExpressionResolveDepth + 2),
			wantNames:   nil,
			wantLimited: true,
		},
		{
			name:        "queries nested within the limit are all walked",
			analysis:    nestedQueries(resolve.MaxExpressionResolveDepth - 2),
			wantNames:   []string{"hidden"},
			wantLimited: false,
		},
		{
			name:        "an expression nested past the query limit is walked to the bottom",
			analysis:    &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: deepExpression}}},
			wantNames:   []string{"hidden"},
			wantLimited: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			collected := collectFunctionCallsWithinLimit(testCase.analysis)
			names, limited := collected.functions, collected.limited
			assert.Equal(t, testCase.wantNames, names)
			assert.Equal(t, testCase.wantLimited, limited)
		})
	}
}

func TestCollectFunctionCallsFollowsEveryExpressionKindAndQuery(t *testing.T) {
	t.Parallel()

	writer := func() ast.Expression { return &ast.FunctionCallExpression{FunctionName: "writer"} }
	var typedNil *ast.FunctionCallExpression
	testCases := []struct {
		name     string
		analysis *ast.RawQueryAnalysis
		want     []string
	}{
		{
			name: "a named argument",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: &ast.FunctionCallExpression{
				FunctionName: "w2", Arguments: []ast.Expression{&ast.NamedArgumentExpression{Name: "x", Value: writer()}},
			}}}},
			want: []string{"w2", "writer"},
		},
		{
			name: "a struct constructor",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: &ast.StructExpression{
				Names: []string{"a"}, Values: []ast.Expression{writer()},
			}}}},
			want: []string{"writer"},
		},
		{
			name: "a CTE body's clauses",
			analysis: &ast.RawQueryAnalysis{CTEDefinitions: []ast.RawCTEDefinition{{Name: "c", Query: &ast.RawQueryAnalysis{
				ClauseExpressions: []ast.ClauseExpression{{Expression: writer()}},
			}}}},
			want: []string{"writer"},
		},
		{
			name: "a table-valued function's arguments",
			analysis: &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{{
				FunctionName: "generate_series", Arguments: []ast.Expression{writer()},
				Functions: []ast.TableFunctionCall{{FunctionName: "unnest", Arguments: []ast.Expression{
					&ast.FunctionCallExpression{FunctionName: "listing"},
				}}},
			}}},
			want: []string{"listing", "writer"},
		},
		{
			name:     "an ARRAY JOIN source",
			analysis: &ast.RawQueryAnalysis{ArrayJoinClauses: []ast.RawArrayJoinClause{{Source: writer(), Alias: "item"}}},
			want:     []string{"writer"},
		},
		{
			name: "a subquery inside a named argument",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: &ast.FunctionCallExpression{
				FunctionName: "outer_call", Arguments: []ast.Expression{&ast.NamedArgumentExpression{Name: "x",
					Value: &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
						OutputColumns: []ast.RawOutputColumn{{Expression: writer()}},
					}}}},
			}}}},
			want: []string{"outer_call", "writer"},
		},
		{
			name:     "a call that is a typed nil",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: typedNil}}},
			want:     nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var result []string
			require.NotPanics(t, func() { result = collectFunctionCallsWithinLimit(testCase.analysis).functions })
			assert.Equal(t, testCase.want, result)
		})
	}
}

func TestCollectFunctionCallsKeepsTheNamesTheDialectGave(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		analysis       *ast.RawQueryAnalysis
		name           string
		wantFunctions  []string
		wantTableCalls []string
	}{
		{
			name: "a quoted mixed-case call keeps its case",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
				{Expression: &ast.FunctionCallExpression{FunctionName: "Purge"}},
				{Expression: &ast.FunctionCallExpression{FunctionName: "purge"}},
			}},
			wantFunctions: []string{"Purge", "purge"},
		},
		{
			name: "a qualified call keeps the case of its schema",
			analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
				{Expression: &ast.FunctionCallExpression{FunctionName: "Write", Schema: "Audit"}},
			}},
			wantFunctions: []string{"Audit.Write"},
		},
		{
			name: "a table function read as a FROM item keeps its case",
			analysis: &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
				{FunctionName: "Series"},
				{Functions: []ast.TableFunctionCall{{FunctionName: "public.Rows"}}},
			}},
			wantTableCalls: []string{"Series", "public.Rows"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			collected := collectFunctionCallsWithinLimit(testCase.analysis)

			assert.Equal(t, testCase.wantFunctions, collected.functions)
			assert.Equal(t, testCase.wantTableCalls, collected.tables)
		})
	}
}
