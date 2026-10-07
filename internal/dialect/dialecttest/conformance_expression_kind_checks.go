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

package dialecttest

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

// maxWalkDepth bounds how deeply the walker follows nested queries and expressions.
const maxWalkDepth = 64

// foreignSyntaxStatements write expression syntax some engines have and others lack, such
// as subscripts, lambdas, field access on a parenthesised value and struct constructors.
// A dialect whose engine lacks one must reject the statement rather than analyse it into
// an expression kind its Info does not declare.
var foreignSyntaxStatements = []string{
	"SELECT (SELECT id FROM items)[1] FROM items",
	"SELECT name[1] FROM items",
	"SELECT id FROM items WHERE name[1] = 'a'",
	"SELECT id FROM items ORDER BY name[1]",
	"SELECT transform(name, x -> x) FROM items",
	"SELECT (name).first FROM items",
	"SELECT id FROM items WHERE (name).first = 'a'",
	"SELECT id || name FROM items",
	"SELECT id FROM items WHERE name -> 'a' = 'b'",
	"SELECT id FROM items WHERE EXISTS (SELECT 1 FROM items)",
	"SELECT (SELECT max(id) FROM items) FROM items",
	"SELECT row_number() OVER (ORDER BY id) FROM items",
	"SELECT id & 1, id | 2 FROM items",
	"SELECT {'a': id} FROM items",
	"SELECT struct_pack(a := id) FROM items",
}

// expressionKindSubject is one statement whose expressions the expression-kind check
// examines.
type expressionKindSubject struct {
	// analysis is the dialect's analysis of the statement.
	analysis *ast.RawQueryAnalysis

	// label names the statement in messages.
	label string
}

// expressionKindWalker collects the undeclared features of the expressions it walks.
type expressionKindWalker struct {
	// info declares the dialect's expression kinds.
	info *dialect.Info

	// seen records the features already reported.
	seen map[ast.ExpressionFeature]bool

	// found lists the undeclared features, in the order first met.
	found []ast.ExpressionFeature
}

// analysis walks the expressions of an analysis and of the queries nested in it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis, or nil.
// Takes depth (int) which is how deeply the walk has nested.
func (walker *expressionKindWalker) analysis(analysis *ast.RawQueryAnalysis, depth int) {
	if analysis == nil || depth > maxWalkDepth {
		return
	}
	for index := range analysis.OutputColumns {
		walker.expression(analysis.OutputColumns[index].Expression, depth+1)
	}
	for index := range analysis.ClauseExpressions {
		walker.expression(analysis.ClauseExpressions[index].Expression, depth+1)
	}
	for index := range analysis.CTEDefinitions {
		walker.analysis(analysis.CTEDefinitions[index].Query, depth+1)
	}
	for index := range analysis.RawDerivedTables {
		walker.analysis(analysis.RawDerivedTables[index].InnerQuery, depth+1)
	}
	for index := range analysis.CompoundBranches {
		walker.analysis(analysis.CompoundBranches[index].Query, depth+1)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		walker.analysis(subquery, depth+1)
	}
	walker.analysis(analysis.InsertSelect, depth+1)
}

// expression notes the feature an expression needs, when undeclared, and walks the
// expressions and queries nested in it.
//
// Takes expression (ast.Expression) which is the expression, or nil.
// Takes depth (int) which is how deeply the walk has nested.
func (walker *expressionKindWalker) expression(expression ast.Expression, depth int) {
	if expression == nil || depth > maxWalkDepth {
		return
	}
	if feature := ast.RequiredFeature(expression, walker.info.OperatorFeatures); feature != 0 &&
		!walker.info.Expressions.Has(feature) && !walker.seen[feature] {
		walker.seen[feature] = true
		walker.found = append(walker.found, feature)
	}
	switch typed := expression.(type) {
	case *ast.ExistsExpression:
		walker.analysis(typed.InnerQuery, depth+1)
	case *ast.ScalarSubqueryExpression:
		walker.analysis(typed.InnerQuery, depth+1)
	default:
		for _, child := range nestedExpressions(expression) {
			walker.expression(child, depth+1)
		}
	}
}

// checkExpressionKindsFollowInfo checks that a dialect never analyses a statement into an
// expression kind its Info.Expressions does not declare, so syntax the engine lacks is
// rejected where the dialect reads it rather than reported as an unsupported expression
// later.
//
// It examines statements written in syntax some engines lack, and the statements of the
// fixture's span corpora.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkExpressionKindsFollowInfo(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := new(engine.Info())
	subjects := foreignSyntaxSubjects(ctx, engine)
	if fixture.Spans != nil {
		corpus := collectSpanCorpus(ctx, fixture)
		for _, subject := range corpus.subjects {
			subjects = append(subjects, expressionKindSubject{analysis: subject.analysis, label: subject.label})
		}
	}
	var problems []string
	for _, subject := range subjects {
		for _, feature := range undeclaredFeatures(subject.analysis, info) {
			problems = append(problems, fmt.Sprintf("%s holds a %s, which Info.Expressions does not declare", subject.label, feature))
		}
	}
	return outcomeOf(problems)
}

// foreignSyntaxSubjects analyses each statement written in syntax some engines lack,
// keeping those the dialect accepts.
//
// Takes engine (dialect.DialectPort) which parses and analyses.
//
// Returns []expressionKindSubject which holds the statements the dialect accepted.
func foreignSyntaxSubjects(ctx context.Context, engine dialect.DialectPort) []expressionKindSubject {
	subjects := make([]expressionKindSubject, 0, len(foreignSyntaxStatements))
	for _, statement := range foreignSyntaxStatements {
		parsed, err := engine.ParseStatements(ctx, statement)
		if err != nil || len(parsed) != 1 {
			continue
		}
		analysis, err := engine.AnalyseQuery(ctx, nil, parsed[0])
		if err != nil || analysis == nil {
			continue
		}
		subjects = append(subjects, expressionKindSubject{analysis: analysis, label: fmt.Sprintf("%q", statement)})
	}
	return subjects
}

// undeclaredFeatures walks every expression of an analysis, those of its nested queries
// included, and returns each feature an expression needs that the dialect does not
// declare, once.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to walk.
// Takes info (*dialect.Info) which declares the dialect's expression kinds.
//
// Returns []ast.ExpressionFeature which holds the undeclared features found.
func undeclaredFeatures(analysis *ast.RawQueryAnalysis, info *dialect.Info) []ast.ExpressionFeature {
	walker := &expressionKindWalker{info: info, seen: make(map[ast.ExpressionFeature]bool), found: nil}
	walker.analysis(analysis, 0)
	return walker.found
}

// nestedExpressions returns the expressions written directly inside an expression.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []ast.Expression which holds them, some possibly nil.
func nestedExpressions(expression ast.Expression) []ast.Expression {
	switch typed := expression.(type) {
	case *ast.FunctionCallExpression:
		return append([]ast.Expression{typed.FilterExpression}, typed.Arguments...)
	case *ast.WindowFunctionExpression:
		if typed.Function == nil {
			return nil
		}
		return []ast.Expression{typed.Function}
	case *ast.BinaryOpExpression:
		return []ast.Expression{typed.Left, typed.Right}
	case *ast.ComparisonExpression:
		return []ast.Expression{typed.Left, typed.Right}
	case *ast.UnaryOpExpression:
		return []ast.Expression{typed.Operand}
	case *ast.LogicalOpExpression:
		return typed.Operands
	case *ast.RowValueExpression:
		return typed.Elements
	case *ast.StructExpression:
		return typed.Values
	case *ast.CoalesceExpression:
		return typed.Arguments
	case *ast.CaseWhenExpression:
		return caseWhenExpressions(typed)
	default:
		return accessExpressions(expression)
	}
}

// caseWhenExpressions returns the conditions and results of a CASE expression.
//
// Takes expression (*ast.CaseWhenExpression) which is the CASE expression.
//
// Returns []ast.Expression which holds them.
func caseWhenExpressions(expression *ast.CaseWhenExpression) []ast.Expression {
	nested := make([]ast.Expression, 0, len(expression.Branches)*2+1)
	nested = append(nested, expression.ElseResult)
	for _, branch := range expression.Branches {
		nested = append(nested, branch.Condition, branch.Result)
	}
	return nested
}

// accessExpressions returns the expressions written inside a cast, a test or an access
// expression.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []ast.Expression which holds them, or nil for an expression that holds none.
func accessExpressions(expression ast.Expression) []ast.Expression {
	switch typed := expression.(type) {
	case *ast.CastExpression:
		return []ast.Expression{typed.Inner}
	case *ast.IsNullExpression:
		return []ast.Expression{typed.Inner}
	case *ast.InListExpression:
		return append([]ast.Expression{typed.Inner}, typed.Values...)
	case *ast.BetweenExpression:
		return []ast.Expression{typed.Inner, typed.Low, typed.High}
	case *ast.ArraySubscriptExpression:
		return []ast.Expression{typed.Array, typed.Index}
	case *ast.LambdaExpression:
		return []ast.Expression{typed.Body}
	case *ast.StructFieldAccessExpression:
		return []ast.Expression{typed.Struct}
	case *ast.NamedArgumentExpression:
		return []ast.Expression{typed.Value}
	default:
		return nil
	}
}
