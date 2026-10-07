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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

var (
	testSpan      = diagbuild.TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 2}
	testQuerySpec = CallSpec{
		Positionals: []PositionalSpec{
			{Name: "name", Required: true, Kind: KeywordArgumentGoName, AllowedValues: nil},
			{Name: "command", Required: true, Kind: KeywordArgumentIdentifier, AllowedValues: []string{"one", "many"}},
		},
		KeywordArguments: []KeywordArgumentSpec{
			{Name: "readonly", Kind: KeywordArgumentBool, AllowedValues: booleanValues, Required: false},
			{Name: "max", Kind: KeywordArgumentInt, AllowedValues: nil, Required: false},
		},
	}
)

func codesOf(diagnostics []diag.SourceError) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func positionals(values ...string) []ParsedPositional {
	parsed := make([]ParsedPositional, 0, len(values))
	for _, value := range values {
		parsed = append(parsed, ParsedPositional{Value: ParsedValue{Raw: value}, Span: testSpan})
	}
	return parsed
}

func keyword(key, value string) ParsedKeywordArgument {
	return ParsedKeywordArgument{Key: key, Value: ParsedValue{Raw: value}, KeySpan: testSpan, ValueSpan: testSpan}
}

func TestValidateCallChecksTheArgumentsAgainstTheSpec(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		call  CallArguments
		codes []string
	}{
		{name: "both positionals filled", call: CallArguments{Closed: true, Positionals: positionals("GetUser", "one")}, codes: []string{}},
		{name: "a required positional missing", call: CallArguments{Closed: true, CloseSpan: testSpan}, codes: []string{diag.CodeMissingRequired, diag.CodeMissingRequired}},
		{
			name:  "an extra positional",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one", "extra")},
			codes: []string{diag.CodeDirectiveSyntax},
		},
		{
			name:  "a value outside the allowed values",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "notACommand")},
			codes: []string{diag.CodeInvalidKeywordArgumentValue},
		},
		{
			name:  "a name that is not an exportable Go name",
			call:  CallArguments{Closed: true, Positionals: positionals("1st", "one")},
			codes: []string{diag.CodeInvalidKeywordArgumentValue},
		},
		{
			name:  "positionals passed by name",
			call:  CallArguments{Closed: true, KeywordArguments: []ParsedKeywordArgument{keyword("name", "GetUser"), keyword("command", "one")}},
			codes: []string{},
		},
		{
			name:  "a slot filled twice",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("name", "Other")}},
			codes: []string{diag.CodeDuplicateKeywordArgument},
		},
		{
			name: "a keyword argument given twice",
			call: CallArguments{
				Closed: true, Positionals: positionals("GetUser", "one"),
				KeywordArguments: []ParsedKeywordArgument{keyword("readonly", "true"), keyword("readonly", "false")},
			},
			codes: []string{diag.CodeDuplicateKeywordArgument},
		},
		{
			name:  "an unknown keyword argument",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("readonyl", "true")}},
			codes: []string{diag.CodeUnknownKeywordArgument},
		},
		{
			name:  "a value that is not a boolean",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("readonly", "trueish")}},
			codes: []string{diag.CodeInvalidKeywordArgumentValue},
		},
		{
			name:  "a value that is not an integer",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("max", "manymany")}},
			codes: []string{diag.CodeInvalidKeywordArgumentValue},
		},
		{
			name:  "a boolean in any case",
			call:  CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("readonly", "TRUE")}},
			codes: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := ValidateCall(&testQuerySpec, &testCase.call, diagbuild.NewErrorBuilder("test.sql"), "acme.query")

			require.Equal(t, testCase.codes, codesOf(diagnostics), "%v", diagnostics)
		})
	}
}

func TestValidateCallSuggestsTheClosestArgument(t *testing.T) {
	t.Parallel()

	call := CallArguments{Closed: true, Positionals: positionals("GetUser", "one"), KeywordArguments: []ParsedKeywordArgument{keyword("readonyl", "true")}}

	diagnostics := ValidateCall(&testQuerySpec, &call, diagbuild.NewErrorBuilder("test.sql"), "acme.query")

	require.Len(t, diagnostics, 1)
	assert.Equal(t, "readonly", diagnostics[0].Suggestion)
	assert.Equal(t, `unknown keyword argument "readonyl" on acme.query; did you mean "readonly"?`, diagnostics[0].Message)
}

func TestValidateCallCapsTheDiagnosticsOfOneCall(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		unknown  int
		kept     int
		notice   string
		noticeOf int
	}{
		{name: "fewer than the cap are all reported", unknown: maxDiagnosticsPerCall, kept: maxDiagnosticsPerCall, notice: ""},
		{name: "one past the cap is summarised", unknown: maxDiagnosticsPerCall + 1, kept: maxDiagnosticsPerCall, notice: "and 1 more diagnostic was left out"},
		{name: "many past the cap are summarised once", unknown: 5000, kept: maxDiagnosticsPerCall, notice: fmt.Sprintf("and %d more diagnostics were left out", 5000-maxDiagnosticsPerCall)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call := CallArguments{
				Closed:      true,
				OpenSpan:    diagbuild.TextSpan{Line: 3, Column: 10, EndLine: 3, EndColumn: 11},
				CloseSpan:   diagbuild.TextSpan{Line: 3, Column: 90, EndLine: 3, EndColumn: 91},
				Positionals: positionals("GetUser", "one"),
			}
			for index := range testCase.unknown {
				call.KeywordArguments = append(call.KeywordArguments, keyword(fmt.Sprintf("unknown_%d", index), "1"))
			}

			diagnostics := ValidateCall(&testQuerySpec, &call, diagbuild.NewErrorBuilder("test.sql"), "acme.query")

			if testCase.notice == "" {
				require.Len(t, diagnostics, testCase.kept)
				return
			}
			require.Len(t, diagnostics, testCase.kept+1)
			notice := diagnostics[testCase.kept]
			assert.Equal(t, diag.CodeDiagnosticLimit, notice.Code)
			assert.Equal(t, diag.SeverityError, notice.Severity)
			assert.Equal(t, testCase.notice, notice.Message)
			assert.Equal(t, []int{3, 10, 3, 91}, []int{notice.Line, notice.Column, notice.EndLine, notice.EndColumn})
		})
	}
}

func TestValidateKeywordArgumentValueChecksTheShapeOfEachKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		spec  KeywordArgumentSpec
		value ParsedValue
		valid bool
	}{
		{name: "a list", spec: KeywordArgumentSpec{Name: "columns", Kind: KeywordArgumentList}, value: ParsedValue{IsList: true, AsList: []string{"a", "b"}}, valid: true},
		{name: "not a list", spec: KeywordArgumentSpec{Name: "columns", Kind: KeywordArgumentList}, value: ParsedValue{Raw: "a"}, valid: false},
		{name: "an integer", spec: KeywordArgumentSpec{Name: "max", Kind: KeywordArgumentInt}, value: ParsedValue{Raw: "100"}, valid: true},
		{name: "a negative integer", spec: KeywordArgumentSpec{Name: "max", Kind: KeywordArgumentInt}, value: ParsedValue{Raw: "-5"}, valid: true},
		{name: "an integer past 64 bits", spec: KeywordArgumentSpec{Name: "max", Kind: KeywordArgumentInt}, value: ParsedValue{Raw: "9223372036854775808"}, valid: false},
		{name: "true", spec: KeywordArgumentSpec{Name: "readonly", Kind: KeywordArgumentBool, AllowedValues: booleanValues}, value: ParsedValue{Raw: "true"}, valid: true},
		{name: "False", spec: KeywordArgumentSpec{Name: "readonly", Kind: KeywordArgumentBool, AllowedValues: booleanValues}, value: ParsedValue{Raw: "False"}, valid: true},
		{name: "yes", spec: KeywordArgumentSpec{Name: "readonly", Kind: KeywordArgumentBool, AllowedValues: booleanValues}, value: ParsedValue{Raw: "yes"}, valid: false},
		{name: "any identifier", spec: KeywordArgumentSpec{Name: "type", Kind: KeywordArgumentIdentifier}, value: ParsedValue{Raw: "uuid"}, valid: true},
		{name: "an allowed identifier in any case", spec: KeywordArgumentSpec{Name: "dynamic", Kind: KeywordArgumentIdentifier, AllowedValues: []string{"runtime"}}, value: ParsedValue{Raw: "RuntimE"}, valid: true},
		{name: "an identifier not allowed", spec: KeywordArgumentSpec{Name: "dynamic", Kind: KeywordArgumentIdentifier, AllowedValues: []string{"runtime"}}, value: ParsedValue{Raw: "compile"}, valid: false},
		{name: "any string", spec: KeywordArgumentSpec{Name: "go_type", Kind: KeywordArgumentString}, value: ParsedValue{Raw: "bad type"}, valid: true},
		{name: "an exportable name", spec: KeywordArgumentSpec{Name: "row", Kind: KeywordArgumentGoName}, value: ParsedValue{Raw: "get_user"}, valid: true},
		{name: "a name with a hyphen", spec: KeywordArgumentSpec{Name: "row", Kind: KeywordArgumentGoName}, value: ParsedValue{Raw: "get-user"}, valid: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			argument := ParsedKeywordArgument{Key: testCase.spec.Name, Value: testCase.value, ValueSpan: testSpan}

			diagnostic := validateKeywordArgumentValue(&testCase.spec, &argument, diagbuild.NewErrorBuilder("test.sql"))

			if testCase.valid {
				require.Nil(t, diagnostic)
				return
			}
			require.NotNil(t, diagnostic)
			assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostic.Code)
		})
	}
}

func TestValidateBoolKeywordValueSuggestsTheClosestValue(t *testing.T) {
	t.Parallel()

	spec := KeywordArgumentSpec{Name: "readonly", Kind: KeywordArgumentBool, AllowedValues: booleanValues}
	argument := ParsedKeywordArgument{Key: "readonly", Value: ParsedValue{Raw: "trueish"}, ValueSpan: testSpan}

	diagnostic := validateBoolKeywordValue(&spec, &argument, diagbuild.NewErrorBuilder("test.sql"))

	require.NotNil(t, diagnostic)
	assert.Equal(t, "true", diagnostic.Suggestion)
}

func TestIsExportableGoName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"GetUser":  true,
		"get_user": true,
		"élan":     true,
		"":         false,
		"1st":      false,
		"_hidden":  false,
		"get-user": false,
		"\xff":     false,
	} {
		assert.Equal(t, want, isExportableGoName(name), name)
	}
}
