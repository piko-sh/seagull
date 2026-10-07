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
	"errors"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// engineNameArray is the canonical ClickHouse Array(T) engine name.
	engineNameArray = "Array"

	// engineNameMap is the canonical ClickHouse Map(K, V) engine name.
	engineNameMap = "Map"

	// engineNameTuple is the canonical ClickHouse Tuple(...) engine name.
	engineNameTuple = "Tuple"

	// engineNameAggregateFunction is the canonical ClickHouse AggregateFunction(name, T)
	// engine name.
	engineNameAggregateFunction = "AggregateFunction"

	// engineNameSimpleAggregateFunction is the ClickHouse SimpleAggregateFunction(name, T)
	// type, whose column holds plain values of T.
	engineNameSimpleAggregateFunction = "SimpleAggregateFunction"

	// functionFinalizeAggregation is finalizeAggregation, lower-cased, which reads an
	// aggregate state's result.
	functionFinalizeAggregation = "finalizeaggregation"

	// engineNameFloat64 is the canonical ClickHouse Float64 engine name.
	engineNameFloat64 = "Float64"

	// engineNameString is the canonical ClickHouse String engine name.
	engineNameString = "String"

	// engineNameBool is the canonical ClickHouse Bool engine name.
	engineNameBool = "Bool"

	// typeWrapperNullable is the Nullable(T) wrapper's name.
	typeWrapperNullable = "Nullable"

	// ifFunctionArgumentCount is the number of arguments the if() form takes: (condition,
	// then, else).
	ifFunctionArgumentCount = 3

	// widthBucketArguments is the number of arguments widthBucket takes: (operand, low,
	// high, count).
	widthBucketArguments = 4

	// maxStackedCombinators caps how many combinator suffixes the resolver will strip from a
	// single function name.
	//
	// Real ClickHouse names stack at most a handful (for example quantileExactIfOrNull uses
	// two), so this bound sits comfortably above any genuine case and only pathological
	// inputs are refused.
	maxStackedCombinators = 8

	// maxResolvableFunctionNameLength caps the length of a function name the resolver will
	// attempt to split. Names longer than this are returned unsplit, since no real
	// ClickHouse function name approaches it and an unbounded identifier would otherwise
	// drive quadratic strip work.
	maxResolvableFunctionNameLength = 256

	// integerRank8Bit is the rank for 8-bit integer engine names.
	integerRank8Bit = 1

	// integerRank16Bit is the rank for 16-bit integer engine names.
	integerRank16Bit = 2

	// integerRank32Bit is the rank for 32-bit integer engine names.
	integerRank32Bit = 3

	// integerRank64Bit is the rank for 64-bit integer engine names.
	integerRank64Bit = 4

	// integerRank128Bit is the rank for 128-bit integer engine names.
	integerRank128Bit = 5

	// integerRank256Bit is the rank for 256-bit integer engine names.
	integerRank256Bit = 6
)

var (
	// errResolverNoOpinion signals that the resolver has no answer for a call where the
	// absence of an answer is not an error.
	//
	// Callers wanting to distinguish "no opinion" from "no error and no result" can
	// errors.Is the returned error against this sentinel.
	errResolverNoOpinion = errors.New("clickhouse: resolver has no opinion")

	// combinatorSuffixOrder holds the ordered list of recognised combinator suffixes, with
	// longer suffixes that overlap shorter ones placed first.
	//
	// The order matters because the strip loop tries suffixes in order and takes the first
	// match; without this order, MergeState would strip as State first and leave a Merge
	// tail.
	combinatorSuffixOrder = []string{
		"MergeState",
		"SimpleState",
		"OrNull",
		"OrDefault",
		"Resample",
		"Distinct",
		"ForEach",
		"ArgMin",
		"ArgMax",
		"State",
		"Merge",
		engineNameArray,
		engineNameMap,
		"If",
	}
)

var (
	// dateUnitAdders maps the functions that move a date or time by the unit they name,
	// addDays to subtractNanoseconds, to their resolvers, keyed in lower case.
	dateUnitAdders = namedUnitAdders()
)

// functionResolver resolves function calls whose return type cannot be expressed by a
// single static signature.
//
// ClickHouse's function library is heavy on polymorphic and combinator-based shapes, so
// static catalogue lookups need a fallback. The resolver covers arrayMap(lambda, arr),
// whose return type is Array of the lambda result; arrayFilter(lambda, arr), whose return
// type matches the input array; arrayReduce('agg', arr), whose return type is
// approximated by the array's element type; arrayJoin(arr), whose return type is the
// array's element type; tupleElement(t, n), resolved only for a single-field tuple;
// if(cond, a, b), whose return type is the wider of a and b; coalesce(a, b, ...), whose
// return type unifies its arguments; the passthrough aggregates any, anyLast, argMin,
// argMax, max, and min, whose return type matches the first input argument; and the
// combinator suffixes If, Array, OrNull, OrDefault, and Distinct, which apply a function
// transformer to the base function's return.
//
// The shared analyser invokes this resolver when the static catalogue has no matching
// signature or leaves its result type unresolved. A nil resolution with
// errResolverNoOpinion lets the analyser apply its unknown-function or unknown-type
// fallback.
type functionResolver struct {
	// functions is the dialect's built-in function catalogue, which types the base of a
	// combinator, such as uniq in uniqMerge, that has no dedicated resolver; nil means the
	// dedicated resolvers alone.
	functions *ast.FunctionCatalogue

	// types resolves the type names a lambda function's body names, through the dialect's
	// hooks and extra types.
	types *typeResolver

	// limits are the parse limits a lambda function's body is parsed under.
	limits dialectkit.Limits
}

// newFunctionResolver constructs a resolver that knows only its dedicated resolvers and
// the built-in types.
//
// The resolver keeps no state between calls, so the same instance can be shared across
// all queries.
//
// Returns *functionResolver which is the ready-to-use resolver.
func newFunctionResolver() *functionResolver {
	return newDialectFunctionResolver(nil, nil, dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 0})
}

// newCatalogueFunctionResolver constructs a resolver that also types a combinator's base
// from the dialect's built-in function catalogue, knowing the built-in types alone.
//
// Takes functions (*ast.FunctionCatalogue) which is the built-in catalogue.
//
// Returns *functionResolver which is the ready-to-use resolver.
func newCatalogueFunctionResolver(functions *ast.FunctionCatalogue) *functionResolver {
	return newDialectFunctionResolver(functions, nil, dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 0})
}

// newDialectFunctionResolver constructs the resolver a dialect types calls with: its
// function catalogue, and the types and parse limits a lambda function's body is read
// with.
//
// Takes functions (*ast.FunctionCatalogue) which is the dialect's function catalogue.
// Takes types (*typeResolver) which resolves type names; nil resolves the built-in types
// alone.
// Takes limits (dialectkit.Limits) which are the dialect's parse limits.
//
// Returns *functionResolver which is the ready-to-use resolver.
func newDialectFunctionResolver(functions *ast.FunctionCatalogue, types *typeResolver, limits dialectkit.Limits) *functionResolver {
	if types == nil {
		types = newTypeResolver(nil, nil, defaultMaxTypeParseDepth)
	}
	return &functionResolver{functions: functions, types: types, limits: limits}
}

// ResolveFunctionCall produces a FunctionResolution for a call when the static catalogue
// cannot.
//
// An alias, such as date_diff for dateDiff, is read as the function it names, before and
// after its combinator suffixes are stripped. Combinator suffixes are stripped and the
// base name with its transformer pair handles every shape this resolver knows. A call of
// a function created as a lambda is typed from its body, each body typed at most once for
// each set of argument types, within a budget. Callers can errors.Is the returned error
// against errResolverNoOpinion to distinguish "no opinion" from a real failure, after
// which the analyser falls through to its own default.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies stored lambda functions.
// Takes name (string) which is the function name being resolved.
// Takes schema (string) which is the schema the call appears in, currently unused.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the resolved argument types.
//
// Returns *ast.FunctionResolution which is the resolved shape, or nil when the resolver
// has no opinion.
// Returns error when the resolver has no answer, set to errResolverNoOpinion, or the
// context's cause once it is done while a lambda function is typed.
func (r *functionResolver) ResolveFunctionCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	_ = schema
	return r.resolveCall(ctx, newLambdaTyping(), catalogue, name, argumentTypes)
}

// resolveCall is ResolveFunctionCall with what typing the call has learnt about lambda
// functions so far.
//
// Takes typing (*lambdaTyping) which is what typing the call has learnt so far.
// Takes catalogue (*sqlmodel.Catalogue) which supplies stored lambda functions; nil
// leaves lambda functions out.
// Takes name (string) which is the function name being resolved.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the resolved argument types.
//
// Returns *ast.FunctionResolution which is the resolved shape, or nil when the resolver
// has no opinion.
// Returns error when the resolver has no answer, or the context is done.
func (r *functionResolver) resolveCall(
	ctx context.Context,
	typing *lambdaTyping,
	catalogue *sqlmodel.Catalogue,
	name string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	if target, isAlias := functionAliases()[strings.ToLower(name)]; isAlias {
		name = target
	}
	if resolution, err := r.resolveLambdaFunction(ctx, typing, catalogue, name, argumentTypes); err != nil || resolution != nil {
		return resolution, err
	}
	if strings.EqualFold(name, functionFinalizeAggregation) {
		return r.resolveFinalizeAggregation(ctx, typing, catalogue, argumentTypes)
	}

	baseName, combinators := splitCombinatorSuffixes(name)
	baseName = canonicalFunctionName(baseName)
	baseArguments := combinatorBaseArguments(combinators, argumentTypes)

	resolution := r.resolveBase(baseName, baseArguments)
	if resolution == nil && len(combinators) > 0 {
		resolution = r.resolveFromCatalogue(baseName, baseArguments)
	}
	if resolution == nil {
		return nil, errResolverNoOpinion
	}

	call := combinatorCall{baseArguments: baseArguments, arguments: argumentTypes}
	for _, combinator := range combinators {
		resolution = applyCombinator(resolution, combinator, call)
	}
	return resolution, nil
}

// resolveFromCatalogue types a combinator's base from the built-in catalogue when no
// dedicated resolver covers it, taking the first signature that accepts the argument
// count and declares a return type.
//
// Takes name (string) which is the base function's name.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the base's argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when no signature fits.
func (r *functionResolver) resolveFromCatalogue(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if r.functions == nil {
		return nil
	}
	for _, signature := range r.functions.Functions[strings.ToLower(name)] {
		if signature.ReturnType.EngineName == "" || !acceptsArgumentCount(signature, len(argumentTypes)) {
			continue
		}
		return &ast.FunctionResolution{
			ReturnType:        signature.ReturnType,
			NullingArgument:   signature.NullingArgument,
			NullableBehaviour: signature.NullableBehaviour,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       signature.IsAggregate,
			ReturnsSet:        signature.ReturnsSet,
			ElementsKnown:     false,
		}
	}
	return nil
}

// resolveBase handles base functions whose return type depends on argument types.
//
// The lookup runs through baseResolvers, the table that maps each lower-cased function
// name to the function typing it, then the functions that move a date by a unit, named in
// the function, such as addDays, or given as its first argument, such as date_add; each
// typing function describes its own rule.
//
// Takes name (string) which is the base function name to resolve.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the resolved argument types.
//
// Returns *ast.FunctionResolution which is the resolved shape, or nil when the function
// is not one the resolver covers.
func (*functionResolver) resolveBase(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	lower := strings.ToLower(name)
	if resolver, found := baseResolvers[lower]; found {
		return resolver(argumentTypes, lower)
	}
	if resolver, found := dateUnitAdders[lower]; found {
		return resolver(argumentTypes, lower)
	}
	if unitArgumentAdders[lower] {
		return resolveUnitArgumentAdder(argumentTypes)
	}
	return nil
}

// resolveFinalizeAggregation types finalizeAggregation(state): the result of the
// aggregate that built the state, as the aggregate's -Merge combinator gives it, for each
// row rather than as an aggregate. A state whose type does not record its aggregate, such
// as one initializeAggregation builds, gives a value of unknown type.
//
// Takes typing (*lambdaTyping) which is what typing the call has learnt so far.
// Takes catalogue (*sqlmodel.Catalogue) which is passed to the merge's resolution.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the state
// being the first one.
//
// Returns *ast.FunctionResolution which is the finalised value's resolution.
// Returns error when no argument is supplied, set to errResolverNoOpinion.
func (r *functionResolver) resolveFinalizeAggregation(
	ctx context.Context,
	typing *lambdaTyping,
	catalogue *sqlmodel.Catalogue,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	if len(argumentTypes) == 0 {
		return nil, errResolverNoOpinion
	}
	aggregate, recorded := stateAggregate(&argumentTypes[0])
	if !recorded {
		return newReadOnlyResolution(unknownType()), nil
	}
	merged, err := r.resolveCall(ctx, typing, catalogue, aggregate+"Merge", argumentTypes[:1])
	if err != nil || merged == nil {
		return newReadOnlyResolution(unknownType()), nil
	}
	finalised := *merged
	finalised.IsAggregate = false
	return &finalised, nil
}

// combinatorCall describes the call a combinator applies to: the base aggregate's
// arguments, and the arguments the combined function was called with.
type combinatorCall struct {
	// baseArguments are the types the base aggregate aggregates.
	baseArguments []sqlmodel.SQLType

	// arguments are the types the combined function was called with.
	arguments []sqlmodel.SQLType
}

// baseResolverFunc is the call signature of every resolveBase handler. Most handlers
// ignore the lowercased name; sum / avg use it to pick the appropriate promotion target.
type baseResolverFunc func(argumentTypes []sqlmodel.SQLType, loweredName string) *ast.FunctionResolution

var (
	// baseResolvers maps each known function spelling to its handler.
	//
	// Multiple spellings such as arraySort and arrayReverseSort share a handler when they
	// have the same resolution rules.
	baseResolvers = map[string]baseResolverFunc{
		"plus":                   arithmeticResolver(arithmeticAddition),
		"minus":                  arithmeticResolver(arithmeticSubtraction),
		"multiply":               arithmeticResolver(arithmeticMultiplication),
		"divide":                 arithmeticResolver(arithmeticDivision),
		"modulo":                 arithmeticResolver(arithmeticModulo),
		"moduloorzero":           arithmeticResolver(arithmeticModulo),
		"positivemodulo":         arithmeticResolver(arithmeticPositiveModulo),
		"widthbucket":            ignoreName(resolveWidthBucket),
		"intdiv":                 arithmeticResolver(arithmeticIntegerDivision),
		"intdivorzero":           arithmeticResolver(arithmeticIntegerDivision),
		"arraymap":               higherOrderResolver(resolveLambdaArrayMap),
		"arrayfilter":            higherOrderResolver(resolveFirstArrayPassthrough),
		"arrayfill":              higherOrderResolver(resolveFirstArrayPassthrough),
		"arrayreversefill":       higherOrderResolver(resolveFirstArrayPassthrough),
		"arraysplit":             higherOrderResolver(resolveArraySplitCall),
		"arrayreversesplit":      higherOrderResolver(resolveArraySplitCall),
		"arrayfirstornull":       higherOrderResolver(resolveArrayElementOrNullCall),
		"arraylastornull":        higherOrderResolver(resolveArrayElementOrNullCall),
		"arrayavg":               higherOrderResolver(fixedArrayCallResolver(engineNameFloat64)),
		"arrayproduct":           higherOrderResolver(fixedArrayCallResolver(engineNameFloat64)),
		"arraycumsum":            higherOrderResolver(resolveArrayCumulativeSumCall),
		"arraycumsumnonnegative": higherOrderResolver(resolveArrayCumulativeSumCall),
		"arraycount":             higherOrderResolver(fixedArrayCallResolver(engineNameUInt32)),
		"arrayfirstindex":        higherOrderResolver(fixedArrayCallResolver(engineNameUInt32)),
		"arraylastindex":         higherOrderResolver(fixedArrayCallResolver(engineNameUInt32)),
		"arrayexists":            higherOrderResolver(fixedArrayCallResolver(engineNameUInt8)),
		"arrayall":               higherOrderResolver(fixedArrayCallResolver(engineNameUInt8)),
		"arrayfold":              ignoreName(resolveArrayFold),
		"arrayreduce":            ignoreName(resolveArrayReduce),
		"arrayreducebackward":    ignoreName(resolveArrayReduce),
		"arrayjoin":              ignoreName(resolveArrayJoin),
		"arrayflatten":           ignoreName(resolveArrayFlatten),
		"arraysort":              higherOrderResolver(resolveFirstArrayPassthrough),
		"arrayreversesort":       higherOrderResolver(resolveFirstArrayPassthrough),
		"arraydistinct":          ignoreName(resolvePassthroughArray),
		"arraycompact":           ignoreName(resolvePassthroughArray),
		"arrayfirst":             higherOrderResolver(resolveArrayElementCall),
		"arraylast":              higherOrderResolver(resolveArrayElementCall),
		"arraymin":               higherOrderResolver(resolveArrayExtremeCall),
		"arraymax":               higherOrderResolver(resolveArrayExtremeCall),
		"arrayelement":           ignoreName(resolveArrayElement),
		"tupleelement":           ignoreName(resolveTupleElement),
		"if":                     ignoreName(resolveIf),
		"multiif":                ignoreName(resolveIf),
		"coalesce":               ignoreName(resolveCoalesce),
		"exp":                    ignoreName(resolveFloatResult),
		"log":                    ignoreName(resolveFloatResult),
		"ifnull":                 ignoreName(resolveCoalesce),
		"nullif":                 ignoreName(resolveNullIf),
		"any":                    ignoreName(resolveAggregateIdentity),
		"anylast":                ignoreName(resolveAggregateIdentity),
		"anyheavy":               ignoreName(resolveAggregateIdentity),
		"groupbitand":            ignoreName(resolveAggregateIdentity),
		"groupbitor":             ignoreName(resolveAggregateIdentity),
		"groupbitxor":            ignoreName(resolveAggregateIdentity),
		"min":                    ignoreName(resolveAggregateIdentity),
		"max":                    ignoreName(resolveAggregateIdentity),
		"argmin":                 ignoreName(resolveArgMinMax),
		"argmax":                 ignoreName(resolveArgMinMax),
		"sum":                    resolveSumAvg,
		"avg":                    resolveSumAvg,
		"count":                  ignoreName(resolveCount),
		"round":                  ignoreName(resolveRounding),
		"roundbankers":           ignoreName(resolveRounding),
		"floor":                  ignoreName(resolveRounding),
		"ceil":                   ignoreName(resolveRounding),
		"ceiling":                ignoreName(resolveRounding),
		"truncate":               ignoreName(resolveRounding),
		"trunc":                  ignoreName(resolveRounding),
		"quantileexact":          ignoreName(resolveAggregateIdentity),
		"quantileexactlow":       ignoreName(resolveAggregateIdentity),
		"quantileexacthigh":      ignoreName(resolveAggregateIdentity),
		"quantileexactweighted":  ignoreName(resolveAggregateIdentity),
		"quantilesexact":         ignoreName(resolveCollectingAggregate),
		"quantilesexactlow":      ignoreName(resolveCollectingAggregate),
		"quantilesexacthigh":     ignoreName(resolveCollectingAggregate),
		"abs":                    ignoreName(resolveAbsolute),
		"negate":                 ignoreName(resolveNegation),
		"array":                  ignoreName(resolveArrayConstructor),
		"arrayreverse":           ignoreName(resolvePassthroughArray),
		"arrayshuffle":           ignoreName(resolvePassthroughArray),
		"arrayslice":             ignoreName(resolvePassthroughArray),
		"arrayresize":            ignoreName(resolvePassthroughArray),
		"arraypushback":          ignoreName(resolvePassthroughArray),
		"arraypushfront":         ignoreName(resolvePassthroughArray),
		"arraypopback":           ignoreName(resolvePassthroughArray),
		"arraypopfront":          ignoreName(resolvePassthroughArray),
		"arrayconcat":            ignoreName(resolvePassthroughArray),
		"arraysum":               higherOrderResolver(resolveArraySumCall),
		"grouparray":             ignoreName(resolveCollectingAggregate),
		"groupuniqarray":         ignoreName(resolveCollectingAggregate),
		"grouparraylast":         ignoreName(resolveCollectingAggregate),
		"grouparraysample":       ignoreName(resolveCollectingAggregate),
		"grouparraysorted":       ignoreName(resolveCollectingAggregate),
		"grouparrayinsertat":     ignoreName(resolveCollectingAggregate),
		"topk":                   ignoreName(resolveCollectingAggregate),
		"topkweighted":           ignoreName(resolveCollectingAggregate),
		"laginframe":             ignoreName(resolveScalarIdentity),
		"leadinframe":            ignoreName(resolveScalarIdentity),
		"first_value":            ignoreName(resolveScalarIdentity),
		"last_value":             ignoreName(resolveScalarIdentity),
		"nth_value":              ignoreName(resolveScalarIdentity),
		"greatest":               ignoreName(resolveSupertype),
		"least":                  ignoreName(resolveSupertype),
		"untuple":                ignoreName(resolveUntuple),
		"tuple":                  ignoreName(resolveTuple),
		"map":                    ignoreName(resolveMap),
		"mapkeys":                ignoreName(resolveMapKeys),
		"mapvalues":              ignoreName(resolveMapValues),
		"mapcontains":            ignoreName(resolveMapContains),
		"and":                    ignoreName(resolveLogicalFunction),
		"or":                     ignoreName(resolveLogicalFunction),
		"xor":                    ignoreName(resolveLogicalFunction),
		"not":                    ignoreName(resolveLogicalFunction),
		"mapadd":                 ignoreName(resolveMapAdd),
		"mapsubtract":            ignoreName(resolveMapAdd),
		"mapfilter":              ignoreName(resolveMapPassthrough),
		"mapapply":               ignoreName(resolveMapPassthrough),
		"variantelement":         ignoreName(resolveVariantElement),
		"varianttype":            ignoreName(resolveVariantType),
		"dynamicelement":         ignoreName(resolveVariantElement),
		"dynamictype":            ignoreName(resolveVariantType),
		"initializeaggregation":  ignoreName(resolveInitializeAggregation),
		"finalizearray":          ignoreName(resolveFinalizeArray),
		"tostartofinterval":      ignoreName(resolveStartOfInterval),
		"transform":              ignoreName(resolveTransform),
	}
)

// combinatorBaseArguments derives an aggregate's base arguments from its combinators.
//
// Trailing combinator arguments are removed, collection arguments are unwrapped, and
// aggregate states contribute their stored argument types.
//
// Takes combinators ([]string) which are the stripped suffixes, innermost first.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns []sqlmodel.SQLType which are the base aggregate's argument types.
func combinatorBaseArguments(combinators []string, argumentTypes []sqlmodel.SQLType) []sqlmodel.SQLType {
	arguments := argumentTypes
	for _, combinator := range slices.Backward(combinators) {
		switch combinator {
		case "If", "ArgMin", "ArgMax", "Resample":
			if len(arguments) > 1 {
				arguments = arguments[:len(arguments)-1]
			}
		case engineNameArray, "ForEach", engineNameMap:
			arguments = elementTypesOf(arguments)
		case "Merge", "MergeState":
			arguments = stateArguments(arguments)
		}
	}
	return arguments
}

// elementTypesOf maps each array or map type to its element or value type, leaving any
// other type as it is.
//
// Takes types ([]sqlmodel.SQLType) which are the types.
//
// Returns []sqlmodel.SQLType which holds the element types.
func elementTypesOf(types []sqlmodel.SQLType) []sqlmodel.SQLType {
	elements := make([]sqlmodel.SQLType, len(types))
	for index := range types {
		elements[index] = types[index]
		if types[index].ElementType != nil {
			elements[index] = *types[index].ElementType
		}
	}
	return elements
}

// stateArguments returns the argument types an AggregateFunction state was built from:
// the value type it holds, or none for a state such as AggregateFunction(count) that
// aggregates no value.
//
// Takes types ([]sqlmodel.SQLType) which are the merge call's argument types, the state
// first.
//
// Returns []sqlmodel.SQLType which are the state's argument types.
func stateArguments(types []sqlmodel.SQLType) []sqlmodel.SQLType {
	if len(types) == 0 || types[0].Category != sqlmodel.TypeCategoryAggregateState || types[0].ElementType == nil {
		return types
	}
	element := *types[0].ElementType
	if element.Category == sqlmodel.TypeCategoryUnknown && element.EngineName == "" {
		return nil
	}
	return []sqlmodel.SQLType{element}
}

// acceptsArgumentCount reports whether a signature accepts a number of arguments.
//
// Takes signature (*sqlmodel.Function) which is the signature.
// Takes count (int) which is the number of arguments.
//
// Returns bool which is true when the count fits the signature's arity.
func acceptsArgumentCount(signature *sqlmodel.Function, count int) bool {
	if signature.IsVariadic {
		return count >= signature.RequiredArguments()
	}
	return count >= signature.RequiredArguments() && count <= len(signature.Arguments)
}

// splitCombinatorSuffixes strips known ClickHouse aggregate-function combinator suffixes
// from the function name.
//
// The recognised combinators are If for a filtered aggregate (countIf, sumIf); Array for
// an aggregate over array elements (groupArrayArray); OrNull, which returns Nullable(T)
// when the input is empty; OrDefault, which returns the type's default value when the
// input is empty; Resample for a bucketed aggregate; Distinct, which deduplicates before
// aggregating; Map for an aggregate over Map values, returning Map(K, T); ForEach for an
// aggregate at each array position, returning Array(T); State for an
// AggregateFunction(name, T) intermediate state; SimpleState for a
// SimpleAggregateFunction(name, T) intermediate state; MergeState, which combines states
// and returns AggregateFunction(name, T); Merge, which combines AggregateFunction states
// and returns base T; and ArgMin and ArgMax, which run argMin or argMax over the
// aggregate by an extra column and return base T.
//
// Suffixes can stack, so quantileExactIfOrNull strips to quantileExact with combinators
// If and OrNull. Suffixes are accumulated in strip order, outermost first, then reversed
// once at the end to match the bottom-up application order, which avoids the quadratic
// prepend pattern that the obvious loop body would produce.
//
// The known list is ordered so longer suffixes that share a tail with shorter ones come
// first. Without this, MergeState would strip State first leaving an orphan Merge tail,
// and the same concern applies to SimpleState.
//
// The strip loop is bounded so a pathological identifier cannot drive quadratic work at
// resolve time: names longer than maxResolvableFunctionNameLength are never split, and no
// more than maxStackedCombinators suffixes are stripped. Both bounds sit well above any
// real ClickHouse name, so legitimate resolution is unaffected.
//
// Takes name (string) which is the function name to split.
//
// Returns string which is the base function name after suffix stripping.
// Returns []string which is the ordered list of stripped suffixes for bottom-up
// application.
func splitCombinatorSuffixes(name string) (string, []string) {
	if len(name) > maxResolvableFunctionNameLength {
		return name, nil
	}
	if isStandaloneFunctionName(name) {
		return name, nil
	}
	var stripped []string
	current := name
	for len(stripped) < maxStackedCombinators && !isStandaloneFunctionName(current) {
		matched := false
		for _, suffix := range combinatorSuffixOrder {
			if strings.HasSuffix(current, suffix) && len(current) > len(suffix) {
				current = current[:len(current)-len(suffix)]
				stripped = append(stripped, suffix)
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	slices.Reverse(stripped)
	return current, stripped
}

// isStandaloneFunctionName reports whether the function name must not be split into a
// base and combinator pair.
//
// Some ClickHouse functions have names that end in tokens resembling combinator suffixes
// (for example arrayMap ends with "Map") but are not derived from a combinator. The
// resolver consults this list before attempting suffix stripping, and matching names
// short-circuit the strip loop.
//
// Takes name (string) which is the function name to test.
//
// Returns bool which is true when the name must not be split.
func isStandaloneFunctionName(name string) bool {
	switch strings.ToLower(name) {
	case "arraymap", "arrayfilter", "arrayreduce", "arrayreducebackward",
		"arrayfold", "arrayjoin", "arrayflatten", "arraysort",
		"arrayreversesort", "arraydistinct", "arraycompact",
		"arrayfirst", "arraylast", "arrayfirstornull", "arraylastornull",
		"arraymin", "arraymax",
		"arrayfirstindex", "arraylastindex", "arrayelement",
		"arrayenumerate", "arrayconcat", "arrayslice", "arraypushback",
		"arraypushfront", "arraypopback", "arraypopfront", "arrayresize",
		"has", "hasall", "hasany", "indexof", "tupleelement",
		"untuple", "tuple", "mapkeys", "mapvalues", "mapcontains",
		"mapadd", "mapsubtract", "mapfilter", "mapapply",
		"variantelement", "varianttype", "dynamicelement", "dynamictype",
		"finalizeaggregation", "initializeaggregation", "finalizearray",
		"laginframe", "leadinframe", "nth_value",
		"percent_rank", "cume_dist", "denserank", "percentrank",
		"length", "lower", "upper", "reverse", "concat",

		"grouparray", "groupuniqarray", "multiif", "nullif":
		return true
	}
	return false
}

// applyCombinator transforms a base resolution according to the combinator's semantic.
//
// The combinator order matters because the resolver applies suffixes inner to outer. If,
// Array, OrDefault, Distinct, ArgMin and ArgMax leave the resolution unchanged; their
// effect on the arguments was applied before the base was resolved. Merge finalises a
// state, and the base was resolved over the state's value types, so the result is the
// base's own result, NULL only when the state's value may be. Resample and ForEach wrap
// the result in Array(T), and Map in Map(K, T) with the key of the map argument. State
// and MergeState build an AggregateFunction state holding the base's argument type, as a
// column declared AggregateFunction(name, T) holds T, SimpleState leaves the result as it
// is, since the SimpleAggregateFunction it gives holds plain values of the result's type,
// and OrNull makes the result NULL when the aggregate sees no rows.
//
// Takes base (*ast.FunctionResolution) which is the resolution to transform.
// Takes combinator (string) which is the combinator suffix to apply.
// Takes call (combinatorCall) which describes the base and combined calls.
//
// Returns *ast.FunctionResolution which is the transformed resolution.
func applyCombinator(base *ast.FunctionResolution, combinator string, call combinatorCall) *ast.FunctionResolution {
	clone := *base
	switch combinator {
	case "Merge":
		if clone.ReturnType.Category == sqlmodel.TypeCategoryAggregateState && clone.ReturnType.ElementType != nil {
			clone.ReturnType = *clone.ReturnType.ElementType
		}
		clone.NullableBehaviour = mergedNullBehaviour(call.arguments)
	case "OrNull":
		clone.ReturnType.Nullable = true
		clone.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	case "Resample", "ForEach":
		clone.ReturnType = arrayOf(clone.ReturnType)
	case engineNameMap:
		clone.ReturnType = mapOf(mapKeyType(call.arguments), clone.ReturnType)
	case "State", "MergeState":
		clone.ReturnType = aggregateStateOf(engineNameAggregateFunction, stateValueType(call))
		clone.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	}
	return &clone
}

// mergedNullBehaviour returns the NULL behaviour of a merged state: NULL only when the
// state holds Nullable values.
//
// Takes arguments ([]sqlmodel.SQLType) which are the merge call's argument types.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the behaviour.
func mergedNullBehaviour(arguments []sqlmodel.SQLType) sqlmodel.FunctionNullableBehaviour {
	if len(arguments) > 0 && arguments[0].ElementType != nil && arguments[0].ElementType.Nullable {
		return sqlmodel.FunctionNullableCalledOnNull
	}
	return sqlmodel.FunctionNullableNeverNull
}

// mapKeyType returns the key type of the first map argument, or String when there is
// none.
//
// Takes arguments ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns sqlmodel.SQLType which is the key type.
func mapKeyType(arguments []sqlmodel.SQLType) sqlmodel.SQLType {
	for index := range arguments {
		if arguments[index].Category == sqlmodel.TypeCategoryMap && arguments[index].KeyType != nil {
			return *arguments[index].KeyType
		}
	}
	return primitiveType(engineNameString)
}

// stateValueType returns the value type an aggregate state holds: the base aggregate's
// first argument, or an unknown type when it takes none.
//
// Takes call (combinatorCall) which describes the base call.
//
// Returns sqlmodel.SQLType which is the held value type.
func stateValueType(call combinatorCall) sqlmodel.SQLType {
	if len(call.baseArguments) == 0 {
		return unknownType()
	}
	return call.baseArguments[0]
}

// ignoreName adapts a single-argument resolver to the baseResolverFunc shape, discarding
// the lowered-name argument that only sum and avg need.
//
// Takes function (func([]sqlmodel.SQLType) *ast.FunctionResolution) which is the
// single-argument resolver to adapt.
//
// Returns baseResolverFunc which calls fn and ignores the lowered name.
func ignoreName(function func(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution) baseResolverFunc {
	return func(argumentTypes []sqlmodel.SQLType, _ string) *ast.FunctionResolution {
		return function(argumentTypes)
	}
}

// resolveArrayReduce approximates the result with the final array argument's element
// type. The aggregate name is a literal value, unavailable to the type-only resolver.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// being the last one.
//
// Returns *ast.FunctionResolution which is the element-type resolution, or nil when the
// input is not a usable array.
func resolveArrayReduce(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	arrayArgument := argumentTypes[len(argumentTypes)-1]
	if arrayArgument.Category != sqlmodel.TypeCategoryArray || arrayArgument.ElementType == nil {
		return nil
	}
	element := *arrayArgument.ElementType
	return newReadOnlyResolution(element)
}

// resolveArrayJoin returns the array's element type, which is what the projection sees
// after the array is unrolled.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// being the first one.
//
// Returns *ast.FunctionResolution which is the element-type resolution, or nil when the
// input is not a usable array.
func resolveArrayJoin(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryArray || argument.ElementType == nil {
		return nil
	}
	resolution := newReadOnlyResolution(*argument.ElementType)
	resolution.ReturnsSet = true
	return resolution
}

// resolveArrayFlatten returns Array(T) where T is the inner element of a nested
// Array(Array(T)).
//
// When the input is not nested, the input array type is returned unchanged.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// being the first one.
//
// Returns *ast.FunctionResolution which is the flattened array resolution, or nil when
// the input is not a usable array.
func resolveArrayFlatten(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryArray || argument.ElementType == nil {
		return nil
	}
	if argument.ElementType.Category == sqlmodel.TypeCategoryArray && argument.ElementType.ElementType != nil {
		return newReadOnlyResolution(arrayOf(*argument.ElementType.ElementType))
	}
	return newReadOnlyResolution(argument)
}

// resolvePassthroughArray returns the input array type unchanged for functions that
// reorder, deduplicate, or compact elements without changing the shape.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// being the first one.
//
// Returns *ast.FunctionResolution which is the unchanged array resolution, or nil when
// the input is not an array.
func resolvePassthroughArray(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryArray {
		return nil
	}
	return newReadOnlyResolution(argument)
}

// resolveArrayElement returns an array's element type or a map's value type.
//
// ClickHouse also accepts arr[idx] and map[key] subscript syntax, which the parser lowers
// to arrayElement(). An index out of range or a missing key yields the type's default
// value, not NULL, so the result is NULL only when the container or index may be, or when
// the elements are Nullable themselves, as in Array(Nullable(String)).
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// being the first one.
//
// Returns *ast.FunctionResolution which is the element-type resolution, or nil when the
// input is not a usable array.
func resolveArrayElement(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryArray && argument.Category != sqlmodel.TypeCategoryMap || argument.ElementType == nil {
		return nil
	}
	resolution := newReadOnlyResolution(*argument.ElementType)
	if argument.ElementType.Nullable {
		resolution.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	}
	return resolution
}

// resolveTupleElement returns the n-th field of a tuple.
//
// When the tuple has named fields, the index can also be a string literal, and the
// resolver returns the catch-all Unknown when the index value is not statically known.
// The resolver receives only the argument types, not the literal index value, so for a
// tuple with more than one field it cannot tell which field tupleElement(t, n) selects
// and falls back to Unknown. The single-field case is unambiguous and resolved exactly.
// Callers that need the precise field type for a multi-field tuple use the dotted
// accessor form t.n, which the parser lowers to a statically-known field selection.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the tuple
// being the first one.
//
// Returns *ast.FunctionResolution which is the field-type resolution, or nil when the
// input is not a tuple.
func resolveTupleElement(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	tupleArgument := argumentTypes[0]
	if tupleArgument.Category != sqlmodel.TypeCategoryStruct || len(tupleArgument.StructFields) == 0 {
		return nil
	}

	returnType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if len(tupleArgument.StructFields) == 1 {
		returnType = tupleArgument.StructFields[0].SQLType
	}
	return newReadOnlyResolution(returnType)
}

// resolveIf produces the return type for if(cond, then, else) and multiIf(cond1, then1,
// cond2, then2, ..., else).
//
// The result type is the unification of the then and else branches. For if, the arguments
// are (cond, then, else) and the result is the wider of then and else. For multiIf, the
// arguments are alternating (cond, then) pairs followed by an else value, and the result
// is the wider of every then branch together with the final else value.
//
// Type unification follows the ClickHouse rules approximated by unifyBranchTypes:
// identical types collapse to that type, one Unknown yields the other, mixed integers
// widen to the wider integer, mixed integer and float widen to Float64, mixed text yields
// String, and otherwise the first non-Unknown wins.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the unified branch resolution, or nil when
// fewer than two arguments are supplied.
func resolveIf(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	branchTypes := collectBranchTypes(argumentTypes)
	unified := unifyBranchTypes(branchTypes)
	return newReadOnlyResolution(unified)
}

// collectBranchTypes extracts the result branches from an if or multiIf argument list.
//
// For if(cond, then, else) the result is the then and else types. For multiIf(c1, t1, c2,
// t2, ..., else) the result is each then type followed by the trailing else type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns []sqlmodel.SQLType which is the ordered list of branch result types.
func collectBranchTypes(argumentTypes []sqlmodel.SQLType) []sqlmodel.SQLType {
	if len(argumentTypes) == ifFunctionArgumentCount {
		return []sqlmodel.SQLType{argumentTypes[1], argumentTypes[2]}
	}

	branches := []sqlmodel.SQLType{}
	for index := 1; index+1 < len(argumentTypes); index += 2 {
		branches = append(branches, argumentTypes[index])
	}
	if len(argumentTypes)%2 == 1 {
		branches = append(branches, argumentTypes[len(argumentTypes)-1])
	}
	return branches
}

// resolveCoalesce returns the union of every argument's type.
//
// The return type is non-nullable when at least one argument is known to be non-nullable,
// which the analyser handles downstream. Type unification follows the same rules as
// resolveIf, so coalesce(int32_col, int64_col) widens to Int64 and coalesce(int_col,
// float_col) widens to Float64.
//
// Arguments after the first that cannot be NULL take no part.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, each marked
// nullable when the argument may be NULL.
//
// Returns *ast.FunctionResolution which is the unified resolution, or nil when no
// arguments are supplied.
func resolveCoalesce(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	reached := len(argumentTypes)
	for index := range argumentTypes {
		if argumentTypes[index].Category != sqlmodel.TypeCategoryUnknown && !argumentTypes[index].Nullable {
			reached = index + 1
			break
		}
	}
	unified := unifyBranchTypes(argumentTypes[:reached])
	return newReadOnlyResolution(unified)
}

// unifyBranchTypes approximates a common SQLType for the input branches.
//
// The rule chain is described in resolveIf's doc comment. Unknown is returned when every
// branch is unknown or the list is empty; an unsupported pairing keeps the left type.
//
// Takes branches ([]sqlmodel.SQLType) which are the branch types to unify.
//
// Returns sqlmodel.SQLType which is the unified type, or Unknown when no known type is
// supplied.
func unifyBranchTypes(branches []sqlmodel.SQLType) sqlmodel.SQLType {
	if len(branches) == 0 {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}

	candidates := []sqlmodel.SQLType{}
	for index := range branches {
		if branches[index].Category != sqlmodel.TypeCategoryUnknown {
			candidates = append(candidates, branches[index])
		}
	}
	if len(candidates) == 0 {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}
	current := candidates[0]
	for index := 1; index < len(candidates); index++ {
		current = unifyPair(current, candidates[index])
	}
	return current
}

// unifyPair returns the wider of two types per ClickHouse's permissive promotion rules,
// mirroring the union-compatibility rules used by set operations.
//
// Takes left (sqlmodel.SQLType) which is the first operand type.
// Takes right (sqlmodel.SQLType) which is the second operand type.
//
// Returns sqlmodel.SQLType which is the wider of the two types.
func unifyPair(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == right.Category && left.EngineName == right.EngineName {
		return left
	}
	if left.Category == right.Category {
		switch left.Category {
		case sqlmodel.TypeCategoryInteger:
			return widerInteger(left, right)
		case sqlmodel.TypeCategoryFloat:
			return primitiveType(engineNameFloat64)
		case sqlmodel.TypeCategoryText:
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String")
		default:

			return left
		}
	}
	if isPromotableNumericCategory(left.Category) && isPromotableNumericCategory(right.Category) {
		return crossCategoryNumericPromotion(left, right)
	}
	return left
}

// widerInteger returns the wider of two ClickHouse integer types using the same rank
// table as set-operation unification.
//
// The ranking maps bit width to a numeric rank so the comparison is a single integer
// compare regardless of signed and unsigned mixing. When the two operands share a width
// rank but differ in sign (for example Int32 and UInt32) the least-common-type is the
// next wider signed type, because the signed range cannot hold the unsigned operand's
// high-bit values at the same width. The 64-bit pair (Int64 and UInt64) is excluded
// because stock ClickHouse throws rather than promoting to Int128, so the resolver leaves
// the left operand in place for that pair.
//
// Takes left (sqlmodel.SQLType) which is the first integer operand.
// Takes right (sqlmodel.SQLType) which is the second integer operand.
//
// Returns sqlmodel.SQLType which is the wider integer type.
func widerInteger(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	leftRank := integerRank(left.EngineName)
	rightRank := integerRank(right.EngineName)
	if leftRank == rightRank && leftRank != 0 &&
		isUnsignedInteger(left.EngineName) != isUnsignedInteger(right.EngineName) {
		if widened, found := nextWiderSignedInteger(leftRank); found {
			return widened
		}
	}
	if rightRank > leftRank {
		return right
	}
	return left
}

// isUnsignedInteger reports whether a ClickHouse integer engine name denotes an unsigned
// type in the UInt8 to UInt256 range.
//
// Takes engineName (string) which is the integer engine name to test.
//
// Returns bool which is true when the name denotes an unsigned integer.
func isUnsignedInteger(engineName string) bool {
	return strings.HasPrefix(strings.ToUpper(engineName), "UINT")
}

// nextWiderSignedInteger returns the signed integer type one width rank above the
// supplied rank.
//
// It is used when mixed-sign operands of equal width must widen to a signed type. No
// wider signed type exists for the 64-bit rank because stock ClickHouse throws rather
// than widening to Int128.
//
// Takes rank (int) which is the current integer width rank.
//
// Returns sqlmodel.SQLType which is the next wider signed integer type.
// Returns bool which is true when a wider signed type exists.
func nextWiderSignedInteger(rank int) (sqlmodel.SQLType, bool) {
	switch rank {
	case integerRank8Bit:
		return primitiveType("Int16"), true
	case integerRank16Bit:
		return primitiveType("Int32"), true
	case integerRank32Bit:
		return primitiveType("Int64"), true
	default:
		return sqlmodel.SQLType{}, false
	}
}

// integerRank assigns a numeric rank to ClickHouse integer type names, where a higher
// rank means a wider type.
//
// Takes engineName (string) which is the integer engine name to rank.
//
// Returns int which is the width rank, or zero when the name is not a known integer type.
func integerRank(engineName string) int {
	switch strings.ToUpper(engineName) {
	case "UINT8", "INT8":
		return integerRank8Bit
	case "UINT16", "INT16":
		return integerRank16Bit
	case "UINT32", "INT32":
		return integerRank32Bit
	case "UINT64", "INT64":
		return integerRank64Bit
	case "UINT128", "INT128":
		return integerRank128Bit
	case "UINT256", "INT256":
		return integerRank256Bit
	}
	return 0
}

// resolveNullIf returns the first argument's type.
//
// The result is Nullable; the analyser sets the flag on the consumer.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the first-argument resolution, or nil when
// fewer than two arguments are supplied.
func resolveNullIf(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	resolution := newReadOnlyResolution(argumentTypes[0])
	resolution.NullableBehaviour = sqlmodel.FunctionNullableCalledOnNull
	return resolution
}

// resolveCount resolves the count aggregate and every combinator form that strips to it.
//
// The covered forms include countIf, countDistinct, and countIfDistinct. count always
// returns a non-null UInt64 row count regardless of its arguments, so the argument types
// are ignored. A dedicated base resolver keeps countIf and related forms from falling
// through to an Unknown type, a common telemetry pattern that would otherwise generate an
// any column.
//
// Returns *ast.FunctionResolution which is the non-null UInt64 count resolution.
func resolveCount(_ []sqlmodel.SQLType) *ast.FunctionResolution {
	resolution := newAggregateResolution(primitiveType("UInt64"))
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}

// resolveAggregateIdentity returns the first argument's type with the IsAggregate flag
// set.
//
// It is used by passthrough aggregates such as any, anyLast, min, max, and anyHeavy,
// where the aggregate yields one row of the same shape as one input row.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the first-argument resolution flagged as an
// aggregate, or nil when no arguments are supplied.
func resolveAggregateIdentity(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	return newAggregateResolution(argumentTypes[0])
}

// resolveSupertype types greatest and least by the least supertype of their arguments.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the unified resolution, or nil when no
// arguments are supplied.
func resolveSupertype(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	return newReadOnlyResolution(unifyBranchTypes(argumentTypes))
}

// resolveFloatResult types exp and log as ClickHouse computes them. A Float32 keeps its
// width, and any other number gives a Float64.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the argument is
// not a number.
func resolveFloatResult(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 || !isPromotableNumericCategory(argumentTypes[0].Category) {
		return nil
	}
	if argumentTypes[0].Category == sqlmodel.TypeCategoryFloat && argumentTypes[0].Width == widthWord {
		return newReadOnlyResolution(argumentTypes[0])
	}
	return newReadOnlyResolution(primitiveType(engineNameFloat64))
}

// resolveScalarIdentity returns the first argument's type without the IsAggregate flag.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the first-argument resolution, or nil when no
// arguments are supplied.
func resolveScalarIdentity(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	return newReadOnlyResolution(argumentTypes[0])
}

// resolveRounding types round, roundBankers, floor, ceil, trunc and their spellings,
// which ClickHouse computes in the type of the number they round: a Decimal keeps its
// precision and scale, an integer its width and sign, and a float its width.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the number
// being the first one.
//
// Returns *ast.FunctionResolution which is the resolution, or nil when the first argument
// is not a number.
func resolveRounding(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	switch argumentTypes[0].Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return newReadOnlyResolution(argumentTypes[0])
	default:
		return nil
	}
}

// resolveArgMinMax returns the first argument's type because argMin(x, y) returns the x
// that minimises y, so the result has x's shape.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the first-argument resolution flagged as an
// aggregate, or nil when fewer than two arguments are supplied.
func resolveArgMinMax(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	return newAggregateResolution(argumentTypes[0])
}

// resolveSumAvg types sum by ClickHouse's summing rule, see sumResultType, and avg as a
// Float64.
//
// Takes argumentTypes ([]sqlmodel.SQLType), the argument list.
// Takes name (string), the lowercase function spelling ("sum" / "avg").
//
// Returns the resolution with widened return type, or nil for empty input.
func resolveSumAvg(argumentTypes []sqlmodel.SQLType, name string) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	if name == "avg" {
		return newAggregateResolution(primitiveType(engineNameFloat64))
	}
	if sum := sumResultType(argumentTypes[0]); sum.Category != sqlmodel.TypeCategoryUnknown {
		return newAggregateResolution(sum)
	}
	return newAggregateResolution(argumentTypes[0])
}

// resolveUntuple returns the first tuple field's type as a best-effort approximation.
//
// untuple spreads a tuple into one value per field. The parser refuses it as an item of a
// select list, where it would give a column per field; elsewhere, as when it spreads into
// a function's arguments, its first field stands for it.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the tuple
// being the first one.
//
// Returns *ast.FunctionResolution which is the first field's resolution, or nil when the
// input is not a tuple.
func resolveUntuple(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryStruct || len(argument.StructFields) == 0 {
		return nil
	}
	return newReadOnlyResolution(argument.StructFields[0].SQLType)
}

// resolveTuple constructs a Tuple(...) from the argument types.
//
// The result's StructFields are anonymous, synthesised as _1, _2, and so on. A field is
// Nullable where its argument may be NULL, and the tuple itself never is, as ClickHouse
// does not wrap a Tuple in Nullable.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the tuple field types.
//
// Returns *ast.FunctionResolution which is the constructed Tuple resolution.
func resolveTuple(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	fields := make([]sqlmodel.StructField, len(argumentTypes))
	for index := range argumentTypes {
		fields[index] = sqlmodel.StructField{
			Name:       synthesiseAnonymousFieldName(index + 1),
			SQLType:    argumentTypes[index],
			Positional: true,
		}
	}
	resolution := newReadOnlyResolution(tupleOf(fields))
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}

// resolveMap constructs a Map(K, V) from alternating key and value argument pairs.
//
// ClickHouse's map(k1, v1, k2, v2, ...) calls require even arity. The resolver uses the
// first key and value pair to derive types and trusts the analyser to flag arity
// mistakes. The map is never NULL, as ClickHouse does not wrap a Map in Nullable; its
// values are Nullable where the value argument may be NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the alternating key and value types.
//
// Returns *ast.FunctionResolution which is the constructed Map resolution, or nil when
// fewer than two arguments are supplied.
func resolveMap(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	resolution := newReadOnlyResolution(mapOf(argumentTypes[0], argumentTypes[1]))
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}

// resolveMapKeys returns Array(K) where K is the map's key type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the map
// being the first one.
//
// Returns *ast.FunctionResolution which is the Array(K) resolution, or nil when the input
// is not a usable map.
func resolveMapKeys(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryMap || argument.KeyType == nil {
		return nil
	}
	return newReadOnlyResolution(arrayOf(*argument.KeyType))
}

// resolveMapValues returns Array(V) where V is the map's value type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the map
// being the first one.
//
// Returns *ast.FunctionResolution which is the Array(V) resolution, or nil when the input
// is not a usable map.
func resolveMapValues(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryMap || argument.ElementType == nil {
		return nil
	}
	return newReadOnlyResolution(arrayOf(*argument.ElementType))
}

// resolveMapContains returns UInt8, 1 or 0, for mapContains(map, key).
//
// nil is returned only when no map argument is supplied, so the analyser can fall through
// to its Unknown default.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the UInt8 resolution, or nil when no arguments
// are supplied.
func resolveMapContains(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	return newReadOnlyResolution(primitiveType(engineNameUInt8))
}

// resolveLogicalFunction types and, or, xor and not as ClickHouse does: a Bool when an
// argument is a Bool, and a UInt8, 1 or 0, otherwise, NULL when an argument may be.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the Bool or UInt8 resolution, or nil when an
// argument's type is unknown.
func resolveLogicalFunction(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	for index := range argumentTypes {
		if argumentTypes[index].Category == sqlmodel.TypeCategoryUnknown {
			return nil
		}
	}
	return newReadOnlyResolution(logicalResult(argumentTypes...))
}

// logicalResult returns the type of a logical operation over operands of the given types:
// a Bool when one of them is a Bool, and a UInt8 otherwise.
//
// Takes operands (...sqlmodel.SQLType) which are the operands' types.
//
// Returns sqlmodel.SQLType which is the result's type.
func logicalResult(operands ...sqlmodel.SQLType) sqlmodel.SQLType {
	for index := range operands {
		if operands[index].Category == sqlmodel.TypeCategoryBoolean {
			return primitiveType(engineNameBool)
		}
	}
	return primitiveType(engineNameUInt8)
}

// resolveMapAdd approximates mapAdd and mapSubtract with the first argument's map type.
// It does not promote the value type or inspect the remaining arguments.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the map
// being the first one.
//
// Returns *ast.FunctionResolution which is the map resolution, or nil when the input is
// not a map.
func resolveMapAdd(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryMap {
		return nil
	}
	return newReadOnlyResolution(argument)
}

// resolveMapPassthrough returns the map's type unchanged.
//
// It is used by mapFilter and approximates mapApply with the input map's shape. This
// helper does not derive transformed key or value types from the lambda body.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the map
// being the last one.
//
// Returns *ast.FunctionResolution which is the unchanged map resolution, or nil when the
// input is not a map.
func resolveMapPassthrough(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[len(argumentTypes)-1]
	if argument.Category != sqlmodel.TypeCategoryMap {
		return nil
	}
	return newReadOnlyResolution(argument)
}

// resolveVariantElement approximates variantElement from the variant's member types.
//
// Because the resolver interface sees only argument types rather than literal values, the
// second argument's value is not statically known, so only a variant of one member gives
// that member's type; a variant of several gives a value of unknown type rather than a
// member's type the call may not read. The result is nullable because variantElement
// returns NULL when the requested type is not the active variant member.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the variant
// being the first one.
//
// Returns *ast.FunctionResolution which is the nullable member-type resolution, or nil
// when no arguments are supplied.
func resolveVariantElement(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	variant := argumentTypes[0]
	if len(variant.UnionMembers) != 1 {
		unknownMember := unknownType()
		unknownMember.Nullable = true
		return newReadOnlyResolution(unknownMember)
	}
	member := variant.UnionMembers[0].SQLType
	member.Nullable = true
	return newReadOnlyResolution(member)
}

// resolveVariantType returns String because ClickHouse's variantType and dynamicType
// accessors yield the textual name of the active variant member.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the String resolution, or nil when no
// arguments are supplied.
func resolveVariantType(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	return newReadOnlyResolution(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String"))
}

// stateAggregate returns the name of the aggregate an AggregateFunction state's type
// records, as uniq in AggregateFunction(uniq, String).
//
// Takes state (*sqlmodel.SQLType) which is the state's type.
//
// Returns string which is the aggregate's name.
// Returns bool which is false when the type is no state or records no aggregate.
func stateAggregate(state *sqlmodel.SQLType) (string, bool) {
	if state.Category != sqlmodel.TypeCategoryAggregateState {
		return "", false
	}
	_, body, found := strings.Cut(state.EngineName, "(")
	if !found {
		return "", false
	}
	aggregate, _, _ := strings.Cut(strings.TrimSuffix(body, ")"), ",")
	aggregate = strings.TrimSpace(aggregate)
	return aggregate, aggregate != ""
}

// resolveInitializeAggregation wraps a value type into an AggregateFunction state.
//
// ClickHouse uses this to build aggregate states from raw inputs for incremental
// materialised view paths. The argument list is (aggregateName, value...), and the
// resolver returns the AggregateFunction shape holding the first value's type so codegen
// sees a concrete state type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the first
// value being the second one.
//
// Returns *ast.FunctionResolution which is the AggregateFunction state resolution, or nil
// when fewer than two arguments are supplied.
func resolveInitializeAggregation(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) < 2 {
		return nil
	}
	return newReadOnlyResolution(aggregateStateOf(engineNameAggregateFunction, argumentTypes[1]))
}

// resolveFinalizeArray finalises an Array(AggregateFunction(...)) into an Array of the
// underlying value type.
//
// The input shape comes from arrayMap over a column of aggregate states, and the result
// is Array(T) where T is the finalised value type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types, the array
// of states being the first one.
//
// Returns *ast.FunctionResolution which is the finalised Array(T) resolution, or nil when
// the input is not a usable array.
func resolveFinalizeArray(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	argument := argumentTypes[0]
	if argument.Category != sqlmodel.TypeCategoryArray || argument.ElementType == nil {
		return nil
	}
	inner := *argument.ElementType
	finalised := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if inner.ElementType != nil {
		finalised = *inner.ElementType
	}
	return newReadOnlyResolution(arrayOf(finalised))
}

// newReadOnlyResolution builds a read-only, non-aggregate, single-row function resolution
// shared by most resolver handlers. Its result is NULL exactly when an argument is, the
// server's default implementation for NULLs.
//
// Takes returnType (sqlmodel.SQLType) which is the resolved return type.
//
// Returns *ast.FunctionResolution which is the resolution carrying returnType.
func newReadOnlyResolution(returnType sqlmodel.SQLType) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// newAggregateResolution builds a read-only aggregate resolution whose nullability
// conservatively follows its arguments.
//
// Takes returnType (sqlmodel.SQLType) which is the resolved return type.
//
// Returns *ast.FunctionResolution which is the aggregate resolution carrying returnType.
func newAggregateResolution(returnType sqlmodel.SQLType) *ast.FunctionResolution {
	resolution := newReadOnlyResolution(returnType)
	resolution.IsAggregate = true
	return resolution
}

// arithmeticResolver returns the resolver of an arithmetic function, such as plus or
// intDiv, which types its two arguments the way the matching operator does.
//
// Takes kind (arithmeticKind) which is the operation.
//
// Returns baseResolverFunc which resolves the function.
func arithmeticResolver(kind arithmeticKind) baseResolverFunc {
	return func(argumentTypes []sqlmodel.SQLType, _ string) *ast.FunctionResolution {
		if len(argumentTypes) != 2 {
			return nil
		}
		return newReadOnlyResolution(arithmeticResult(kind, argumentTypes[0], argumentTypes[1]))
	}
}

// resolveWidthBucket types widthBucket(operand, low, high, count): an unsigned integer
// twice as wide as the bucket count's, up to 64 bits, which holds every bucket number,
// count + 1 included. ClickHouse takes only an unsigned count, so any other is left to
// the signature, which refuses it.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil without four arguments
// or an unsigned count.
func resolveWidthBucket(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) != widthBucketArguments {
		return nil
	}
	count := argumentTypes[widthBucketArguments-1]
	if count.Category != sqlmodel.TypeCategoryInteger || !count.Unsigned {
		return nil
	}
	return newReadOnlyResolution(integerType(false, widenedInteger(count.Width)))
}

// resolveNegation types negate the way a prefix minus is typed.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil without one argument.
func resolveNegation(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) != 1 {
		return nil
	}
	return newReadOnlyResolution(negationResult(argumentTypes[0]))
}

// resolveAbsolute types abs: an integer becomes unsigned at its width.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the resolution, or nil without one argument.
func resolveAbsolute(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) != 1 {
		return nil
	}
	return newReadOnlyResolution(absoluteResult(argumentTypes[0]))
}

// resolveArrayConstructor types array(...) and the literal [a, b]: an array of its
// elements' common type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the element types.
//
// Returns *ast.FunctionResolution which is the array resolution.
func resolveArrayConstructor(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	resolution := newReadOnlyResolution(arrayOf(unifyBranchTypes(argumentTypes)))
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}

// sumResultType returns the type ClickHouse sums a value type to.
//
// Takes element (sqlmodel.SQLType) which is the summed type.
//
// Returns sqlmodel.SQLType which is the sum's type.
func sumResultType(element sqlmodel.SQLType) sqlmodel.SQLType {
	switch element.Category {
	case sqlmodel.TypeCategoryInteger:
		if element.Width > widthLong {
			return element
		}
		return integerType(!element.Unsigned, widthLong)
	case sqlmodel.TypeCategoryFloat:
		return primitiveType(engineNameFloat64)
	case sqlmodel.TypeCategoryDecimal:
		sum := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal128")
		sum.Precision = new(decimal128ImpliedPrecision)
		sum.Scale = element.Scale
		return sum
	default:
		return unknownType()
	}
}

// resolveCollectingAggregate types the aggregates that collect values into an array, such
// as groupArray and topK: an array of the first argument's type, whose elements are never
// NULL because NULLs are skipped.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call argument types.
//
// Returns *ast.FunctionResolution which is the aggregate's resolution, or nil without an
// argument.
func resolveCollectingAggregate(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) == 0 {
		return nil
	}
	element := argumentTypes[0]
	element.Nullable = false
	resolution := newAggregateResolution(arrayOf(element))
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}
