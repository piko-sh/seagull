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

package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

const (
	// maxDiagnosticsPerCall is the most diagnostics parsing one directive call, or
	// validating it, reports; the rest are summarised in one diagnostic that says how many
	// were left out.
	maxDiagnosticsPerCall = 32
)

// ValidateCall checks a parsed directive call against the arguments its directive
// accepts.
//
// At most len(spec.Positionals) positional arguments are accepted. A keyword argument
// whose key matches a positional name fills that slot when it has not already been filled
// positionally. A required slot that ends up empty (no positional and no matching keyword
// argument) produces Q031. A keyword argument whose key matches neither a positional name
// nor a keyword argument name produces Q027 with a suggestion, and the same keyword
// argument key seen twice produces Q029. Positionals carry a Kind that is validated the
// same way keyword argument values are. At most maxDiagnosticsPerCall diagnostics are
// reported, followed by one Q066 diagnostic on the call when any were left out.
//
// Takes spec (*CallSpec) which declares the call's arguments.
// Takes call (*CallArguments) which holds the parsed call arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
//
// Returns []diag.SourceError which holds the collected diagnostics.
func ValidateCall(spec *CallSpec, call *CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string) []diag.SourceError {
	limiter := diagbuild.NewLimiter(maxDiagnosticsPerCall)
	if extra := firstExtraPositional(spec, call); extra != nil {
		limiter.Add(errorBuilder.DirectiveSyntax(extra.Span, fmt.Sprintf("%s accepts at most %d positional argument(s)", directiveName, len(spec.Positionals))))
	}
	filled := make([]bool, len(spec.Positionals))
	validatePositionalCallArguments(spec, call, filled, errorBuilder, limiter)
	validateKeywordCallArguments(spec, call, filled, errorBuilder, directiveName, limiter)
	collectMissingRequired(spec, call, filled, errorBuilder, directiveName, limiter)
	return limiter.Finish(callNoticePlace(errorBuilder, call.span()))
}

// isExportableGoName reports whether a name can become an exported Go identifier when its
// first rune is upper-cased: the first rune is a letter with an upper-case form, and
// every other rune is a letter, a digit or an underscore.
//
// Takes name (string) which is the candidate name.
//
// Returns bool which is true when the name is usable.
func isExportableGoName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for index, character := range name {
		if index == 0 {
			if !unicode.IsLetter(character) || !unicode.IsUpper(unicode.ToUpper(character)) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' {
			return false
		}
	}
	return name != ""
}

// callNoticePlace returns how the diagnostic summarising the diagnostics a call left out
// is placed: on the call, at the severity it is given.
//
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
// Takes span (diagbuild.TextSpan) which locates the call.
//
// Returns func(string, diag.ErrorSeverity, string) diag.SourceError which builds the
// diagnostic.
func callNoticePlace(errorBuilder diagbuild.ErrorBuilder, span diagbuild.TextSpan) func(string, diag.ErrorSeverity, string) diag.SourceError {
	return func(code string, severity diag.ErrorSeverity, message string) diag.SourceError {
		notice := errorBuilder.At(span, code, message)
		notice.Severity = severity
		return notice
	}
}

// firstExtraPositional returns the first positional argument past the ones the spec
// accepts that did not fail to parse, so a malformed argument whose syntax error was
// already reported does not also count against the arity.
//
// Takes spec (*CallSpec) which declares the positional slots.
// Takes call (*CallArguments) which holds the parsed positionals.
//
// Returns *ParsedPositional which is the first extra positional, or nil.
func firstExtraPositional(spec *CallSpec, call *CallArguments) *ParsedPositional {
	for index := len(spec.Positionals); index < len(call.Positionals); index++ {
		if !call.Positionals[index].Failed {
			return &call.Positionals[index]
		}
	}
	return nil
}

// validatePositionalCallArguments walks the parsed positionals, marking the filled slots
// and reporting value-shape diagnostics.
//
// Positionals beyond spec.Positionals are ignored here because ValidateCall already
// reported the arity overflow.
//
// Takes spec (*CallSpec) which declares the positional slots.
// Takes call (*CallArguments) which holds the parsed positionals.
// Takes filled ([]bool) which is marked true for each slot a positional fills.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes limiter (*diagbuild.Limiter) which receives the value-shape diagnostics.
func validatePositionalCallArguments(spec *CallSpec, call *CallArguments, filled []bool, errorBuilder diagbuild.ErrorBuilder, limiter *diagbuild.Limiter) {
	for index := range call.Positionals {
		if index >= len(spec.Positionals) {
			break
		}
		filled[index] = true
		if call.Positionals[index].Failed {
			continue
		}
		if valueDiagnostic := validatePositionalValue(&spec.Positionals[index], &call.Positionals[index], errorBuilder); valueDiagnostic != nil {
			limiter.Add(*valueDiagnostic)
		}
	}
}

// validateKeywordCallArguments verifies each parsed keyword argument.
//
// It catches duplicates, routes positional-named keywords into their matching slots, and
// dispatches value-shape checks for everything else. The filled slice is updated for
// downstream missing-required detection.
//
// Takes spec (*CallSpec) which declares the call's arguments.
// Takes call (*CallArguments) which holds the parsed keyword arguments.
// Takes filled ([]bool) which is marked true for each positional slot a keyword fills.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
// Takes limiter (*diagbuild.Limiter) which receives the diagnostics.
func validateKeywordCallArguments(spec *CallSpec, call *CallArguments, filled []bool, errorBuilder diagbuild.ErrorBuilder, directiveName string, limiter *diagbuild.Limiter) {
	seenKeywordArguments := make(map[string]int, len(call.KeywordArguments))
	for index := range call.KeywordArguments {
		keywordArgument := &call.KeywordArguments[index]
		if _, exists := seenKeywordArguments[keywordArgument.Key]; exists {
			limiter.Add(errorBuilder.Duplicate(keywordArgument.KeySpan, keywordArgument.Key))
			continue
		}
		seenKeywordArguments[keywordArgument.Key] = index
		if keywordArgument.Failed {
			if _, positionalIndex, isPositional := spec.Positional(keywordArgument.Key); isPositional {
				filled[positionalIndex] = true
			}
			continue
		}
		processKeywordCallArgument(spec, keywordArgument, filled, errorBuilder, directiveName, limiter)
	}
}

// processKeywordCallArgument classifies one keyword argument against spec.
//
// It may fill an unused positional slot, match a keyword-only spec, or be reported as
// unknown.
//
// Takes spec (*CallSpec) which declares the call's arguments.
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword argument to
// classify.
// Takes filled ([]bool) which is marked true when the keyword fills a positional slot.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
// Takes limiter (*diagbuild.Limiter) which receives the diagnostics for this keyword.
func processKeywordCallArgument(
	spec *CallSpec,
	keywordArgument *ParsedKeywordArgument,
	filled []bool,
	errorBuilder diagbuild.ErrorBuilder,
	directiveName string,
	limiter *diagbuild.Limiter,
) {
	if positionalSpec, positionalIndex, isPositional := spec.Positional(keywordArgument.Key); isPositional {
		if filled[positionalIndex] {
			limiter.Add(errorBuilder.Duplicate(keywordArgument.KeySpan, keywordArgument.Key))
			return
		}
		filled[positionalIndex] = true
		synthetic := ParsedPositional{Value: keywordArgument.Value, Span: keywordArgument.ValueSpan, Failed: false}
		if valueDiagnostic := validatePositionalValue(positionalSpec, &synthetic, errorBuilder); valueDiagnostic != nil {
			limiter.Add(*valueDiagnostic)
		}
		return
	}

	keywordArgumentSpec, found := spec.KeywordArgument(keywordArgument.Key)
	if !found {
		limiter.Add(errorBuilder.UnknownKeywordArgument(keywordArgument.KeySpan, directiveName, keywordArgument.Key, spec.ArgumentNames()))
		return
	}
	if valueDiagnostic := validateKeywordArgumentValue(keywordArgumentSpec, keywordArgument, errorBuilder); valueDiagnostic != nil {
		limiter.Add(*valueDiagnostic)
	}
}

// collectMissingRequired reports one Q031 diagnostic per required positional slot the
// caller failed to fill.
//
// The diagnostic is anchored to the closing paren when present, or the opener when the
// call was unterminated.
//
// Takes spec (*CallSpec) which declares the positional slots.
// Takes call (*CallArguments) which supplies the spans for anchoring diagnostics.
// Takes filled ([]bool) which records which slots were filled.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which prefixes the messages.
// Takes limiter (*diagbuild.Limiter) which receives the missing-required diagnostics.
func collectMissingRequired(spec *CallSpec, call *CallArguments, filled []bool, errorBuilder diagbuild.ErrorBuilder, directiveName string, limiter *diagbuild.Limiter) {
	missingSpan := call.CloseSpan
	if !call.Closed {
		missingSpan = call.OpenSpan
	}
	for index := range spec.Positionals {
		positional := &spec.Positionals[index]
		if !positional.Required || filled[index] {
			continue
		}
		limiter.Add(errorBuilder.MissingRequired(missingSpan, directiveName, positional.Name))
	}
}

// validatePositionalValue runs the same kind-specific value-shape check as
// validateKeywordArgumentValue, against a positional slot's spec.
//
// The PositionalSpec mirrors the KeywordArgumentSpec value-shape fields, so this is a
// thin adapter onto the same logic.
//
// Takes spec (*PositionalSpec) which declares the positional's value shape.
// Takes positional (*ParsedPositional) which is the positional value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the value-shape diagnostic, or nil when the value is
// well formed.
func validatePositionalValue(spec *PositionalSpec, positional *ParsedPositional, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	adapter := KeywordArgumentSpec{
		Name:          spec.Name,
		Kind:          spec.Kind,
		Required:      spec.Required,
		AllowedValues: spec.AllowedValues,
	}
	syntheticKeywordArgument := ParsedKeywordArgument{
		Key:       spec.Name,
		Value:     positional.Value,
		ValueSpan: positional.Span,
		Span:      diagbuild.TextSpan{},
		KeySpan:   diagbuild.TextSpan{},
		Failed:    false,
	}
	return validateKeywordArgumentValue(&adapter, &syntheticKeywordArgument, errorBuilder)
}

// validateKeywordArgumentValue dispatches per spec.Kind to the kind-specific value
// validator.
//
// It returns a pointer to the kind's Q-coded diagnostic so the caller can fold it into
// its own list.
//
// Takes spec (*KeywordArgumentSpec) which declares the value shape.
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is well
// formed.
func validateKeywordArgumentValue(spec *KeywordArgumentSpec, keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	switch spec.Kind {
	case KeywordArgumentList:
		return validateListKeywordValue(keywordArgument, errorBuilder)
	case KeywordArgumentInt:
		return validateIntKeywordValue(keywordArgument, errorBuilder)
	case KeywordArgumentBool:
		return validateBoolKeywordValue(spec, keywordArgument, errorBuilder)
	case KeywordArgumentIdentifier:
		return validateIdentifierKeywordValue(spec, keywordArgument, errorBuilder)
	case KeywordArgumentGoName:
		return validateGoNameKeywordValue(keywordArgument, errorBuilder)
	case KeywordArgumentQualifiedIdentifier, KeywordArgumentString:
		return nil
	}
	return nil
}

// validateGoNameKeywordValue checks that a directive value can become an exported Go
// name.
//
// Invalid names are reported at the directive before they can produce uncompilable
// source.
//
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is a usable
// name.
func validateGoNameKeywordValue(keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	if isExportableGoName(keywordArgument.Value.Raw) {
		return nil
	}
	return new(errorBuilder.InvalidValueShape(keywordArgument.ValueSpan, keywordArgument.Key,
		"a name that starts with a letter and holds only letters, digits and underscores, such as GetUser"))
}

// validateListKeywordValue checks that the value carries the list shape expected by the
// spec, returning the canonical Q-coded diagnostic when it does not.
//
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is a list.
func validateListKeywordValue(keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	if keywordArgument.Value.IsList {
		return nil
	}
	return new(errorBuilder.InvalidValueShape(keywordArgument.ValueSpan, keywordArgument.Key, "a list like [a, b, c]"))
}

// validateIntKeywordValue verifies the raw value parses as a decimal integer literal that
// fits in a 64-bit integer.
//
// Validating at a fixed 64-bit width (rather than the platform-dependent strconv.Atoi)
// keeps the accepted range identical regardless of the host architecture so validation
// and the apply phase always agree.
//
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is a valid
// integer literal.
func validateIntKeywordValue(keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	if _, parseErr := strconv.ParseInt(keywordArgument.Value.Raw, 10, 64); parseErr == nil {
		return nil
	}
	return new(errorBuilder.InvalidValueShape(keywordArgument.ValueSpan, keywordArgument.Key, "an integer literal"))
}

// validateBoolKeywordValue ensures the value is true or false in any case, returning an
// InvalidEnum diagnostic otherwise.
//
// Takes spec (*KeywordArgumentSpec) which supplies the allowed values.
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is a boolean
// literal.
func validateBoolKeywordValue(spec *KeywordArgumentSpec, keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	if _, isBoolean := parseBoolean(keywordArgument.Value.Raw); isBoolean {
		return nil
	}
	return new(errorBuilder.InvalidEnum(keywordArgument.ValueSpan, keywordArgument.Key, keywordArgument.Value.Raw, spec.AllowedValues))
}

// validateIdentifierKeywordValue checks the value against the spec's allowed-values
// enumeration, when one is present.
//
// An empty allow-list short-circuits to nil because the parser already accepted the
// identifier shape.
//
// Takes spec (*KeywordArgumentSpec) which supplies the allowed values.
// Takes keywordArgument (*ParsedKeywordArgument) which is the keyword value to check.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
//
// Returns *diag.SourceError which is the diagnostic, or nil when the value is allowed.
func validateIdentifierKeywordValue(spec *KeywordArgumentSpec, keywordArgument *ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) *diag.SourceError {
	if len(spec.AllowedValues) == 0 {
		return nil
	}
	for _, candidate := range spec.AllowedValues {
		if strings.EqualFold(candidate, keywordArgument.Value.Raw) {
			return nil
		}
	}
	return new(errorBuilder.InvalidEnum(keywordArgument.ValueSpan, keywordArgument.Key, keywordArgument.Value.Raw, spec.AllowedValues))
}

// parseBoolean reads a true or false value, ignoring case.
//
// Takes value (string) which is the value as written.
//
// Returns parsed (bool) which is the value.
// Returns isBoolean (bool) which is false when the value is neither true nor false.
func parseBoolean(value string) (parsed bool, isBoolean bool) {
	switch strings.ToLower(value) {
	case literalTrue:
		return true, true
	case literalFalse:
		return false, true
	default:
		return false, false
	}
}
