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

package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

func TestIdentifierQuoteQuote(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
		quote    dialect.IdentifierQuote
	}{
		{name: "double quotes", quote: dialect.IdentifierQuote{Open: `"`, Close: `"`}, input: "order", expected: `"order"`},
		{name: "an embedded closing quote is doubled", quote: dialect.IdentifierQuote{Open: "`", Close: "`"}, input: "a`b", expected: "`a``b`"},
		{name: "distinct opening and closing text", quote: dialect.IdentifierQuote{Open: "[", Close: "]"}, input: "a]b", expected: "[a]]b]"},
		{name: "no quote leaves the name", quote: dialect.IdentifierQuote{Open: "", Close: ""}, input: "plain", expected: "plain"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.quote.Quote(testCase.input))
		})
	}
}

func TestJSONOperators(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		features map[string]ast.ExpressionFeature
		name     string
		expected []string
	}{
		{name: "no features", features: nil, expected: nil},
		{name: "no JSON operators", features: map[string]ast.ExpressionFeature{"||": ast.SQLFeatureStringConcat}, expected: nil},
		{
			name: "only the JSON operators, sorted",
			features: map[string]ast.ExpressionFeature{
				"->>": ast.SQLFeatureJSONOp,
				"#>":  ast.SQLFeatureJSONOp,
				"->":  ast.SQLFeatureJSONOp,
				"||":  ast.SQLFeatureStringConcat,
			},
			expected: []string{"#>", "->", "->>"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, dialect.JSONOperators(testCase.features))
		})
	}
}
