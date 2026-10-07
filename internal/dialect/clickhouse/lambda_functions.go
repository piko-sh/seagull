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

package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// lambdaFunctionLanguage is the language a function created as a lambda, by CREATE
	// FUNCTION name AS x -> body, is recorded in; its body is the lambda's source text.
	lambdaFunctionLanguage = "lambda"

	// maxLambdaCallDepth caps how deeply calls to lambda functions are followed into one
	// another while typing a call, so functions that call each other cannot recurse without
	// end.
	maxLambdaCallDepth = 16

	// maxLambdaEvaluations caps how many lambda bodies typing one call may type. Typing
	// gives up with no opinion past it, so lambda functions that fan out into many
	// differently typed calls of one another cannot make one call cost without bound.
	maxLambdaEvaluations = 4096

	// maxLambdaTypedNodes caps how many expression nodes of lambda bodies typing one call
	// may type, so a few huge bodies typed again and again cannot make one call cost without
	// bound either.
	maxLambdaTypedNodes = 1 << 16
)

// lambdaTyping is what typing one call learns about the lambda functions it reaches: the
// calls already typed, the functions being typed, the bodies already parsed, and what is
// left of its budget. It lives for one call, since the catalogue may change between
// calls.
type lambdaTyping struct {
	// resolved holds the resolution of each call already typed, nil when the call could not
	// be typed, keyed by the function and its argument types.
	resolved map[lambdaCall]*ast.FunctionResolution

	// inProgress holds the functions whose bodies are being typed, so a function its own
	// body reaches again yields no type at once rather than recursing.
	inProgress map[*sqlmodel.Function]bool

	// sources holds each lambda source already parsed, keyed by its text.
	sources map[string]lambdaSource

	// evaluations counts the bodies typed so far.
	evaluations int

	// typedNodes counts the body nodes typed so far.
	typedNodes int

	// exhausted reports that the budget ran out, after which the call has no opinion.
	exhausted bool
}

// newLambdaTyping returns the empty state typing one call starts from.
//
// Returns *lambdaTyping which has typed nothing yet.
func newLambdaTyping() *lambdaTyping {
	return &lambdaTyping{
		resolved:    make(map[lambdaCall]*ast.FunctionResolution),
		inProgress:  make(map[*sqlmodel.Function]bool),
		sources:     make(map[string]lambdaSource),
		evaluations: 0,
		typedNodes:  0,
		exhausted:   false,
	}
}

// beginEvaluation charges one body against the budget before it is typed.
//
// Returns bool which is false when the context is done or the budget has run out, so the
// body is not typed.
func (typing *lambdaTyping) beginEvaluation(ctx context.Context) bool {
	if typing.exhausted || context.Cause(ctx) != nil {
		return false
	}
	if typing.evaluations >= maxLambdaEvaluations {
		typing.exhausted = true
		return false
	}
	typing.evaluations++
	return true
}

// chargeNode charges one body node against the budget before it is typed.
//
// Returns bool which is false when the budget has run out, so the node is not typed.
func (typing *lambdaTyping) chargeNode() bool {
	if typing.exhausted {
		return false
	}
	if typing.typedNodes >= maxLambdaTypedNodes {
		typing.exhausted = true
		return false
	}
	typing.typedNodes++
	return true
}

// lambdaCall identifies a call of a lambda function by the function and its argument
// types.
type lambdaCall struct {
	// function is the called function's signature.
	function *sqlmodel.Function

	// arguments renders the argument types, as writeTypeKey renders them.
	arguments string
}

// lambdaSource is a lambda's source text parsed into its parameters and body.
type lambdaSource struct {
	// body is the lambda's body.
	body ast.Expression

	// parameters holds the lambda's parameter names.
	parameters []string

	// parsed is false when the text is not a whole lambda the parser models.
	parsed bool
}

// lambdaEvaluator types a lambda body with its parameters bound to argument types.
type lambdaEvaluator struct {
	// resolver types the functions the body calls.
	resolver *functionResolver

	// typing is what typing the call being resolved has learnt so far.
	typing *lambdaTyping

	// catalogue holds the created functions the body may call.
	catalogue *sqlmodel.Catalogue

	// bindings maps each parameter to its argument's type.
	bindings map[string]sqlmodel.SQLType

	// depth is how many lambda calls enclose the body.
	depth int
}

// typeOf types one node of the body.
//
// Takes expression (ast.Expression) which is the node.
//
// Returns sqlmodel.SQLType which is its type, unknown when it cannot be told or the
// typing budget has run out.
func (e lambdaEvaluator) typeOf(ctx context.Context, expression ast.Expression) sqlmodel.SQLType {
	if !e.typing.chargeNode() {
		return unknownType()
	}
	switch node := expression.(type) {
	case *ast.LiteralExpression:
		return e.resolver.types.normalise(node.TypeName)
	case *ast.CastExpression:
		return e.resolver.types.normalise(node.TypeName)
	case *ast.ColumnRefExpression:
		if bound, found := e.bindings[node.ColumnName]; found && node.TableAlias == "" {
			return bound
		}
		return unknownType()
	case *ast.BinaryOpExpression:
		resultType, _, typed := binaryResult(node.Operator, e.typeOf(ctx, node.Left), e.typeOf(ctx, node.Right))
		if !typed {
			return unknownType()
		}
		return resultType
	case *ast.UnaryOpExpression:
		resultType, _, typed := unaryResult(node.Operator, e.typeOf(ctx, node.Operand))
		if !typed {
			return unknownType()
		}
		return resultType
	case *ast.FunctionCallExpression:
		return e.callType(ctx, node)
	case *ast.CoalesceExpression:
		return unifyBranchTypes(e.typesOf(ctx, node.Arguments))
	case *ast.ComparisonExpression, *ast.LogicalOpExpression, *ast.IsNullExpression, *ast.InListExpression, *ast.BetweenExpression:
		return primitiveType(engineNameBool)
	default:
		return unknownType()
	}
}

// typesOf types each of several nodes.
//
// Takes expressions ([]ast.Expression) which are the nodes.
//
// Returns []sqlmodel.SQLType which holds their types in order.
func (e lambdaEvaluator) typesOf(ctx context.Context, expressions []ast.Expression) []sqlmodel.SQLType {
	types := make([]sqlmodel.SQLType, len(expressions))
	for index, expression := range expressions {
		types[index] = e.typeOf(ctx, expression)
	}
	return types
}

// callType types a function the body calls: another lambda function, a function with a
// dedicated resolver, or a built-in function of the catalogue with a fixed return type.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
//
// Returns sqlmodel.SQLType which is the call's type, unknown when it cannot be told.
func (e lambdaEvaluator) callType(ctx context.Context, call *ast.FunctionCallExpression) sqlmodel.SQLType {
	argumentTypes := e.typesOf(ctx, call.Arguments)
	if resolution := e.resolver.resolveLambdaCall(ctx, e.typing, e.catalogue, call.FunctionName, argumentTypes, e.depth+1); resolution != nil {
		return resolution.ReturnType
	}
	if resolution, err := e.resolver.resolveCall(ctx, e.typing, nil, call.FunctionName, argumentTypes); err == nil && resolution != nil {
		return resolution.ReturnType
	}
	if resolution := e.resolver.resolveFromCatalogue(call.FunctionName, argumentTypes); resolution != nil {
		return resolution.ReturnType
	}
	return unknownType()
}

// resolveLambdaFunction types a call to a stored lambda using the argument types to
// resolve its body.
//
// Takes typing (*lambdaTyping) which is what typing the call has learnt so far.
// Takes catalogue (*sqlmodel.Catalogue) which holds the created functions.
// Takes name (string) which is the called function's name.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which is the call's resolution, or nil when the name is
// not a lambda function taking that many arguments or its body cannot be typed.
// Returns error which is the context's cause once it is done, or errResolverNoOpinion
// when typing ran out of budget.
func (r *functionResolver) resolveLambdaFunction(
	ctx context.Context,
	typing *lambdaTyping,
	catalogue *sqlmodel.Catalogue,
	name string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	resolution := r.resolveLambdaCall(ctx, typing, catalogue, name, argumentTypes, 0)
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if typing.exhausted {
		return nil, errResolverNoOpinion
	}
	return resolution, nil
}

// resolveLambdaCall is resolveLambdaFunction with the depth of the lambda calls being
// followed.
//
// A call already typed returns its earlier resolution, and a call of a function whose
// body is being typed returns nil at once, so recursive functions stop where they recurse
// and functions that call one another many times are typed once per argument types.
//
// Takes typing (*lambdaTyping) which is what typing the call has learnt so far.
// Takes catalogue (*sqlmodel.Catalogue) which holds the created functions.
// Takes name (string) which is the called function's name.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
// Takes depth (int) which is how many lambda calls enclose this one.
//
// Returns *ast.FunctionResolution which is the call's resolution, or nil.
func (r *functionResolver) resolveLambdaCall(
	ctx context.Context,
	typing *lambdaTyping,
	catalogue *sqlmodel.Catalogue,
	name string,
	argumentTypes []sqlmodel.SQLType,
	depth int,
) *ast.FunctionResolution {
	if depth >= maxLambdaCallDepth {
		return nil
	}
	signature := lookupLambdaFunction(catalogue, name, len(argumentTypes))
	if signature == nil || typing.inProgress[signature] {
		return nil
	}
	key, keyed := lambdaCallKey(signature, argumentTypes)
	if resolution, found := typing.resolved[key]; keyed && found {
		return resolution
	}
	if !typing.beginEvaluation(ctx) {
		return nil
	}
	typing.inProgress[signature] = true
	resolution := r.evaluateLambda(ctx, typing, catalogue, signature, argumentTypes, depth)
	delete(typing.inProgress, signature)
	if keyed && !typing.exhausted {
		typing.resolved[key] = resolution
	}
	return resolution
}

// evaluateLambda types a lambda function's body with its parameters bound to the call's
// argument types.
//
// Takes typing (*lambdaTyping) which is what typing the call has learnt so far.
// Takes catalogue (*sqlmodel.Catalogue) which holds the created functions.
// Takes signature (*sqlmodel.Function) which is the lambda function.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
// Takes depth (int) which is how many lambda calls enclose this one.
//
// Returns *ast.FunctionResolution which is the call's resolution, or nil when the body
// cannot be typed.
func (r *functionResolver) evaluateLambda(
	ctx context.Context,
	typing *lambdaTyping,
	catalogue *sqlmodel.Catalogue,
	signature *sqlmodel.Function,
	argumentTypes []sqlmodel.SQLType,
	depth int,
) *ast.FunctionResolution {
	source := r.lambdaSource(ctx, typing, signature.BodySQL)
	if !source.parsed || len(source.parameters) != len(argumentTypes) {
		return nil
	}
	evaluator := lambdaEvaluator{
		resolver:  r,
		typing:    typing,
		catalogue: catalogue,
		bindings:  make(map[string]sqlmodel.SQLType, len(source.parameters)),
		depth:     depth,
	}
	for index, parameter := range source.parameters {
		evaluator.bindings[parameter] = argumentTypes[index]
	}
	returnType := evaluator.typeOf(ctx, source.body)
	if returnType.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return newReadOnlyResolution(returnType)
}

// lambdaSource returns a lambda's source text parsed, parsing each text once for one
// call.
//
// Takes typing (*lambdaTyping) which holds the texts already parsed.
// Takes text (string) which is the lambda's source text.
//
// Returns lambdaSource which is the parsed lambda.
func (r *functionResolver) lambdaSource(ctx context.Context, typing *lambdaTyping, text string) lambdaSource {
	if source, found := typing.sources[text]; found {
		return source
	}
	parameters, body, parsed := r.parseLambdaSource(ctx, text)
	source := lambdaSource{body: body, parameters: parameters, parsed: parsed}
	typing.sources[text] = source
	return source
}

// parseLambdaSource parses a lambda's source text, `x -> body` or `(x, y) -> body`, with
// the dialect's types and parse limits. The parse stops once the context is done, as a
// statement's does.
//
// Takes source (string) which is the lambda's text.
//
// Returns []string which holds the parameter names.
// Returns ast.Expression which is the body.
// Returns bool which is false when the text is not a whole lambda, holds more tokens than
// the dialect's per-statement budget, or the context is done.
func (r *functionResolver) parseLambdaSource(ctx context.Context, source string) ([]string, ast.Expression, bool) {
	statements, err := dialectkit.LexStatements(ctx, lexical.Rules(), source, statementTokens, tokenEOF, r.limits.TokensPerStatement())
	if err != nil || len(statements) != 1 || statements[0].OverBudget {
		return nil, nil, false
	}
	tokens := statements[0].Tokens
	lambdaParser := newParser(tokens)
	lambdaParser.Interrupt = dialectkit.WalkGuard(ctx, len(tokens))
	lambdaParser.maxParseDepth = r.limits.ParseDepth()
	lambdaParser.types = r.types
	body, parameters, err := lambdaParser.parseCreateFunctionLambdaBody()
	if err != nil || !lambdaParser.AtEnd() || containsUnknownExpression(body) {
		return nil, nil, false
	}
	return parameters, body, true
}

// lookupLambdaFunction finds the created lambda function of a name that takes a number of
// arguments, searching the catalogue's search path in order.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the created functions; nil finds
// nothing.
// Takes name (string) which is the function's name.
// Takes argumentCount (int) which is the number of arguments the call passes.
//
// Returns *sqlmodel.Function which is the function, or nil.
func lookupLambdaFunction(catalogue *sqlmodel.Catalogue, name string, argumentCount int) *sqlmodel.Function {
	for _, overloads := range catalogue.FunctionsOnPath("", name) {
		for _, signature := range overloads {
			if signature != nil && signature.Language == lambdaFunctionLanguage && len(signature.Arguments) == argumentCount {
				return signature
			}
		}
	}
	return nil
}

// lambdaCallKey identifies a call of a lambda function by the function and its argument
// types.
//
// Takes function (*sqlmodel.Function) which is the called function.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns lambdaCall which identifies the call.
// Returns bool which is false when an argument type nests too deeply to render, so the
// call is typed without being remembered.
func lambdaCallKey(function *sqlmodel.Function, argumentTypes []sqlmodel.SQLType) (lambdaCall, bool) {
	var builder strings.Builder
	for index := range argumentTypes {
		if !writeTypeKey(&builder, &argumentTypes[index], 0) {
			return lambdaCall{function: nil, arguments: ""}, false
		}
		builder.WriteByte(';')
	}
	return lambdaCall{function: function, arguments: builder.String()}, true
}

// writeTypeKey renders a type so that two types render alike only when they are alike:
// the types it is built from by their own rendering, and every other field as Go syntax
// prints it, so a field added to the type later still tells two types apart.
//
// Takes builder (*strings.Builder) which receives the rendering.
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes depth (int) which is how deeply the type is nested in the argument rendered.
//
// Returns bool which is false when the type nests sqlmodel.MaxTypeDepth levels or more.
func writeTypeKey(builder *strings.Builder, sqlType *sqlmodel.SQLType, depth int) bool {
	if depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	scalars := *sqlType
	scalars.Precision, scalars.Scale, scalars.Length = nil, nil, nil
	scalars.ElementType, scalars.KeyType = nil, nil
	scalars.StructFields, scalars.UnionMembers = nil, nil
	_, _ = fmt.Fprintf(builder, "%#v", scalars)
	for _, modifier := range []*int{sqlType.Precision, sqlType.Scale, sqlType.Length} {
		if modifier == nil {
			builder.WriteString("|-")
			continue
		}
		_, _ = fmt.Fprintf(builder, "|%d", *modifier)
	}
	for _, nested := range []*sqlmodel.SQLType{sqlType.ElementType, sqlType.KeyType} {
		if !writeNestedTypeKey(builder, "", nested, depth) {
			return false
		}
	}
	for index := range sqlType.StructFields {
		field := &sqlType.StructFields[index]
		if !writeNestedTypeKey(builder, fmt.Sprintf("field %q %t ", field.Name, field.Positional), &field.SQLType, depth) {
			return false
		}
	}
	for index := range sqlType.UnionMembers {
		member := &sqlType.UnionMembers[index]
		if !writeNestedTypeKey(builder, fmt.Sprintf("member %q ", member.Tag), &member.SQLType, depth) {
			return false
		}
	}
	return true
}

// writeNestedTypeKey renders a type another is built from, in parentheses after a label
// that says what part of it the type is.
//
// Takes builder (*strings.Builder) which receives the rendering.
// Takes label (string) which names the part, such as a field and its name.
// Takes nested (*sqlmodel.SQLType) which is the type of the part; nil renders as empty
// parentheses.
// Takes depth (int) which is how deeply the type holding the part is nested.
//
// Returns bool which is false when the part nests too deeply to render.
func writeNestedTypeKey(builder *strings.Builder, label string, nested *sqlmodel.SQLType, depth int) bool {
	builder.WriteByte('|')
	builder.WriteString(label)
	builder.WriteByte('(')
	if nested != nil && !writeTypeKey(builder, nested, depth+1) {
		return false
	}
	builder.WriteByte(')')
	return true
}

// normaliseBuiltinTypeName resolves a type name through the built-in type grammar, or to
// an unknown type when it does not parse.
//
// Takes name (string) which is the type name.
//
// Returns sqlmodel.SQLType which is the type.
func normaliseBuiltinTypeName(name string) sqlmodel.SQLType {
	return newTypeResolver(nil, nil, defaultMaxTypeParseDepth).normalise(name)
}
