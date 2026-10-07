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

package clickhouse

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func tokeniseStatement(t *testing.T, sql string) []token {
	t.Helper()
	statements := splitTokens(t, sql)
	require.Len(t, statements, 1)
	return statements[0]
}

func TestCollectParenthesisedSharesParserTokens(t *testing.T) {
	t.Parallel()

	statementParser := newParser(tokeniseStatement(t, "(a, (b), c) d"))
	inner, err := statementParser.collectParenthesised()
	require.NoError(t, err)

	require.Len(t, inner, 7)
	assert.Same(t, &statementParser.Tokens[1], &inner[0], "the group must be a view of the parser's tokens, not a copy")
	assert.Equal(t, len(inner), cap(inner), "capacity must be clipped so an append cannot overwrite later tokens")
	assert.Equal(t, "d", statementParser.Current().Value)

	extended := append(inner, token{Kind: tokenIdentifier, Value: "x", Start: 0})
	require.Len(t, extended, 8)
	assert.Equal(t, tokenRightParen, statementParser.Tokens[8].Kind, "appending to the group must leave the parser's tokens intact")
}

func TestCollectParenthesisedErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "cursor not on an opening paren", sql: "a (b)", wantMessage: "expected '('"},
		{name: "unterminated group", sql: "(a, (b)", wantMessage: messageUnmatchedParenthesis},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := newParser(tokeniseStatement(t, testCase.sql))
			inner, err := statementParser.collectParenthesised()
			require.Error(t, err)
			assert.Nil(t, inner)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestDeeplyNestedDerivedTablesAllocateLinearly(t *testing.T) {
	const (
		nesting     = 200
		paddingRows = 5_000
		allowance   = 32 << 20
	)
	var builder strings.Builder
	builder.WriteString(strings.Repeat("SELECT * FROM (", nesting))
	builder.WriteString("SELECT 1 AS x WHERE x IN (")
	for index := range paddingRows {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("1")
	}
	builder.WriteString(")")
	builder.WriteString(strings.Repeat(") AS t", nesting))

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), builder.String())
	require.NoError(t, err)
	require.Len(t, statements, 1)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	runtime.ReadMemStats(&after)

	require.NoError(t, err)
	require.NotNil(t, analysis)
	allocated := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, allocated, uint64(allowance),
		"re-parsing each nesting level must not copy the remaining tokens")
}

func TestExpectKeywordSequence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantErr      string
		keywords     []string
		wantPosition int
	}{
		{name: "all keywords present", sql: "DROP TABLE t", keywords: []string{"DROP", "TABLE"}, wantPosition: 2},
		{name: "case insensitive", sql: "drop table t", keywords: []string{"DROP", "TABLE"}, wantPosition: 2},
		{name: "second keyword missing", sql: "DROP VIEW t", keywords: []string{"DROP", "TABLE"}, wantErr: `near "VIEW": expected keyword TABLE`, wantPosition: 1},
		{name: "first keyword missing", sql: "SELECT 1", keywords: []string{"DROP"}, wantErr: `near "SELECT": expected keyword DROP`, wantPosition: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := newParser(tokeniseStatement(t, testCase.sql))
			err := statementParser.expectKeywordSequence(testCase.keywords...)
			if testCase.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
			}
			assert.Equal(t, testCase.wantPosition, statementParser.Position)
		})
	}
}

func TestNewChildParserInheritsLimits(t *testing.T) {
	t.Parallel()

	parent := newParser(nil)
	parent.analysisDepth = 3
	parent.expressionDepth = 5
	parent.maxParseDepth = 17
	parent.types = newTypeResolver(nil, nil, 9)
	parent.syntaxError = dialectkit.ErrParseDepth

	child := parent.newChildParser([]token{{Kind: tokenIdentifier, Value: "x", Start: 0}})

	assert.Equal(t, 3, child.analysisDepth)
	assert.Equal(t, 5, child.expressionDepth)
	assert.Equal(t, 17, child.maxParseDepth)
	assert.Same(t, parent.types, child.types)
	assert.NoError(t, child.syntaxError, "a child starts without its parent's syntax error")
	assert.Equal(t, 0, child.Position)
}

func TestRecordSyntaxErrorKeepsFirst(t *testing.T) {
	t.Parallel()

	first := errors.New("first")
	statementParser := newParser(nil)
	statementParser.recordSyntaxError(first)
	statementParser.recordSyntaxError(errors.New("second"))

	assert.Same(t, first, statementParser.syntaxError)
}

func TestAbsorbChildFailureRecordsOnlyDepthLimits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err       error
		name      string
		wantStick bool
	}{
		{name: "nil failure", err: nil, wantStick: false},
		{name: "ordinary syntax failure", err: errors.New("expected SELECT"), wantStick: false},
		{name: "analysis depth", err: dialectkit.ErrParseDepth, wantStick: true},
		{name: "expression depth", err: dialectkit.ErrParseDepth, wantStick: true},
		{name: "type depth wrapped", err: errors.Join(errors.New("context"), dialectkit.ErrParseDepth), wantStick: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := newParser(nil)
			statementParser.absorbChildFailure(testCase.err)
			if testCase.wantStick {
				assert.ErrorIs(t, statementParser.syntaxError, testCase.err)
				return
			}
			assert.NoError(t, statementParser.syntaxError)
		})
	}
}

func TestEveryDDLHandlerRejectsAWrongLeadingKeyword(t *testing.T) {
	t.Parallel()

	engine := New()
	tokens := tokeniseStatement(t, "SELECT 1")
	for kind, handler := range ddlParserDispatch {
		if handler == nil {
			continue
		}
		statement := ast.ParsedStatement{
			Raw:      &parsedStatement{tokens: tokens, kind: kind},
			Location: 0,
			Length:   len("SELECT 1"),
		}
		mutation, err := engine.ApplyDDL(t.Context(), statement)
		require.Error(t, err, "statement kind %d", kind)
		assert.Nil(t, mutation)
		assert.NotContains(t, err.Error(), "panic", "statement kind %d must fail as a syntax error", kind)
	}
}

func TestApplyDDLReportsSyntaxErrorsWithoutPanicking(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{name: "IF NOT EXISTS before FUNCTION", sql: "CREATE IF NOT EXISTS FUNCTION f AS (x) -> x", wantErr: `near "IF": expected keyword FUNCTION`},
		{name: "IF NOT EXISTS before VIEW", sql: "CREATE IF NOT EXISTS VIEW v AS SELECT 1", wantErr: `near "IF": expected keyword VIEW`},
		{name: "IF NOT EXISTS before DATABASE", sql: "CREATE IF NOT EXISTS DATABASE d", wantErr: `near "IF": expected keyword DATABASE`},
		{name: "IF NOT EXISTS before DICTIONARY", sql: "CREATE IF NOT EXISTS DICTIONARY d", wantErr: `near "IF": expected keyword DICTIONARY`},
		{name: "IF NOT EXISTS before MATERIALIZED VIEW", sql: "CREATE IF NOT EXISTS MATERIALIZED VIEW v AS SELECT 1", wantErr: `near "IF": expected keyword MATERIALIZED`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.Error(t, err)
			assert.Nil(t, mutation)
			assert.Contains(t, err.Error(), testCase.wantErr)
			assert.NotContains(t, err.Error(), "panic")
		})
	}
}

func TestNestingThroughScalarSubqueriesCountsAgainstOneDepthLimit(t *testing.T) {
	t.Parallel()

	const maxDepth = 32
	nested := func(levels int) string {
		return "SELECT " + strings.Repeat("(1 + (SELECT ", levels) + "1" + strings.Repeat("))", levels)
	}
	testCases := []struct {
		name    string
		sql     string
		tooDeep bool
	}{
		{name: "a few levels", sql: nested(4), tooDeep: false},
		{name: "parentheses alternating with subqueries past the limit", sql: nested(20), tooDeep: true},
		{name: "a condition alternating parentheses with subqueries past the limit", sql: "SELECT 1 WHERE " + strings.TrimPrefix(nested(20), "SELECT ") + " = 1", tooDeep: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(maxDepth))
			statement := parseOneStatement(t, engine, testCase.sql)
			_, err := engine.AnalyseQuery(t.Context(), nil, statement)
			if !testCase.tooDeep {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
			assert.Contains(t, err.Error(), "limit 32")
		})
	}
}

func TestUnbalancedBracketFindsTheUnmatchedToken(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		token    string
		message  string
		position int
	}{
		{name: "balanced", sql: "SELECT f((1), [2, (3)])", token: "", message: "", position: 0},
		{name: "an unclosed parenthesis", sql: "SELECT * FROM numbers(10", token: "(", message: messageUnmatchedParenthesis, position: 21},
		{name: "an unclosed bracket", sql: "SELECT [1, 2", token: "[", message: messageUnmatchedBracket, position: 7},
		{name: "the innermost of two unclosed openers", sql: "SELECT (1, [2", token: "[", message: messageUnmatchedBracket, position: 11},
		{name: "a closer with no opener", sql: "SELECT 1)", token: ")", message: "unexpected ')'", position: 8},
		{name: "a closer of the other kind", sql: "SELECT (1]", token: "]", message: "unexpected ']'", position: 9},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := unbalancedBracket(tokeniseStatement(t, testCase.sql))
			if testCase.token == "" {
				require.NoError(t, err)
				return
			}
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, &dialectkit.ParseError{Token: testCase.token, Message: testCase.message, Position: testCase.position}, parseError)
		})
	}
}

func TestApplyDDLRefusesUnbalancedStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "an unclosed column list", sql: "CREATE TABLE t (id UInt64 ENGINE = Memory", token: "("},
		{name: "an unclosed codec", sql: "CREATE TABLE t (id UInt64 CODEC(ZSTD(1)) ENGINE = Memory", token: "("},
		{name: "a stray bracket", sql: "CREATE TABLE t (id UInt64) ENGINE = Memory ORDER BY id]", token: "]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Nil(t, mutation)
		})
	}

	mutation, err := applyDDL(t, "CREATE TABLE t (id UInt64 CODEC(ZSTD(1))) ENGINE = Memory")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 1)
}

func TestClassify_Explain(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindExplain, classify(t, "EXPLAIN SELECT 1"))
	assert.Equal(t, statementKindExplain, classify(t, "EXPLAIN PLAN SELECT id FROM t"))
}

func TestClassify_Describe(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindDescribeTable, classify(t, "DESCRIBE TABLE users"))
	assert.Equal(t, statementKindDescribeTable, classify(t, "DESC users"))
}

func TestClassify_CheckTable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCheckTable, classify(t, "CHECK TABLE users"))
}

func TestClassify_BackupRestore(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindBackup, classify(t, "BACKUP TABLE users TO Disk('backup', 'archive')"))
	assert.Equal(t, statementKindRestore, classify(t, "RESTORE TABLE users FROM Disk('backup', 'archive')"))
}

func TestClassify_Kill(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindKillQuery, classify(t, "KILL QUERY WHERE query_id = 'abc'"))
	assert.Equal(t, statementKindKillMutation, classify(t, "KILL MUTATION WHERE mutation_id = 'abc'"))
}

func TestClassify_AttachDetach(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindAttachTable, classify(t, "ATTACH TABLE users"))
	assert.Equal(t, statementKindDetachTable, classify(t, "DETACH TABLE users"))
}

func TestClassify_RBACCreateAlterDrop(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateUser, classify(t, "CREATE USER alice IDENTIFIED BY 'secret'"))
	assert.Equal(t, statementKindAlterUser, classify(t, "ALTER USER alice IDENTIFIED BY 'newsecret'"))
	assert.Equal(t, statementKindDropUser, classify(t, "DROP USER alice"))

	assert.Equal(t, statementKindCreateRole, classify(t, "CREATE ROLE admins"))
	assert.Equal(t, statementKindAlterRole, classify(t, "ALTER ROLE admins SETTINGS max_memory_usage = 1000"))
	assert.Equal(t, statementKindDropRole, classify(t, "DROP ROLE admins"))

	assert.Equal(t, statementKindCreateQuota, classify(t, "CREATE QUOTA q1 FOR INTERVAL 1 day MAX queries = 100"))
	assert.Equal(t, statementKindAlterQuota, classify(t, "ALTER QUOTA q1 FOR INTERVAL 1 day MAX queries = 200"))
	assert.Equal(t, statementKindDropQuota, classify(t, "DROP QUOTA q1"))

	assert.Equal(t, statementKindCreatePolicy, classify(t, "CREATE ROW POLICY policy1 ON users USING id = 1 TO alice"))
	assert.Equal(t, statementKindAlterPolicy, classify(t, "ALTER ROW POLICY policy1 ON users USING id = 2"))
	assert.Equal(t, statementKindDropPolicy, classify(t, "DROP ROW POLICY policy1 ON users"))

	assert.Equal(t, statementKindCreateSettingsProfile, classify(t,
		"CREATE SETTINGS PROFILE p1 SETTINGS max_memory_usage = 1000"))
	assert.Equal(t, statementKindAlterSettingsProfile, classify(t,
		"ALTER SETTINGS PROFILE p1 SETTINGS max_memory_usage = 2000"))
	assert.Equal(t, statementKindDropSettingsProfile, classify(t, "DROP SETTINGS PROFILE p1"))
}

func TestClassify_GrantRevoke(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindGrant, classify(t, "GRANT SELECT ON db.t TO alice"))
	assert.Equal(t, statementKindRevoke, classify(t, "REVOKE SELECT ON db.t FROM alice"))
}

func classify(t *testing.T, sql string) statementKind {
	t.Helper()
	statements := splitTokens(t, sql)
	require.Len(t, statements, 1, "expected single statement")
	return classifyStatement(statements[0])
}

func TestClassify_Select(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindSelect, classify(t, "SELECT 1"))
	assert.Equal(t, statementKindSelect, classify(t, "select id from users"))
	assert.Equal(t, statementKindSelect, classify(t, "SELECT id, name FROM users WHERE id = {uid:UInt32}"))
}

func TestClassify_Insert(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindInsert, classify(t, "INSERT INTO users VALUES (1, 'a')"))
	assert.Equal(t, statementKindInsert, classify(t, "INSERT INTO users SELECT * FROM other"))
}

func TestClassify_CreateTable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateTable, classify(t, "CREATE TABLE users (id UInt64) ENGINE = MergeTree() ORDER BY id"))
	assert.Equal(t, statementKindCreateTable, classify(t, "CREATE TABLE IF NOT EXISTS t (id UInt64) ENGINE = Log"))
	assert.Equal(t, statementKindCreateTable, classify(t, "CREATE OR REPLACE TABLE t (id UInt64) ENGINE = Log"))
	assert.Equal(t, statementKindCreateTable, classify(t, "CREATE TEMPORARY TABLE t (id UInt64) ENGINE = Memory"))
}

func TestClassify_CreateView(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateView, classify(t, "CREATE VIEW v AS SELECT id FROM t"))
	assert.Equal(t, statementKindCreateView, classify(t, "CREATE OR REPLACE VIEW v AS SELECT id FROM t"))
}

func TestClassify_CreateMaterializedView(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateMaterializedView, classify(t,
		"CREATE MATERIALIZED VIEW mv ENGINE = MergeTree() ORDER BY id AS SELECT id FROM t"))
}

func TestClassify_CreateDictionary(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateDictionary, classify(t,
		"CREATE DICTIONARY dict (id UInt64, name String) PRIMARY KEY id"))
}

func TestClassify_CreateFunction(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateFunction, classify(t,
		"CREATE FUNCTION double AS x -> x * 2"))
}

func TestClassify_CreateDatabase(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindCreateDatabase, classify(t, "CREATE DATABASE analytics"))
	assert.Equal(t, statementKindCreateDatabase, classify(t, "CREATE DATABASE IF NOT EXISTS analytics"))
}

func TestClassify_DropVariants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindDropTable, classify(t, "DROP TABLE t"))
	assert.Equal(t, statementKindDropView, classify(t, "DROP VIEW v"))
	assert.Equal(t, statementKindDropDictionary, classify(t, "DROP DICTIONARY d"))
	assert.Equal(t, statementKindDropFunction, classify(t, "DROP FUNCTION f"))
	assert.Equal(t, statementKindDropDatabase, classify(t, "DROP DATABASE analytics"))
}

func TestClassify_AlterTable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindAlterTable, classify(t, "ALTER TABLE t ADD COLUMN name String"))
	assert.Equal(t, statementKindAlterTable, classify(t, "ALTER TABLE t DROP COLUMN name"))
	assert.Equal(t, statementKindAlterTable, classify(t, "ALTER TABLE t MODIFY COLUMN id UInt64"))
	assert.Equal(t, statementKindAlterTable, classify(t, "ALTER TABLE t UPDATE count = count + 1 WHERE id = 1"))
	assert.Equal(t, statementKindAlterTable, classify(t, "ALTER TABLE t DELETE WHERE id = 1"))
}

func TestClassify_RenameTable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindRenameTable, classify(t, "RENAME TABLE old TO new"))
	assert.Equal(t, statementKindRenameTable, classify(t, "RENAME DICTIONARY d1 TO d2"))
	assert.Equal(t, statementKindRenameTable, classify(t, "RENAME DATABASE old TO new"))
}

func TestClassify_ExchangeTables(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindExchangeTables, classify(t, "EXCHANGE TABLES a AND b"))
	assert.Equal(t, statementKindExchangeTables, classify(t, "EXCHANGE TABLES analytics.events_new AND analytics.events_old"))
}

func TestClassify_TruncateOptimize(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindTruncate, classify(t, "TRUNCATE TABLE t"))
	assert.Equal(t, statementKindOptimize, classify(t, "OPTIMIZE TABLE t FINAL"))
}

func TestClassify_System(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindSystem, classify(t, "SYSTEM FLUSH LOGS"))
	assert.Equal(t, statementKindSystem, classify(t, "SYSTEM RELOAD DICTIONARY d"))
}

func TestClassify_UseShowSet(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindUse, classify(t, "USE analytics"))
	assert.Equal(t, statementKindShow, classify(t, "SHOW TABLES"))
	assert.Equal(t, statementKindSet, classify(t, "SET max_threads = 4"))
}

func TestClassify_WithCTE(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindSelect, classify(t, "WITH cte AS (SELECT id FROM t) SELECT * FROM cte"))
	assert.Equal(t, statementKindInsert, classify(t,
		"WITH cte AS (SELECT id FROM t) INSERT INTO dest SELECT * FROM cte"))
}

func TestClassify_Unknown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, statementKindUnknown, classify(t, "UNKNOWN_STATEMENT"))
}

func TestParser_ParseDatabaseQualifiedName(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("analytics.events")
	require.NoError(t, err)
	statementParser := newParser(tokens)
	database, name, err := statementParser.parseDatabaseQualifiedName()
	require.NoError(t, err)
	assert.Equal(t, "analytics", database)
	assert.Equal(t, "events", name)
}

func TestParser_ParseDatabaseQualifiedNameSingleIdent(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("events")
	require.NoError(t, err)
	statementParser := newParser(tokens)
	database, name, err := statementParser.parseDatabaseQualifiedName()
	require.NoError(t, err)
	assert.Equal(t, "", database)
	assert.Equal(t, "events", name)
}

func TestParser_MatchKeywordCaseInsensitive(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("Select id")
	require.NoError(t, err)
	statementParser := newParser(tokens)
	assert.True(t, statementParser.MatchKeyword("SELECT"))
	assert.False(t, statementParser.MatchKeyword("FROM"))
}

func TestParser_SkipParenthesisedBalanced(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("(a, b, (c, d), e)")
	require.NoError(t, err)
	statementParser := newParser(tokens)
	require.NoError(t, statementParser.skipParenthesised())
	assert.True(t, statementParser.AtEnd())
}

func TestParser_CollectParenthesised(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("(a, b, c)")
	require.NoError(t, err)
	statementParser := newParser(tokens)
	inner, err := statementParser.collectParenthesised()
	require.NoError(t, err)

	assert.Len(t, inner, 5)
}

func TestParserDepthLimitPreventsStackOverflow(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	engine := New()

	t.Run("nested expression parentheses", func(t *testing.T) {
		t.Parallel()
		sql := "SELECT " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + " FROM t"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("nested subqueries", func(t *testing.T) {
		t.Parallel()

		sql := "SELECT " + strings.Repeat("(SELECT ", depth) + "1" + strings.Repeat(")", depth)
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})
}

func TestParserDepthLimitIsConfigurable(t *testing.T) {
	t.Parallel()

	const nesting = 64
	sql := "SELECT " + strings.Repeat("(", nesting) + "1" + strings.Repeat(")", nesting) + " FROM t"

	t.Run("small custom cap terminates without crashing", func(t *testing.T) {
		t.Parallel()
		engine := New(WithMaxParseDepth(8))
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("default cap admits the same nesting", func(t *testing.T) {
		t.Parallel()
		engine := New()
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})
}

func TestAnalysersRequireTheEndOfTheStatement(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id FROM t WHERE id = 1 ) )",
		"SELECT id FROM t GROUP BY id ) x",
		"DELETE FROM t WHERE id = 1 ) extra",
		"SELECT id FROM t FORMAT JSON extra",
	} {
		err := analyseError(t, sql)
		require.Error(t, err, sql)
		assert.ErrorAs(t, err, new(*dialectkit.ParseError), sql)
	}
	require.NoError(t, analyseError(t, "SELECT id FROM t;"))
}
