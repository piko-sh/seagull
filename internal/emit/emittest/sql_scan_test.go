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

package emittest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqllex"
)

func statementOf(sql string, arguments ...laneArgument) laneStatement {
	return laneStatement{SQL: sql, Arguments: arguments}
}

func positional(text string) laneArgument {
	return laneArgument{Text: text}
}

func named(name, text string) laneArgument {
	return laneArgument{Name: name, Text: text}
}

func TestArgumentProblemsMatchArgumentsToPlaceholders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		engine    dialect.DialectPort
		statement laneStatement
		expected  []string
	}{
		{name: "numbered placeholders bound once each", engine: postgres.New(),
			statement: statementOf("SELECT 1 FROM t WHERE a = $1 AND b = $2 OR c = $1", positional("x"), positional("y")), expected: nil},
		{name: "numbers that skip", engine: postgres.New(), statement: statementOf("SELECT 1 FROM t WHERE a = $1 AND b = $3", positional("x"), positional("y")),
			expected: []string{"binds 2 arguments to 3 placeholders", "numbers its placeholders [1 3], skipping some"}},
		{name: "anonymous placeholders bound once per occurrence", engine: mysql.New(),
			statement: statementOf("SELECT 1 FROM t WHERE a = ? OR b = ?", positional("x")), expected: []string{"binds 1 arguments to 2 placeholders"}},
		{name: "named placeholders bound by name", engine: clickhouse.New(),
			statement: statementOf("SELECT 1 FROM t WHERE a = {p_a:UInt64}", named("p_a", "1")), expected: nil},
		{name: "a named placeholder no argument is named for", engine: clickhouse.New(),
			statement: statementOf("SELECT 1 FROM t WHERE a = {p_a:UInt64}", named("p_b", "1")), expected: []string{"binds no argument named p_a"}},
		{name: "the positional form the dialect's lexical rules leave out", engine: clickhouse.New(),
			statement: statementOf("SELECT 1 FROM t WHERE a = $1 AND b = $2", positional("x"), positional("y")), expected: nil},
		{name: "a source form sent to the driver", engine: postgres.New(), statement: statementOf("SELECT 1 FROM t WHERE a = :a"),
			expected: []string{`sends placeholders [":a"], which are not in the execution form $1`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := testCase.engine.Info()
			require.Equal(t, testCase.expected, argumentProblems(testCase.statement, &info))
		})
	}
}

func TestStatementScanFindsClausesListsAndRows(t *testing.T) {
	t.Parallel()

	info := postgres.New().Info()
	scan, err := scanStatement("INSERT INTO t (a, b) VALUES ($1, lower($2)), ($3, $4) ON CONFLICT (a) DO UPDATE SET b = $2", &info)
	require.NoError(t, err)
	rows, outside := scan.valuesRows()
	require.Equal(t, 2, rows)
	require.Equal(t, 1, outside)

	scan, err = scanStatement("SELECT a FROM t WHERE a IN ($1, $2, $3) AND b NOT IN ($4) AND c IN () ORDER BY a LIMIT 5", &info)
	require.NoError(t, err)
	require.Equal(t, []int{3, 1, 0}, scan.inListPlaceholders())
	require.Equal(t, 1, scan.emptyLists())
	require.Len(t, scan.clauses("ORDER", "BY"), 1)
	require.Len(t, scan.clauses("LIMIT"), 1)
}

func TestConditionMixesConnectivesOnlyOutsideParentheses(t *testing.T) {
	t.Parallel()

	info := postgres.New().Info()
	testCases := []struct {
		sql      string
		expected bool
	}{
		{sql: "SELECT a FROM t WHERE a = 1 OR b = 2 AND c = 3", expected: true},
		{sql: "SELECT a FROM t WHERE (a = 1 OR b = 2) AND c = 3", expected: false},
		{sql: "SELECT a FROM t WHERE a = 1 AND b = 2", expected: false},
		{sql: "SELECT a FROM t WHERE a = 1 OR b = 2 ORDER BY a", expected: false},
		{sql: "SELECT a FROM t", expected: false},
	}
	for _, testCase := range testCases {
		scan, err := scanStatement(testCase.sql, &info)
		require.NoError(t, err)
		require.Equal(t, testCase.expected, scan.conditionMixesConnectives(), testCase.sql)
	}
}

func TestExecutionRulesReadEveryFormTheGeneratedCodeWrites(t *testing.T) {
	t.Parallel()

	for _, engine := range []dialect.DialectPort{postgres.New(), mysql.New(), clickhouse.New()} {
		info := engine.Info()
		rules := executionRules(&info)
		for _, sigil := range []dialect.PlaceholderSigil{info.Placeholders.Execution, info.Placeholders.PositionalForm()} {
			require.True(t, readsAsParameter(rules, sigil.Render(1, "name", "Type")), "%s reads %s", info.Name, sigil.Render(1, "name", "Type"))
		}
	}
}

func TestParameterRuleExtensionsTeachTheRulesEachPlaceholderForm(t *testing.T) {
	t.Parallel()

	base := sqllex.Rules{Parameters: sqllex.ParameterRules{NamedSigils: ":"}}
	testCases := []struct {
		name     string
		sigil    dialect.PlaceholderSigil
		expected []sqllex.ParameterRules
		reads    int
	}{
		{
			name:  "a named sigil is added to the named sigils",
			sigil: dialect.PlaceholderSigil{Prefix: "@", Kind: dialect.PlaceholderNamed},
			expected: []sqllex.ParameterRules{
				{NamedSigils: ":", DollarNumbers: true},
				{NamedSigils: ":", QuestionMark: true},
				{NamedSigils: ":", QuestionMark: true, NumberedQuestionMark: true},
				{NamedSigils: ":", Braced: true},
				{NamedSigils: ":@"},
			},
			reads: 4,
		},
		{
			name:  "a form without a prefix adds no sigil",
			sigil: dialect.PlaceholderSigil{Prefix: "", Kind: dialect.PlaceholderAnonymous},
			expected: []sqllex.ParameterRules{
				{NamedSigils: ":", DollarNumbers: true},
				{NamedSigils: ":", QuestionMark: true},
				{NamedSigils: ":", QuestionMark: true, NumberedQuestionMark: true},
				{NamedSigils: ":", Braced: true},
				{NamedSigils: ":"},
			},
			reads: -1,
		},
		{
			name:     "dollar numbers are read by the first extension",
			sigil:    dialect.PlaceholderSigil{Prefix: "$", Kind: dialect.PlaceholderNumbered},
			expected: nil,
			reads:    0,
		},
		{
			name:     "question marks are read by the second extension",
			sigil:    dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderAnonymous},
			expected: nil,
			reads:    1,
		},
		{
			name:     "numbered question marks are read by the third extension",
			sigil:    dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderNumbered},
			expected: nil,
			reads:    2,
		},
		{
			name:     "braced typed names are read by the fourth extension",
			sigil:    dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed},
			expected: nil,
			reads:    3,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			extensions := parameterRuleExtensions(base, testCase.sigil)

			require.Len(t, extensions, 5)
			if testCase.expected != nil {
				parameters := make([]sqllex.ParameterRules, 0, len(extensions))
				for _, extended := range extensions {
					parameters = append(parameters, extended.Parameters)
				}
				require.Equal(t, testCase.expected, parameters)
			}
			if testCase.reads < 0 {
				return
			}
			sample := testCase.sigil.Render(1, "name", "Type")
			require.False(t, readsAsParameter(base, sample), "the rules given do not read %s", sample)
			require.True(t, readsAsParameter(extensions[testCase.reads], sample), "extension %d reads %s", testCase.reads, sample)
		})
	}
}
