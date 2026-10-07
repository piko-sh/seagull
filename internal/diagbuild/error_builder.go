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

package diagbuild

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/diag"
)

const (
	// similarityThresholdDivisor sets the edit distance a suggestion may be from the input:
	// one more than a third of the input's runes.
	similarityThresholdDivisor = 3

	// minimumDistanceThreshold is the smallest edit distance allowed for an input of more
	// than two runes.
	minimumDistanceThreshold = 2

	// maxSuggestionInputRunes is the longest input, in runes, a suggestion is sought for.
	maxSuggestionInputRunes = 128
)

// ErrorBuilder produces SourceError values with consistent shape and optional "did you
// mean" suggestions. Every directive validation site routes through this builder so
// message wording, severity, and suggestion behaviour stay uniform across the parser.
type ErrorBuilder struct {
	// Filename is recorded on every emitted SourceError so callers do not need to pass the
	// path at every site.
	Filename string
}

// NewErrorBuilder returns a builder bound to the given source file.
//
// Takes filename (string) which is the source file path recorded on every emitted error.
//
// Returns ErrorBuilder which is bound to the supplied filename.
func NewErrorBuilder(filename string) ErrorBuilder {
	return ErrorBuilder{Filename: filename}
}

// At builds a diag.SourceError at the given span with the given code and message and
// SeverityError.
//
// Use this for low-level errors that do not have a canonical builder method.
//
// Takes span (TextSpan) which is the source span the error points at.
// Takes code (string) which is the diagnostic code to record.
// Takes message (string) which is the human-readable error message.
//
// Returns diag.SourceError which carries the supplied span, code, and message.
func (builder ErrorBuilder) At(span TextSpan, code, message string) diag.SourceError {
	return diag.SourceError{
		Filename:   builder.Filename,
		Message:    message,
		Code:       code,
		Line:       span.Line,
		Column:     span.Column,
		EndLine:    span.EndLine,
		EndColumn:  span.EndColumn,
		Severity:   diag.SeverityError,
		Suggestion: "",
	}
}

// UnknownDirective reports a directive name that is not in the registry.
//
// When a Levenshtein neighbour is found in candidates the Suggestion field is populated
// with the closest match.
//
// Takes span (TextSpan) which is the source span of the offending directive.
// Takes given (string) which is the unknown directive name.
// Takes candidates ([]string) which are the known directive names to suggest from.
//
// Returns SourceError describing the unknown directive, with a suggestion when one is
// near.
func (builder ErrorBuilder) UnknownDirective(span TextSpan, given string, candidates []string) diag.SourceError {
	suggestion := ClosestSuggestion(given, candidates)
	message := fmt.Sprintf("unknown directive %q", given)
	if suggestion != "" {
		message = withDidYouMean(message, suggestion)
	}
	return builder.withSuggestion(span, diag.CodeUnknownDirective, message, suggestion)
}

// UnknownKeywordArgument reports a keyword argument key that the directive's spec does
// not accept.
//
// Suggestion carries the closest-matching keyword argument name.
//
// Takes span (TextSpan) which is the source span of the offending key.
// Takes directiveName (string) which is the directive the key was given on.
// Takes given (string) which is the unknown keyword argument key.
// Takes candidates ([]string) which are the accepted keyword argument names.
//
// Returns SourceError describing the unknown keyword argument, with a suggestion when one
// is near.
func (builder ErrorBuilder) UnknownKeywordArgument(span TextSpan, directiveName, given string, candidates []string) diag.SourceError {
	suggestion := ClosestSuggestion(given, candidates)
	message := fmt.Sprintf("unknown keyword argument %q on %s", given, directiveName)
	if suggestion != "" {
		message = withDidYouMean(message, suggestion)
	}
	return builder.withSuggestion(span, diag.CodeUnknownKeywordArgument, message, suggestion)
}

// InvalidEnum reports a keyword argument value that is not in the allowed enum set.
//
// Suggestion carries the closest-matching allowed value.
//
// Takes span (TextSpan) which is the source span of the offending value.
// Takes keywordArgument (string) which is the keyword argument the value was given for.
// Takes given (string) which is the rejected value.
// Takes allowed ([]string) which are the permitted enum values.
//
// Returns SourceError describing the invalid value, with a suggestion when one is near.
func (builder ErrorBuilder) InvalidEnum(span TextSpan, keywordArgument, given string, allowed []string) diag.SourceError {
	suggestion := ClosestSuggestion(given, allowed)
	allowedDisplay := strings.Join(allowed, ", ")
	message := fmt.Sprintf("invalid value %q for keyword argument %q; allowed: %s", given, keywordArgument, allowedDisplay)
	if suggestion != "" {
		message = fmt.Sprintf("invalid value %q for keyword argument %q; %s allowed: %s", given, keywordArgument, diag.DidYouMean(suggestion), allowedDisplay)
	}
	return builder.withSuggestion(span, diag.CodeInvalidKeywordArgumentValue, message, suggestion)
}

// InvalidValueShape reports a keyword argument value that fails the kind-specific shape
// check, such as a non-integer for KeywordArgumentInt or a non-list for
// KeywordArgumentList.
//
// Used when there is no enum to suggest from.
//
// Takes span (TextSpan) which is the source span of the offending value.
// Takes keywordArgument (string) which is the keyword argument the value was given for.
// Takes expected (string) which describes the shape the value should have taken.
//
// Returns SourceError describing the malformed value.
func (builder ErrorBuilder) InvalidValueShape(span TextSpan, keywordArgument, expected string) diag.SourceError {
	message := fmt.Sprintf("invalid value for keyword argument %q: expected %s", keywordArgument, expected)
	return builder.At(span, diag.CodeInvalidKeywordArgumentValue, message)
}

// Duplicate reports the same keyword argument key appearing twice in one call.
//
// Takes span (TextSpan) which is the source span of the repeated key.
// Takes key (string) which is the duplicated keyword argument name.
//
// Returns SourceError describing the duplicate keyword argument.
func (builder ErrorBuilder) Duplicate(span TextSpan, key string) diag.SourceError {
	message := fmt.Sprintf("duplicate keyword argument %q", key)
	return builder.At(span, diag.CodeDuplicateKeywordArgument, message)
}

// MissingRequired reports a required positional argument or keyword argument that is
// absent from a directive call.
//
// Takes span (TextSpan) which is the source span of the directive call.
// Takes directiveName (string) which is the directive missing the argument.
// Takes what (string) which describes the missing required argument.
//
// Returns SourceError describing the missing required argument.
func (builder ErrorBuilder) MissingRequired(span TextSpan, directiveName, what string) diag.SourceError {
	message := fmt.Sprintf("missing required %s on %s", what, directiveName)
	return builder.At(span, diag.CodeMissingRequired, message)
}

// Unclosed reports a multi-line directive call whose paren depth never returns to zero
// before the directive header terminates.
//
// Takes span (TextSpan) which is the source span of the unclosed call.
// Takes opener (string) which names the construct that was left open.
//
// Returns SourceError describing the unclosed directive.
func (builder ErrorBuilder) Unclosed(span TextSpan, opener string) diag.SourceError {
	message := fmt.Sprintf("unclosed %s; expected matching ')'", opener)
	return builder.At(span, diag.CodeUnclosedDirective, message)
}

// DirectiveSyntax reports a low-level lexer or parser failure that does not fit one of
// the structured codes above, such as an unexpected character or unterminated string.
//
// Falls back to the CodeDirectiveSyntax code.
//
// Takes span (TextSpan) which is the source span of the syntax failure.
// Takes message (string) which is the human-readable failure description.
//
// Returns SourceError carrying the supplied message under the directive-syntax code.
func (builder ErrorBuilder) DirectiveSyntax(span TextSpan, message string) diag.SourceError {
	return builder.At(span, diag.CodeDirectiveSyntax, message)
}

// ParameterBindingSyntax reports a malformed parameter-binding line where the expected
// keyword token ("as", the directive prefix, or a role name) did not appear.
//
// When the candidate slice is non-empty and the offending token is a near-miss typo, a
// Levenshtein suggestion is attached so the LSP can offer a quick-fix.
//
// Takes span (TextSpan) which is the source span of the offending token.
// Takes expected (string) which describes the token that was expected.
// Takes given (string) which is the token that was actually found.
// Takes candidates ([]string) which are the keyword tokens to suggest from.
//
// Returns SourceError describing the binding syntax error, with a suggestion when one is
// near.
func (builder ErrorBuilder) ParameterBindingSyntax(span TextSpan, expected, given string, candidates []string) diag.SourceError {
	message := fmt.Sprintf("expected %s, got %q", expected, given)
	suggestion := ClosestSuggestion(given, candidates)
	if suggestion != "" {
		message = withDidYouMean(message, suggestion)
		return builder.withSuggestion(span, diag.CodeParameterBindingSyntax, message, suggestion)
	}
	return builder.At(span, diag.CodeParameterBindingSyntax, message)
}

// InvalidListLiteral reports a bracketed list whose closing token did not match the
// expected ']'.
//
// The value is treated as malformed.
//
// Takes span (TextSpan) which is the source span of the list literal.
// Takes got (string) which is the unexpected closing token that was found.
//
// Returns SourceError describing the malformed list literal.
func (builder ErrorBuilder) InvalidListLiteral(span TextSpan, got string) diag.SourceError {
	message := fmt.Sprintf("expected ']' to close list literal, got %q", got)
	return builder.At(span, diag.CodeInvalidListLiteral, message)
}

// UnterminatedString reports a string literal whose opening quote was not matched before
// the end of the directive line or input.
//
// Takes span (TextSpan) which is the source span of the string literal.
// Takes quote (string) which is the closing quote that was expected.
//
// Returns SourceError describing the unterminated string literal.
func (builder ErrorBuilder) UnterminatedString(span TextSpan, quote string) diag.SourceError {
	message := fmt.Sprintf("unterminated string literal; expected closing %s", quote)
	return builder.At(span, diag.CodeUnterminatedString, message)
}

// UnknownOverrideColumn reports a sgl.column(name, ...) directive in a query header that
// targets an output column not present in the query's SELECT projection.
//
// Suggestion carries the closest-matching actual output column name.
//
// Takes span (TextSpan) which is the source span of the directive.
// Takes directiveName (string) which is the canonical column directive name.
// Takes given (string) which is the unknown output column name.
// Takes candidates ([]string) which are the actual output column names to suggest from.
//
// Returns SourceError describing the unknown override column, with a suggestion when one
// is near.
func (builder ErrorBuilder) UnknownOverrideColumn(span TextSpan, directiveName, given string, candidates []string) diag.SourceError {
	suggestion := ClosestSuggestion(given, candidates)
	message := fmt.Sprintf("%s references unknown output column %q", directiveName, given)
	if suggestion != "" {
		message = withDidYouMean(message, suggestion)
		return builder.withSuggestion(span, diag.CodeUnknownOverrideColumn, message, suggestion)
	}
	return builder.At(span, diag.CodeUnknownOverrideColumn, message)
}

// UnknownOverrideMigrationColumn reports a migration-file sgl.column directive whose
// qualified name does not match any column in the catalogue.
//
// Suggestion carries the closest-matching table.column reference.
//
// Takes span (TextSpan) which is the source span of the directive.
// Takes directiveName (string) which is the canonical column directive name.
// Takes given (string) which is the unknown qualified column name.
// Takes candidates ([]string) which are the catalogue table.column references to suggest
// from.
//
// Returns SourceError describing the unknown migration column, with a suggestion when one
// is near.
func (builder ErrorBuilder) UnknownOverrideMigrationColumn(span TextSpan, directiveName, given string, candidates []string) diag.SourceError {
	suggestion := ClosestSuggestion(given, candidates)
	message := fmt.Sprintf("%s references unknown column %q", directiveName, given)
	if suggestion != "" {
		message = withDidYouMean(message, suggestion)
		return builder.withSuggestion(span, diag.CodeUnknownOverrideMigrationColumn, message, suggestion)
	}
	return builder.At(span, diag.CodeUnknownOverrideMigrationColumn, message)
}

// AliasPrefix reports a directive written with an alias of the canonical prefix while the
// configuration requires the canonical prefix.
//
// The directive is still parsed, so the diagnostic is a hint whose suggestion is the
// canonical prefix.
//
// Takes span (TextSpan) which is the source span of the alias token.
// Takes alias (string) which is the alias prefix as written.
// Takes canonical (string) which is the canonical prefix.
//
// Returns SourceError describing the alias, with the canonical prefix as the suggestion.
func (builder ErrorBuilder) AliasPrefix(span TextSpan, alias, canonical string) diag.SourceError {
	message := fmt.Sprintf("directive prefix %q is an alias; write %q", alias, canonical)
	diagnostic := builder.withSuggestion(span, diag.CodeAliasPrefix, message, canonical)
	diagnostic.Severity = diag.SeverityHint
	return diagnostic
}

// ForeignPrefix reports a directive written with a foreign prefix, one that belongs to
// another tool and is not enabled for this run.
//
// The directive is ignored, so the diagnostic is a warning naming how to enable the
// prefix. Once the prefix is listed after the canonical one it is an alias, which
// formatting the directives rewrites to the canonical prefix; rewriting every directive
// to a named prefix is not suggested, since it would rewrite them to the foreign one. The
// message names no command: an embedding program has no sgl, and the sgl command adds its
// own hint.
//
// Takes span (TextSpan) which is the source span of the foreign directive name.
// Takes commentPrefix (string) which is the dialect's line-comment prefix.
// Takes directiveName (string) which is the directive name as written, such as
// other.query.
// Takes prefix (string) which is the foreign prefix.
//
// Returns SourceError describing the foreign directive.
func (builder ErrorBuilder) ForeignPrefix(span TextSpan, commentPrefix, directiveName, prefix string) diag.SourceError {
	message := fmt.Sprintf(
		"%s %s is not enabled here; add %s to directives.prefixes to accept it, then optionally format the directives to rewrite it to the canonical prefix",
		commentPrefix, directiveName, prefix,
	)
	diagnostic := builder.At(span, diag.CodeForeignPrefix, message)
	diagnostic.Severity = diag.SeverityWarning
	return diagnostic
}

// MutuallyExclusiveKeywordArgument reports two keyword arguments declared on the same
// directive call when only one of them may be set at a time.
//
// The span points at the second of the two; the quick-fix is to delete it.
//
// Takes span (TextSpan) which is the source span of the second argument.
// Takes directiveName (string) which is the directive both arguments were given on.
// Takes first (string) which is the first conflicting keyword argument name.
// Takes second (string) which is the second conflicting keyword argument name.
//
// Returns SourceError describing the mutually exclusive keyword arguments.
func (builder ErrorBuilder) MutuallyExclusiveKeywordArgument(span TextSpan, directiveName, first, second string) diag.SourceError {
	message := fmt.Sprintf("%s: keyword arguments %q and %q are mutually exclusive; pick one", directiveName, first, second)
	return builder.At(span, diag.CodeMutuallyExclusiveKeywordArgument, message)
}

// withSuggestion builds a SourceError at the span and attaches the given suggestion.
//
// Takes span (TextSpan) which is the source span the error points at.
// Takes code (string) which is the diagnostic code to record.
// Takes message (string) which is the human-readable error message.
// Takes suggestion (string) which is the closest-match suggestion to attach.
//
// Returns SourceError carrying the message and the attached suggestion.
func (builder ErrorBuilder) withSuggestion(span TextSpan, code, message, suggestion string) diag.SourceError {
	source := builder.At(span, code, message)
	source.Suggestion = suggestion
	return source
}

// distanceRows holds the two rows of the Levenshtein table, reused from one comparison to
// the next.
type distanceRows struct {
	// previous holds the distances of the row before.
	previous []int

	// current holds the distances of the row being filled.
	current []int
}

// newDistanceRows allocates rows for targets of up to a number of runes.
//
// Takes targetRunes (int) which is the longest target, in runes, compared.
//
// Returns *distanceRows which is ready to compare.
func newDistanceRows(targetRunes int) *distanceRows {
	return &distanceRows{previous: make([]int, 0, targetRunes+1), current: make([]int, 0, targetRunes+1)}
}

// within returns the Levenshtein distance between two strings, or limit+1 as soon as the
// distance is known to exceed limit.
//
// Takes source ([]rune) which is the first string.
// Takes target ([]rune) which is the second string.
// Takes limit (int) which is the largest distance worth computing exactly.
//
// Returns int which is the distance when it is at most limit, and otherwise limit+1.
func (rows *distanceRows) within(source, target []rune, limit int) int {
	if limit < 0 {
		return limit + 1
	}
	if len(source) == 0 || len(target) == 0 {
		return min(max(len(source), len(target)), limit+1)
	}
	previous := rows.previous[:0]
	current := rows.current[:0]
	for column := range len(target) + 1 {
		previous = append(previous, column)
		current = append(current, 0)
	}
	for row := 1; row <= len(source); row++ {
		current[0] = row
		rowMinimum := row
		for column := 1; column <= len(target); column++ {
			cost := 1
			if source[row-1] == target[column-1] {
				cost = 0
			}
			current[column] = min(previous[column]+1, current[column-1]+1, previous[column-1]+cost)
			rowMinimum = min(rowMinimum, current[column])
		}
		if rowMinimum > limit {
			rows.previous, rows.current = previous, current
			return limit + 1
		}
		previous, current = current, previous
	}
	rows.previous, rows.current = previous, current
	return min(previous[len(target)], limit+1)
}

// ClosestSuggestion returns the closest member of candidates to the input by Levenshtein
// distance, with a length-proportional threshold, for a "did you mean" suggestion.
//
// A candidate whose length differs from the input's by more than the threshold is passed
// over unread, since that difference alone is a lower bound on the distance, and a
// comparison stops as soon as the candidate cannot beat the best found so far, so long or
// many candidates cost little more than reading their lengths.
//
// Takes input (string) which is the token to find a near match for.
// Takes candidates ([]string) which are the known values to compare against.
//
// Returns string which is the closest candidate, or empty when none is close enough or
// the inputs are empty.
func ClosestSuggestion(input string, candidates []string) string {
	if input == "" || len(candidates) == 0 {
		return ""
	}
	inputRunes := utf8.RuneCountInString(input)
	if inputRunes > maxSuggestionInputRunes {
		return ""
	}

	threshold := (inputRunes / similarityThresholdDivisor) + 1
	if threshold < minimumDistanceThreshold && inputRunes > 2 {
		threshold = minimumDistanceThreshold
	}

	source := []rune(strings.ToLower(input))
	distances := newDistanceRows(inputRunes + threshold)
	bestDistance := threshold + 1
	bestMatch := ""

	for _, candidate := range candidates {
		if !runeCountWithin(candidate, inputRunes-threshold, inputRunes+threshold) {
			continue
		}
		distance := distances.within(source, []rune(strings.ToLower(candidate)), bestDistance-1)
		if distance < bestDistance {
			bestDistance = distance
			bestMatch = candidate
		}
	}

	if bestDistance <= threshold {
		return bestMatch
	}
	return ""
}

// withDidYouMean appends the "did you mean" question for a suggestion to a message.
//
// Takes message (string) which describes the problem.
// Takes suggestion (string) which is the suggested name or value.
//
// Returns string which is the message followed by the question.
func withDidYouMean(message, suggestion string) string {
	return message + "; " + diag.DidYouMean(suggestion)
}

// runeCountWithin reports whether a text's rune count lies between two bounds, reading no
// more of it than the upper bound needs.
//
// Takes text (string) which is the text.
// Takes lower (int) which is the fewest runes allowed.
// Takes upper (int) which is the most runes allowed.
//
// Returns bool which is true when lower <= runes <= upper.
func runeCountWithin(text string, lower, upper int) bool {
	if len(text) < lower {
		return false
	}
	count := 0
	for range text {
		count++
		if count > upper {
			return false
		}
	}
	return count >= lower
}
