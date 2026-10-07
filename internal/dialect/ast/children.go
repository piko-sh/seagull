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

package ast

import (
	"iter"
	"reflect"
	"slices"
)

// walkStep says how a walk goes on after it visits a node.
type walkStep uint8

const (
	// walkDescend goes on to the children of the node just visited.
	walkDescend walkStep = iota

	// walkSkipChildren leaves out the children of the node just visited and goes on with the
	// rest of the tree.
	walkSkipChildren

	// walkStop ends the walk.
	walkStop
)

// childVisitor hands each direct child of an expression to visit and rebuilds the
// expression around what visit returns, copying a node only when one of its children
// changes.
type childVisitor struct {
	// visit receives each child that is not a nil interface, in the order Children
	// documents, and returns the child's replacement and whether to go on to the next child.
	visit func(child Expression) (Expression, bool)

	// stopped records that visit asked to stop. Every later child is kept as it is and not
	// visited.
	stopped bool
}

// rewrite returns an expression with each direct child replaced by what visit returns for
// it.
//
// Takes expression (Expression) which is the node; a nil interface or a typed-nil node
// has no children.
//
// Returns Expression which is the node itself when no child changed, or a copy holding
// the replacements.
func (visitor *childVisitor) rewrite(expression Expression) Expression {
	if IsNil(expression) {
		return expression
	}
	switch typed := expression.(type) {
	case *FunctionCallExpression:
		return visitor.rewriteCall(typed)
	case *CoalesceExpression:
		return visitor.rewriteCoalesce(typed)
	case *CastExpression:
		return visitor.rewriteCast(typed)
	case *BinaryOpExpression:
		return visitor.rewriteBinary(typed)
	case *ComparisonExpression:
		return visitor.rewriteComparison(typed)
	case *IsNullExpression:
		return visitor.rewriteIsNull(typed)
	case *InListExpression:
		return visitor.rewriteInList(typed)
	case *BetweenExpression:
		return visitor.rewriteBetween(typed)
	case *LogicalOpExpression:
		return visitor.rewriteLogical(typed)
	case *UnaryOpExpression:
		return visitor.rewriteUnary(typed)
	default:
		return visitor.rewriteCompound(expression)
	}
}

// rewriteCompound is rewrite for the kinds that hold branches, reach into another value
// or carry a list of values. Every kind it does not name is a leaf.
//
// Takes expression (Expression) which is the node, not nil.
//
// Returns Expression which is the node itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteCompound(expression Expression) Expression {
	switch typed := expression.(type) {
	case *CaseWhenExpression:
		return visitor.rewriteCase(typed)
	case *WindowFunctionExpression:
		return visitor.rewriteWindow(typed)
	case *ArraySubscriptExpression:
		return visitor.rewriteSubscript(typed)
	case *LambdaExpression:
		return visitor.rewriteLambda(typed)
	case *StructFieldAccessExpression:
		return visitor.rewriteFieldAccess(typed)
	case *NamedArgumentExpression:
		return visitor.rewriteNamedArgument(typed)
	case *RowValueExpression:
		return visitor.rewriteRow(typed)
	case *StructExpression:
		return visitor.rewriteStruct(typed)
	default:
		return expression
	}
}

// rewriteCall rewrites a function call's arguments, then its FILTER condition.
//
// Takes call (*FunctionCallExpression) which is the call, not nil.
//
// Returns Expression which is the call itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteCall(call *FunctionCallExpression) Expression {
	arguments, argumentsChanged := visitor.list(call.Arguments)
	filter, filterChanged := visitor.one(call.FilterExpression)
	if !argumentsChanged && !filterChanged {
		return call
	}
	copied := *call
	copied.Arguments = arguments
	copied.FilterExpression = filter
	return &copied
}

// rewriteCoalesce rewrites the arguments of a COALESCE.
//
// Takes coalesce (*CoalesceExpression) which is the COALESCE, not nil.
//
// Returns Expression which is the COALESCE itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteCoalesce(coalesce *CoalesceExpression) Expression {
	arguments, changed := visitor.list(coalesce.Arguments)
	if !changed {
		return coalesce
	}
	copied := *coalesce
	copied.Arguments = arguments
	return &copied
}

// rewriteCast rewrites the expression a cast converts.
//
// Takes cast (*CastExpression) which is the cast, not nil.
//
// Returns Expression which is the cast itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteCast(cast *CastExpression) Expression {
	inner, changed := visitor.one(cast.Inner)
	if !changed {
		return cast
	}
	copied := *cast
	copied.Inner = inner
	return &copied
}

// rewriteBinary rewrites a binary operator's left operand, then its right.
//
// Takes operation (*BinaryOpExpression) which is the operation, not nil.
//
// Returns Expression which is the operation itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteBinary(operation *BinaryOpExpression) Expression {
	left, right, changed := visitor.pair(operation.Left, operation.Right)
	if !changed {
		return operation
	}
	copied := *operation
	copied.Left = left
	copied.Right = right
	return &copied
}

// rewriteComparison rewrites a comparison's left operand, then its right.
//
// Takes comparison (*ComparisonExpression) which is the comparison, not nil.
//
// Returns Expression which is the comparison itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteComparison(comparison *ComparisonExpression) Expression {
	left, right, changed := visitor.pair(comparison.Left, comparison.Right)
	if !changed {
		return comparison
	}
	copied := *comparison
	copied.Left = left
	copied.Right = right
	return &copied
}

// rewriteIsNull rewrites the expression an IS NULL tests.
//
// Takes test (*IsNullExpression) which is the test, not nil.
//
// Returns Expression which is the test itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteIsNull(test *IsNullExpression) Expression {
	inner, changed := visitor.one(test.Inner)
	if !changed {
		return test
	}
	copied := *test
	copied.Inner = inner
	return &copied
}

// rewriteInList rewrites the expression an IN tests, then its values.
//
// Takes test (*InListExpression) which is the test, not nil.
//
// Returns Expression which is the test itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteInList(test *InListExpression) Expression {
	inner, innerChanged := visitor.one(test.Inner)
	values, valuesChanged := visitor.list(test.Values)
	if !innerChanged && !valuesChanged {
		return test
	}
	copied := *test
	copied.Inner = inner
	copied.Values = values
	return &copied
}

// rewriteBetween rewrites the expression a BETWEEN tests, then its low bound, then its
// high bound.
//
// Takes test (*BetweenExpression) which is the test, not nil.
//
// Returns Expression which is the test itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteBetween(test *BetweenExpression) Expression {
	inner, innerChanged := visitor.one(test.Inner)
	low, high, boundsChanged := visitor.pair(test.Low, test.High)
	if !innerChanged && !boundsChanged {
		return test
	}
	copied := *test
	copied.Inner = inner
	copied.Low = low
	copied.High = high
	return &copied
}

// rewriteLogical rewrites the operands of AND or OR.
//
// Takes operation (*LogicalOpExpression) which is the operation, not nil.
//
// Returns Expression which is the operation itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteLogical(operation *LogicalOpExpression) Expression {
	operands, changed := visitor.list(operation.Operands)
	if !changed {
		return operation
	}
	copied := *operation
	copied.Operands = operands
	return &copied
}

// rewriteUnary rewrites a unary operator's operand.
//
// Takes operation (*UnaryOpExpression) which is the operation, not nil.
//
// Returns Expression which is the operation itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteUnary(operation *UnaryOpExpression) Expression {
	operand, changed := visitor.one(operation.Operand)
	if !changed {
		return operation
	}
	copied := *operation
	copied.Operand = operand
	return &copied
}

// rewriteCase rewrites each branch of a CASE, its condition then its result, and then the
// ELSE result.
//
// Takes expression (*CaseWhenExpression) which is the CASE, not nil.
//
// Returns Expression which is the CASE itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteCase(expression *CaseWhenExpression) Expression {
	branches, branchesChanged := rewriteSlice(visitor, expression.Branches, visitor.branch)
	elseResult, elseChanged := visitor.one(expression.ElseResult)
	if !branchesChanged && !elseChanged {
		return expression
	}
	copied := *expression
	copied.Branches = branches
	copied.ElseResult = elseResult
	return &copied
}

// branch rewrites a CASE branch's condition, then its result.
//
// Takes branch (CaseWhenBranch) which is the branch.
//
// Returns CaseWhenBranch which holds the replacements.
// Returns bool which is true when either part changed.
func (visitor *childVisitor) branch(branch CaseWhenBranch) (CaseWhenBranch, bool) {
	condition, result, changed := visitor.pair(branch.Condition, branch.Result)
	return CaseWhenBranch{Condition: condition, Result: result}, changed
}

// rewriteWindow rewrites the function call a window function applies. A replacement that
// is not a function call leaves the window as it is.
//
// Takes window (*WindowFunctionExpression) which is the window function, not nil.
//
// Returns Expression which is the window itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteWindow(window *WindowFunctionExpression) Expression {
	if window.Function == nil {
		return window
	}
	replacement, changed := visitor.one(window.Function)
	call, isCall := replacement.(*FunctionCallExpression)
	if !changed || !isCall {
		return window
	}
	copied := *window
	copied.Function = call
	return &copied
}

// rewriteSubscript rewrites the subscripted value, then the index.
//
// Takes subscript (*ArraySubscriptExpression) which is the subscript, not nil.
//
// Returns Expression which is the subscript itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteSubscript(subscript *ArraySubscriptExpression) Expression {
	array, index, changed := visitor.pair(subscript.Array, subscript.Index)
	if !changed {
		return subscript
	}
	copied := *subscript
	copied.Array = array
	copied.Index = index
	return &copied
}

// rewriteLambda rewrites a lambda's body, then its bindings.
//
// Takes lambda (*LambdaExpression) which is the lambda, not nil.
//
// Returns Expression which is the lambda itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteLambda(lambda *LambdaExpression) Expression {
	body, bodyChanged := visitor.one(lambda.Body)
	bindings, bindingsChanged := visitor.list(lambda.Bindings)
	if !bodyChanged && !bindingsChanged {
		return lambda
	}
	copied := *lambda
	copied.Body = body
	copied.Bindings = bindings
	return &copied
}

// rewriteFieldAccess rewrites the struct a field access reads.
//
// Takes access (*StructFieldAccessExpression) which is the access, not nil.
//
// Returns Expression which is the access itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteFieldAccess(access *StructFieldAccessExpression) Expression {
	structure, changed := visitor.one(access.Struct)
	if !changed {
		return access
	}
	copied := *access
	copied.Struct = structure
	return &copied
}

// rewriteNamedArgument rewrites the value of an argument passed by name.
//
// Takes argument (*NamedArgumentExpression) which is the argument, not nil.
//
// Returns Expression which is the argument itself or a copy holding the replacement.
func (visitor *childVisitor) rewriteNamedArgument(argument *NamedArgumentExpression) Expression {
	value, changed := visitor.one(argument.Value)
	if !changed {
		return argument
	}
	copied := *argument
	copied.Value = value
	return &copied
}

// rewriteRow rewrites the elements of a row constructor.
//
// Takes row (*RowValueExpression) which is the row, not nil.
//
// Returns Expression which is the row itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteRow(row *RowValueExpression) Expression {
	elements, changed := visitor.list(row.Elements)
	if !changed {
		return row
	}
	copied := *row
	copied.Elements = elements
	return &copied
}

// rewriteStruct rewrites the field values of a struct constructor, keeping its names.
//
// Takes structure (*StructExpression) which is the constructor, not nil.
//
// Returns Expression which is the constructor itself or a copy holding the replacements.
func (visitor *childVisitor) rewriteStruct(structure *StructExpression) Expression {
	values, changed := visitor.list(structure.Values)
	if !changed {
		return structure
	}
	copied := *structure
	copied.Values = values
	return &copied
}

// pair passes two children to visit, the first then the second.
//
// Takes first (Expression) which is the first child.
// Takes second (Expression) which is the second child.
//
// Returns firstReplacement (Expression) which replaces the first child.
// Returns secondReplacement (Expression) which replaces the second child.
// Returns changed (bool) which is true when either replacement differs from its child.
func (visitor *childVisitor) pair(first, second Expression) (firstReplacement, secondReplacement Expression, changed bool) {
	firstReplacement, firstChanged := visitor.one(first)
	secondReplacement, secondChanged := visitor.one(second)
	return firstReplacement, secondReplacement, firstChanged || secondChanged
}

// list passes each child of a list to visit, in order.
//
// Takes children ([]Expression) which is the list.
//
// Returns []Expression which is the list itself when no child changed, or a new list
// holding the replacements.
// Returns bool which is true for a new list.
func (visitor *childVisitor) list(children []Expression) ([]Expression, bool) {
	return rewriteSlice(visitor, children, visitor.one)
}

// one passes a single child to visit, unless the child is a nil interface or the visitor
// has stopped.
//
// Takes child (Expression) which is the child.
//
// Returns Expression which is the replacement.
// Returns bool which is true when it is not the child itself.
func (visitor *childVisitor) one(child Expression) (Expression, bool) {
	if child == nil || visitor.stopped {
		return child, false
	}
	replacement, more := visitor.visit(child)
	if !more {
		visitor.stopped = true
	}
	return replacement, !sameExpression(replacement, child)
}

// Children returns the expressions directly inside an expression, in source order.
//
// A subquery is never entered: EXISTS and a scalar subquery are leaves, and InnerQuery
// gives the query they hold. A child that is a nil interface, which stands for SQL NULL,
// is skipped. A typed-nil node is yielded like any other and has no children of its own;
// IsNil detects one.
//
// The order of the children is part of the contract:
//   - a function call yields its arguments, then its FILTER condition;
//   - COALESCE yields its arguments, AND and OR their operands, a row constructor its
//     elements, and a struct constructor its field values;
//   - a cast, IS NULL, a unary operator, a struct field access and a named argument yield
//     their one operand;
//   - a binary operator and a comparison yield the left operand, then the right;
//   - IN yields the tested expression, then the values;
//   - BETWEEN yields the tested expression, then the low bound, then the high bound;
//   - CASE yields each branch's condition, then its result, then the ELSE result;
//   - a window function yields its function call, unless that is nil;
//   - a subscript yields the subscripted value, then the index;
//   - a lambda yields its body, then its bindings;
//   - a column reference, a literal, a parameter, EXISTS, a scalar subquery and an
//     unknown expression yield nothing.
//
// Takes expression (Expression) which is the node; nil has no children.
//
// Returns iter.Seq[Expression] which yields the children in order.
func Children(expression Expression) iter.Seq[Expression] {
	return func(yield func(Expression) bool) {
		visitor := &childVisitor{
			visit: func(child Expression) (Expression, bool) {
				return child, yield(child)
			},
			stopped: false,
		}
		visitor.rewrite(expression)
	}
}

// RewriteChildren returns an expression with each direct child replaced by what rewrite
// returns for it.
//
// rewrite is called once for each child Children yields, in the same order. A child that
// is a nil interface keeps its place and is not passed to it, and a replacement that is
// nil stands for SQL NULL. A window function keeps its function call unless rewrite
// returns a *FunctionCallExpression for it.
//
// A node is copied only when one of its children changes, and the copy shares every list
// whose children did not change, so a rewrite that returns each child as it is gives back
// the same pointer. The nodes and lists of the original are never modified.
//
// Takes expression (Expression) which is the node; nil and a typed-nil node come back as
// they are.
// Takes rewrite (func(Expression) Expression) which returns the replacement for a child;
// nil leaves the expression as it is.
//
// Returns Expression which is the node itself or a shallow copy holding the replacements.
func RewriteChildren(expression Expression, rewrite func(Expression) Expression) Expression {
	if rewrite == nil {
		return expression
	}
	visitor := &childVisitor{
		visit: func(child Expression) (Expression, bool) {
			return rewrite(child), true
		},
		stopped: false,
	}
	return visitor.rewrite(expression)
}

// Walk calls visit for every node of an expression tree, each parent before its children
// and the children in the order Children gives. When visit returns false for a node the
// walk leaves out that node's children and goes on with the rest of the tree.
//
// The walk keeps its own stack instead of recursing, so a tree of any depth is walked
// without exhausting the goroutine's stack. A node reachable along more than one path, as
// a lambda's binding is when it is also an argument of the call that binds it, is visited
// once, so shared nodes cannot make the walk grow exponentially. It does not enter
// subqueries; InnerQuery gives the query of EXISTS or a scalar subquery for a caller to
// walk on its own.
//
// Takes expression (Expression) which is the root; nil visits nothing.
// Takes visit (func(Expression) bool) which is called for each node and returns whether
// to walk that node's children; nil visits nothing.
func Walk(expression Expression, visit func(Expression) bool) {
	if visit == nil {
		return
	}
	walk(expression, func(node Expression) walkStep {
		if visit(node) {
			return walkDescend
		}
		return walkSkipChildren
	})
}

// Preorder returns every node of an expression tree, each parent before its children, in
// the order Walk visits them. Leaving the loop early ends the walk.
//
// Takes expression (Expression) which is the root; nil yields nothing.
//
// Returns iter.Seq[Expression] which yields the nodes in order.
func Preorder(expression Expression) iter.Seq[Expression] {
	return func(yield func(Expression) bool) {
		walk(expression, func(node Expression) walkStep {
			if yield(node) {
				return walkDescend
			}
			return walkStop
		})
	}
}

// InnerQuery returns the subquery an EXISTS or a scalar subquery holds, which Children
// and Walk do not enter.
//
// Takes expression (Expression) which is the expression.
//
// Returns *RawQueryAnalysis which is the subquery.
// Returns bool which is true when the expression is EXISTS or a scalar subquery and holds
// one.
func InnerQuery(expression Expression) (*RawQueryAnalysis, bool) {
	if IsNil(expression) {
		return nil, false
	}
	var inner *RawQueryAnalysis
	switch typed := expression.(type) {
	case *ExistsExpression:
		inner = typed.InnerQuery
	case *ScalarSubqueryExpression:
		inner = typed.InnerQuery
	default:
		return nil, false
	}
	return inner, inner != nil
}

// IsNil reports whether an expression is nil: a nil interface, which stands for SQL NULL,
// or a node whose pointer is nil, which holds nothing to read.
//
// Takes expression (Expression) which is the expression.
//
// Returns bool which is true for a nil interface or a typed-nil node.
func IsNil(expression Expression) bool {
	if expression == nil {
		return true
	}
	value := reflect.ValueOf(expression)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

// walk calls visit for every node of an expression tree, each parent before its children,
// and goes on as the step visit returns says.
//
// An explicit stack avoids recursion, and a node shared by several paths is visited only
// once.
//
// Takes expression (Expression) which is the root; nil visits nothing.
// Takes visit (func(Expression) walkStep) which is called for each node.
func walk(expression Expression, visit func(Expression) walkStep) {
	if expression == nil {
		return
	}
	var children []Expression
	collector := &childVisitor{
		visit: func(child Expression) (Expression, bool) {
			children = append(children, child)
			return child, true
		},
		stopped: false,
	}
	visited := make(map[Expression]struct{})
	stack := []Expression{expression}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack[last] = nil
		stack = stack[:last]
		if shareable(node) {
			if _, seen := visited[node]; seen {
				continue
			}
			visited[node] = struct{}{}
		}
		step := visit(node)
		if step == walkStop {
			return
		}
		if step == walkSkipChildren {
			continue
		}
		clear(children)
		children = children[:0]
		collector.rewrite(node)
		slices.Reverse(children)
		stack = append(stack, children...)
	}
}

// shareable reports whether a node can be reached along more than one path: only a
// pointer node can be held by two parents, and only a pointer is safe to use as a map key
// whatever the node's type holds.
//
// Takes node (Expression) which is a node the walk reached.
//
// Returns bool which is true for a pointer node.
func shareable(node Expression) bool {
	return reflect.TypeOf(node).Kind() == reflect.Pointer
}

// rewriteSlice rewrites each item of a slice in order, until the visitor stops.
//
// Takes visitor (*childVisitor) which says whether the rewrite has stopped.
// Takes items ([]Item) which is the slice.
// Takes rewriteItem (func(Item) (Item, bool)) which returns an item's replacement and
// whether it changed.
//
// Returns []Item which is the slice itself when no item changed, or a new slice holding
// the replacements.
// Returns bool which is true for a new slice.
func rewriteSlice[Item any](visitor *childVisitor, items []Item, rewriteItem func(Item) (Item, bool)) ([]Item, bool) {
	var rewritten []Item
	for index, item := range items {
		if visitor.stopped {
			break
		}
		replacement, changed := rewriteItem(item)
		if rewritten == nil && !changed {
			continue
		}
		if rewritten == nil {
			rewritten = slices.Clone(items)
		}
		rewritten[index] = replacement
	}
	if rewritten == nil {
		return items, false
	}
	return rewritten, true
}

// sameExpression reports whether a replacement is the very child it replaces. Two values
// of a type that cannot be compared are taken to differ, so comparing them never panics.
//
// Takes replacement (Expression) which is the replacement.
// Takes child (Expression) which is the child it replaces.
//
// Returns bool which is true when both are the same value.
func sameExpression(replacement, child Expression) bool {
	if replacement == nil || child == nil {
		return replacement == nil && child == nil
	}
	replacementType := reflect.TypeOf(replacement)
	if replacementType != reflect.TypeOf(child) || !replacementType.Comparable() {
		return false
	}
	return replacement == child
}
