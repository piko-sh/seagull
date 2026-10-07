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

package resolve

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// argumentScoreExactMatch holds the score awarded when an argument type matches the
	// expected parameter type exactly.
	argumentScoreExactMatch = 3
)

// FunctionResolver matches function calls against their overloaded signatures. It scores
// argument types and selects the most specific match.
//
// A function a migration creates is keyed by the schema that holds it as well as its
// name, so same-named functions in two schemas never replace each other: a qualified call
// reaches its schema's function, and an unqualified call the function of the first schema
// on the search path, ahead of a built-in of the same signature.
//
// A call reaches a created function only when the catalogue's identifier rules give the
// called name the key the function is filed under, so PostgreSQL's quoted "Foo"() and
// unquoted foo() reach different functions, while a built-in is reached by its name in
// any letter case.
type FunctionResolver struct {
	// functions maps lower-cased function names to their candidates, built-ins first in the
	// order the dialect lists them, then the catalogue's in schema order.
	functions map[string][]functionCandidate

	// catalogue holds the user-defined schema catalogue used for fallback resolution.
	catalogue *sqlmodel.Catalogue

	// engine holds the engine type system used for implicit cast checks and engine-specific
	// function resolution.
	engine dialect.TypeSystemPort
}

// NewFunctionResolver creates a function resolver by merging built-in functions from the
// engine with user-defined functions from the catalogue. User-defined functions with the
// same name and argument types override built-ins.
//
// Takes builtins (*ast.FunctionCatalogue) which holds the engine's built-in function
// signatures.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined schema objects including
// functions.
// Takes engine (dialect.TypeSystemPort) which provides implicit cast checks and optional
// engine-level function resolution.
//
// Returns *FunctionResolver which holds the merged function map ready for overload
// resolution.
func NewFunctionResolver(
	builtins *ast.FunctionCatalogue,
	catalogue *sqlmodel.Catalogue,
	engine dialect.TypeSystemPort,
) *FunctionResolver {
	merged := make(map[string][]functionCandidate)

	mergeBuiltinFunctions(merged, builtins)
	mergeCatalogueFunctions(merged, catalogue)

	return &FunctionResolver{
		functions: merged,
		catalogue: catalogue,
		engine:    engine,
	}
}

// NamedArgumentType returns the declared type of the function's parameter of a name, for
// a placeholder a call passes by that name. It returns a type only when every overload
// the call can reach that has the parameter declares the same type for it.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes argumentName (string) which is the parameter's name.
//
// Returns sqlmodel.SQLType which is the declared type when resolved.
// Returns bool which is true when a unique type was found.
func (r *FunctionResolver) NamedArgumentType(name string, argumentName string) (sqlmodel.SQLType, bool) {
	schema, bareName := splitFunctionName(name)
	var resolved *sqlmodel.SQLType
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		slot := namedArgumentSlot(candidate, argumentName)
		if slot < 0 {
			continue
		}
		argumentType := candidate.Arguments[slot].Type
		if resolved != nil && !functionArgumentTypesEqual(*resolved, argumentType) {
			return sqlmodel.SQLType{}, false
		}
		resolved = new(argumentType)
	}
	if resolved == nil {
		return sqlmodel.SQLType{}, false
	}
	return *resolved, true
}

// ArgumentType returns the declared type of the ordinal-th argument of the function.
//
// It back-propagates a declared parameter type onto a placeholder passed as that arg (for
// example $1 in get_pages_with_latest_version($3, $1, $2)). The call site rarely knows
// the other argument types, so this does not score overloads. It returns a type only when
// the name (optionally schema-qualified) resolves to a single applicable overload, or
// when every applicable overload declares the same type at that ordinal. Otherwise it
// returns false and the caller keeps the conservative unknown fallback.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
//
// Returns sqlmodel.SQLType which is the declared argument type when resolved.
// Returns bool which is true when a unique applicable argument type was found.
func (r *FunctionResolver) ArgumentType(name string, ordinal int) (sqlmodel.SQLType, bool) {
	return r.argumentTypeAmong(name, ordinal, -1)
}

// ArgumentTypeAtArity is ArgumentType for a call of a known number of arguments: only the
// overloads that accept that many arguments are considered, so a call whose argument
// count picks one overload, as pg_advisory_lock($1) picks the one taking a bigint, types
// a placeholder passed to it.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
// Takes arity (int) which is the number of arguments the call passes.
//
// Returns sqlmodel.SQLType which is the declared argument type when resolved.
// Returns bool which is true when a unique applicable argument type was found.
func (r *FunctionResolver) ArgumentTypeAtArity(name string, ordinal, arity int) (sqlmodel.SQLType, bool) {
	return r.argumentTypeAmong(name, ordinal, arity)
}

// ArgumentTypeAmongExactMatches types an untyped argument from the overloads of the
// call's argument count that match the most of its typed arguments exactly, as PostgreSQL
// types mod($1, i4) by the integer overload.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the untyped argument's zero-based position.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the call's argument types in
// order, unknown where an argument is untyped.
//
// Returns sqlmodel.SQLType which is the type the kept overloads give the position.
// Returns bool which is false when no typed argument matches an overload exactly, or the
// kept overloads disagree.
func (r *FunctionResolver) ArgumentTypeAmongExactMatches(name string, ordinal int, argumentTypes []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if ordinal < 0 || ordinal >= len(argumentTypes) {
		return sqlmodel.SQLType{}, false
	}
	schema, bareName := splitFunctionName(name)
	best, agreed := 0, false
	var kept sqlmodel.SQLType
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		target, ok := functionArgumentSlot(candidate, ordinal)
		if !ok || !acceptsArgumentCount(candidate, len(argumentTypes)) {
			continue
		}
		matches := exactArgumentMatches(candidate, ordinal, argumentTypes)
		switch {
		case matches > best:
			best, kept, agreed = matches, candidate.Arguments[target].Type, true
		case matches == best && best > 0:
			agreed = agreed && functionArgumentTypesEqual(kept, candidate.Arguments[target].Type)
		}
	}
	if best == 0 || !agreed {
		return sqlmodel.SQLType{}, false
	}
	return kept, true
}

// SharesArgumentType reports whether every reachable overload accepts an untyped argument
// at a position and shares its type with other untyped arguments and the result.
// Functions with an independent result type do not qualify.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
//
// Returns bool which is true when every such overload shares the argument's type with its
// result.
func (r *FunctionResolver) SharesArgumentType(name string, ordinal int) bool {
	if ordinal < 0 {
		return false
	}
	schema, bareName := splitFunctionName(name)
	shares := false
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		slot, ok := functionArgumentSlot(candidate, ordinal)
		if !ok {
			continue
		}
		if !sharesResultType(candidate, slot) {
			return false
		}
		shares = true
	}
	return shares
}

// ArgumentName returns the name the function's signature gives the argument at a
// call-site position, so a placeholder passed there can be named after it.
//
// It returns a name only when every overload the call can reach that accepts an argument
// at the position names it the same; overloads that name it differently, or leave it
// unnamed, give no name.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
//
// Returns string which is the argument's name.
// Returns bool which is true when a single name was found.
func (r *FunctionResolver) ArgumentName(name string, ordinal int) (string, bool) {
	return r.argumentNameAmong(name, ordinal, -1)
}

// ArgumentNameAtArity is ArgumentName for a call of a known number of arguments: only the
// overloads that accept that many arguments are considered.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
// Takes arity (int) which is the number of arguments the call passes.
//
// Returns string which is the argument's name.
// Returns bool which is true when a single name was found.
func (r *FunctionResolver) ArgumentNameAtArity(name string, ordinal, arity int) (string, bool) {
	return r.argumentNameAmong(name, ordinal, arity)
}

// ArgumentAcceptsNull reports whether a function signature permits NULL at an argument
// position.
//
// Strict functions and signatures with unspecified behaviour keep arguments non-null.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
//
// Returns bool which is true when every reachable overload accepting an argument at the
// position takes NULL there as a value.
func (r *FunctionResolver) ArgumentAcceptsNull(name string, ordinal int) bool {
	if ordinal < 0 {
		return false
	}
	schema, bareName := splitFunctionName(name)
	accepts := false
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		slot, ok := functionArgumentSlot(candidate, ordinal)
		if !ok {
			continue
		}
		if !argumentAcceptsNull(candidate, slot) {
			return false
		}
		accepts = true
	}
	return accepts
}

// argumentTypeAmong returns the declared type every applicable overload of a function
// gives an argument position, an overload applying when it takes an argument there and,
// for a known arity, accepts that many arguments.
//
// When the overloads of a call of known arity disagree, an engine implementing
// dialect.UntypedArgumentPort may still choose the type.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
// Takes arity (int) which is the number of arguments the call passes, or -1 when unknown.
//
// Returns sqlmodel.SQLType which is the declared argument type when resolved.
// Returns bool which is true when a unique applicable argument type was found.
func (r *FunctionResolver) argumentTypeAmong(name string, ordinal, arity int) (sqlmodel.SQLType, bool) {
	if ordinal < 0 {
		return sqlmodel.SQLType{}, false
	}
	schema, bareName := splitFunctionName(name)
	candidates := r.visibleCandidates(bareName, schema)
	if len(candidates) == 0 {
		return sqlmodel.SQLType{}, false
	}

	var applicable []sqlmodel.SQLType
	agreed := true
	for _, candidate := range candidates {
		if arity >= 0 && !acceptsArgumentCount(candidate, arity) {
			continue
		}
		slot, ok := functionArgumentSlot(candidate, ordinal)
		if !ok {
			continue
		}
		argumentType := candidate.Arguments[slot].Type
		if len(applicable) > 0 && !functionArgumentTypesEqual(applicable[0], argumentType) {
			agreed = false
		}
		applicable = append(applicable, argumentType)
	}

	switch {
	case len(applicable) == 0:
		return sqlmodel.SQLType{}, false
	case agreed:
		return applicable[0], true
	case arity < 0:
		return sqlmodel.SQLType{}, false
	}
	chooser, chooses := r.engine.(dialect.UntypedArgumentPort)
	if !chooses {
		return sqlmodel.SQLType{}, false
	}
	return chooser.UntypedArgumentType(applicable)
}

// argumentNameAmong returns the name every applicable overload of a function gives an
// argument position, an overload applying when it takes an argument there and, for a
// known arity, accepts that many arguments.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the zero-based call-site argument slot.
// Takes arity (int) which is the number of arguments the call passes, or -1 when unknown.
//
// Returns string which is the argument's name.
// Returns bool which is true when a single name was found.
func (r *FunctionResolver) argumentNameAmong(name string, ordinal, arity int) (string, bool) {
	if ordinal < 0 {
		return "", false
	}
	schema, bareName := splitFunctionName(name)
	argumentName := ""
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		if arity >= 0 && !acceptsArgumentCount(candidate, arity) {
			continue
		}
		slot, ok := functionArgumentSlot(candidate, ordinal)
		if !ok {
			continue
		}
		declared := candidate.Arguments[slot].Name
		if declared == "" || (argumentName != "" && !strings.EqualFold(argumentName, declared)) {
			return "", false
		}
		argumentName = declared
	}
	return argumentName, argumentName != ""
}

// resolveCall finds the best matching overload for a call whose arguments may be passed
// by name: positional arguments fill the leading parameters and each named argument the
// parameter of its name, and an overload lacking a named parameter, or leaving a required
// one unfilled, does not match.
//
// When no overload matches, the dialect's own resolver is asked; a panic it recovered is
// the call's problem in place of the unknown-function one.
//
// Takes name (string) which is the function name to resolve.
// Takes schema (string) which is the schema qualifier, or empty for unqualified calls.
// Takes arguments ([]callArgument) which holds the call's arguments in order.
//
// Returns *FunctionMatch which holds the resolved overload metadata, or nil on failure.
// Returns *Error which is a Q005 error when no matching overload is found, or a Q070
// error when the dialect's resolver panicked.
// Returns error which is the interruption that stopped the dialect's resolver, or nil.
func (r *FunctionResolver) resolveCall(
	ctx context.Context,
	name string,
	schema string,
	arguments []callArgument,
) (*FunctionMatch, *Error, error) {
	argumentTypes := make([]sqlmodel.SQLType, len(arguments))
	for index := range arguments {
		argumentTypes[index] = arguments[index].sqlType
	}
	engineArgumentTypes := nullableArgumentTypes(arguments)

	candidates := r.visibleCandidates(name, schema)
	var bestMatch *sqlmodel.Function
	if len(candidates) > 0 {
		bestMatch = r.findBestCandidate(candidates, arguments)
	}
	if bestMatch == nil {
		resolved, problem, err := r.tryEngineResolver(ctx, name, schema, engineArgumentTypes)
		switch {
		case err != nil:
			return nil, nil, err
		case resolved != nil:
			return resolved, nil, nil
		case problem != nil:
			return nil, problem, nil
		case len(candidates) == 0:
			return nil, newError(diag.CodeUnknownFunction, "unknown function %q", name), nil
		default:
			return nil, newError(diag.CodeUnknownFunction, "no matching overload for function %q with %d arguments",
				name, len(arguments)), nil
		}
	}

	match, err := r.matchCandidate(ctx, bestMatch, name, schema, argumentTypes, engineArgumentTypes)
	if err != nil {
		return nil, nil, err
	}
	if r.conversionMayGiveNull(bestMatch, arguments) {
		match.nullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	}
	return match, nil, nil
}

// matchCandidate types a call of the overload it matched: by the overload's return type,
// by the argument the overload names as giving it, or, when the overload leaves it
// unknown, by the engine's own resolver where it types the call.
//
// A panic the engine's resolver recovered leaves the overload's own typing, with the
// panic as the match's notice.
//
// Takes candidate (*sqlmodel.Function) which is the matched overload.
// Takes name (string) which is the function name.
// Takes schema (string) which is the schema qualifier, or empty.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the call's argument types.
// Takes engineArgumentTypes ([]sqlmodel.SQLType) which holds them as the engine's
// resolver sees them, each marked nullable when the argument may be NULL.
//
// Returns *FunctionMatch which types the call.
// Returns error which is the interruption that stopped the engine's resolver, or nil.
func (r *FunctionResolver) matchCandidate(
	ctx context.Context,
	candidate *sqlmodel.Function,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
	engineArgumentTypes []sqlmodel.SQLType,
) (*FunctionMatch, error) {
	var notice *Error
	returnType := candidate.ReturnType
	if position := candidate.ReturnTypeFromArgument; position > 0 && position <= len(argumentTypes) &&
		argumentTypes[position-1].Category != sqlmodel.TypeCategoryUnknown {
		returnType = argumentTypes[position-1]
	} else if returnType.Category == sqlmodel.TypeCategoryUnknown && returnType.EngineName == "" {
		resolved, problem, err := r.tryEngineResolver(ctx, name, schema, engineArgumentTypes)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			return resolved, nil
		}
		notice = problem
	}

	return &FunctionMatch{
		returnType:        returnType,
		nullableBehaviour: candidate.NullableBehaviour,
		nullingArgument:   candidate.NullingArgument,
		dataAccess:        candidate.DataAccess,
		isAggregate:       candidate.IsAggregate,
		returnsSet:        candidate.ReturnsSet,
		elementsKnown:     false,
		notice:            notice,
	}, nil
}

// conversionMayGiveNull reports whether argument conversion can make a function result
// nullable.
//
// Arguments are matched to overload parameters by their names or positions.
//
// Takes candidate (*sqlmodel.Function) which is the matched overload.
// Takes arguments ([]callArgument) which holds the call's arguments in order.
//
// Returns bool which is true when such a conversion may make the call NULL.
func (r *FunctionResolver) conversionMayGiveNull(candidate *sqlmodel.Function, arguments []callArgument) bool {
	if len(candidate.Arguments) == 0 {
		return false
	}
	if slices.ContainsFunc(arguments, func(argument callArgument) bool { return argument.name != "" }) {
		slots, _ := arrangeArguments(candidate, arguments)
		for slot := range slots {
			if r.convertsToNull(candidate.Arguments[slot].Type, slots[slot]) {
				return true
			}
		}
		return false
	}
	for position := range arguments {
		if r.convertsToNull(candidate.Arguments[min(position, len(candidate.Arguments)-1)].Type, arguments[position]) {
			return true
		}
	}
	return false
}

// convertsToNull reports whether an argument matches its parameter only through a
// conversion the engine makes as a function argument, giving NULL for a value it cannot
// convert.
//
// Takes expected (sqlmodel.SQLType) which is the parameter's declared type.
// Takes argument (callArgument) which is the argument.
//
// Returns bool which is true when the conversion may give NULL.
func (r *FunctionResolver) convertsToNull(expected sqlmodel.SQLType, argument callArgument) bool {
	if r.castScore(expected, argument.sqlType) > 0 {
		return false
	}
	coercion, coerced := r.coerceArgument(argument, expected)
	return coerced && coercion.MayBeNull
}

// visibleCandidates returns the functions a call can reach, in the order used to break
// ties.
//
// Qualified calls search their schema and then the built-ins. Unqualified calls search
// schemas in search-path order, then the built-ins. Catalogue functions follow the
// catalogue's identifier matching rules; built-in names are matched without regard to
// case.
//
// Takes name (string) which is the called function's unqualified name.
// Takes schema (string) which is the call's schema qualifier, or empty.
//
// Returns []*sqlmodel.Function which holds the reachable overloads in order.
func (r *FunctionResolver) visibleCandidates(name string, schema string) []*sqlmodel.Function {
	candidates := r.functions[strings.ToLower(name)]
	key := r.catalogue.Key(name)
	visible := make([]functionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if (candidate.builtin || candidate.key == key) && r.reaches(candidate, schema) {
			visible = append(visible, candidate)
		}
	}
	if r.catalogue != nil {
		searchPath := r.catalogue.SchemaNamesFor(schema)
		slices.SortStableFunc(visible, func(left, right functionCandidate) int {
			return cmp.Compare(searchPathRank(searchPath, left), searchPathRank(searchPath, right))
		})
	}
	functions := make([]*sqlmodel.Function, len(visible))
	for index := range visible {
		functions[index] = visible[index].function
	}
	return functions
}

// reaches reports whether a call qualified by schema, or unqualified when schema is
// empty, can reach a candidate.
//
// Schema names are compared using the catalogue's identifier rules. In a case-sensitive
// dialect, names differing only in letter case refer to different schemas.
//
// Takes candidate (functionCandidate) which is the overload.
// Takes schema (string) which is the call's schema qualifier, or empty.
//
// Returns bool which is true when the call reaches the candidate.
func (r *FunctionResolver) reaches(candidate functionCandidate, schema string) bool {
	switch {
	case schema == "" && (candidate.builtin || r.catalogue == nil):
		return true
	case schema == "":
		return r.catalogue.OnSearchPath(candidate.schema)
	case candidate.builtin && candidate.schema == "":
		return true
	default:
		return r.catalogue.Key(candidate.schema) == r.catalogue.Key(schema)
	}
}

// namedArgumentAcceptsNull reports whether every reachable overload with a parameter of a
// name takes NULL there as a value, as ArgumentAcceptsNull does for a position.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes argumentName (string) which is the parameter's name.
//
// Returns bool which is true when NULL is a value for the parameter.
func (r *FunctionResolver) namedArgumentAcceptsNull(name string, argumentName string) bool {
	schema, bareName := splitFunctionName(name)
	accepts := false
	for _, candidate := range r.visibleCandidates(bareName, schema) {
		slot := namedArgumentSlot(candidate, argumentName)
		if slot < 0 {
			continue
		}
		if !argumentAcceptsNull(candidate, slot) {
			return false
		}
		accepts = true
	}
	return accepts
}

// findBestCandidate scores each candidate overload against the actual argument types and
// returns the highest-scoring match.
//
// When two viable overloads tie on both total score and exact-match count the first such
// candidate in iteration order is kept (the strict greater-than comparisons below never
// replace an equal incumbent), so a residual tie resolves to the earliest-considered
// overload rather than erroring.
//
// Takes candidates ([]*sqlmodel.Function) which holds the reachable overloads to
// evaluate, in tie-break order.
// Takes arguments ([]callArgument) which holds the call-site arguments.
//
// Returns *sqlmodel.Function which is the best matching overload, or nil if no candidate
// is viable.
func (r *FunctionResolver) findBestCandidate(
	candidates []*sqlmodel.Function,
	arguments []callArgument,
) *sqlmodel.Function {
	var bestMatch *sqlmodel.Function
	bestScore := -1
	bestExactCount := -1

	for _, candidate := range candidates {
		totalScore, exactCount, viable := r.scoreCall(candidate, arguments)
		if !viable {
			continue
		}

		if totalScore > bestScore || (totalScore == bestScore && exactCount > bestExactCount) {
			bestMatch = candidate
			bestScore = totalScore
			bestExactCount = exactCount
		}
	}

	return bestMatch
}

// scoreCandidate scores a single candidate overload against the actual arguments by
// summing per-argument scores and checking arity constraints.
//
// Takes candidate (*sqlmodel.Function) which is the overload to score.
// Takes arguments ([]callArgument) which holds the call-site arguments.
//
// Returns totalScore (int) which is the sum of per-argument match scores.
// Returns exactCount (int) which is the number of arguments that matched exactly.
// Returns viable (bool) which indicates whether the candidate passed arity checks and all
// arguments matched.
func (r *FunctionResolver) scoreCandidate(
	candidate *sqlmodel.Function,
	arguments []callArgument,
) (totalScore int, exactCount int, viable bool) {
	if !acceptsArgumentCount(candidate, len(arguments)) {
		return 0, 0, false
	}

	totalScore = 0
	exactCount = 0

	for index := range arguments {
		expectedIndex := index
		if expectedIndex >= len(candidate.Arguments) {
			expectedIndex = len(candidate.Arguments) - 1
		}
		score := r.scoreArgument(candidate.Arguments[expectedIndex].Type, arguments[index])
		if score == 0 {
			return 0, 0, false
		}
		totalScore += score
		if score == argumentScoreExactMatch {
			exactCount++
		}
	}

	if candidate.IsVariadic && len(arguments) > len(candidate.Arguments) {
		totalScore--
	}

	if len(arguments) == 0 {
		totalScore = 1
	}

	return totalScore, exactCount, true
}

// scoreCall scores a candidate overload against a call's arguments: positionally when no
// argument is named, and otherwise by matching each named argument to the parameter of
// its name.
//
// Takes candidate (*sqlmodel.Function) which is the overload to score.
// Takes arguments ([]callArgument) which holds the call-site arguments.
//
// Returns totalScore (int) which is the sum of per-argument match scores.
// Returns exactCount (int) which is the number of arguments that matched exactly.
// Returns viable (bool) which indicates whether the candidate accepts the call.
func (r *FunctionResolver) scoreCall(
	candidate *sqlmodel.Function,
	arguments []callArgument,
) (totalScore int, exactCount int, viable bool) {
	if !slices.ContainsFunc(arguments, func(argument callArgument) bool { return argument.name != "" }) {
		return r.scoreCandidate(candidate, arguments)
	}
	slots, arranged := arrangeArguments(candidate, arguments)
	if !arranged {
		return 0, 0, false
	}
	for slot := range slots {
		score := r.scoreArgument(candidate.Arguments[slot].Type, slots[slot])
		if score == 0 {
			return 0, 0, false
		}
		totalScore += score
		if score == argumentScoreExactMatch {
			exactCount++
		}
	}
	return totalScore, exactCount, true
}

// tryEngineResolver attempts to resolve a function call through the engine's own
// resolver, if the engine implements dialect.FunctionResolverPort.
//
// An error the resolver returns is sorted by engineFailure: a recovered panic is the
// call's Q070 problem, an interruption is returned, and any other error means the engine
// does not resolve the call.
//
// Takes name (string) which is the function name.
// Takes schema (string) which is the schema qualifier.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the call-site argument types.
//
// Returns *FunctionMatch which holds the resolved overload metadata, with a Q045 notice
// when the engine declared no data access, or nil if the engine does not support function
// resolution or resolution fails.
// Returns *Error which is the Q070 error when the engine's resolver panicked, or nil.
// Returns error which is the interruption that stopped the engine's resolver, or nil.
func (r *FunctionResolver) tryEngineResolver(
	ctx context.Context,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (*FunctionMatch, *Error, error) {
	engineResolver, ok := r.engine.(dialect.FunctionResolverPort)
	if !ok {
		return nil, nil, nil
	}
	resolution, resolveError := engineResolver.ResolveFunctionCall(ctx, r.catalogue, name, schema, argumentTypes)
	if resolveError != nil {
		problem, err := engineFailure(ctx, resolveError, fmt.Sprintf("function %q", name))
		return nil, problem, err
	}
	if resolution == nil {
		return nil, nil, nil
	}

	dataAccess := resolution.DataAccess
	var notice *Error
	if dataAccess == sqlmodel.DataAccessUnknown {
		dataAccess = sqlmodel.DataAccessReadOnly
		notice = newError(diag.CodeFunctionDataAccessUndeclared,
			"engine resolver returned function %q without a declared data access; defaulting to read-only", name)
	}
	return &FunctionMatch{
		returnType:        resolution.ReturnType,
		nullableBehaviour: resolution.NullableBehaviour,
		nullingArgument:   resolution.NullingArgument,
		dataAccess:        dataAccess,
		isAggregate:       resolution.IsAggregate,
		returnsSet:        resolution.ReturnsSet,
		elementsKnown:     resolution.ElementsKnown,
		notice:            notice,
	}, nil, nil
}

// scoreArgument scores how well an actual argument type matches an expected parameter
// type. Returns 3 for exact match, 2 for same category, 1 for implicitly castable, 0 for
// no match.
//
// An expected type of unknown category with no name is a wildcard and accepts any
// argument. One with a name is a dialect's opaque type, such as an aggregate state, and
// accepts only an argument of the same name or of unknown type, so a flavour's overload
// on its own type never captures calls made with ordinary values. An argument the engine
// converts to the parameter's type only as a function argument, as a
// dialect.ArgumentCoercerPort reports, scores as an implicitly castable one does.
//
// Takes expected (sqlmodel.SQLType) which is the parameter type declared by the function
// signature.
// Takes argument (callArgument) which is the call-site argument with its resolved type.
//
// Returns int which is the match score from 0 (no match) to 3 (exact match).
func (r *FunctionResolver) scoreArgument(expected sqlmodel.SQLType, argument callArgument) int {
	if score := r.castScore(expected, argument.sqlType); score > 0 {
		return score
	}
	if _, coerced := r.coerceArgument(argument, expected); coerced {
		return 1
	}
	return 0
}

// coerceArgument asks the engine, when it implements dialect.ArgumentCoercerPort, whether
// it converts a function argument to a parameter of another type.
//
// Takes argument (callArgument) which is the argument.
// Takes parameter (sqlmodel.SQLType) which is the parameter's declared type.
//
// Returns dialect.ArgumentCoercion which says how the engine converts the argument.
// Returns bool which is false when the engine does not convert it, or has no say.
func (r *FunctionResolver) coerceArgument(argument callArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	coercer, ok := r.engine.(dialect.ArgumentCoercerPort)
	if !ok {
		return dialect.ArgumentCoercion{}, false
	}
	return coercer.CoerceArgument(dialect.CallArgument{Type: argument.sqlType, Literal: argument.literal}, parameter)
}

// castScore scores how well an argument type matches a parameter type by category and the
// engine's implicit casts alone, as scoreArgument does before it asks the engine to
// convert the argument.
//
// Takes expected (sqlmodel.SQLType) which is the parameter type declared by the function
// signature.
// Takes actual (sqlmodel.SQLType) which is the resolved type of the call-site argument.
//
// Returns int which is the match score from 0 (no match) to 3 (exact match).
func (r *FunctionResolver) castScore(expected sqlmodel.SQLType, actual sqlmodel.SQLType) int {
	if actual.Category == sqlmodel.TypeCategoryUnknown && expected.Category == sqlmodel.TypeCategoryUnknown &&
		actual.EngineName != "" && expected.EngineName != "" {
		if strings.EqualFold(actual.EngineName, expected.EngineName) {
			return argumentScoreExactMatch
		}
		return 1
	}

	if actual.Category == sqlmodel.TypeCategoryUnknown {
		return 2
	}

	if expected.Category == sqlmodel.TypeCategoryUnknown {
		if expected.EngineName != "" {
			return 0
		}
		return 2
	}

	if expected.Category == actual.Category && strings.EqualFold(expected.EngineName, actual.EngineName) {
		return argumentScoreExactMatch
	}

	if expected.Category == actual.Category {
		return 2
	}

	if r.engine.CanImplicitCast(actual, expected) {
		return 1
	}

	return 0
}

// FunctionMatch holds the result of function overload resolution.
type FunctionMatch struct {
	// notice is a problem worth reporting about a call that still resolved, such as an
	// engine resolution that declared no data access, or nil.
	notice *Error

	// returnType holds the SQL type returned by the matched function overload.
	returnType sqlmodel.SQLType

	// nullingArgument is the zero-based position of the argument whose NULL makes the result
	// NULL, read when nullableBehaviour is FunctionNullableNullWhenArgumentNull.
	nullingArgument int

	// nullableBehaviour holds the nullable propagation behaviour of the matched function.
	nullableBehaviour sqlmodel.FunctionNullableBehaviour

	// dataAccess holds whether the function reads or modifies data.
	dataAccess sqlmodel.FunctionDataAccess

	// isAggregate indicates whether the matched function is an aggregate function.
	isAggregate bool

	// returnsSet indicates whether the matched function returns a set of rows.
	returnsSet bool

	// elementsKnown reports that the nullability the return type gives its elements is what
	// the call gives, as ast.FunctionResolution.ElementsKnown says.
	elementsKnown bool
}

// functionCandidate is one overload a call by name can resolve to, with the schema that
// holds it.
type functionCandidate struct {
	// function is the overload.
	function *sqlmodel.Function

	// schema is the key of the catalogue schema holding a created function, or the schema a
	// built-in declares, which is empty for most.
	schema string

	// key is the key the catalogue files a created function under, which a called name's key
	// must equal for the call to reach it; it is empty for a built-in.
	key string

	// builtin reports a function of the dialect rather than of the catalogue: an unqualified
	// call always reaches it, and a qualified one when it declares no schema or the
	// qualifier's.
	builtin bool
}

// callArgument is one argument of a call as overload resolution sees it: its type,
// whether it may be NULL, whether the query writes it as a literal and, when the call
// passes it by name, the name of the parameter it is passed as.
type callArgument struct {
	// name is the parameter the argument is passed as, or empty for a positional argument.
	name string

	// sqlType is the argument's type.
	sqlType sqlmodel.SQLType

	// nullable reports whether the argument may be NULL.
	nullable bool

	// literal reports an argument the query writes as a literal, which a dialect's
	// ArgumentCoercerPort may read as the type of the parameter it is passed to.
	literal bool
}

// exactArgumentMatches counts the typed arguments of a call, other than the one at skip,
// whose type an overload declares exactly for their position.
//
// Takes candidate (*sqlmodel.Function) which is the overload.
// Takes skip (int) which is the position left out, the argument being typed.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the call's argument types in
// order, unknown where an argument is untyped.
//
// Returns int which is the number of exact matches.
func exactArgumentMatches(candidate *sqlmodel.Function, skip int, argumentTypes []sqlmodel.SQLType) int {
	matches := 0
	for position := range argumentTypes {
		slot, inSignature := functionArgumentSlot(candidate, position)
		if position == skip || !inSignature || argumentTypes[position].Category == sqlmodel.TypeCategoryUnknown {
			continue
		}
		if functionArgumentTypesEqual(candidate.Arguments[slot].Type, argumentTypes[position]) {
			matches++
		}
	}
	return matches
}

// mergeBuiltinFunctions copies all built-in function signatures into the merged map,
// keyed by lowercase function name.
//
// Takes merged (map[string][]functionCandidate) which is the target map to populate.
// Takes builtins (*ast.FunctionCatalogue) which holds the engine's built-in function
// signatures.
func mergeBuiltinFunctions(
	merged map[string][]functionCandidate,
	builtins *ast.FunctionCatalogue,
) {
	if builtins == nil {
		return
	}
	for name, signatures := range builtins.Functions {
		key := strings.ToLower(name)
		for _, signature := range signatures {
			merged[key] = append(merged[key], functionCandidate{function: signature, schema: signature.Schema, key: "", builtin: true})
		}
	}
}

// mergeCatalogueFunctions copies the functions the catalogue's schemas hold into the
// merged map, each under its lower-cased name and carrying its schema and the key the
// catalogue files it under. A later signature with the key and argument types of an
// earlier one in the same schema replaces it.
//
// Takes merged (map[string][]functionCandidate) which is the target map to populate.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined schema objects including
// functions.
func mergeCatalogueFunctions(
	merged map[string][]functionCandidate,
	catalogue *sqlmodel.Catalogue,
) {
	if catalogue == nil {
		return
	}
	for _, schemaName := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(schema.Functions)) {
			lowered := strings.ToLower(name)
			for _, signature := range schema.Functions[name] {
				candidate := functionCandidate{function: signature, schema: schemaName, key: catalogue.Key(name), builtin: false}
				merged[lowered] = mergeOrAppendSignature(merged[lowered], candidate)
			}
		}
	}
}

// mergeOrAppendSignature replaces the overload of the same schema and key with matching
// argument types, or appends the candidate when there is none.
//
// Takes existing ([]functionCandidate) which holds the current candidates for a name.
// Takes candidate (functionCandidate) which is the new overload to merge or append.
//
// Returns []functionCandidate which holds the updated candidates.
func mergeOrAppendSignature(
	existing []functionCandidate,
	candidate functionCandidate,
) []functionCandidate {
	for index, overload := range existing {
		if !overload.builtin && overload.schema == candidate.schema && overload.key == candidate.key &&
			ArgumentTypesMatch(overload.function.Arguments, candidate.function.Arguments) {
			existing[index] = candidate
			return existing
		}
	}
	return append(existing, candidate)
}

// searchPathRank returns where a schema sits among the schemas a call searches, built-ins
// and other schemas ranking after every searched one.
//
// Takes searchPath ([]string) which holds the searched schema keys in order: the call's
// qualifier alone, or the search path.
// Takes candidate (functionCandidate) which is the overload to rank.
//
// Returns int which is the rank.
func searchPathRank(searchPath []string, candidate functionCandidate) int {
	if position := slices.Index(searchPath, candidate.schema); position >= 0 && !candidate.builtin {
		return position
	}
	return len(searchPath)
}

// namedArgumentSlot returns the position of a signature's parameter of a name.
//
// Takes signature (*sqlmodel.Function) which is the overload.
// Takes argumentName (string) which is the parameter's name.
//
// Returns int which is the declared argument index, or -1 when the overload has none of
// that name.
func namedArgumentSlot(signature *sqlmodel.Function, argumentName string) int {
	for index := range signature.Arguments {
		if signature.Arguments[index].Name != "" && strings.EqualFold(signature.Arguments[index].Name, argumentName) {
			return index
		}
	}
	return -1
}

// sharesResultType reports whether one signature declares no type for an argument and
// returns the type it takes.
//
// Takes signature (*sqlmodel.Function) which is the overload.
// Takes slot (int) which is the declared argument index.
//
// Returns bool which is true when the argument's type is the result's.
func sharesResultType(signature *sqlmodel.Function, slot int) bool {
	if signature.Arguments[slot].Type.Category != sqlmodel.TypeCategoryUnknown {
		return false
	}
	if signature.ReturnTypeFromArgument != 0 {
		return signature.ReturnTypeFromArgument == slot+1
	}
	return signature.ReturnType.Category == sqlmodel.TypeCategoryUnknown
}

// argumentAcceptsNull reports whether one signature takes NULL as a value at a declared
// argument.
//
// Takes signature (*sqlmodel.Function) which is the overload.
// Takes slot (int) which is the declared argument index.
//
// Returns bool which is true when NULL there is a value rather than an absent one; an
// argument declared NotNull never takes NULL.
func argumentAcceptsNull(signature *sqlmodel.Function, slot int) bool {
	if slot >= 0 && slot < len(signature.Arguments) && signature.Arguments[slot].NotNull {
		return false
	}
	switch signature.NullableBehaviour {
	case sqlmodel.FunctionNullableNeverNull:
		return true
	case sqlmodel.FunctionNullableNullWhenArgumentNull:
		return slot != signature.NullingArgument
	default:
		return false
	}
}

// functionArgumentSlot maps a zero-based call-site ordinal to a signature's declared
// argument index.
//
// It honours optional (DEFAULT) and variadic arguments the same way the arity check in
// scoreCandidate does, so an ordinal at or beyond the last declared argument is valid
// only for a variadic function, where it clamps to the final (variadic) slot.
//
// Takes signature (*sqlmodel.Function) which is the overload to inspect.
// Takes ordinal (int) which is the zero-based call-site argument slot.
//
// Returns int which is the declared-argument index.
// Returns bool which is true when the ordinal is within the overload's accepted arity.
func functionArgumentSlot(signature *sqlmodel.Function, ordinal int) (int, bool) {
	if len(signature.Arguments) == 0 {
		return 0, false
	}
	maxArguments := len(signature.Arguments)
	if signature.IsVariadic {
		maxArguments = math.MaxInt
	}
	if ordinal >= maxArguments {
		return 0, false
	}
	if ordinal >= len(signature.Arguments) {
		return len(signature.Arguments) - 1, true
	}
	return ordinal, true
}

// functionArgumentTypesEqual reports whether two declared argument types are the same
// when deciding whether competing overloads agree at an ordinal. Category plus engine
// name is sufficient: two arguments sharing both are interchangeable for
// back-propagation.
//
// Takes left (sqlmodel.SQLType) which is the first SQL type to compare.
// Takes right (sqlmodel.SQLType) which is the second SQL type to compare.
//
// Returns bool which is true when the two types match by category and engine name.
func functionArgumentTypesEqual(left sqlmodel.SQLType, right sqlmodel.SQLType) bool {
	return left.Category == right.Category && strings.EqualFold(left.EngineName, right.EngineName)
}

// splitFunctionName splits an optionally schema-qualified function name into its schema
// and bare-name parts on the last dot. A name with no dot yields an empty schema.
//
// Takes name (string) which is the function name, optionally "schema.name".
//
// Returns schema (string) which is the schema, empty when unqualified.
// Returns bareName (string) which is the function name without the schema qualifier.
func splitFunctionName(name string) (schema string, bareName string) {
	if before, after, ok := strings.CutLast(name, "."); ok {
		return before, after
	}
	return "", name
}

// nullableArgumentTypes marks argument types nullable for the dialect's call resolver.
//
// Takes arguments ([]callArgument) which are the call's typed arguments.
//
// Returns []sqlmodel.SQLType which holds, parallel to arguments, their types.
func nullableArgumentTypes(arguments []callArgument) []sqlmodel.SQLType {
	types := make([]sqlmodel.SQLType, len(arguments))
	for index := range arguments {
		types[index] = arguments[index].sqlType
		types[index].Nullable = types[index].Nullable || arguments[index].nullable
	}
	return types
}

// arrangeArguments places a call's arguments on a signature's parameters: positional
// arguments on the leading parameters, and each named argument on the parameter of its
// name.
//
// Takes candidate (*sqlmodel.Function) which is the overload.
// Takes arguments ([]callArgument) which holds the call's arguments in order.
//
// Returns map[int]callArgument which maps each filled parameter to its argument.
// Returns bool which is false when an argument names no parameter, two arguments fill one
// parameter, or a required parameter is left unfilled.
func arrangeArguments(candidate *sqlmodel.Function, arguments []callArgument) (map[int]callArgument, bool) {
	slots := make(map[int]callArgument, len(arguments))
	for position := range arguments {
		slot := position
		if arguments[position].name != "" {
			slot = namedArgumentSlot(candidate, arguments[position].name)
		}
		if _, filled := slots[slot]; slot < 0 || slot >= len(candidate.Arguments) || filled {
			return nil, false
		}
		slots[slot] = arguments[position]
	}
	for slot := range candidate.RequiredArguments() {
		if _, filled := slots[slot]; !filled {
			return nil, false
		}
	}
	return slots, true
}

// acceptsArgumentCount reports whether a candidate overload's arity admits a call with
// argumentCount arguments.
//
// A variadic candidate's MinArguments is authoritative, so a zero minimum lets a call
// such as json_build_object() pass no arguments; a non-variadic candidate with a zero
// minimum derives it from the arguments' optional flags.
//
// Takes candidate (*sqlmodel.Function) which is the overload to check.
// Takes argumentCount (int) which is the number of call-site arguments.
//
// Returns bool which is true when the count lies within the candidate's arity.
func acceptsArgumentCount(candidate *sqlmodel.Function, argumentCount int) bool {
	minArguments := candidate.RequiredArguments()
	maxArguments := len(candidate.Arguments)
	if candidate.IsVariadic && len(candidate.Arguments) > 0 {
		maxArguments = math.MaxInt
	}
	return argumentCount >= minArguments && argumentCount <= maxArguments
}
