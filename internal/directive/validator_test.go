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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive/syntax"
)

var span1 = diagbuild.TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 2}

func hasCode(diagnostics []diag.SourceError, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestValidateCall_BothPositionalsFilledNoDiagnostics(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("query")
	require.True(t, ok)

	call := &syntax.CallArguments{
		Closed: true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "GetUser"}, Span: span1},
			{Value: syntax.ParsedValue{Raw: "one"}, Span: span1},
		},
	}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")

	diagnostics := validateCall(spec, call, errorBuilder, "sgl")

	assert.Empty(t, diagnostics)
}

func TestValidateCall_MissingRequiredEmitsQ031(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("query")
	require.True(t, ok)

	call := &syntax.CallArguments{
		Closed:    true,
		CloseSpan: span1,
	}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")

	diagnostics := validateCall(spec, call, errorBuilder, "sgl")

	require.NotEmpty(t, diagnostics)
	assert.True(t, hasCode(diagnostics, diag.CodeMissingRequired))
}

func TestValidateCall_ExtraPositionalEmitsSyntax(t *testing.T) {
	t.Parallel()

	spec, ok := lookupDirective("query")
	require.True(t, ok)

	call := &syntax.CallArguments{
		Closed: true,
		Positionals: []syntax.ParsedPositional{
			{Value: syntax.ParsedValue{Raw: "GetUser"}, Span: span1},
			{Value: syntax.ParsedValue{Raw: "one"}, Span: span1},
			{Value: syntax.ParsedValue{Raw: "extra"}, Span: span1},
		},
	}
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")

	diagnostics := validateCall(spec, call, errorBuilder, "sgl")

	assert.True(t, hasCode(diagnostics, diag.CodeDirectiveSyntax))
}

func TestValidateCallChecksAParametersDefaultAndMax(t *testing.T) {
	t.Parallel()

	defaultSpan := diagbuild.TextSpan{Line: 2, Column: 30, EndLine: 2, EndColumn: 33}
	maxSpan := diagbuild.TextSpan{Line: 2, Column: 40, EndLine: 2, EndColumn: 42}
	testCases := []struct {
		name      string
		arguments map[string]string
		want      []diagbuild.TextSpan
		messages  []string
	}{
		{name: "a default below the max", arguments: map[string]string{"default": "10", "max": "50"}, want: nil, messages: nil},
		{name: "a default equal to the max", arguments: map[string]string{"default": "50", "max": "50"}, want: nil, messages: nil},
		{name: "a zero default", arguments: map[string]string{"default": "0"}, want: nil, messages: nil},
		{
			name:      "a negative max",
			arguments: map[string]string{"default": "500", "max": "-5"},
			want:      []diagbuild.TextSpan{maxSpan},
			messages:  []string{`invalid value for keyword argument "max": expected a non-negative integer`},
		},
		{
			name:      "a negative default",
			arguments: map[string]string{"default": "-1"},
			want:      []diagbuild.TextSpan{defaultSpan},
			messages:  []string{`invalid value for keyword argument "default": expected a non-negative integer`},
		},
		{
			name:      "a default above the max",
			arguments: map[string]string{"default": "500", "max": "50"},
			want:      []diagbuild.TextSpan{defaultSpan},
			messages: []string{"sgl.param: default 500 is greater than max 50, so the default would be capped on every " +
				"call that relies on it; lower default or raise max"},
		},
		{name: "a value that is not an integer is reported once", arguments: map[string]string{"max": "lots"}, want: []diagbuild.TextSpan{maxSpan}, messages: []string{`invalid value for keyword argument "max": expected an integer literal`}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec, ok := lookupDirective(roleParam)
			require.True(t, ok)
			call := &syntax.CallArguments{
				Closed:      true,
				Positionals: []syntax.ParsedPositional{{Value: syntax.ParsedValue{Raw: "page_size"}, Span: span1}},
			}
			for _, key := range []string{"default", "max"} {
				value, given := testCase.arguments[key]
				if !given {
					continue
				}
				valueSpan := defaultSpan
				if key == "max" {
					valueSpan = maxSpan
				}
				call.KeywordArguments = append(call.KeywordArguments, syntax.ParsedKeywordArgument{
					Key: key, Value: syntax.ParsedValue{Raw: value}, KeySpan: span1, ValueSpan: valueSpan,
				})
			}

			diagnostics := validateCall(spec, call, diagbuild.NewErrorBuilder("test.sql"), "sgl.param")

			require.Len(t, diagnostics, len(testCase.want))
			for index, diagnostic := range diagnostics {
				assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostic.Code)
				assert.Equal(t, diag.SeverityError, diagnostic.Severity)
				assert.Equal(t, testCase.messages[index], diagnostic.Message)
				assert.Equal(t, testCase.want[index].Column, diagnostic.Column)
			}
		})
	}
}
