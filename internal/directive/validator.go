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

package directive

import (
	"fmt"
	"strconv"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive/syntax"
)

const (
	// keywordDefault is the parameter directive keyword that gives an omitted numeric
	// parameter its value.
	keywordDefault = "default"

	// keywordMax is the parameter directive keyword that caps a numeric parameter at call
	// time.
	keywordMax = "max"
)

// exclusiveKeywordPairs lists the pairs of keyword arguments one directive cannot carry
// together: a parameter cannot both fall back to a value and be dropped when the caller
// passes nil, and a query's rows cannot be read into a table's model and a named row at
// once.
var exclusiveKeywordPairs = [][2]string{
	{keywordDefault, "optional"},
	{KeywordModel, KeywordRow},
}

// limitBound is one integer keyword argument of a parameter directive.
type limitBound struct {
	// argument is the keyword argument, or nil when the call gives none that parses.
	argument *syntax.ParsedKeywordArgument

	// value is the argument's value.
	value int64
}

// validateCall enforces directive spec rules against the parsed call.
//
// The arguments are checked against the spec as syntax.ValidateCall checks them, then
// against the rules that span arguments: keyword arguments the directive cannot carry
// together, and a parameter's default and max, each of which must be non-negative, with
// the default no greater than the max.
//
// Takes spec (*DirectiveSpec) which declares the directive's parameters.
// Takes call (*syntax.CallArguments) which holds the parsed call arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
//
// Returns []diag.SourceError which holds the collected diagnostics.
func validateCall(spec *DirectiveSpec, call *syntax.CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string) []diag.SourceError {
	callSpec := syntax.CallSpec{Positionals: spec.Positionals, KeywordArguments: spec.KeywordArguments}
	diagnostics := syntax.ValidateCall(&callSpec, call, errorBuilder, directiveName)
	diagnostics = append(diagnostics, validateMutuallyExclusiveKeywords(spec, call, errorBuilder, directiveName)...)
	return append(diagnostics, validateLimitBounds(spec, call, errorBuilder, directiveName)...)
}

// validateMutuallyExclusiveKeywords enforces that no directive carries both keyword
// arguments of a pair exclusiveKeywordPairs lists. A pair applies only to directives
// whose spec declares both of its keyword arguments, so it stays silent for directives
// that define neither.
//
// Takes spec (*DirectiveSpec) which declares the directive's keyword arguments.
// Takes call (*syntax.CallArguments) which holds the parsed keyword arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
// Takes directiveName (string) which prefixes the message.
//
// Returns []diag.SourceError with one diagnostic for each pair the call carries both of,
// at the second of the pair.
func validateMutuallyExclusiveKeywords(spec *DirectiveSpec, call *syntax.CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string) []diag.SourceError {
	var diagnostics []diag.SourceError
	for _, pair := range exclusiveKeywordPairs {
		if diagnostic, found := exclusivePairDiagnostic(spec, call, pair, errorBuilder, directiveName); found {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics
}

// exclusivePairDiagnostic reports a call carrying both keyword arguments of an exclusive
// pair its directive's spec declares.
//
// Takes spec (*DirectiveSpec) which declares the directive's keyword arguments.
// Takes call (*syntax.CallArguments) which holds the parsed keyword arguments.
// Takes pair ([2]string) which names the two keyword arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
// Takes directiveName (string) which prefixes the message.
//
// Returns diag.SourceError which is the diagnostic, at the second keyword argument.
// Returns bool which is true when the call carries both.
func exclusivePairDiagnostic(spec *DirectiveSpec, call *syntax.CallArguments, pair [2]string, errorBuilder diagbuild.ErrorBuilder, directiveName string) (diag.SourceError, bool) {
	if _, declaresFirst := LookupKeywordArgument(spec, pair[0]); !declaresFirst {
		return diag.SourceError{}, false
	}
	if _, declaresSecond := LookupKeywordArgument(spec, pair[1]); !declaresSecond {
		return diag.SourceError{}, false
	}
	var second *syntax.ParsedKeywordArgument
	sawFirst := false
	for index := range call.KeywordArguments {
		switch call.KeywordArguments[index].Key {
		case pair[0]:
			sawFirst = true
		case pair[1]:
			second = &call.KeywordArguments[index]
		}
	}
	if !sawFirst || second == nil {
		return diag.SourceError{}, false
	}
	return errorBuilder.MutuallyExclusiveKeywordArgument(second.KeySpan, directiveName, pair[0], pair[1]), true
}

// validateLimitBounds checks the default and max a parameter directive gives a numeric
// parameter: each must be non-negative, since a negative LIMIT fails in the database, and
// the default must not exceed the max, which would cap every call that relies on the
// default.
//
// A value that is not an integer was reported already, so it is skipped here.
//
// Takes spec (*DirectiveSpec) which declares the directive's keyword arguments.
// Takes call (*syntax.CallArguments) which holds the parsed keyword arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
//
// Returns []diag.SourceError which holds a Q028 diagnostic on each value out of range.
func validateLimitBounds(spec *DirectiveSpec, call *syntax.CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string) []diag.SourceError {
	var diagnostics []diag.SourceError
	defaultValue, hasDefault := boundValue(spec, call, keywordDefault)
	maxValue, hasMax := boundValue(spec, call, keywordMax)
	for _, bound := range []limitBound{defaultValue, maxValue} {
		if bound.argument != nil && bound.value < 0 {
			diagnostics = append(diagnostics, errorBuilder.InvalidValueShape(bound.argument.ValueSpan, bound.argument.Key, "a non-negative integer"))
		}
	}
	if hasDefault && hasMax && defaultValue.value >= 0 && maxValue.value >= 0 && defaultValue.value > maxValue.value {
		diagnostics = append(diagnostics, errorBuilder.At(defaultValue.argument.ValueSpan, diag.CodeInvalidKeywordArgumentValue,
			fmt.Sprintf("%s: default %d is greater than max %d, so the default would be capped on every call that relies on it; "+
				"lower default or raise max", directiveName, defaultValue.value, maxValue.value)))
	}
	return diagnostics
}

// boundValue reads the first keyword argument of a key as an integer, when the
// directive's spec declares the key as an integer argument.
//
// Takes spec (*DirectiveSpec) which declares the directive's keyword arguments.
// Takes call (*syntax.CallArguments) which holds the parsed keyword arguments.
// Takes key (string) which names the keyword argument.
//
// Returns limitBound which holds the argument and its value.
// Returns bool which is true when the call gives the argument and it parses.
func boundValue(spec *DirectiveSpec, call *syntax.CallArguments, key string) (limitBound, bool) {
	declared, found := LookupKeywordArgument(spec, key)
	if !found || declared.Kind != KeywordArgumentInt {
		return limitBound{argument: nil, value: 0}, false
	}
	for index := range call.KeywordArguments {
		argument := &call.KeywordArguments[index]
		if argument.Key != key {
			continue
		}
		if argument.Failed {
			break
		}
		value, parseErr := strconv.ParseInt(argument.Value.Raw, 10, 64)
		if parseErr != nil {
			break
		}
		return limitBound{argument: argument, value: value}, true
	}
	return limitBound{argument: nil, value: 0}, false
}
