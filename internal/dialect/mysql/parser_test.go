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

package mysql

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/testkit/scaling"
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
		name    string
		sql     string
		message string
	}{
		{name: "unterminated group", sql: "(a, (b)", message: unmatchedParenthesis},
		{name: "missing opening parenthesis", sql: "a)", message: "expected '('"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			inner, err := newParser(tokens).collectParenthesised()

			assert.Nil(t, inner)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.message, parseError.Message)
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

func TestClassifyDropStatementReadsATemporaryTable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want statementKind
	}{
		{name: "a table", sql: "DROP TABLE a", want: statementKindDropTable},
		{name: "a temporary table", sql: "DROP TEMPORARY TABLE IF EXISTS a", want: statementKindDropTable},
		{name: "TEMPORARY alone", sql: "DROP TEMPORARY", want: statementKindUnknown},
		{name: "TEMPORARY before something else", sql: "DROP TEMPORARY VIEW v", want: statementKindUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			assert.Equal(t, testCase.want, classifyStatement(tokens[:len(tokens)-1]))
		})
	}
}

func TestParserDepthLimitPreventsStackOverflow(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	engine := New(WithMaxTokensPerStatement(4 * depth))

	t.Run("nested expression parentheses", func(t *testing.T) {
		t.Parallel()
		sql := "SELECT " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + " FROM t"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

		require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		assert.Nil(t, analysis)
	})
}

func TestDataModifyingAnalysersHonourDepthGuard(t *testing.T) {
	t.Parallel()

	analysers := map[string]func(*parser) (*ast.RawQueryAnalysis, error){
		"insert": (*parser).analyseInsert,
		"update": (*parser).analyseUpdate,
		"delete": (*parser).analyseDelete,
		"select": (*parser).analyseSelect,
	}
	for name, analyse := range analysers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newParser(nil)
			p.maxParseDepth = 4
			p.analysisDepth = p.maxParseDepth
			_, err := analyse(p)
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		})
	}
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

func TestAnalyseQueryCollectsCompoundArmsIntoOneFlatList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantOperators []ast.CompoundOperator
	}{
		{
			name:          "three arms with mixed operators",
			sql:           "SELECT id FROM a UNION SELECT id FROM b INTERSECT SELECT id FROM c ORDER BY id LIMIT ?",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnion, ast.CompoundIntersect},
		},
		{
			name:          "union all followed by except with a locking clause",
			sql:           "SELECT id FROM a UNION ALL SELECT id FROM b EXCEPT SELECT id FROM c FOR UPDATE",
			wantOperators: []ast.CompoundOperator{ast.CompoundUnionAll, ast.CompoundExcept},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.NoError(t, err)
			require.Len(t, analysis.CompoundBranches, len(testCase.wantOperators))
			for index, branch := range analysis.CompoundBranches {
				assert.Equal(t, testCase.wantOperators[index], branch.Operator)
				require.NotNil(t, branch.Query)
				assert.Empty(t, branch.Query.CompoundBranches, "arms must not nest inside one another")
			}
		})
	}
}

func TestAnalyseQueryHandlesLongCompoundChains(t *testing.T) {
	t.Parallel()

	const arms = 10_000
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1"+strings.Repeat(" UNION SELECT 1", arms-1))
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	assert.Len(t, analysis.CompoundBranches, arms-1)
}

func TestUnbalancedParenthesisFindsTheUnmatchedOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantPosition int
		wantMessage  string
	}{
		{name: "every parenthesis matched", sql: "SELECT f((1), (2))", wantPosition: -1, wantMessage: ""},
		{name: "no parenthesis", sql: "SELECT 1", wantPosition: -1, wantMessage: ""},
		{name: "the innermost of several left open", sql: "SELECT (((1", wantPosition: 9, wantMessage: unmatchedParenthesis},
		{name: "one left open before a matched pair", sql: "SELECT (a, (b)", wantPosition: 7, wantMessage: unmatchedParenthesis},
		{name: "a closing one with nothing to close", sql: "SELECT 1) + (2", wantPosition: 8, wantMessage: "unexpected ')'"},
		{name: "a parenthesis in a string or a comment", sql: "SELECT '(' /* ( */, \"((\"", wantPosition: -1, wantMessage: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			balanceError := unbalancedParenthesis(tokens)

			if testCase.wantPosition < 0 {
				require.NoError(t, balanceError)
				return
			}
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, balanceError, &parseError)
			assert.Equal(t, testCase.wantPosition, parseError.Position)
			assert.ErrorContains(t, balanceError, testCase.wantMessage)
		})
	}
}

func TestUnbalancedParenthesisReadsADeepNestingInLinearTime(t *testing.T) {
	t.Parallel()

	nested := func(depth int) []token {
		tokens, err := tokenise("SELECT " + strings.Repeat("(", depth) + "1")
		require.NoError(t, err)
		return tokens
	}
	check := func(tokens []token) func() {
		return func() {
			require.Error(t, unbalancedParenthesis(tokens))
		}
	}
	smaller, larger := nested(100_000), nested(400_000)

	scaling.Linear(t, check(smaller), check(larger))
}
