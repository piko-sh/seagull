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

package dialecttest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/sqllex"
)

func skipSpacesAndBlockComments(source string, position int) (int, error) {
	for position < len(source) && source[position] == ' ' {
		position++
	}
	if strings.HasPrefix(source[position:], "/*") {
		end := strings.Index(source[position:], "*/")
		return skipSpacesAndBlockComments(source, position+end+len("*/"))
	}
	return position, nil
}

func TestCompareTokeniserSpans(t *testing.T) {
	t.Parallel()

	source := `SELECT 'a;b' /* c */ "d" x`
	agreeing := []dialecttest.Token{
		{Position: 0, Class: dialecttest.TokenWord},
		{Position: 7, Class: dialecttest.TokenString},
		{Position: 21, Class: dialecttest.TokenQuotedIdentifier},
		{Position: 25, Class: dialecttest.TokenWord},
	}

	testCases := []struct {
		name     string
		source   string
		tokens   []dialecttest.Token
		expected []string
		rules    sqllex.Rules
	}{
		{
			name:     "agreeing spans",
			rules:    sqllex.Rules{},
			source:   source,
			tokens:   agreeing,
			expected: nil,
		},
		{
			name:   "a string the dialect reads as something else",
			rules:  sqllex.Rules{},
			source: source,
			tokens: []dialecttest.Token{
				{Position: 0, Class: dialecttest.TokenWord},
				{Position: 7, Class: dialecttest.TokenOther},
				{Position: 21, Class: dialecttest.TokenQuotedIdentifier},
				{Position: 25, Class: dialecttest.TokenWord},
			},
			expected: []string{`byte 7 ("'a;b' /* c */ \"d\" x"): sqllex reads a string here but the dialect does not`},
		},
		{
			name:   "a token the dialect starts inside a sqllex comment",
			rules:  sqllex.Rules{},
			source: source,
			tokens: []dialecttest.Token{
				{Position: 0, Class: dialecttest.TokenWord},
				{Position: 7, Class: dialecttest.TokenString},
				{Position: 16, Class: dialecttest.TokenWord},
				{Position: 21, Class: dialecttest.TokenQuotedIdentifier},
				{Position: 25, Class: dialecttest.TokenWord},
			},
			expected: []string{
				`byte 7 ("'a;b' /* c */ \"d\" x"): sqllex ends the string at 12 but the dialect's next token starts at 16`,
				`byte 16 ("c */ \"d\" x"): the dialect reads a word here but sqllex reads no token starting there`,
				`byte 13 ("/* c */ \"d\" x"): sqllex reads a comment here but the dialect reads a token at 16`,
			},
		},
		{
			name:   "a string that ends somewhere else",
			rules:  sqllex.Rules{Quotes: sqllex.QuoteRules{SingleQuoteEscape: sqllex.EscapeBackslash}},
			source: `'a\' b'`,
			tokens: []dialecttest.Token{
				{Position: 0, Class: dialecttest.TokenString},
				{Position: 5, Class: dialecttest.TokenWord},
			},
			expected: []string{
				`byte 0 ("'a\\' b'"): sqllex ends the string at 7 but the dialect's next token starts at 5`,
				`byte 5 ("b'"): the dialect reads a word here but sqllex reads no token starting there`,
			},
		},
		{
			name:     "text sqllex cannot scan",
			rules:    sqllex.Rules{},
			source:   "'open",
			tokens:   []dialecttest.Token{{Position: 0, Class: dialecttest.TokenOther}},
			expected: []string{`sqllex cannot scan text the dialect tokenises: unterminated string literal at line 1, column 1 (byte 0) near "'open"`},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, dialecttest.CompareTokeniserSpans(testCase.rules, testCase.source, testCase.tokens, skipSpacesAndBlockComments))
		})
	}
}
