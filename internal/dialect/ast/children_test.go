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

package ast_test

import (
	"fmt"
	"runtime/debug"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type incomparableExpression struct {
	*ast.UnknownExpression
	tags []string
}

type childrenCase struct {
	expression ast.Expression
	rewritten  ast.Expression
	name       string
	children   []ast.Expression
}

func TestChildren_YieldsTheDirectChildrenOfEveryKindInSourceOrder(t *testing.T) {
	t.Parallel()

	for _, testCase := range childrenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireSameExpressions(t, testCase.children, slices.Collect(ast.Children(testCase.expression)))
		})
	}
}

func TestRewriteChildren_ReplacesEveryChildOfEveryKindInTheOrderChildrenGives(t *testing.T) {
	t.Parallel()

	for _, testCase := range childrenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var seen []ast.Expression
			rewritten := ast.RewriteChildren(testCase.expression, func(child ast.Expression) ast.Expression {
				seen = append(seen, child)
				return replacement(len(seen) - 1)
			})

			requireSameExpressions(t, testCase.children, seen)
			assert.Equal(t, testCase.rewritten, rewritten)
			if len(testCase.children) == 0 {
				assert.Same(t, testCase.expression, rewritten)
			} else {
				assert.NotSame(t, testCase.expression, rewritten)
			}
			requireSameExpressions(t, testCase.children, slices.Collect(ast.Children(testCase.expression)))
		})
	}
}

func TestRewriteChildren_ReturnsTheSameNodeWhenNoChildChanges(t *testing.T) {
	t.Parallel()

	for _, testCase := range childrenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rewritten := ast.RewriteChildren(testCase.expression, func(child ast.Expression) ast.Expression {
				return child
			})

			assert.Same(t, testCase.expression, rewritten)
		})
	}
}

func TestRewriteChildren_SharesTheListsWhoseChildrenDidNotChange(t *testing.T) {
	t.Parallel()

	first, second, filter := column("a"), column("b"), column("f")
	call := &ast.FunctionCallExpression{FunctionName: "count", Arguments: []ast.Expression{first, second}, FilterExpression: filter}
	newFilter := column("g")

	rewritten := ast.RewriteChildren(call, func(child ast.Expression) ast.Expression {
		if child == filter {
			return newFilter
		}
		return child
	})

	copied, isCall := rewritten.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	assert.NotSame(t, call, copied)
	assert.Same(t, newFilter, copied.FilterExpression)
	assert.Same(t, filter, call.FilterExpression)
	require.Len(t, copied.Arguments, 2)
	assert.Same(t, &call.Arguments[0], &copied.Arguments[0])
}

func TestRewriteChildren_CopiesAListBeforeChangingOneOfItsChildren(t *testing.T) {
	t.Parallel()

	first, second, third := column("a"), column("b"), column("c")
	operands := []ast.Expression{first, second, third}
	logical := &ast.LogicalOpExpression{Operator: "AND", Operands: operands}
	newSecond := column("z")

	rewritten := ast.RewriteChildren(logical, func(child ast.Expression) ast.Expression {
		if child == second {
			return newSecond
		}
		return child
	})

	copied, isLogical := rewritten.(*ast.LogicalOpExpression)
	require.True(t, isLogical)
	requireSameExpressions(t, []ast.Expression{first, newSecond, third}, copied.Operands)
	requireSameExpressions(t, []ast.Expression{first, second, third}, logical.Operands)
	requireSameExpressions(t, []ast.Expression{first, second, third}, operands)
}

func TestChildren_SkipsNilInterfaceChildrenWhichRewriteKeepsInPlace(t *testing.T) {
	t.Parallel()

	first, second := column("a"), column("b")
	testCases := []struct {
		expression ast.Expression
		rewritten  ast.Expression
		name       string
		children   []ast.Expression
	}{
		{
			name:       "a NULL argument and no FILTER",
			expression: &ast.FunctionCallExpression{FunctionName: "f", Arguments: []ast.Expression{first, nil, second}},
			children:   []ast.Expression{first, second},
			rewritten: &ast.FunctionCallExpression{
				FunctionName: "f",
				Arguments:    []ast.Expression{replacement(0), nil, replacement(1)},
			},
		},
		{
			name:       "a CASE without ELSE",
			expression: &ast.CaseWhenExpression{Branches: []ast.CaseWhenBranch{{Condition: first, Result: nil}}},
			children:   []ast.Expression{first},
			rewritten:  &ast.CaseWhenExpression{Branches: []ast.CaseWhenBranch{{Condition: replacement(0), Result: nil}}},
		},
		{
			name:       "a BETWEEN with a NULL low bound",
			expression: &ast.BetweenExpression{Inner: first, Low: nil, High: second},
			children:   []ast.Expression{first, second},
			rewritten:  &ast.BetweenExpression{Inner: replacement(0), Low: nil, High: replacement(1)},
		},
		{
			name:       "a lambda with a missing binding",
			expression: &ast.LambdaExpression{Body: first, Parameters: []string{"x", "y"}, Bindings: []ast.Expression{nil, second}},
			children:   []ast.Expression{first, second},
			rewritten: &ast.LambdaExpression{
				Body:       replacement(0),
				Parameters: []string{"x", "y"},
				Bindings:   []ast.Expression{nil, replacement(1)},
			},
		},
		{
			name:       "a window without a function call",
			expression: &ast.WindowFunctionExpression{Function: nil},
			children:   nil,
			rewritten:  &ast.WindowFunctionExpression{Function: nil},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireSameExpressions(t, testCase.children, slices.Collect(ast.Children(testCase.expression)))
			position := 0
			rewritten := ast.RewriteChildren(testCase.expression, func(child ast.Expression) ast.Expression {
				require.NotNil(t, child)
				position++
				return replacement(position - 1)
			})
			assert.Equal(t, testCase.rewritten, rewritten)
		})
	}
}

func TestChildren_YieldsATypedNilChildWhichHasNoChildrenOfItsOwn(t *testing.T) {
	t.Parallel()

	var missing *ast.ColumnRefExpression
	right := column("b")
	operation := &ast.BinaryOpExpression{Left: missing, Right: right, Operator: "+"}

	children := slices.Collect(ast.Children(operation))

	require.Len(t, children, 2)
	assert.True(t, ast.IsNil(children[0]))
	assert.Same(t, right, children[1])
	assert.Empty(t, slices.Collect(ast.Children(children[0])))

	var visited []ast.Expression
	ast.Walk(operation, func(node ast.Expression) bool {
		visited = append(visited, node)
		return true
	})
	requireSameExpressions(t, []ast.Expression{operation, missing, right}, visited)
}

func TestTypedNilNodes_HaveNoChildrenAndComeBackFromRewriteAsTheyAre(t *testing.T) {
	t.Parallel()

	for _, node := range typedNilNodes() {
		t.Run(fmt.Sprintf("%T", node), func(t *testing.T) {
			t.Parallel()

			assert.True(t, ast.IsNil(node))
			assert.Empty(t, slices.Collect(ast.Children(node)))
			rewritten := ast.RewriteChildren(node, func(ast.Expression) ast.Expression {
				require.Fail(t, "a typed-nil node has no children to rewrite")
				return nil
			})
			assert.Equal(t, node, rewritten)
			requireSameExpressions(t, []ast.Expression{node}, slices.Collect(ast.Preorder(node)))
			inner, holds := ast.InnerQuery(node)
			assert.Nil(t, inner)
			assert.False(t, holds)
		})
	}
}

func TestChildren_StopsWhenTheLoopEndsEarly(t *testing.T) {
	t.Parallel()

	first, second, third := column("a"), column("b"), column("c")
	testCases := []struct {
		expression ast.Expression
		name       string
		want       []ast.Expression
	}{
		{
			name:       "a list of operands",
			expression: &ast.LogicalOpExpression{Operator: "OR", Operands: []ast.Expression{first, second, third}},
			want:       []ast.Expression{first, second},
		},
		{
			name: "the branches of a CASE",
			expression: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: first, Result: second}, {Condition: third, Result: first}},
				ElseResult: second,
			},
			want: []ast.Expression{first, second},
		},
		{
			name:       "a pair of operands",
			expression: &ast.ComparisonExpression{Left: first, Right: second, Operator: "="},
			want:       []ast.Expression{first},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var collected []ast.Expression
			for child := range ast.Children(testCase.expression) {
				collected = append(collected, child)
				if len(collected) == len(testCase.want) {
					break
				}
			}
			requireSameExpressions(t, testCase.want, collected)
		})
	}
}

func TestRewriteChildren_KeepsAWindowsFunctionUnlessGivenAFunctionCall(t *testing.T) {
	t.Parallel()

	call := &ast.FunctionCallExpression{FunctionName: "row_number"}
	newCall := &ast.FunctionCallExpression{FunctionName: "rank"}
	testCases := []struct {
		replacement ast.Expression
		name        string
		replaced    bool
	}{
		{name: "a function call replaces it", replacement: newCall, replaced: true},
		{name: "a column leaves it", replacement: column("a"), replaced: false},
		{name: "nil leaves it", replacement: nil, replaced: false},
		{name: "the same call leaves it", replacement: call, replaced: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			window := &ast.WindowFunctionExpression{Function: call}
			rewritten := ast.RewriteChildren(window, func(ast.Expression) ast.Expression {
				return testCase.replacement
			})

			copied, isWindow := rewritten.(*ast.WindowFunctionExpression)
			require.True(t, isWindow)
			assert.Same(t, call, window.Function)
			if !testCase.replaced {
				assert.Same(t, window, copied)
				return
			}
			assert.NotSame(t, window, copied)
			assert.Same(t, newCall, copied.Function)
		})
	}
}

func TestRewriteChildren_HandlesEdgeInputsWithoutPanicking(t *testing.T) {
	t.Parallel()

	child := column("a")
	unary := &ast.UnaryOpExpression{Operand: child, Operator: "-"}
	incomparable := incomparableExpression{UnknownExpression: &ast.UnknownExpression{}, tags: []string{"x"}}
	testCases := []struct {
		expression ast.Expression
		rewrite    func(ast.Expression) ast.Expression
		check      func(t *testing.T, rewritten ast.Expression)
		name       string
	}{
		{
			name:       "a nil rewrite leaves the expression",
			expression: unary,
			rewrite:    nil,
			check: func(t *testing.T, rewritten ast.Expression) {
				t.Helper()
				assert.Same(t, unary, rewritten)
			},
		},
		{
			name:       "a nil expression comes back nil",
			expression: nil,
			rewrite:    func(ast.Expression) ast.Expression { return child },
			check: func(t *testing.T, rewritten ast.Expression) {
				t.Helper()
				assert.Nil(t, rewritten)
			},
		},
		{
			name:       "a nil replacement stands for NULL",
			expression: unary,
			rewrite:    func(ast.Expression) ast.Expression { return nil },
			check: func(t *testing.T, rewritten ast.Expression) {
				t.Helper()
				assert.Equal(t, &ast.UnaryOpExpression{Operand: nil, Operator: "-"}, rewritten)
				assert.Same(t, child, unary.Operand)
			},
		},
		{
			name:       "a child of a type that cannot be compared is taken as changed",
			expression: &ast.UnaryOpExpression{Operand: incomparable, Operator: "-"},
			rewrite:    func(child ast.Expression) ast.Expression { return child },
			check: func(t *testing.T, rewritten ast.Expression) {
				t.Helper()
				assert.Equal(t, &ast.UnaryOpExpression{Operand: incomparable, Operator: "-"}, rewritten)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var rewritten ast.Expression
			require.NotPanics(t, func() {
				rewritten = ast.RewriteChildren(testCase.expression, testCase.rewrite)
			})
			testCase.check(t, rewritten)
		})
	}
}

func TestWalk_VisitsAMillionDeepChainWithoutExhaustingTheStack(t *testing.T) {
	const (
		depth = 1_000_000

		stackLimit = 16 << 20
	)
	leaf := column("leaf")
	root := unaryChain(leaf, depth)

	previousLimit := debug.SetMaxStack(stackLimit)
	defer debug.SetMaxStack(previousLimit)

	visited := 0
	var last ast.Expression
	ast.Walk(root, func(node ast.Expression) bool {
		visited++
		last = node
		return true
	})

	assert.Equal(t, depth+1, visited)
	assert.Same(t, leaf, last)

	yielded := 0
	for range ast.Preorder(root) {
		yielded++
	}
	assert.Equal(t, depth+1, yielded)
}

func TestWalk_TakesTimeInProportionToTheTree(t *testing.T) {
	const smallerDepth = 50_000
	smallerChain := unaryChain(column("leaf"), smallerDepth)
	largerChain := unaryChain(column("leaf"), 4*smallerDepth)
	smallerList := wideList(smallerDepth)
	largerList := wideList(4 * smallerDepth)
	testCases := []struct {
		smaller ast.Expression
		larger  ast.Expression
		name    string
	}{
		{name: "a deep chain", smaller: smallerChain, larger: largerChain},
		{name: "a wide list", smaller: smallerList, larger: largerList},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			walkAll := func(root ast.Expression) func() {
				return func() {
					ast.Walk(root, func(ast.Expression) bool { return true })
				}
			}
			scaling.Linear(t, walkAll(testCase.smaller), walkAll(testCase.larger))
		})
	}
}

func TestWalk_LeavesOutTheChildrenOfANodeVisitDeclines(t *testing.T) {
	t.Parallel()

	first, second, third := column("a"), column("b"), column("c")
	declined := &ast.BinaryOpExpression{Left: first, Right: second, Operator: "+"}
	sibling := &ast.UnaryOpExpression{Operand: third, Operator: "-"}
	root := &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{declined, sibling}}

	var visited []ast.Expression
	ast.Walk(root, func(node ast.Expression) bool {
		visited = append(visited, node)
		return node != declined
	})

	requireSameExpressions(t, []ast.Expression{root, declined, sibling, third}, visited)
}

func TestWalk_VisitsNothingForANilRootOrANilVisit(t *testing.T) {
	t.Parallel()

	visited := 0
	ast.Walk(nil, func(ast.Expression) bool {
		visited++
		return true
	})
	assert.Zero(t, visited)
	assert.NotPanics(t, func() { ast.Walk(column("a"), nil) })
	assert.Empty(t, slices.Collect(ast.Preorder(nil)))
}

func TestPreorder_YieldsEachParentBeforeItsChildrenAndStopsWhenAsked(t *testing.T) {
	t.Parallel()

	condition := &ast.ComparisonExpression{Left: column("a"), Right: &ast.LiteralExpression{TypeName: "integer"}, Operator: "="}
	result := &ast.FunctionCallExpression{FunctionName: "upper", Arguments: []ast.Expression{column("b")}}
	elseResult := &ast.UnaryOpExpression{Operand: column("c"), Operator: "-"}
	subquery := &ast.ExistsExpression{InnerQuery: &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Expression: column("hidden")}},
	}}
	root := &ast.CaseWhenExpression{
		Branches:   []ast.CaseWhenBranch{{Condition: condition, Result: result}, {Condition: subquery, Result: nil}},
		ElseResult: elseResult,
	}
	want := []ast.Expression{
		root,
		condition, condition.Left, condition.Right,
		result, result.Arguments[0],
		subquery,
		elseResult, elseResult.Operand,
	}
	testCases := []struct {
		name  string
		limit int
	}{
		{name: "the whole tree", limit: len(want)},
		{name: "stopped after the first branch's condition", limit: 4},
		{name: "stopped at the root", limit: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var yielded []ast.Expression
			for node := range ast.Preorder(root) {
				yielded = append(yielded, node)
				if len(yielded) == testCase.limit {
					break
				}
			}
			requireSameExpressions(t, want[:testCase.limit], yielded)
		})
	}
}

func TestInnerQuery_ReturnsTheSubqueryOfExistsAndScalarSubqueries(t *testing.T) {
	t.Parallel()

	query := &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "orders"}}}
	testCases := []struct {
		expression ast.Expression
		want       *ast.RawQueryAnalysis
		name       string
		holds      bool
	}{
		{name: "EXISTS", expression: &ast.ExistsExpression{InnerQuery: query}, want: query, holds: true},
		{name: "a scalar subquery", expression: &ast.ScalarSubqueryExpression{InnerQuery: query}, want: query, holds: true},
		{name: "EXISTS without a query", expression: &ast.ExistsExpression{InnerQuery: nil}, want: nil, holds: false},
		{name: "a scalar subquery without a query", expression: &ast.ScalarSubqueryExpression{InnerQuery: nil}, want: nil, holds: false},
		{name: "a typed-nil EXISTS", expression: (*ast.ExistsExpression)(nil), want: nil, holds: false},
		{name: "a column", expression: column("a"), want: nil, holds: false},
		{name: "nil", expression: nil, want: nil, holds: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			inner, holds := ast.InnerQuery(testCase.expression)

			assert.Same(t, testCase.want, inner)
			assert.Equal(t, testCase.holds, holds)
		})
	}
}

func TestIsNil_DetectsNilInterfacesAndTypedNilNodes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression ast.Expression
		name       string
		want       bool
	}{
		{name: "a nil interface", expression: nil, want: true},
		{name: "a typed-nil column", expression: (*ast.ColumnRefExpression)(nil), want: true},
		{name: "a typed-nil struct constructor", expression: (*ast.StructExpression)(nil), want: true},
		{name: "a column", expression: column("a"), want: false},
		{name: "an empty unknown expression", expression: &ast.UnknownExpression{}, want: false},
		{
			name:       "a value of a struct type",
			expression: incomparableExpression{UnknownExpression: nil, tags: nil},
			want:       false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, ast.IsNil(testCase.expression))
		})
	}
}

func childrenCases() []childrenCase {
	columnA, columnB, columnC, columnD, columnE := column("a"), column("b"), column("c"), column("d"), column("e")
	call := &ast.FunctionCallExpression{FunctionName: "sum", Arguments: []ast.Expression{columnA}}
	query := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: column("inner")}}}
	span := ast.Span{Start: 3, End: 9}
	return []childrenCase{
		leafCase("column reference", columnA),
		{
			name: "function call",
			expression: &ast.FunctionCallExpression{
				FunctionName: "count", Schema: "pg_catalog", Arguments: []ast.Expression{columnA, columnB}, FilterExpression: columnC, Span: span,
			},
			children: []ast.Expression{columnA, columnB, columnC},
			rewritten: &ast.FunctionCallExpression{
				FunctionName: "count", Schema: "pg_catalog",
				Arguments: []ast.Expression{replacement(0), replacement(1)}, FilterExpression: replacement(2), Span: span,
			},
		},
		{
			name:       "COALESCE",
			expression: &ast.CoalesceExpression{Arguments: []ast.Expression{columnA, columnB}},
			children:   []ast.Expression{columnA, columnB},
			rewritten:  &ast.CoalesceExpression{Arguments: []ast.Expression{replacement(0), replacement(1)}},
		},
		{
			name:       "cast",
			expression: &ast.CastExpression{Inner: columnA, TypeName: "text"},
			children:   []ast.Expression{columnA},
			rewritten:  &ast.CastExpression{Inner: replacement(0), TypeName: "text"},
		},
		leafCase("literal", &ast.LiteralExpression{TypeName: "integer"}),
		{
			name:       "binary operator",
			expression: &ast.BinaryOpExpression{Left: columnA, Right: columnB, Operator: "/", MayBeNull: true},
			children:   []ast.Expression{columnA, columnB},
			rewritten:  &ast.BinaryOpExpression{Left: replacement(0), Right: replacement(1), Operator: "/", MayBeNull: true},
		},
		{
			name:       "comparison",
			expression: &ast.ComparisonExpression{Left: columnA, Right: columnB, Operator: "=", Quantifier: "ANY"},
			children:   []ast.Expression{columnA, columnB},
			rewritten:  &ast.ComparisonExpression{Left: replacement(0), Right: replacement(1), Operator: "=", Quantifier: "ANY"},
		},
		{
			name:       "IS NULL",
			expression: &ast.IsNullExpression{Inner: columnA, Negated: true},
			children:   []ast.Expression{columnA},
			rewritten:  &ast.IsNullExpression{Inner: replacement(0), Negated: true},
		},
		{
			name:       "IN list",
			expression: &ast.InListExpression{Inner: columnA, Values: []ast.Expression{columnB, columnC}},
			children:   []ast.Expression{columnA, columnB, columnC},
			rewritten:  &ast.InListExpression{Inner: replacement(0), Values: []ast.Expression{replacement(1), replacement(2)}},
		},
		{
			name:       "BETWEEN",
			expression: &ast.BetweenExpression{Inner: columnA, Low: columnB, High: columnC},
			children:   []ast.Expression{columnA, columnB, columnC},
			rewritten:  &ast.BetweenExpression{Inner: replacement(0), Low: replacement(1), High: replacement(2)},
		},
		{
			name:       "logical operator",
			expression: &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{columnA, columnB, columnC}},
			children:   []ast.Expression{columnA, columnB, columnC},
			rewritten: &ast.LogicalOpExpression{
				Operator: "AND", Operands: []ast.Expression{replacement(0), replacement(1), replacement(2)},
			},
		},
		{
			name:       "unary operator",
			expression: &ast.UnaryOpExpression{Operand: columnA, Operator: "NOT"},
			children:   []ast.Expression{columnA},
			rewritten:  &ast.UnaryOpExpression{Operand: replacement(0), Operator: "NOT"},
		},
		{
			name: "CASE",
			expression: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: columnA, Result: columnB}, {Condition: columnC, Result: columnD}},
				ElseResult: columnE,
			},
			children: []ast.Expression{columnA, columnB, columnC, columnD, columnE},
			rewritten: &ast.CaseWhenExpression{
				Branches: []ast.CaseWhenBranch{
					{Condition: replacement(0), Result: replacement(1)},
					{Condition: replacement(2), Result: replacement(3)},
				},
				ElseResult: replacement(4),
			},
		},
		leafCase("EXISTS", &ast.ExistsExpression{InnerQuery: query}),
		{
			name:       "window function",
			expression: &ast.WindowFunctionExpression{Function: call},
			children:   []ast.Expression{call},
			rewritten:  &ast.WindowFunctionExpression{Function: replacement(0)},
		},
		leafCase("scalar subquery", &ast.ScalarSubqueryExpression{InnerQuery: query}),
		{
			name:       "array subscript",
			expression: &ast.ArraySubscriptExpression{Array: columnA, Index: columnB, Key: "colour"},
			children:   []ast.Expression{columnA, columnB},
			rewritten:  &ast.ArraySubscriptExpression{Array: replacement(0), Index: replacement(1), Key: "colour"},
		},
		{
			name:       "lambda",
			expression: &ast.LambdaExpression{Body: columnA, Parameters: []string{"x", "y"}, Bindings: []ast.Expression{columnB, columnC}},
			children:   []ast.Expression{columnA, columnB, columnC},
			rewritten: &ast.LambdaExpression{
				Body: replacement(0), Parameters: []string{"x", "y"}, Bindings: []ast.Expression{replacement(1), replacement(2)},
			},
		},
		{
			name:       "struct field access",
			expression: &ast.StructFieldAccessExpression{Struct: columnA, FieldName: "city"},
			children:   []ast.Expression{columnA},
			rewritten:  &ast.StructFieldAccessExpression{Struct: replacement(0), FieldName: "city"},
		},
		{
			name:       "named argument",
			expression: &ast.NamedArgumentExpression{Value: columnA, Name: "factor"},
			children:   []ast.Expression{columnA},
			rewritten:  &ast.NamedArgumentExpression{Value: replacement(0), Name: "factor"},
		},
		leafCase("parameter", &ast.ParameterExpression{Number: 1}),
		{
			name:       "row value",
			expression: &ast.RowValueExpression{Elements: []ast.Expression{columnA, columnB}},
			children:   []ast.Expression{columnA, columnB},
			rewritten:  &ast.RowValueExpression{Elements: []ast.Expression{replacement(0), replacement(1)}},
		},
		{
			name:       "struct constructor",
			expression: &ast.StructExpression{Names: []string{"street", "city"}, Values: []ast.Expression{columnA, columnB}, Span: span},
			children:   []ast.Expression{columnA, columnB},
			rewritten: &ast.StructExpression{
				Names: []string{"street", "city"}, Values: []ast.Expression{replacement(0), replacement(1)}, Span: span,
			},
		},
		leafCase("unknown", &ast.UnknownExpression{}),
	}
}

func leafCase(name string, expression ast.Expression) childrenCase {
	return childrenCase{name: name, expression: expression, children: nil, rewritten: expression}
}

func typedNilNodes() []ast.Expression {
	return []ast.Expression{
		(*ast.ColumnRefExpression)(nil),
		(*ast.FunctionCallExpression)(nil),
		(*ast.CoalesceExpression)(nil),
		(*ast.CastExpression)(nil),
		(*ast.LiteralExpression)(nil),
		(*ast.BinaryOpExpression)(nil),
		(*ast.ComparisonExpression)(nil),
		(*ast.IsNullExpression)(nil),
		(*ast.InListExpression)(nil),
		(*ast.BetweenExpression)(nil),
		(*ast.LogicalOpExpression)(nil),
		(*ast.UnaryOpExpression)(nil),
		(*ast.CaseWhenExpression)(nil),
		(*ast.ExistsExpression)(nil),
		(*ast.WindowFunctionExpression)(nil),
		(*ast.ScalarSubqueryExpression)(nil),
		(*ast.ArraySubscriptExpression)(nil),
		(*ast.LambdaExpression)(nil),
		(*ast.StructFieldAccessExpression)(nil),
		(*ast.NamedArgumentExpression)(nil),
		(*ast.ParameterExpression)(nil),
		(*ast.RowValueExpression)(nil),
		(*ast.StructExpression)(nil),
		(*ast.UnknownExpression)(nil),
	}
}

func column(name string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{ColumnName: name}
}

func replacement(position int) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: fmt.Sprintf("replacement_%d", position)}
}

func unaryChain(leaf ast.Expression, depth int) ast.Expression {
	expression := leaf
	for range depth {
		expression = &ast.UnaryOpExpression{Operand: expression, Operator: "-"}
	}
	return expression
}

func wideList(width int) ast.Expression {
	values := make([]ast.Expression, 0, width)
	for index := range width {
		values = append(values, &ast.ParameterExpression{Number: index + 1})
	}
	return &ast.InListExpression{Inner: column("a"), Values: values}
}

func requireSameExpressions(t *testing.T, want, got []ast.Expression) {
	t.Helper()

	require.Len(t, got, len(want))
	for index := range want {
		require.Same(t, want[index], got[index], "expression %d", index)
	}
}

func TestWalk_VisitsANodeHeldByTwoParentsOnce(t *testing.T) {
	t.Parallel()

	shared := column("shared")
	lambda := &ast.LambdaExpression{Body: column("body"), Bindings: []ast.Expression{shared}}
	call := &ast.FunctionCallExpression{FunctionName: "fold", Arguments: []ast.Expression{lambda, shared}}

	visits := 0
	ast.Walk(call, func(node ast.Expression) bool {
		if node == shared {
			visits++
		}
		return true
	})
	assert.Equal(t, 1, visits)
}

func TestWalk_TakesTimeInProportionToNestedCallsThatShareTheirBindings(t *testing.T) {
	nested := func(depth int) ast.Expression {
		var current ast.Expression = column("seed")
		for range depth {
			lambda := &ast.LambdaExpression{Body: column("accumulator"), Bindings: []ast.Expression{current}}
			current = &ast.FunctionCallExpression{FunctionName: "fold", Arguments: []ast.Expression{lambda, current}}
		}
		return current
	}
	walkAll := func(root ast.Expression) func() {
		return func() { ast.Walk(root, func(ast.Expression) bool { return true }) }
	}
	scaling.Linear(t, walkAll(nested(2_000)), walkAll(nested(8_000)))
}
