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
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

type countdownContext struct {
	context.Context
	remaining *atomic.Int64
}

func (c countdownContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

func (countdownContext) Value(any) any {
	return nil
}

func TestLexStatementsSplitsUnderTheLexicalRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "END CASE closes the CASE without reopening it",
			input:    "CREATE PROCEDURE p() BEGIN CASE x WHEN 1 THEN SELECT 1; END CASE; END; SELECT 2;",
			expected: []string{"CREATE PROCEDURE p() BEGIN CASE x WHEN 1 THEN SELECT 1; END CASE; END", "SELECT 2"},
		},
		{
			name:     "a dollar inside an identifier",
			input:    "SELECT amount$gbp FROM prices; SELECT 1;",
			expected: []string{"SELECT amount$gbp FROM prices", "SELECT 1"},
		},
		{
			name:     "a backslash-escaped quote inside double quotes",
			input:    `INSERT INTO t VALUES ("say \"hi\"; ok"); SELECT 1;`,
			expected: []string{`INSERT INTO t VALUES ("say \"hi\"; ok")`, "SELECT 1"},
		},
		{
			name:     "a DELIMITER block",
			input:    "DELIMITER //\nCREATE PROCEDURE p() BEGIN SELECT 1; END //\nDELIMITER ;\nSELECT 2;",
			expected: []string{"CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := lexStatements(t.Context(), lexical.Rules(), testCase.input, 0)
			require.NoError(t, err)
			texts := make([]string, 0, len(statements))
			for _, statement := range statements {
				texts = append(texts, statement.Span.Text)
				require.Equal(t, statement.Span.Start, statement.Tokens[0].Start)
			}
			require.Equal(t, testCase.expected, texts)

			parsed, err := New().ParseStatements(t.Context(), testCase.input)
			require.NoError(t, err)
			require.Len(t, parsed, len(testCase.expected))
			for index, statement := range parsed {
				require.Equal(t, statements[index].Span.Start, statement.Location)
				require.Equal(t, len(testCase.expected[index]), statement.Length)
			}
		})
	}
}

func TestParseStatementsSkipsALeadingByteOrderMark(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(), sqllex.ByteOrderMark+"SELECT 1;\nSELECT 22")
	require.NoError(t, err)
	require.Len(t, statements, 2)
	require.Equal(t, len(sqllex.ByteOrderMark), statements[0].Location)
	require.Equal(t, len("SELECT 1"), statements[0].Length)
	require.Equal(t, len(sqllex.ByteOrderMark+"SELECT 1;\n"), statements[1].Location)
	require.Equal(t, len("SELECT 22"), statements[1].Length)
}

func TestParseStatementsKeepsExecutableComments(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(), "/*!40101 SET NAMES utf8mb4 */;\nCREATE TABLE t (id INT) /*!50100 ENGINE=InnoDB */;")

	require.NoError(t, err)
	require.Len(t, statements, 2)
}

func TestParseStatementsReportsLexicalErrors(t *testing.T) {
	t.Parallel()

	_, err := New().ParseStatements(t.Context(), "CREATE PROCEDURE p() BEGIN SELECT 1;")
	require.ErrorIs(t, err, sqllex.ErrUnterminatedBlock)
	require.ErrorIs(t, err, sqllex.ErrMalformed)
}

func TestTokeniserErrorNamesTheWholeCharacter(t *testing.T) {
	t.Parallel()

	_, err := tokenise("SELECT 1 € 2")
	var parseError *dialectkit.ParseError
	require.ErrorAs(t, err, &parseError)
	require.Equal(t, &dialectkit.ParseError{Token: "€", Message: "unexpected character", Position: 9}, parseError)
}

func TestApplyDDLReadsAViewInsideAnExecutableComment(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "/*!50001 CREATE VIEW active_users AS SELECT 1 AS id */")

	require.Equal(t, ast.MutationCreateView, mutation.Kind)
	require.Equal(t, "active_users", mutation.TableName)
}

func TestApplyDDLReadsTheViewsMysqldumpWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
	}{
		{name: "modifiers", sql: "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER VIEW `active_users` AS SELECT 1 AS `id`"},
		{name: "OR REPLACE and a quoted host", sql: "CREATE OR REPLACE ALGORITHM = MERGE DEFINER = 'app'@'%' VIEW active_users AS SELECT 1 AS id"},
		{name: "the current user", sql: "CREATE DEFINER=CURRENT_USER() SQL SECURITY INVOKER VIEW active_users AS SELECT 1 AS id"},
		{name: "split across executable comments", sql: "/*!50001 CREATE ALGORITHM=UNDEFINED */\n/*!50013 DEFINER=`root`@`localhost` SQL SECURITY DEFINER */\n/*!50001 VIEW `active_users` AS select 1 AS `id` */"},
		{name: "an ordinary comment between executable ones", sql: "/*!50001 CREATE ALGORITHM=UNDEFINED */ /* not run /*! either */\n/*!50001 VIEW active_users AS select 1 AS id */"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			require.Equal(t, ast.MutationCreateView, mutation.Kind)
			require.Equal(t, "active_users", mutation.TableName)
			require.NotNil(t, mutation.ViewDefinition)
			require.Len(t, mutation.ViewDefinition.OutputColumns, 1)
		})
	}
}

func TestApplyDDLReadsADefinerOnARoutine(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE DEFINER=`root`@`%` FUNCTION f() RETURNS INT DETERMINISTIC RETURN 1")

	require.Equal(t, ast.MutationCreateFunction, mutation.Kind)
	require.Equal(t, "f", mutation.Function.Name)
}

func TestParseStatementsStopsTokenisingAStatementAtTheTokenBudget(t *testing.T) {
	t.Parallel()

	const budget = 5
	engine := New(WithMaxTokensPerStatement(budget))
	statements, err := engine.ParseStatements(t.Context(),
		"CREATE TABLE t (a INT, b INT); INSERT INTO t VALUES (1, 2), (3, 4); TRUNCATE TABLE t")
	require.NoError(t, err)
	require.Len(t, statements, 3)

	testCases := []struct {
		name       string
		statement  ast.ParsedStatement
		overBudget bool
		apply      func(ctx context.Context, statement ast.ParsedStatement) error
		wantBudget bool
	}{
		{
			name: "a DDL statement past the budget fails to apply", statement: statements[0], overBudget: true,
			apply: func(ctx context.Context, statement ast.ParsedStatement) error {
				_, applyError := engine.ApplyDDL(ctx, statement)
				return applyError
			},
			wantBudget: true,
		},
		{
			name: "a statement ApplyDDL passes over stays passed over", statement: statements[1], overBudget: true,
			apply: func(ctx context.Context, statement ast.ParsedStatement) error {
				mutation, applyError := engine.ApplyDDL(ctx, statement)
				assert.Nil(t, mutation)
				return applyError
			},
			wantBudget: false,
		},
		{
			name: "a query past the budget fails to analyse", statement: statements[1], overBudget: true,
			apply: func(ctx context.Context, statement ast.ParsedStatement) error {
				_, analyseError := engine.AnalyseQuery(ctx, nil, statement)
				return analyseError
			},
			wantBudget: true,
		},
		{
			name: "a statement within the budget analyses", statement: statements[2], overBudget: false,
			apply: func(ctx context.Context, statement ast.ParsedStatement) error {
				_, analyseError := engine.AnalyseQuery(ctx, nil, statement)
				return analyseError
			},
			wantBudget: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parsed, isMySQL := testCase.statement.Raw.(*parsedStatement)
			require.True(t, isMySQL)
			assert.Equal(t, testCase.overBudget, parsed.overBudget)
			assert.LessOrEqual(t, len(parsed.tokens), budget)

			err := testCase.apply(t.Context(), testCase.statement)

			if testCase.wantBudget {
				require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
				assert.Contains(t, err.Error(), "more than 5 tokens")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestParseStatementsChecksTheContextBeforeEachStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		checks int64
	}{
		{name: "cancelled before the first statement", checks: 1},
		{name: "cancelled after the second statement", checks: 3},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			remaining := new(atomic.Int64)
			remaining.Store(testCase.checks)
			ctx := countdownContext{Context: t.Context(), remaining: remaining}

			statements, err := New().ParseStatements(ctx, "SELECT 1; SELECT 2; SELECT 3; SELECT 4")

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, statements)
		})
	}
}

func TestParseStatementsHoldsAHugeInsertToTheTokenBudget(t *testing.T) {
	const (
		scriptBytes = 4 << 20
		allowance   = 200 << 20
	)

	var builder strings.Builder
	builder.WriteString("INSERT INTO t VALUES (0, 'first')")
	for builder.Len() < scriptBytes {
		builder.WriteString(", (1, 'row')")
	}
	script := builder.String()
	engine := New()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	statements, err := engine.ParseStatements(t.Context(), script)
	runtime.ReadMemStats(&after)

	require.NoError(t, err)
	require.Len(t, statements, 1)
	parsed, isMySQL := statements[0].Raw.(*parsedStatement)
	require.True(t, isMySQL)
	assert.True(t, parsed.overBudget)
	assert.Len(t, parsed.tokens, dialectkit.DefaultMaxTokensPerStatement)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(allowance))
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Nil(t, mutation)
}

func TestSplitStatements_BeginEndBlockHandling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sql     string
		want    int
		contain []string
	}{
		{
			name: "trigger_without_delimiter",
			sql: `CREATE TABLE accounts (id INT PRIMARY KEY);
CREATE TRIGGER tr_log
    AFTER INSERT ON accounts
    FOR EACH ROW
BEGIN
    INSERT INTO log VALUES (NEW.id);
    INSERT INTO log VALUES (NEW.id);
END;
SELECT 1;`,
			want:    3,
			contain: []string{"CREATE TABLE", "CREATE TRIGGER", "SELECT 1"},
		},
		{
			name: "bare_begin_is_transaction_marker",
			sql:  `BEGIN; SELECT 1; COMMIT;`,
			want: 3,
		},
		{
			name: "stored_procedure_with_end_if",
			sql: `CREATE PROCEDURE p()
BEGIN
    IF x = 1 THEN
        SELECT 1;
    END IF;
END;
SELECT 2;`,
			want:    2,
			contain: []string{"CREATE PROCEDURE", "SELECT 2"},
		},
		{
			name: "stored_procedure_with_end_loop",
			sql: `CREATE PROCEDURE p()
BEGIN
    LOOP
        SET x = x + 1;
    END LOOP;
END;`,
			want:    1,
			contain: []string{"CREATE PROCEDURE"},
		},
		{
			name: "stored_procedure_with_end_while",
			sql: `CREATE PROCEDURE p()
BEGIN
    WHILE x < 10 DO
        SET x = x + 1;
    END WHILE;
END;`,
			want:    1,
			contain: []string{"CREATE PROCEDURE"},
		},
		{
			name: "stored_procedure_with_end_repeat",
			sql: `CREATE PROCEDURE p()
BEGIN
    REPEAT
        SET x = x + 1;
    UNTIL x > 10 END REPEAT;
END;`,
			want:    1,
			contain: []string{"CREATE PROCEDURE"},
		},
		{
			name: "stored_procedure_with_end_case",
			sql: `CREATE PROCEDURE p()
BEGIN
    CASE x
        WHEN 1 THEN SELECT 'one';
        ELSE SELECT 'other';
    END CASE;
END;`,
			want:    1,
			contain: []string{"CREATE PROCEDURE"},
		},
		{

			name:    "begin_used_as_alias_does_not_open_block",
			sql:     `SELECT 1 AS begin; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT 1", "SELECT 2"},
		},
		{

			name:    "begin_used_as_identifier_does_not_open_block",
			sql:     `SELECT begin FROM t; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT begin FROM t", "SELECT 2"},
		},
		{

			name: "scalar_case_in_body_does_not_close_block_early",
			sql: `CREATE PROCEDURE p()
BEGIN
    SET y = CASE x WHEN 1 THEN 2 ELSE 3 END;
    INSERT INTO log VALUES (y);
END;
SELECT 9;`,
			want:    2,
			contain: []string{"CREATE PROCEDURE", "SELECT 9"},
		},
		{

			name: "scalar_case_in_function_return",
			sql: `CREATE FUNCTION f(x INT) RETURNS INT
BEGIN
    RETURN CASE x WHEN 1 THEN 10 ELSE 20 END;
END;
SELECT 7;`,
			want:    2,
			contain: []string{"CREATE FUNCTION", "SELECT 7"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			statements := splitTokens(t, testCase.sql)
			require.Len(t, statements, testCase.want)
			for index, want := range testCase.contain {
				if index >= len(statements) {
					break
				}
				var buffer strings.Builder
				for _, current := range statements[index] {
					buffer.WriteString(current.Value)
					buffer.WriteRune(' ')
				}
				require.Contains(t, buffer.String(), want)
			}
		})
	}
}

func tokeniseSpan(input string, start, end int, doubleQuote sqllex.DoubleQuoteMeaning) ([]token, error) {
	read := statementTokeniser(doubleQuote)(input, start, end)
	var tokens []token
	for {
		current, err := read()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, current)
		if current.Kind == tokenEOF {
			return tokens, nil
		}
	}
}

func tokenise(input string) ([]token, error) {
	return tokeniseSpan(input, 0, len(input), sqllex.DoubleQuoteString)
}

func tokeniseANSIQuotes(input string) ([]token, error) {
	return tokeniseSpan(input, 0, len(input), sqllex.DoubleQuoteIdentifier)
}

func splitTokens(t *testing.T, sql string) [][]token {
	t.Helper()

	statements, err := lexStatements(t.Context(), lexical.Rules(), sql, 0)
	require.NoError(t, err)
	result := make([][]token, 0, len(statements))
	for _, statement := range statements {
		result = append(result, statement.Tokens)
	}
	return result
}
