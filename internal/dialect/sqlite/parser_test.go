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

package sqlite

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestCollectParenthesisedReturnsTheGroupWithoutCopying(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("(a, (b)) c")
	require.NoError(t, err)
	p := newParser(tokens)

	inner, err := p.collectParenthesised()

	require.NoError(t, err)
	require.Len(t, inner, 5)
	assert.Equal(t, "a", inner[0].Value)
	assert.Same(t, &tokens[1], &inner[0], "the group must be a view of the statement's tokens")
	assert.Equal(t, len(inner), cap(inner), "the view must be capacity-capped so appends cannot overwrite later tokens")
	assert.Equal(t, "c", p.Current().Value)
}

func TestCollectParenthesisedRejectsMalformedGroups(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantErr error
		name    string
		sql     string
	}{
		{name: "unterminated group", sql: "(a, (b)", wantErr: fault.ErrParse},
		{name: "missing opening parenthesis", sql: "a)", wantErr: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			inner, err := newParser(tokens).collectParenthesised()

			require.Error(t, err)
			assert.Nil(t, inner)
			if testCase.wantErr != nil {
				assert.ErrorIs(t, err, testCase.wantErr)
			}
		})
	}
}

func TestRecordSyntaxErrorKeepsTheFirstError(t *testing.T) {
	t.Parallel()

	first := errors.New("first")
	parent := newParser(nil)
	child := newParser(nil)

	child.recordSyntaxError(first)
	child.recordSyntaxError(errors.New("second"))
	parent.adoptSyntaxError(newParser(nil))
	parent.adoptSyntaxError(child)

	assert.Same(t, first, parent.syntaxError)
}

func TestParserDepthLimitPreventsStackOverflow(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	engine := New(WithMaxTokensPerStatement(4 * depth))

	sql := "SELECT " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + " FROM t"
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.ErrorIs(t, err, dialectkit.ErrParseDepth)
	assert.Nil(t, analysis)
}

func TestParserDepthLimitIsConfigurable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		nesting  int
		wantFail bool
	}{
		{name: "nesting within the cap analyses", nesting: 4, wantFail: false},
		{name: "nesting past the cap reports the depth error", nesting: 64, wantFail: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(8))
			sql := "SELECT " + strings.Repeat("(", testCase.nesting) + "1" + strings.Repeat(")", testCase.nesting) + " FROM t"
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

			if testCase.wantFail {
				require.ErrorIs(t, err, dialectkit.ErrParseDepth)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWithMaxParseDepthIgnoresNonPositiveValues(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{0, -1} {
		engine := New(WithMaxParseDepth(depth))

		assert.Equal(t, dialectkit.DefaultMaxParseDepth, engine.settings.Limits.ParseDepth())
	}
}
