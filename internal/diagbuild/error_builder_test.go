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
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var sampleSpan = TextSpan{Line: 3, Column: 5, EndLine: 3, EndColumn: 12}

func TestLevenshteinDistance_ReturnsEditDistanceBetweenStrings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		source   string
		target   string
		expected int
	}{
		{name: "identical strings have zero distance", source: "kitten", target: "kitten", expected: 0},
		{name: "single substitution", source: "kitten", target: "sitten", expected: 1},
		{name: "classic kitten to sitting is three", source: "kitten", target: "sitting", expected: 3},
		{name: "empty source returns target length", source: "", target: "abc", expected: 3},
		{name: "empty target returns source length", source: "abc", target: "", expected: 3},
		{name: "both empty returns zero", source: "", target: "", expected: 0},
		{name: "single insertion", source: "cat", target: "cats", expected: 1},
		{name: "single deletion", source: "cats", target: "cat", expected: 1},
		{name: "multibyte runes counted per rune", source: "café", target: "cafe", expected: 1},
		{name: "completely different equal length", source: "abc", target: "xyz", expected: 3},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, levenshteinDistance(testCase.source, testCase.target))
		})
	}
}

func TestLevenshteinDistance_IsSymmetric(t *testing.T) {
	t.Parallel()

	forward := levenshteinDistance("sunday", "saturday")
	backward := levenshteinDistance("saturday", "sunday")
	assert.Equal(t, forward, backward)
	assert.Equal(t, 3, forward)
}

func TestFindClosestSuggestion_PicksNearestCandidateWithinThreshold(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		input      string
		candidates []string
		expected   string
	}{
		{
			name:       "empty input yields no suggestion",
			input:      "",
			candidates: []string{"alpha", "beta"},
			expected:   "",
		},
		{
			name:       "nil candidates yield no suggestion",
			input:      "alpha",
			candidates: nil,
			expected:   "",
		},
		{
			name:       "empty candidate slice yields no suggestion",
			input:      "alpha",
			candidates: []string{},
			expected:   "",
		},
		{
			name:       "single character typo returns the close candidate",
			input:      "embedd",
			candidates: []string{"embed", "column", "param"},
			expected:   "embed",
		},
		{
			name:       "case is folded before comparison",
			input:      "EMBED",
			candidates: []string{"embed", "column"},
			expected:   "embed",
		},
		{
			name:       "candidate casing is preserved in the returned value",
			input:      "embad",
			candidates: []string{"Embed", "Column"},
			expected:   "Embed",
		},
		{
			name:       "wildly different input returns no suggestion",
			input:      "zzzzzzzzzz",
			candidates: []string{"embed", "column"},
			expected:   "",
		},
		{
			name:       "first close candidate wins on equal distance",
			input:      "aaa",
			candidates: []string{"aab", "aac"},
			expected:   "aab",
		},
		{
			name:       "short input below minimum threshold still matches one edit",
			input:      "ab",
			candidates: []string{"ac"},
			expected:   "ac",
		},
		{
			name:       "exact match is its own suggestion",
			input:      "param",
			candidates: []string{"param", "embed"},
			expected:   "param",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, ClosestSuggestion(testCase.input, testCase.candidates))
		})
	}
}

func TestFindClosestSuggestion_AppliesLengthProportionalThreshold(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "transmission", ClosestSuggestion("transaction", []string{"transmission"}))

	assert.Equal(t, "", ClosestSuggestion("abc", []string{"xyz"}))
}

func TestNewErrorBuilder_BindsFilename(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("queries/users.sql")
	assert.Equal(t, "queries/users.sql", builder.Filename)
}

func TestErrorBuilderAt_ProducesErrorSeverityWithSpanCopiedAcross(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.At(sampleSpan, diag.CodeDirectiveSyntax, "boom")

	assert.Equal(t, diag.SourceError{
		Filename:  "file.sql",
		Message:   "boom",
		Code:      diag.CodeDirectiveSyntax,
		Line:      3,
		Column:    5,
		EndLine:   3,
		EndColumn: 12,
		Severity:  diag.SeverityError,
	}, source)
}

func TestErrorBuilderUnknownDirective_AttachesSuggestionWhenCandidateIsClose(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownDirective(sampleSpan, "sgl.embedd", []string{"sgl.embed", "sgl.column"})

	assert.Equal(t, diag.CodeUnknownDirective, source.Code)
	assert.Equal(t, "sgl.embed", source.Suggestion)
	assert.Equal(t, `unknown directive "sgl.embedd"; did you mean "sgl.embed"?`, source.Message)
}

func TestErrorBuilderUnknownDirective_OmitsSuggestionWhenNoCandidateIsClose(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownDirective(sampleSpan, "totallyunrelated", []string{"sgl.embed"})

	assert.Equal(t, diag.CodeUnknownDirective, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `unknown directive "totallyunrelated"`, source.Message)
}

func TestErrorBuilderUnknownKeywordArgument_AttachesSuggestionAndNamesDirective(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownKeywordArgument(sampleSpan, "sgl.embed", "form", []string{"from", "as"})

	assert.Equal(t, diag.CodeUnknownKeywordArgument, source.Code)
	assert.Equal(t, "from", source.Suggestion)
	assert.Equal(t, `unknown keyword argument "form" on sgl.embed; did you mean "from"?`, source.Message)
}

func TestErrorBuilderUnknownKeywordArgument_OmitsSuggestionWhenFarFromCandidates(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownKeywordArgument(sampleSpan, "sgl.embed", "nonsense", []string{"from"})

	assert.Equal(t, diag.CodeUnknownKeywordArgument, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `unknown keyword argument "nonsense" on sgl.embed`, source.Message)
}

func TestErrorBuilderInvalidEnum_AttachesSuggestionAndListsAllowedValues(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.InvalidEnum(sampleSpan, "command", "ome", []string{"one", "many", "exec"})

	assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, source.Code)
	assert.Equal(t, "one", source.Suggestion)
	assert.Equal(t, `invalid value "ome" for keyword argument "command"; did you mean "one"? allowed: one, many, exec`, source.Message)
}

func TestErrorBuilderInvalidEnum_OmitsSuggestionButStillListsAllowedValues(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.InvalidEnum(sampleSpan, "command", "zzzzz", []string{"one", "many"})

	assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `invalid value "zzzzz" for keyword argument "command"; allowed: one, many`, source.Message)
}

func TestErrorBuilderInvalidValueShape_DescribesExpectedShapeWithoutSuggestion(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.InvalidValueShape(sampleSpan, "max", "an integer")

	assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `invalid value for keyword argument "max": expected an integer`, source.Message)
}

func TestErrorBuilderDuplicate_ReportsRepeatedKey(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.Duplicate(sampleSpan, "type")

	assert.Equal(t, diag.CodeDuplicateKeywordArgument, source.Code)
	assert.Equal(t, `duplicate keyword argument "type"`, source.Message)
}

func TestErrorBuilderMissingRequired_NamesDirectiveAndMissingPart(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.MissingRequired(sampleSpan, "sgl", "positional argument name")

	assert.Equal(t, diag.CodeMissingRequired, source.Code)
	assert.Equal(t, "missing required positional argument name on sgl", source.Message)
}

func TestErrorBuilderUnclosed_RequestsMatchingParen(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.Unclosed(sampleSpan, "directive call")

	assert.Equal(t, diag.CodeUnclosedDirective, source.Code)
	assert.Equal(t, "unclosed directive call; expected matching ')'", source.Message)
}

func TestErrorBuilderDirectiveSyntax_FallsBackToSyntaxCode(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.DirectiveSyntax(sampleSpan, "unexpected character '#'")

	assert.Equal(t, diag.CodeDirectiveSyntax, source.Code)
	assert.Equal(t, "unexpected character '#'", source.Message)
}

func TestErrorBuilderParameterBindingSyntax_AttachesSuggestionWhenTokenIsNearMiss(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.ParameterBindingSyntax(sampleSpan, `"as"`, "az", []string{"as", "sgl"})

	assert.Equal(t, diag.CodeParameterBindingSyntax, source.Code)
	assert.Equal(t, "as", source.Suggestion)
	assert.Equal(t, `expected "as", got "az"; did you mean "as"?`, source.Message)
}

func TestErrorBuilderParameterBindingSyntax_OmitsSuggestionWhenNoCandidateClose(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.ParameterBindingSyntax(sampleSpan, `"as"`, "wildlydifferent", []string{"as"})

	assert.Equal(t, diag.CodeParameterBindingSyntax, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `expected "as", got "wildlydifferent"`, source.Message)
}

func TestErrorBuilderParameterBindingSyntax_OmitsSuggestionWhenCandidatesEmpty(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.ParameterBindingSyntax(sampleSpan, `"as"`, "as", nil)

	assert.Equal(t, diag.CodeParameterBindingSyntax, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `expected "as", got "as"`, source.Message)
}

func TestErrorBuilderInvalidListLiteral_ReportsUnexpectedClosingToken(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.InvalidListLiteral(sampleSpan, ")")

	assert.Equal(t, diag.CodeInvalidListLiteral, source.Code)
	assert.Equal(t, `expected ']' to close list literal, got ")"`, source.Message)
}

func TestErrorBuilderUnterminatedString_NamesExpectedClosingQuote(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnterminatedString(sampleSpan, `"`)

	assert.Equal(t, diag.CodeUnterminatedString, source.Code)
	assert.Equal(t, `unterminated string literal; expected closing "`, source.Message)
}

func TestErrorBuilderUnknownOverrideColumn_AttachesSuggestionWhenColumnClose(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownOverrideColumn(sampleSpan, "sgl.column", "emial", []string{"email", "name"})

	assert.Equal(t, diag.CodeUnknownOverrideColumn, source.Code)
	assert.Equal(t, "email", source.Suggestion)
	assert.Equal(t, `sgl.column references unknown output column "emial"; did you mean "email"?`, source.Message)
}

func TestErrorBuilderUnknownOverrideColumn_OmitsSuggestionWhenColumnFar(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownOverrideColumn(sampleSpan, "sgl.column", "nonexistent", []string{"email"})

	assert.Equal(t, diag.CodeUnknownOverrideColumn, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `sgl.column references unknown output column "nonexistent"`, source.Message)
}

func TestErrorBuilderUnknownOverrideMigrationColumn_AttachesSuggestionWhenClose(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownOverrideMigrationColumn(sampleSpan, "sgl.column", "users.emial", []string{"users.email", "users.name"})

	assert.Equal(t, diag.CodeUnknownOverrideMigrationColumn, source.Code)
	assert.Equal(t, "users.email", source.Suggestion)
	assert.Equal(t, `sgl.column references unknown column "users.emial"; did you mean "users.email"?`, source.Message)
}

func TestErrorBuilderUnknownOverrideMigrationColumn_OmitsSuggestionWhenFar(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.UnknownOverrideMigrationColumn(sampleSpan, "sgl.column", "wholly.different", []string{"users.email"})

	assert.Equal(t, diag.CodeUnknownOverrideMigrationColumn, source.Code)
	assert.Empty(t, source.Suggestion)
	assert.Equal(t, `sgl.column references unknown column "wholly.different"`, source.Message)
}

func TestErrorBuilderMutuallyExclusiveKeywordArgument_NamesBothKeywordArguments(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.MutuallyExclusiveKeywordArgument(sampleSpan, "sgl.column", "type", "go_type")

	assert.Equal(t, diag.CodeMutuallyExclusiveKeywordArgument, source.Code)
	assert.Equal(t, `sgl.column: keyword arguments "type" and "go_type" are mutually exclusive; pick one`, source.Message)
}

func TestErrorBuilderWithSuggestion_SetsSuggestionFieldOnEmittedError(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("file.sql")
	source := builder.withSuggestion(sampleSpan, diag.CodeUnknownDirective, "msg", "didyoumean")

	require.Equal(t, "didyoumean", source.Suggestion)
	assert.Equal(t, diag.CodeUnknownDirective, source.Code)
	assert.Equal(t, "msg", source.Message)
	assert.Equal(t, "file.sql", source.Filename)
}

func referenceSuggestion(input string, candidates []string) string {
	inputRunes := len([]rune(input))
	if input == "" || len(candidates) == 0 || inputRunes > maxSuggestionInputRunes {
		return ""
	}
	threshold := inputRunes/similarityThresholdDivisor + 1
	if threshold < minimumDistanceThreshold && inputRunes > 2 {
		threshold = minimumDistanceThreshold
	}
	bestDistance, bestMatch := threshold+1, ""
	for _, candidate := range candidates {
		if distance := levenshteinDistance(strings.ToLower(input), strings.ToLower(candidate)); distance < bestDistance {
			bestDistance, bestMatch = distance, candidate
		}
	}
	return bestMatch
}

func TestClosestSuggestionSkipsOnlyCandidatesThatCannotWin(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"title", "titles", "tilt", "TITLE", "subtitle", "t", "", "café", "cafe", "cafés", "author_id", "authors",
		"author", "a_very_long_column_name_that_is_far_longer_than_the_input", "emial", "email", "e-mail", "mail",
	}
	testCases := []struct {
		name  string
		input string
	}{
		{name: "a close typo", input: "tilte"},
		{name: "an exact match in another case", input: "Title"},
		{name: "a multi-byte input", input: "cafè"},
		{name: "a short input", input: "ab"},
		{name: "a one-rune input", input: "x"},
		{name: "an input longer than every candidate", input: "a_very_long_column_name_that_is_longer"},
		{name: "a transposition", input: "emial"},
		{name: "an input with ties", input: "athor"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, referenceSuggestion(testCase.input, candidates), ClosestSuggestion(testCase.input, candidates))
			reversed := slices.Clone(candidates)
			slices.Reverse(reversed)
			assert.Equal(t, referenceSuggestion(testCase.input, reversed), ClosestSuggestion(testCase.input, reversed))
		})
	}
}

func TestDistanceRowsStopOnceTheLimitIsPassed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		source   string
		target   string
		limit    int
		expected int
	}{
		{name: "a distance within the limit is exact", source: "kitten", target: "sitting", limit: 3, expected: 3},
		{name: "a distance past the limit reads as one past it", source: "kitten", target: "sitting", limit: 2, expected: 3},
		{name: "a far target reads as one past the limit", source: "abc", target: "xyzxyzxyz", limit: 1, expected: 2},
		{name: "an empty source is the target's length", source: "", target: "abc", limit: 5, expected: 3},
		{name: "an empty source past the limit", source: "", target: "abcdef", limit: 2, expected: 3},
		{name: "a negative limit", source: "a", target: "a", limit: -1, expected: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rows := newDistanceRows(len([]rune(testCase.target)))
			assert.Equal(t, testCase.expected, rows.within([]rune(testCase.source), []rune(testCase.target), testCase.limit))
		})
	}
}

func TestClosestSuggestionAmongManyLongCandidatesStaysWithinBudget(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("column_", 20_000)
	candidates := make([]string, 0, 20_000)
	for range 20_000 {
		candidates = append(candidates, long)
	}
	candidates = append(candidates, "colunm")

	scaling.Within(t, time.Second, func(context.Context) {
		assert.Equal(t, "colunm", ClosestSuggestion("column", candidates))
	})
}

func TestClosestSuggestionGrowsLinearlyWithTheCandidates(t *testing.T) {
	t.Parallel()

	candidatesOf := func(count int) []string {
		candidates := make([]string, 0, count)
		for index := range count {
			candidates = append(candidates, strings.Repeat("x", index%40)+"name")
		}
		return candidates
	}
	smaller, larger := candidatesOf(20_000), candidatesOf(80_000)
	scaling.Linear(t,
		func() { ClosestSuggestion("nmae_of_a_column", smaller) },
		func() { ClosestSuggestion("nmae_of_a_column", larger) },
	)
}

func TestSuggestionsArePhrasedAsThePresentationExpects(t *testing.T) {
	t.Parallel()

	builder := NewErrorBuilder("q.sql")
	span := TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 2}
	testCases := []struct {
		name          string
		diagnostic    diag.SourceError
		wantPresented string
	}{
		{name: "an unknown directive", diagnostic: builder.UnknownDirective(span, "sgl.embedd", []string{"sgl.embed"}),
			wantPresented: `unknown directive "sgl.embedd"`},
		{name: "an invalid enum value", diagnostic: builder.InvalidEnum(span, "command", "ome", []string{"one", "many"}),
			wantPresented: `invalid value "ome" for keyword argument "command"; allowed: one, many`},
		{name: "an unknown output column", diagnostic: builder.UnknownOverrideColumn(span, "sgl.column", "emial", []string{"email"}),
			wantPresented: `sgl.column references unknown output column "emial"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, testCase.diagnostic.Suggestion)
			assert.Contains(t, testCase.diagnostic.Message, diag.DidYouMean(testCase.diagnostic.Suggestion))
			presented := diag.Present(testCase.diagnostic, "q.sql", "")
			assert.Equal(t, testCase.wantPresented, presented.Message)
			assert.Equal(t, diag.DidYouMean(testCase.diagnostic.Suggestion), presented.Help)
		})
	}
}

func levenshteinDistance(source, target string) int {
	sourceRunes := []rune(source)
	targetRunes := []rune(target)
	limit := max(len(sourceRunes), len(targetRunes))
	return newDistanceRows(len(targetRunes)).within(sourceRunes, targetRunes, limit)
}
