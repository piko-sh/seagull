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

package sqllex_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

var executableCommentRules = sqllex.Rules{Comments: sqllex.CommentRules{ExecutableComments: true}}

func triggerRules() sqllex.Rules {
	return withRules(func(rules *sqllex.Rules) {
		rules.ProceduralBlock = func(leadingWords []string) bool {
			return len(leadingWords) > 1 && leadingWords[0] == "CREATE" && leadingWords[1] == "TRIGGER"
		}
	})
}

func delimiterRules() sqllex.Rules {
	return withRules(func(rules *sqllex.Rules) {
		rules.DelimiterCommand = true
		rules.DollarInIdentifiers = true
		rules.Comments.HashLineComments = true
		rules.ProceduralBlock = func(leadingWords []string) bool {
			return len(leadingWords) > 0 && leadingWords[0] == "CREATE"
		}
	})
}

func statementTexts(t *testing.T, rules sqllex.Rules, input string) []string {
	t.Helper()
	statements, err := sqllex.Split(rules, input)
	require.NoError(t, err)
	texts := make([]string, 0, len(statements))
	for _, statement := range statements {
		require.Equal(t, input[statement.Start:statement.End], statement.Text)
		texts = append(texts, statement.Text)
	}
	return texts
}

func TestSplitStatementBoundaries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
		rules    sqllex.Rules
	}{
		{
			name:     "two statements",
			rules:    standardRules,
			input:    "SELECT 1; SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "missing final terminator",
			rules:    standardRules,
			input:    "SELECT 1;\nSELECT 2",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "stray terminators and a comment-only tail make no statements",
			rules:    standardRules,
			input:    ";; SELECT 1;;\n-- trailing note\n/* and more */",
			expected: []string{"SELECT 1"},
		},
		{
			name:     "empty input",
			rules:    standardRules,
			input:    "",
			expected: []string{},
		},
		{
			name:     "comments inside a statement stay",
			rules:    standardRules,
			input:    "SELECT /* ; */ 1 -- ;\n + 2;",
			expected: []string{"SELECT /* ; */ 1 -- ;\n + 2"},
		},
		{
			name:     "terminators inside quotes and identifiers",
			rules:    standardRules,
			input:    `CREATE TABLE "a;b" (c text DEFAULT 'x;y'); SELECT 1;`,
			expected: []string{`CREATE TABLE "a;b" (c text DEFAULT 'x;y')`, "SELECT 1"},
		},
		{
			name:     "a quote inside a quoted identifier",
			rules:    standardRules,
			input:    `CREATE TABLE "o'brien" (id int); SELECT 1;`,
			expected: []string{`CREATE TABLE "o'brien" (id int)`, "SELECT 1"},
		},
		{
			name:     "terminators inside dollar quotes",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DollarQuoting = true }),
			input:    "CREATE FUNCTION f() AS $$ SELECT 1; $$; SELECT 2;",
			expected: []string{"CREATE FUNCTION f() AS $$ SELECT 1; $$", "SELECT 2"},
		},
		{
			name:     "begin as a column name opens no block",
			rules:    triggerRules(),
			input:    "CREATE TABLE t (begin int); CREATE TABLE u (id int);",
			expected: []string{"CREATE TABLE t (begin int)", "CREATE TABLE u (id int)"},
		},
		{
			name:     "begin as an alias opens no block",
			rules:    triggerRules(),
			input:    "SELECT 1 AS begin; SELECT 2;",
			expected: []string{"SELECT 1 AS begin", "SELECT 2"},
		},
		{
			name:     "a leading begin is a transaction statement",
			rules:    triggerRules(),
			input:    "BEGIN; SELECT 1; COMMIT;",
			expected: []string{"BEGIN", "SELECT 1", "COMMIT"},
		},
		{
			name:  "a procedural block holds its terminators",
			rules: triggerRules(),
			input: "CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN 1 THEN 2 END; " +
				"UPDATE d SET e = 1; END; SELECT 1;",
			expected: []string{
				"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN 1 THEN 2 END; UPDATE d SET e = 1; END",
				"SELECT 1",
			},
		},
		{
			name:  "inner control structures do not close the block",
			rules: triggerRules(),
			input: "CREATE TRIGGER t BEGIN IF x THEN y; END IF; LOOP z; END LOOP; WHILE a DO b; END WHILE; " +
				"REPEAT c; END REPEAT; END; SELECT 1;",
			expected: []string{
				"CREATE TRIGGER t BEGIN IF x THEN y; END IF; LOOP z; END LOOP; WHILE a DO b; END WHILE; REPEAT c; END REPEAT; END",
				"SELECT 1",
			},
		},
		{
			name:     "end case closes a case statement once",
			rules:    triggerRules(),
			input:    "CREATE TRIGGER t BEGIN CASE x WHEN 1 THEN y; END /* c */ CASE; END; SELECT 1;",
			expected: []string{"CREATE TRIGGER t BEGIN CASE x WHEN 1 THEN y; END /* c */ CASE; END", "SELECT 1"},
		},
		{
			name:     "nested blocks",
			rules:    triggerRules(),
			input:    "CREATE TRIGGER t BEGIN BEGIN x; END; y; END; SELECT 1;",
			expected: []string{"CREATE TRIGGER t BEGIN BEGIN x; END; y; END", "SELECT 1"},
		},
		{
			name:     "without a procedural predicate every terminator splits",
			rules:    standardRules,
			input:    "CREATE TRIGGER t BEGIN x; END;",
			expected: []string{"CREATE TRIGGER t BEGIN x", "END"},
		},
		{
			name:     "parentheses hold the terminator when asked",
			rules:    withRules(func(rules *sqllex.Rules) { rules.TerminatorOutsideParentheses = true }),
			input:    "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2)); SELECT 1;",
			expected: []string{"CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2))", "SELECT 1"},
		},
		{
			name:     "parentheses do not hold the terminator by default",
			rules:    standardRules,
			input:    "DO (a; b);",
			expected: []string{"DO (a", "b)"},
		},
		{
			name:     "a stray closing parenthesis cannot go negative",
			rules:    withRules(func(rules *sqllex.Rules) { rules.TerminatorOutsideParentheses = true }),
			input:    "SELECT 1); SELECT 2;",
			expected: []string{"SELECT 1)", "SELECT 2"},
		},
		{
			name:  "delimiter lines change the terminator",
			rules: delimiterRules(),
			input: "CREATE TABLE t (id int);\n\nDELIMITER //\nCREATE FUNCTION f() RETURNS INT BEGIN DECLARE x INT; RETURN 1; END //\n" +
				"DELIMITER ;\nSELECT f();",
			expected: []string{
				"CREATE TABLE t (id int)",
				"CREATE FUNCTION f() RETURNS INT BEGIN DECLARE x INT; RETURN 1; END",
				"SELECT f()",
			},
		},
		{
			name:     "a custom terminator ends a statement inside a word",
			rules:    delimiterRules(),
			input:    "DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; END$$\nDELIMITER ;\nSELECT 2;",
			expected: []string{"CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2"},
		},
		{
			name:     "a custom terminator inside quotes or comments does not split",
			rules:    delimiterRules(),
			input:    "DELIMITER //\nSELECT '//' # //\n, 1 //\nDELIMITER ;",
			expected: []string{"SELECT '//' # //\n, 1"},
		},
		{
			name:     "a custom terminator ignores blocks",
			rules:    delimiterRules(),
			input:    "DELIMITER //\nCREATE PROCEDURE p() BEGIN SELECT 1; // SELECT 2 //",
			expected: []string{"CREATE PROCEDURE p() BEGIN SELECT 1;", "SELECT 2"},
		},
		{
			name:     "delimiter mid-statement is a word",
			rules:    delimiterRules(),
			input:    "SELECT delimiter //;\nSELECT 2;",
			expected: []string{"SELECT delimiter //", "SELECT 2"},
		},
		{
			name:     "delimiter without an argument is a word",
			rules:    delimiterRules(),
			input:    "DELIMITER\n; SELECT 1;",
			expected: []string{"DELIMITER", "SELECT 1"},
		},
		{
			name:     "delimiter needs the command enabled",
			rules:    standardRules,
			input:    "DELIMITER //\nSELECT 1 //\n",
			expected: []string{"DELIMITER //\nSELECT 1 //"},
		},
		{
			name:     "meta-command lines belong to no statement",
			rules:    withRules(func(rules *sqllex.Rules) { rules.MetaCommands = true }),
			input:    "\\restrict abc\nSET x = 0;\nCREATE TABLE t (id int); \\connect other\n\\unrestrict abc\n",
			expected: []string{"SET x = 0", "CREATE TABLE t (id int)"},
		},
		{
			name:     "a backslash inside a statement is not a meta-command",
			rules:    withRules(func(rules *sqllex.Rules) { rules.MetaCommands = true }),
			input:    "SELECT 1 \\gset\nSELECT '\\n';",
			expected: []string{"SELECT 1 \\gset\nSELECT '\\n'"},
		},
		{
			name:     "meta-commands need the rule enabled",
			rules:    standardRules,
			input:    "\\restrict abc\nSELECT 1;",
			expected: []string{"\\restrict abc\nSELECT 1"},
		},
		{
			name:     "a rules terminator other than the default",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Terminator = "!!" }),
			input:    "SELECT 1; SELECT 2 !! SELECT '!!' !!",
			expected: []string{"SELECT 1; SELECT 2", "SELECT '!!'"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, statementTexts(t, testCase.rules, testCase.input))
		})
	}
}

func TestSplitRecordsSpansAndPositions(t *testing.T) {
	t.Parallel()

	input := "\xef\xbb\xbf-- sgl.query(name: A)\nSELECT 'é' ; \n  /* b */ SELECT\n 2 -- tail\n;"
	statements, err := sqllex.Split(standardRules, input)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	first := statements[0]
	require.Equal(t, "SELECT 'é'", first.Text)
	require.Equal(t, 0, first.LeadingStart)
	require.Equal(t, "\xef\xbb\xbf-- sgl.query(name: A)\nSELECT 'é'", input[first.LeadingStart:first.End])
	require.Equal(t, 2, first.Line)
	require.Equal(t, 1, first.Column)

	second := statements[1]
	require.Equal(t, "SELECT\n 2", second.Text)
	require.Equal(t, " \n  /* b */ SELECT\n 2", input[second.LeadingStart:second.End])
	require.Equal(t, 3, second.Line)
	require.Equal(t, 11, second.Column)
}

func TestSplitColumnsCountRunes(t *testing.T) {
	t.Parallel()

	statements, err := sqllex.Split(standardRules, "SELECT 'ü'; SELECT 2;")
	require.NoError(t, err)
	require.Len(t, statements, 2)
	require.Equal(t, 1, statements[1].Line)
	require.Equal(t, 13, statements[1].Column)
	require.Equal(t, 13, statements[1].Start)
}

func TestSplitErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sentinel error
		name     string
		input    string
		message  string
		rules    sqllex.Rules
		offset   int
		line     int
		column   int
	}{
		{
			name:     "unterminated string",
			rules:    standardRules,
			input:    "SELECT 1;\nSELECT 'abc",
			sentinel: sqllex.ErrUnterminatedString,
			offset:   17,
			line:     2,
			column:   8,
			message:  `unterminated string literal at line 2, column 8 (byte 17) near "'abc"`,
		},
		{
			name:     "unterminated quoted identifier",
			rules:    standardRules,
			input:    `SELECT "a`,
			sentinel: sqllex.ErrUnterminatedQuotedIdentifier,
			offset:   7,
			line:     1,
			column:   8,
			message:  `unterminated quoted identifier at line 1, column 8 (byte 7) near "\"a"`,
		},
		{
			name:     "unterminated bracket identifier",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.Brackets = true }),
			input:    "SELECT [a",
			sentinel: sqllex.ErrUnterminatedQuotedIdentifier,
			offset:   7,
			line:     1,
			column:   8,
			message:  `unterminated quoted identifier at line 1, column 8 (byte 7) near "[a"`,
		},
		{
			name:     "unterminated backslash escape at the end",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.SingleQuoteEscape = sqllex.EscapeBackslash }),
			input:    `'a\`,
			sentinel: sqllex.ErrUnterminatedString,
			offset:   0,
			line:     1,
			column:   1,
			message:  `unterminated string literal at line 1, column 1 (byte 0) near "'a\\"`,
		},
		{
			name:     "unterminated block comment after multi-byte text",
			rules:    standardRules,
			input:    "SELECT 'ü'; /* open",
			sentinel: sqllex.ErrUnterminatedComment,
			offset:   13,
			line:     1,
			column:   13,
			message:  `unterminated block comment at line 1, column 13 (byte 13) near "/* open"`,
		},
		{
			name:     "unterminated nested block comment",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Comments.NestedBlockComments = true }),
			input:    "/* a /* b */",
			sentinel: sqllex.ErrUnterminatedComment,
			offset:   0,
			line:     1,
			column:   1,
			message:  `unterminated block comment at line 1, column 1 (byte 0) near "/* a /* b */"`,
		},
		{
			name:     "unterminated dollar quote",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DollarQuoting = true }),
			input:    "DO $body$ BEGIN END $other$",
			sentinel: sqllex.ErrUnterminatedDollarQuote,
			offset:   3,
			line:     1,
			column:   4,
			message:  `unterminated dollar-quoted string at line 1, column 4 (byte 3) near "$body$ BEGIN END..."`,
		},
		{
			name:     "unterminated procedural block",
			rules:    triggerRules(),
			input:    "CREATE TRIGGER t\nBEGIN SELECT 1;",
			sentinel: sqllex.ErrUnterminatedBlock,
			offset:   17,
			line:     2,
			column:   1,
			message:  `unterminated procedural block at line 2, column 1 (byte 17) near "BEGIN SELECT 1;"`,
		},
		{
			name:     "unterminated case inside a block",
			rules:    triggerRules(),
			input:    "CREATE TRIGGER t BEGIN CASE x WHEN 1 THEN 2; END",
			sentinel: sqllex.ErrUnterminatedBlock,
			offset:   17,
			line:     1,
			column:   18,
			message:  `unterminated procedural block at line 1, column 18 (byte 17) near "BEGIN CASE x WHE..."`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			statements, err := sqllex.Split(testCase.rules, testCase.input)
			require.Nil(t, statements)
			require.ErrorIs(t, err, testCase.sentinel)
			require.ErrorIs(t, err, sqllex.ErrMalformed)
			var lexicalError *sqllex.Error
			require.ErrorAs(t, err, &lexicalError)
			require.Equal(t, testCase.offset, lexicalError.Offset)
			require.Equal(t, testCase.line, lexicalError.Line)
			require.Equal(t, testCase.column, lexicalError.Column)
			require.Equal(t, testCase.message, err.Error())
		})
	}
}

func TestErrorExcerptStaysValidUTF8(t *testing.T) {
	t.Parallel()

	_, err := sqllex.Tokens(standardRules, "'\xff日本語のテキストはここで終わらない")
	var lexicalError *sqllex.Error
	require.ErrorAs(t, err, &lexicalError)
	require.Equal(t, "'�日本語のテキストはここで終わ...", lexicalError.Excerpt)
}

func TestProceduralBlockSeesLeadingWords(t *testing.T) {
	t.Parallel()

	var seen [][]string
	rules := withRules(func(rules *sqllex.Rules) {
		rules.ProceduralBlock = func(leadingWords []string) bool {
			seen = append(seen, append([]string(nil), leadingWords...))
			return false
		}
	})
	_, err := sqllex.Split(rules, `create "quoted" 'text' or Replace begin; begin; x begin`)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"CREATE", "OR", "REPLACE"}, {"X"}}, seen)
}

func TestProceduralBlockLeadingWordLimit(t *testing.T) {
	t.Parallel()

	var longest int
	rules := withRules(func(rules *sqllex.Rules) {
		rules.ProceduralBlock = func(leadingWords []string) bool {
			longest = max(longest, len(leadingWords))
			return false
		}
	})
	input := "SELECT"
	for range sqllex.LeadingWordLimit + 10 {
		input += " w"
	}
	_, err := sqllex.Split(rules, input+" begin;")
	require.NoError(t, err)
	require.Equal(t, sqllex.LeadingWordLimit, longest)
}

func TestSplitReturnsProceduralBlockPanicAsCallbackError(t *testing.T) {
	t.Parallel()

	errLeadingWords := errors.New("leading words rejected")

	testCases := []struct {
		procedural   sqllex.ProceduralBlockFunc
		name         string
		input        string
		message      string
		valueIsError bool
	}{
		{
			name:       "string value",
			procedural: func([]string) bool { panic("boom") },
			input:      "CREATE TRIGGER t\nBEGIN SELECT 1; END;\nCREATE TRIGGER u BEGIN SELECT 2; END;",
			message:    "checking BEGIN at line 2, column 1 (byte 17): lexical rules callback ProceduralBlock panicked: boom",
		},
		{
			name:         "error value",
			procedural:   func([]string) bool { panic(errLeadingWords) },
			input:        "SELECT 1; create procedure p() begin select 1; end; x begin;",
			message:      "checking begin at line 1, column 32 (byte 31): lexical rules callback ProceduralBlock panicked: leading words rejected",
			valueIsError: true,
		},
		{
			name: "runtime error value",
			procedural: func(leadingWords []string) bool {
				return leadingWords[sqllex.LeadingWordLimit] == "CREATE"
			},
			input:        "CREATE TRIGGER t BEGIN SELECT 1; END; x BEGIN;",
			message:      "checking BEGIN at line 1, column 18 (byte 17): lexical rules callback ProceduralBlock panicked: runtime error: index out of range [32] with length 3",
			valueIsError: true,
		},
		{
			name:         "nil value",
			procedural:   func([]string) bool { panic(nil) },
			input:        "x BEGIN; y BEGIN;",
			message:      "checking BEGIN at line 1, column 3 (byte 2): lexical rules callback ProceduralBlock panicked: runtime error: panic called with nil argument",
			valueIsError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			rules := withRules(func(rules *sqllex.Rules) {
				rules.ProceduralBlock = func(leadingWords []string) bool {
					calls++
					return testCase.procedural(leadingWords)
				}
			})
			statements, err := sqllex.Split(rules, testCase.input)
			require.Nil(t, statements)
			require.Equal(t, 1, calls)
			require.ErrorIs(t, err, sqllex.ErrRulesCallback)
			require.NotErrorIs(t, err, sqllex.ErrMalformed)
			require.Equal(t, testCase.message, err.Error())
			var callbackError *sqllex.CallbackError
			require.ErrorAs(t, err, &callbackError)
			require.Equal(t, "ProceduralBlock", callbackError.Callback)
			wrapped, isError := callbackError.Value.(error)
			require.Equal(t, testCase.valueIsError, isError)
			if isError {
				require.ErrorIs(t, err, wrapped)
			}
		})
	}
}

func TestSplitKeepsExecutableComments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "statement that is only an executable comment",
			input:    "/*!40101 SET NAMES utf8mb4 */;\nCREATE TABLE t (id INT);",
			expected: []string{"/*!40101 SET NAMES utf8mb4 */", "CREATE TABLE t (id INT)"},
		},
		{
			name:     "trailing executable comment stays in the statement",
			input:    "CREATE TABLE t (id INT) /*!50100 ENGINE=InnoDB */;",
			expected: []string{"CREATE TABLE t (id INT) /*!50100 ENGINE=InnoDB */"},
		},
		{
			name:     "versioned mariadb executable comment",
			input:    "/*M!100100 SET sql_mode = 'ANSI' */;",
			expected: []string{"/*M!100100 SET sql_mode = 'ANSI' */"},
		},
		{
			name:     "terminator inside an executable comment does not split",
			input:    "/*!40101 SET @a = 1; */;",
			expected: []string{"/*!40101 SET @a = 1; */"},
		},
		{
			name:     "ordinary block comment is still noise",
			input:    "/* note */ SELECT 1;",
			expected: []string{"SELECT 1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := sqllex.Split(executableCommentRules, testCase.input)

			require.NoError(t, err)
			texts := make([]string, 0, len(statements))
			for _, statement := range statements {
				texts = append(texts, statement.Text)
			}
			require.Equal(t, testCase.expected, texts)
		})
	}
}

func TestExecutableCommentsAreNoiseWhenTheRuleIsOff(t *testing.T) {
	t.Parallel()

	statements, err := sqllex.Split(sqllex.Rules{}, "/*!40101 SET NAMES utf8mb4 */;\nSELECT 1;")

	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Equal(t, "SELECT 1", statements[0].Text)
}
