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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var errAnalysisAbandoned = errors.New("analysis abandoned")

type foreignStatement struct{}

type panickingContext struct {
	context.Context
}

var (
	sampleDDLStatements = []string{
		"CREATE TABLE IF NOT EXISTS main.t (a INTEGER PRIMARY KEY AUTOINCREMENT, b TEXT NOT NULL DEFAULT (lower('x')) " +
			"CHECK (length(b) > 0) REFERENCES u(id) ON DELETE CASCADE, c TEXT COLLATE NOCASE CONSTRAINT n " +
			"GENERATED ALWAYS AS (b || 'x') STORED, CONSTRAINT k UNIQUE (b), FOREIGN KEY (c) REFERENCES u(id) " +
			"ON UPDATE SET NULL, CHECK (a > 0)) STRICT, WITHOUT ROWID",
		"DROP TABLE IF EXISTS main.t",
		"ALTER TABLE t RENAME COLUMN a TO b",
		"ALTER TABLE t ADD COLUMN d INTEGER DEFAULT 0 REFERENCES u(id)",
		"ALTER TABLE t RENAME TO u",
		"ALTER TABLE t DROP COLUMN d",
		"CREATE VIEW IF NOT EXISTS v (x) AS SELECT a FROM t",
		"DROP VIEW IF EXISTS v",
		"CREATE UNIQUE INDEX IF NOT EXISTS i ON t (a, b) WHERE a > 0",
		"DROP INDEX IF EXISTS i",
		"CREATE VIRTUAL TABLE f USING fts5(a, b, tokenize = 'porter')",
		"CREATE TRIGGER IF NOT EXISTS tr AFTER INSERT ON t BEGIN UPDATE t SET a = 1; END",
		"DROP TRIGGER IF EXISTS tr",
	}
	sampleQueryStatements = []string{
		"WITH c AS (SELECT a FROM t) SELECT DISTINCT a, CAST(? AS VARCHAR(10)) FROM c JOIN u USING (id) " +
			"WHERE a IN (SELECT a FROM t) AND EXISTS (SELECT 1 FROM (SELECT 1) d) GROUP BY a HAVING count(*) > 1 " +
			"UNION SELECT 1 ORDER BY 1 LIMIT ? OFFSET ?",
		"INSERT INTO t (a, b) VALUES (?, ?) ON CONFLICT (a) WHERE a > 0 DO UPDATE SET b = excluded.b WHERE a > 0 RETURNING a",
		"UPDATE OR IGNORE t SET a = ? FROM u WHERE t.id = u.id RETURNING a",
		"DELETE FROM t WHERE a = ? RETURNING a",
		"VALUES (1, ?)",
	}
)

type abandonedAfterChecks struct {
	context.Context
	checks int
	calls  int
}

func (foreignStatement) IsParsedStatement() {}

func (panickingContext) Err() error {
	panic("context bug")
}

func (c *abandonedAfterChecks) Err() error {
	c.calls++
	if c.calls > c.checks {
		return errAnalysisAbandoned
	}
	return nil
}

func (*abandonedAfterChecks) Value(any) any {
	return nil
}

func TestNewSQLiteEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, "sqlite", engine.Info().Name, "dialect should be sqlite")
	assert.Equal(t, []string{"main", "temp"}, engine.Info().SchemaSearchPath, "SQLite default schema should be main")
	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution, "SQLite uses question-mark parameters")
	assert.NotZero(t, engine.Info().Returning, "SQLite supports RETURNING clauses")
	assert.NotNil(t, engine.BuiltinFunctions(), "function catalogue should be initialised")
	assert.NotNil(t, engine.BuiltinTypes(), "type catalogue should be initialised")
}

func TestNormaliseTypeName(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name           string
		input          string
		wantEngineName string
		wantCategory   sqlmodel.SQLTypeCategory
	}{

		{
			name:           "INTEGER maps to integer",
			input:          "INTEGER",
			wantEngineName: "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "REAL maps to real",
			input:          "REAL",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "TEXT maps to text",
			input:          "TEXT",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "BLOB maps to blob",
			input:          "BLOB",
			wantEngineName: "blob",
			wantCategory:   sqlmodel.TypeCategoryBinary,
		},

		{
			name:           "int maps to integer via built-in lookup",
			input:          "int",
			wantEngineName: "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "bigint maps to bigint",
			input:          "bigint",
			wantEngineName: "bigint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "smallint maps to smallint",
			input:          "smallint",
			wantEngineName: "smallint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "varchar maps to text",
			input:          "varchar",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "float maps to real",
			input:          "float",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "double maps to real",
			input:          "double",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "boolean maps to boolean",
			input:          "boolean",
			wantEngineName: "boolean",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
		},

		{
			name:           "case insensitive normalisation",
			input:          "Integer",
			wantEngineName: "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},

		{
			name:           "unknown type containing INT falls back to integer affinity",
			input:          "myinttype",
			wantEngineName: "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},

		{
			name:           "unknown type containing CHAR falls back to text affinity",
			input:          "nativechar",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},

		{
			name:           "unknown type with no affinity match defaults to numeric",
			input:          "currency",
			wantEngineName: "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.NormaliseTypeName(testCase.input)

			assert.Equal(t, testCase.wantEngineName, result.EngineName, "engine name mismatch")
			assert.Equal(t, testCase.wantCategory, result.Category, "category mismatch")
		})
	}
}

func TestPromoteType(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name  string
		left  sqlmodel.SQLType
		right sqlmodel.SQLType
	}{
		{
			name:  "integer and integer returns left",
			left:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"},
			right: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"},
		},
		{
			name:  "real and real returns left",
			left:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat, EngineName: "real"},
			right: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat, EngineName: "real"},
		},
		{
			name:  "text and text returns left",
			left:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
			right: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
		},
		{
			name:  "blob and blob returns left",
			left:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary, EngineName: "blob"},
			right: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary, EngineName: "blob"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.PromoteType(testCase.left, testCase.right)
			assert.Equal(t, testCase.left, result, "SQLite promotion should always return the left operand")
		})
	}
}

func TestCanImplicitCast(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("numeric types are castable to wider numeric types", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")),
			"integer to float should be allowed")
		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "")),
			"integer to decimal should be allowed")
		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")),
			"decimal to float should be allowed")
	})

	t.Run("text types are castable to text", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")),
			"text to text should be allowed")
	})

	t.Run("incompatible casts are rejected", func(t *testing.T) {
		t.Parallel()

		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")),
			"float to integer should not be allowed")
		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")),
			"text to integer should not be allowed")
	})
}

func TestDefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{"main", "temp"}, engine.Info().SchemaSearchPath,
		"SQLite default schema should be main")
}

func TestBuiltinFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinFunctions()

	require.NotNil(t, catalogue, "function catalogue must not be nil")
	require.NotNil(t, catalogue.Functions, "function map must not be nil")

	expectedFunctions := []string{"abs", "count", "length", "typeof"}
	for _, name := range expectedFunctions {
		signatures, exists := catalogue.Functions[name]
		assert.True(t, exists, "expected built-in function %q to be registered", name)
		assert.NotEmpty(t, signatures, "expected at least one signature for %q", name)
	}
}

func TestBuiltinTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinTypes()

	require.NotNil(t, catalogue, "type catalogue must not be nil")
	require.NotNil(t, catalogue.Types, "type map must not be nil")

	expectedTypes := []string{"integer", "real", "text", "blob", "boolean", "json", "varchar", "int"}
	for _, name := range expectedTypes {
		_, exists := catalogue.Types[name]
		assert.True(t, exists, "expected built-in type %q to be registered", name)
	}
}

func TestNormaliseTypeName_AffinityRules(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name           string
		input          string
		modifiers      []int
		wantEngineName string
		wantCategory   sqlmodel.SQLTypeCategory
	}{
		{
			name:           "type containing BLOB falls back to blob affinity",
			input:          "myblob",
			wantEngineName: "blob",
			wantCategory:   sqlmodel.TypeCategoryBinary,
		},
		{
			name:           "type containing REAL falls back to real affinity",
			input:          "surreal",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "type containing FLOA falls back to real affinity",
			input:          "floating",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "type containing DOUB falls back to real affinity",
			input:          "redoubled",
			wantEngineName: "real",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "type containing CLOB falls back to text affinity",
			input:          "myclobtype",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "type containing TEXT falls back to text affinity",
			input:          "metatext",
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "empty type name defaults to blob",
			input:          "",
			wantEngineName: "blob",
			wantCategory:   sqlmodel.TypeCategoryBinary,
		},
		{
			name:           "decimal with precision modifier",
			input:          "numeric",
			modifiers:      []int{8},
			wantEngineName: "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "decimal with precision and scale modifiers",
			input:          "decimal",
			modifiers:      []int{10, 2},
			wantEngineName: "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "text with length modifier",
			input:          "varchar",
			modifiers:      []int{255},
			wantEngineName: "text",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "integer with modifier ignored",
			input:          "integer",
			modifiers:      []int{11},
			wantEngineName: "integer",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.NormaliseTypeName(testCase.input, testCase.modifiers...)
			assert.Equal(t, testCase.wantEngineName, result.EngineName, "engine name mismatch")
			assert.Equal(t, testCase.wantCategory, result.Category, "category mismatch")
		})
	}
}

func TestApplyDDLAndAnalyseQueryRefuseAStatementAnotherDialectParsed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw         ast.ParsedStatementRaw
		name        string
		wantMessage string
		analyse     bool
	}{
		{name: "ApplyDDL given no statement", raw: nil, analyse: false,
			wantMessage: "the statement was parsed by another dialect: <nil>"},
		{name: "ApplyDDL given another dialect's statement", raw: foreignStatement{}, analyse: false,
			wantMessage: "the statement was parsed by another dialect: sqlite.foreignStatement"},
		{name: "AnalyseQuery given no statement", raw: nil, analyse: true,
			wantMessage: "the statement was parsed by another dialect: <nil>"},
		{name: "AnalyseQuery given another dialect's statement", raw: foreignStatement{}, analyse: true,
			wantMessage: "the statement was parsed by another dialect: sqlite.foreignStatement"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statement := ast.ParsedStatement{Raw: testCase.raw, Location: 0, Length: 0}

			var err error
			if testCase.analyse {
				_, err = engine.AnalyseQuery(t.Context(), nil, statement)
			} else {
				_, err = engine.ApplyDDL(t.Context(), statement)
			}

			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			assert.EqualError(t, err, testCase.wantMessage)
		})
	}
}

func TestAnalyseQueryRejectsUnrecognisedStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a misspelt SELECT", sql: "SELEC id FROM t", wantToken: "SELEC"},
		{name: "a parenthesised query, which SQLite does not run", sql: "(SELECT id FROM t)", wantToken: "("},
		{name: "a statement of another engine", sql: "TRUNCATE t", wantToken: "TRUNCATE"},
		{name: "a WITH clause leading to no statement", sql: "WITH c AS (SELECT 1) SELEC 1", wantToken: "SELEC"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.Nil(t, analysis)
			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			require.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestAnalyseQueryAcceptsStatementsWithNothingToAnalyse(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"PRAGMA foreign_keys = ON",
		"VACUUM",
		"ANALYZE",
		"REINDEX t",
		"ATTACH DATABASE 'other.db' AS other",
		"DETACH DATABASE other",
		"BEGIN IMMEDIATE",
		"COMMIT",
		"END TRANSACTION",
		"ROLLBACK",
		"SAVEPOINT s",
		"RELEASE s",
		"EXPLAIN QUERY PLAN SELECT 1",
		"CREATE TABLE t (id INTEGER)",
		"DROP TABLE t",
		"ALTER TABLE t ADD COLUMN name TEXT",
	} {
		engine := New()
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err, sql)
		require.Len(t, statements, 1, sql)

		analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

		require.NoError(t, err, sql)
		require.NotNil(t, analysis, sql)
		require.Empty(t, analysis.OutputColumns, sql)
	}
}

func TestApplyDDLReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantError string
	}{
		{name: "rename column without TO", sql: "ALTER TABLE t RENAME COLUMN a b", wantError: "expected keyword TO"},
		{name: "unterminated DEFAULT expression", sql: "CREATE TABLE t (a INTEGER DEFAULT (1", wantError: "unmatched parenthesis"},
		{name: "unterminated CHECK expression", sql: "CREATE TABLE t (a INTEGER CHECK (a > 0", wantError: "unmatched parenthesis"},
		{name: "unterminated REFERENCES column list", sql: "CREATE TABLE t (a INTEGER REFERENCES u(id", wantError: "unmatched parenthesis"},
		{name: "unterminated generated column expression", sql: "CREATE TABLE t (a INTEGER GENERATED ALWAYS AS (a + 1", wantError: "unmatched parenthesis"},
		{name: "unterminated table CHECK constraint", sql: "CREATE TABLE t (a INTEGER, CHECK (a > 0", wantError: "unmatched parenthesis"},
		{name: "unterminated virtual table arguments", sql: "CREATE VIRTUAL TABLE t USING fts5(a, b", wantError: "parsing virtual table arguments"},
		{name: "view without AS", sql: "CREATE VIEW v x SELECT 1", wantError: "expected keyword AS"},
		{name: "index without ON", sql: "CREATE INDEX i t (a)", wantError: "expected keyword ON"},
		{name: "trigger without ON", sql: "CREATE TRIGGER tr AFTER INSERT users BEGIN SELECT 1; END", wantError: "expected keyword ON"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.Error(t, err)
			assert.Nil(t, mutation)
			assert.Contains(t, err.Error(), testCase.wantError)
			assert.NotContains(t, err.Error(), "panic")
			assert.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestAnalyseQueryReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantSentinel error
		name         string
		sql          string
		wantError    string
		options      []Option
	}{
		{name: "UPDATE without SET", sql: "UPDATE t a = 1", wantError: "expected keyword SET"},
		{name: "DELETE without FROM", sql: "DELETE t", wantError: "expected keyword FROM"},
		{name: "compound arm that is not a SELECT", sql: "SELECT 1 UNION DELETE FROM t", wantError: "expected keyword SELECT"},
		{name: "ORDER BY after a VALUES list", sql: "SELECT 1 UNION VALUES (2) ORDER BY 1", wantError: "no ORDER BY or LIMIT after a VALUES list"},
		{name: "unterminated USING list", sql: "SELECT * FROM a JOIN b USING (id", wantError: "unmatched parenthesis"},
		{name: "unterminated ON CONFLICT target", sql: "INSERT INTO t (a) VALUES (1) ON CONFLICT (a DO NOTHING", wantError: "unmatched parenthesis"},
		{name: "unterminated CAST type modifiers", sql: "SELECT CAST(? AS VARCHAR(10 FROM t", wantError: "parsing CAST type modifiers"},
		{
			name:         "expression nested past the cap inside a CTE",
			sql:          "WITH c AS (SELECT ((((((((((1)))))))))) AS x) SELECT x FROM c",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
		{
			name:         "expression nested past the cap inside a scalar subquery",
			sql:          "SELECT (SELECT ((((((((((1))))))))))) FROM t",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
		{
			name:         "expression nested past the cap inside a derived table",
			sql:          "SELECT * FROM (SELECT ((((((((((1)))))))))) AS x) d",
			options:      []Option{WithMaxParseDepth(8)},
			wantSentinel: dialectkit.ErrParseDepth,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.Error(t, err)
			assert.Nil(t, analysis)
			assert.NotContains(t, err.Error(), "panic")
			if testCase.wantSentinel != nil {
				assert.ErrorIs(t, err, testCase.wantSentinel)
				assert.ErrorIs(t, err, fault.ErrLimit)
				return
			}
			assert.Contains(t, err.Error(), testCase.wantError)
			assert.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestApplyDDLRecoversFromHandlerPanic(t *testing.T) {
	original := ddlHandlers[statementKindCreateTable]
	ddlHandlers[statementKindCreateTable] = func(*parser, *Dialect) (*ast.CatalogueMutation, error) {
		panic("handler bug")
	}
	t.Cleanup(func() { ddlHandlers[statementKindCreateTable] = original })

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (a INTEGER)")
	require.NoError(t, err)
	ctx, _ := newCapturingContext(t)

	mutation, err := engine.ApplyDDL(ctx, statements[0])

	require.Error(t, err)
	assert.Nil(t, mutation)
	assert.Equal(t, "sqlite: apply DDL: panic: handler bug", err.Error())

	require.ErrorAs(t, err, new(*dialectkit.PanicError))
	assert.ErrorIs(t, err, fault.ErrPanic)
}

func TestAnalyseQueryRecoversFromAnalyserPanic(t *testing.T) {
	original := queryAnalysers[statementKindSelect]
	queryAnalysers[statementKindSelect] = func(*parser) (*ast.RawQueryAnalysis, error) {
		panic("analyser bug")
	}
	t.Cleanup(func() { queryAnalysers[statementKindSelect] = original })

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1")
	require.NoError(t, err)
	ctx, _ := newCapturingContext(t)

	analysis, err := engine.AnalyseQuery(ctx, nil, statements[0])

	require.Error(t, err)
	assert.Nil(t, analysis)
	assert.Equal(t, "sqlite: analyse query: panic: analyser bug", err.Error())

	require.ErrorAs(t, err, new(*dialectkit.PanicError))
	assert.ErrorIs(t, err, fault.ErrPanic)
}

func panickingTypeHook(string, []int) *sqlmodel.SQLType {
	panic("type hook bug")
}

func cancellingTypeHook(cancel context.CancelCauseFunc) func(string, []int) *sqlmodel.SQLType {
	return func(string, []int) *sqlmodel.SQLType {
		cancel(errAnalysisAbandoned)
		return nil
	}
}

func newCapturingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func decodeRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func parseOne(t *testing.T, engine *Dialect, sql string) ast.ParsedStatement {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return statements[0]
}

func wideCreateTable(columns int) string {
	var builder strings.Builder
	builder.WriteString("CREATE TABLE t (c0 INTEGER")
	for index := 1; index < columns; index++ {
		fmt.Fprintf(&builder, ", c%d INTEGER", index)
	}
	builder.WriteString(")")
	return builder.String()
}

func TestEntryPointsReturnAPanicAsAPanicErrorAndLogItOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call      func(t *testing.T, ctx context.Context) (any, error)
		name      string
		operation string
		value     string
	}{
		{
			name:      "ParseStatements whose context panics",
			operation: "sqlite: parse statements",
			value:     "context bug",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				return New().ParseStatements(panickingContext{Context: ctx}, "SELECT 1")
			},
		},
		{
			name:      "ApplyDDL whose type normaliser hook panics",
			operation: "sqlite: apply DDL",
			value:     "type hook bug",
			call: func(t *testing.T, ctx context.Context) (any, error) {
				engine := New(WithTypeNormaliserHook(panickingTypeHook))
				return engine.ApplyDDL(ctx, parseOne(t, engine, "CREATE TABLE t (a INTEGER)"))
			},
		},
		{
			name:      "AnalyseQuery whose type normaliser hook panics",
			operation: "sqlite: analyse query",
			value:     "type hook bug",
			call: func(t *testing.T, ctx context.Context) (any, error) {
				engine := New(WithTypeNormaliserHook(panickingTypeHook))
				return engine.AnalyseQuery(ctx, nil, parseOne(t, engine, "SELECT CAST(? AS INTEGER)"))
			},
		},
		{
			name:      "TableValuedFunctionColumnsFromCatalogue whose context panics",
			operation: "sqlite: table-valued function columns",
			value:     "context bug",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				return New().TableValuedFunctionColumnsFromCatalogue(panickingContext{Context: ctx}, nil, "docs")
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newCapturingContext(t)
			var (
				result any
				err    error
			)
			require.NotPanics(t, func() { result, err = testCase.call(t, ctx) })

			assert.Nil(t, result)
			require.EqualError(t, err, testCase.operation+": panic: "+testCase.value)
			require.ErrorIs(t, err, fault.ErrPanic)
			panicError, ok := errors.AsType[*dialectkit.PanicError](err)
			require.True(t, ok)
			assert.Equal(t, testCase.operation, panicError.Operation)
			assert.Equal(t, testCase.value, panicError.Value)
			records := decodeRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, testCase.operation, records[0]["operation"])
			assert.Equal(t, testCase.value, records[0]["panic"])
			assert.Contains(t, records[0]["stack"], "seagull.sh/seagull/internal/dialect/sqlite.")
		})
	}
}

func TestEntryPointsStopWithTheContextCauseOnceCancelled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call    func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error)
		wantErr error
		name    string
	}{
		{
			name:    "ParseStatements cancelled before it starts",
			wantErr: errAnalysisAbandoned,
			call: func(_ *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				cancel(errAnalysisAbandoned)
				return New().ParseStatements(ctx, "SELECT 1")
			},
		},
		{
			name:    "ApplyDDL cancelled before it starts",
			wantErr: errAnalysisAbandoned,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				engine := New()
				statement := parseOne(t, engine, "CREATE TABLE t (a INTEGER)")
				cancel(errAnalysisAbandoned)
				return engine.ApplyDDL(ctx, statement)
			},
		},
		{
			name:    "ApplyDDL cancelled while it walks the statement",
			wantErr: errAnalysisAbandoned,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				engine := New(WithTypeNormaliserHook(cancellingTypeHook(cancel)))
				return engine.ApplyDDL(ctx, parseOne(t, engine, wideCreateTable(2_000)))
			},
		},
		{
			name:    "AnalyseQuery cancelled before it starts",
			wantErr: errAnalysisAbandoned,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				engine := New()
				statement := parseOne(t, engine, "SELECT 1")
				cancel(errAnalysisAbandoned)
				return engine.AnalyseQuery(ctx, nil, statement)
			},
		},
		{
			name:    "AnalyseQuery cancelled while it walks the statement",
			wantErr: errAnalysisAbandoned,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				engine := New(WithTypeNormaliserHook(cancellingTypeHook(cancel)))
				statement := parseOne(t, engine, "SELECT CAST(? AS INTEGER)"+strings.Repeat(", 1", 2_000))
				return engine.AnalyseQuery(ctx, nil, statement)
			},
		},
		{
			name:    "TableValuedFunctionColumnsFromCatalogue cancelled before it starts",
			wantErr: errAnalysisAbandoned,
			call: func(_ *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				cancel(errAnalysisAbandoned)
				return New().TableValuedFunctionColumnsFromCatalogue(ctx, nil, "docs")
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			capturing, logs := newCapturingContext(t)
			ctx, cancel := context.WithCancelCause(capturing)
			defer cancel(nil)

			result, err := testCase.call(t, ctx, cancel)

			assert.Nil(t, result)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.NotErrorIs(t, err, fault.ErrPanic)
			assert.Empty(t, decodeRecords(t, logs))
		})
	}
}

func TestWithMaxTokensPerStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit is applied", limit: 500, want: 500},
		{name: "zero keeps the default", limit: 0, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative keeps the default", limit: -3, want: dialectkit.DefaultMaxTokensPerStatement},
		{
			name:  "a limit above the ceiling is held at the ceiling",
			limit: dialectkit.MaxTokensPerStatementCeiling + 1,
			want:  dialectkit.MaxTokensPerStatementCeiling,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.limit))

			assert.Equal(t, testCase.want, engine.settings.Limits.TokensPerStatement())
		})
	}
}

func TestTokenBudgetRejectsWalkedStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		isDDL bool
	}{
		{name: "DDL statement over budget", sql: "CREATE TABLE t (a INTEGER, b TEXT, c BLOB)", isDDL: true},
		{name: "query over budget", sql: "SELECT a, b, c FROM t WHERE a = ?", isDDL: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(5))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			if testCase.isDDL {
				_, err = engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			}

			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			require.ErrorIs(t, err, fault.ErrLimit)
			assert.Contains(t, err.Error(), "more than 5 tokens")
		})
	}
}

func TestTokenBudgetIgnoresStatementsTheParserDoesNotWalk(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxTokensPerStatement(5))
	statements, err := engine.ParseStatements(t.Context(), "INSERT INTO t (a, b, c) VALUES (1, 2, 3); PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	require.Len(t, statements, 2)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	assert.Nil(t, mutation)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[1])
	require.NoError(t, err)
	assert.Equal(t, &ast.RawQueryAnalysis{ConflictAction: ast.ConflictActionNone}, analysis)
}

func TestTokenBudgetRejectsDeeplyNestedCTEsByDefault(t *testing.T) {
	t.Parallel()

	const nesting = 100_000
	engine := New()
	sql := strings.Repeat("WITH a AS (", nesting) + "SELECT 1" + strings.Repeat(") SELECT 1", nesting)
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
}

func TestAnalyseQueryRejectsNestedCTEsWithinBudgetPastTheDepthCap(t *testing.T) {
	t.Parallel()

	const nesting = 14_000
	engine := New()
	sql := strings.Repeat("WITH a AS (", nesting) + "SELECT 1" + strings.Repeat(") SELECT 1", nesting)
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.ErrorIs(t, err, dialectkit.ErrParseDepth)
	assert.Nil(t, analysis)
}

func TestAnalyseQueryAnalysesNestedCTEsWithinTheDepthCap(t *testing.T) {
	t.Parallel()

	const nesting = 100
	engine := New()
	sql := strings.Repeat("WITH a AS (", nesting) + "SELECT 1" + strings.Repeat(") SELECT 1", nesting)
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	require.NotNil(t, analysis)
	assert.Len(t, analysis.CTEDefinitions, 1)
}

func TestParseStatementsRecordsPerStatementByteLength(t *testing.T) {
	t.Parallel()

	const first = "SELECT 1"
	const second = "SELECT 22"
	sql := first + "; " + second

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	assert.Equal(t, 0, statements[0].Location)
	assert.Equal(t, len(first), statements[0].Length,
		"first statement Length should span only its own tokens, not the whole source")

	secondLocation := len(first) + len("; ")
	assert.Equal(t, secondLocation, statements[1].Location)
	assert.Equal(t, len(second), statements[1].Length,
		"second statement Length should span only its own tokens")
}

func TestApplyDDLReturnsErrorsForTruncatedAndMismatchedStatements(t *testing.T) {
	t.Parallel()

	engine := New()
	for kind, handler := range ddlHandlers {
		if handler == nil {
			continue
		}
		for _, sql := range sampleDDLStatements {
			for _, tokens := range statementPrefixes(t, sql) {
				statement := ast.ParsedStatement{Raw: &parsedStatement{tokens: tokens, kind: statementKind(kind)}}

				_, err := engine.ApplyDDL(t.Context(), statement)

				if err != nil {
					assert.NotContains(t, err.Error(), "panic", "kind %d over %d tokens of %q", kind, len(tokens), sql)
				}
			}
		}
	}
}

func TestAnalyseQueryReturnsErrorsForTruncatedAndMismatchedStatements(t *testing.T) {
	t.Parallel()

	engine := New()
	for kind, analyser := range queryAnalysers {
		if analyser == nil {
			continue
		}
		for _, sql := range append(sampleQueryStatements, sampleDDLStatements...) {
			for _, tokens := range statementPrefixes(t, sql) {
				statement := ast.ParsedStatement{Raw: &parsedStatement{tokens: tokens, kind: statementKind(kind)}}

				_, err := engine.AnalyseQuery(t.Context(), nil, statement)

				if err != nil {
					assert.NotContains(t, err.Error(), "panic", "kind %d over %d tokens of %q", kind, len(tokens), sql)
				}
			}
		}
	}
}

func statementPrefixes(t *testing.T, sql string) [][]token {
	t.Helper()

	statements := splitTokens(t, sql)
	require.NotEmpty(t, statements)
	whole := statements[0]

	prefixes := make([][]token, 0, len(whole)+1)
	for length := range len(whole) + 1 {
		prefixes = append(prefixes, whole[:length:length])
	}
	return prefixes
}

func TestWithExtraTypesResolveEverywhereTheDialectNormalises(t *testing.T) {
	t.Parallel()

	vector := sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "vector")
	point := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "point")
	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"VECTOR": vector}),
		WithExtraTypes(map[string]sqlmodel.SQLType{"point": point}),
	)

	assert.Equal(t, vector, engine.NormaliseTypeName("vector"))
	assert.Equal(t, point, engine.NormaliseTypeName("Point"))
	assert.Contains(t, engine.BuiltinTypes().Types, "vector")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, engine.NormaliseTypeName("integer").Category, "built-in types stay")

	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (embedding VECTOR NOT NULL)")
	require.NoError(t, err)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 1)
	assert.Equal(t, "vector", mutation.Columns[0].SQLType.EngineName)

	statements, err = engine.ParseStatements(t.Context(), "SELECT id FROM t WHERE embedding = CAST(? AS VECTOR)")
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.Len(t, analysis.ParameterReferences, 1)
	require.NotNil(t, analysis.ParameterReferences[0].CastType)
	assert.Equal(t, "vector", analysis.ParameterReferences[0].CastType.EngineName)
}

func TestTypeSystemHooksChainMostRecentFirst(t *testing.T) {
	t.Parallel()

	first := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first")
	second := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "second")
	allowed, refused := true, false
	engine := New(
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "shared" || name == "older" {
				return &first
			}
			return nil
		}),
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "shared" {
				return &second
			}
			return nil
		}),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryText {
				return &allowed
			}
			return nil
		}),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryInteger {
				return &refused
			}
			return nil
		}),
		WithPromoteTypeHook(func(_, right sqlmodel.SQLType) *sqlmodel.SQLType {
			return &right
		}),
	)

	assert.Equal(t, second, engine.NormaliseTypeName("shared"), "the most recent hook wins")
	assert.Equal(t, first, engine.NormaliseTypeName("older"), "a declining hook falls through to an earlier one")
	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("text").Category, "declined names reach the built-ins")

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")), "unclaimed casts keep the built-in rule")

	left := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
	right := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int8")
	assert.Equal(t, right, engine.PromoteType(left, right))
	assert.Equal(t, left, New().PromoteType(left, right))
}

func TestJSONPassesAsText(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")))
	assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "")))
}

func TestAUUIDPassesAsTextButTextDoesNotPassAsAUUID(t *testing.T) {
	t.Parallel()

	engine := New()
	uuidType := engine.NormaliseTypeName("uuid")
	textType := engine.NormaliseTypeName("text")
	require.True(t, engine.CanImplicitCast(uuidType, textType))
	require.False(t, engine.CanImplicitCast(textType, uuidType))
}

func TestApplyDDLRequiresTheEndOfTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a dropped table", sql: "DROP TABLE a garbage here", wantToken: "garbage"},
		{name: "a dropped view", sql: "DROP VIEW v x", wantToken: "x"},
		{name: "a dropped index", sql: "DROP INDEX i x", wantToken: "x"},
		{name: "a dropped trigger", sql: "DROP TRIGGER tr x", wantToken: "x"},
		{name: "a renamed table", sql: "ALTER TABLE t RENAME TO u garbage", wantToken: "garbage"},
		{name: "a renamed column", sql: "ALTER TABLE t RENAME COLUMN a TO b garbage", wantToken: "garbage"},
		{name: "a dropped column", sql: "ALTER TABLE t DROP COLUMN a garbage", wantToken: "garbage"},
		{name: "an added column", sql: "ALTER TABLE t ADD COLUMN c INT NOT NULL nonsense", wantToken: "nonsense"},
		{name: "an index", sql: "CREATE INDEX i ON t (a) whatever 1 2 3", wantToken: "whatever"},
		{name: "a virtual table", sql: "CREATE VIRTUAL TABLE v USING fts5(a) extra", wantToken: "extra"},
		{name: "a table after its options", sql: "CREATE TABLE t (a INT) STRICT nonsense", wantToken: "nonsense"},
		{name: "a view", sql: "CREATE VIEW v AS SELECT 1 AS a extra words", wantToken: "extra"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()

			mutation, err := engine.ApplyDDL(t.Context(), parseOne(t, engine, testCase.sql))

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestApplyDDLLeavesATriggerBodyUnread(t *testing.T) {
	t.Parallel()

	engine := New()

	mutation, err := engine.ApplyDDL(t.Context(),
		parseOne(t, engine, "CREATE TRIGGER tr AFTER INSERT ON t FOR EACH ROW WHEN new.a > 0 BEGIN UPDATE u SET b = 1; END"))

	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
	assert.Equal(t, "t", mutation.TableName)
}

func TestParseStatementsKeepsNoMoreOfAStatementThanTheTokenBudget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		budget         int
		wantTokens     int
		wantOverBudget bool
	}{
		{name: "a statement over the budget keeps the budget's tokens", sql: "INSERT INTO t VALUES (1, 2, 3, 4)", budget: 5, wantTokens: 5, wantOverBudget: true},
		{name: "a statement at the budget keeps every token", sql: "SELECT 1, 2", budget: 4, wantTokens: 4, wantOverBudget: false},
		{name: "a statement under the budget keeps every token", sql: "SELECT 1", budget: 5, wantTokens: 2, wantOverBudget: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.budget))

			parsed, ok := parseOne(t, engine, testCase.sql).Raw.(*parsedStatement)

			require.True(t, ok)
			assert.Len(t, parsed.tokens, testCase.wantTokens)
			assert.Equal(t, testCase.wantOverBudget, parsed.overBudget)
		})
	}
}

func TestAStatementOverTheTokenBudgetIsRefusedWhereverItWouldBeRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		ddl         bool
		wantRefused bool
	}{
		{name: "a seed INSERT applied as DDL stays a no-op", sql: "INSERT INTO t VALUES (1, 2, 3, 4)", ddl: true, wantRefused: false},
		{name: "a CREATE TABLE applied as DDL", sql: "CREATE TABLE t (a INTEGER, b TEXT)", ddl: true, wantRefused: true},
		{name: "a query", sql: "SELECT a, b, c FROM t", ddl: false, wantRefused: true},
		{name: "a statement the analyser does not read, whose parameters it cannot see", sql: "BEGIN DEFERRED TRANSACTION ?", ddl: false, wantRefused: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(3))
			statement := parseOne(t, engine, testCase.sql)

			var err error
			if testCase.ddl {
				_, err = engine.ApplyDDL(t.Context(), statement)
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statement)
			}

			if !testCase.wantRefused {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			require.ErrorIs(t, err, fault.ErrLimit)
			assert.ErrorContains(t, err, "more than 3 tokens")
		})
	}
}

func TestParseStatementsBoundsAHugeStatementByTheTokenBudget(t *testing.T) {
	const rows = 800_000
	var builder strings.Builder
	builder.WriteString("INSERT INTO t (a) VALUES ")
	for index := range rows {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("(1)")
	}
	sql := builder.String() + "; SELECT 1"
	require.Greater(t, len(sql), 4_000_000)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	var statements []ast.ParsedStatement
	scaling.Within(t, 2*time.Second, func(ctx context.Context) {
		var err error
		statements, err = New().ParseStatements(ctx, sql)
		require.NoError(t, err)
	})

	runtime.ReadMemStats(&after)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(200<<20))
	require.Len(t, statements, 2)
	parsed, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok)
	assert.Len(t, parsed.tokens, dialectkit.DefaultMaxTokensPerStatement)
	assert.True(t, parsed.overBudget)
	assert.Equal(t, statementKindInsert, parsed.kind)
}

func TestParseStatementsStopsBetweenStatementsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		checks    int
		wantCalls int
	}{
		{name: "done before the first statement", checks: 1, wantCalls: 2},
		{name: "done after two statements", checks: 3, wantCalls: 4},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := &abandonedAfterChecks{Context: t.Context(), checks: testCase.checks, calls: 0}

			statements, err := New().ParseStatements(ctx, strings.Repeat("SELECT 1; ", 1_000))

			assert.Nil(t, statements)
			require.ErrorIs(t, err, errAnalysisAbandoned)
			assert.Equal(t, testCase.wantCalls, ctx.calls)
		})
	}
}

func TestApplyDDLAndAnalyseQueryReturnTheErrorBuildingTheDialectMet(t *testing.T) {
	t.Parallel()

	errSpecDecode := errors.New("spec does not decode")
	engine := newDialect(func() (*dialectkit.FunctionSpec, error) { return nil, errSpecDecode })

	testCases := []struct {
		call func(t *testing.T) error
		name string
	}{
		{
			name: "ApplyDDL",
			call: func(t *testing.T) error {
				_, err := engine.ApplyDDL(t.Context(), parseOne(t, engine, "CREATE TABLE t (a INTEGER)"))
				return err
			},
		},
		{
			name: "AnalyseQuery",
			call: func(t *testing.T) error {
				_, err := engine.AnalyseQuery(t.Context(), nil, parseOne(t, engine, "SELECT 1"))
				return err
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.call(t)

			require.ErrorIs(t, err, errSpecDecode)
			assert.ErrorContains(t, err, "sqlite: building the built-in function catalogue")
		})
	}
}

func TestADialectBuiltWholeReportsNoErrorBuildingIt(t *testing.T) {
	t.Parallel()

	engine := New()

	require.NoError(t, engine.initError)
	assert.NotEmpty(t, engine.BuiltinFunctions().Functions)
}

func TestInfoSharesNoSliceOrMapWithTheDialect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(info *dialect.Info)
		name   string
	}{
		{name: "the rowid aliases", change: func(info *dialect.Info) { info.RowIDAliases[0] = "changed" }},
		{name: "the operator features", change: func(info *dialect.Info) { clear(info.OperatorFeatures) }},
		{name: "the schema search path", change: func(info *dialect.Info) { info.SchemaSearchPath[0] = "changed" }},
		{name: "the drivers", change: func(info *dialect.Info) { info.Drivers[0] = "changed" }},
		{name: "the placeholder sigils", change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "changed" }},
		{name: "the JSON path functions", change: func(info *dialect.Info) { info.Filters.JSONPathFunctions[0] = "changed" }},
		{name: "the cast forms", change: func(info *dialect.Info) { info.Filters.Casts[0].Before = "changed" }},
		{name: "the cast types", change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := engine.Info()

			testCase.change(new(engine.Info()))

			got := engine.Info()
			want.Lexical, got.Lexical = sqllex.Rules{}, sqllex.Rules{}
			assert.Equal(t, want, got)
		})
	}
}

func TestTableValuedFunctionColumnsReturnsColumnsTheCallerMayChange(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function string
		sharer   string
	}{
		{name: "the JSON table columns json_each and json_tree share", function: "json_each", sharer: "json_tree"},
		{name: "the schema table shape its names share", function: "sqlite_schema", sharer: "sqlite_master"},
		{name: "a pragma function's columns", function: "pragma_table_info", sharer: "pragma_table_info"},
		{name: "a pragma statement's result", function: "pragma_user_version", sharer: "pragma_user_version"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := engine.TableValuedFunctionColumns(testCase.function)
			wantShared := engine.TableValuedFunctionColumns(testCase.sharer)
			require.NotEmpty(t, want)
			changed := engine.TableValuedFunctionColumns(testCase.function)

			changed[0].Name = "changed"
			changed[0].SQLType.EngineName = "changed"
			changed[0].Nullable = !changed[0].Nullable

			assert.Equal(t, want, engine.TableValuedFunctionColumns(testCase.function))
			assert.Equal(t, wantShared, New().TableValuedFunctionColumns(testCase.sharer))
		})
	}
}

func TestCheckRunOnFindsARunOnAnywhereInAStatement(t *testing.T) {
	t.Parallel()

	seed := "INSERT INTO a VALUES " + strings.TrimSuffix(strings.Repeat("(1, 'x'), ", 2000), ", ")
	testCases := []struct {
		name         string
		sql          string
		maxTokens    int
		wantError    string
		wantPosition int
	}{
		{name: "a run-on within the budget is found", sql: "INSERT INTO a VALUES (1)\nDROP TABLE b", maxTokens: 100,
			wantError: "a semicolon is missing before DROP", wantPosition: 25},
		{name: "a run-on among the kept tokens of a statement past the budget is found",
			sql: "INSERT INTO a VALUES (1)\nDROP TABLE b, c, d, e", maxTokens: 10,
			wantError: "a semicolon is missing before DROP", wantPosition: 25},
		{name: "a run-on past the budget of a statement passed over is found",
			sql: "INSERT INTO a VALUES (1, 2, 3, 4)\nDROP TABLE b", maxTokens: 8,
			wantError: "a semicolon is missing before DROP", wantPosition: 34},
		{name: "a run-on after a seed many times the budget is found", sql: seed + "\nDROP TABLE b", maxTokens: 100,
			wantError: "a semicolon is missing before DROP", wantPosition: len(seed) + 1},
		{name: "a statement past the budget that does not run on passes", sql: seed, maxTokens: 100},
		{name: "a character the tokeniser refuses past the budget is reported",
			sql: "INSERT INTO a VALUES (1, 2, 3, 4) \u20ac", maxTokens: 8, wantError: "unexpected character"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.maxTokens))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			assert.Nil(t, mutation)
			if testCase.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantError)
			if testCase.wantPosition > 0 {
				parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
				require.True(t, isParseError, "got %v", err)
				assert.Equal(t, testCase.wantPosition, parseError.Position)
			}
		})
	}
}

func TestNormaliseTypeNameReadsTheModifiersWrittenInTheNameAsADeclaredColumnDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		spelling       string
		wantCategory   sqlmodel.SQLTypeCategory
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
	}{
		{name: "a precision and a scale", spelling: "decimal(10, 2)", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "a negative scale written without spaces", spelling: "DECIMAL(10,-2)",
			wantCategory: sqlmodel.TypeCategoryDecimal, wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(-2)},
		{name: "a signed precision", spelling: "numeric( + 5 )", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric", wantPrecision: new(5)},
		{name: "a length", spelling: "varchar(255)", wantCategory: sqlmodel.TypeCategoryText, wantEngineName: "text",
			wantLength: new(255)},
		{name: "a hexadecimal length", spelling: "VARCHAR(0x10)", wantCategory: sqlmodel.TypeCategoryText,
			wantEngineName: "text", wantLength: new(16)},
		{name: "a length on a spelling read by its affinity", spelling: "character varying(40)",
			wantCategory: sqlmodel.TypeCategoryText, wantEngineName: "text", wantLength: new(40)},
		{name: "a precision past the largest modifier is held at it", spelling: "decimal(99999999999999999999, 2)",
			wantCategory: sqlmodel.TypeCategoryDecimal, wantEngineName: "numeric",
			wantPrecision: new(dialectkit.MaxTypeModifier), wantScale: new(2)},
		{name: "a temporal spelling with a modifier", spelling: "timestamp(3)", wantCategory: sqlmodel.TypeCategoryText,
			wantEngineName: "timestamp"},
		{name: "an integer spelling with a display width", spelling: "bigint(20)",
			wantCategory: sqlmodel.TypeCategoryInteger, wantEngineName: "bigint"},
		{name: "a name with no list", spelling: "decimal", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			declared := applyDDL(t, "CREATE TABLE t (a "+testCase.spelling+")")
			require.Len(t, declared.Columns, 1)

			sqlType := engine.NormaliseTypeName(testCase.spelling)

			assert.Equal(t, declared.Columns[0].SQLType, sqlType)
			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Equal(t, testCase.wantScale, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}

func TestNormaliseTypeNameReadsAListThatHoldsNoModifiersAsPartOfTheName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		spelling       string
		modifiers      []int
		wantCategory   sqlmodel.SQLTypeCategory
		wantEngineName string
		wantPrecision  *int
		wantLength     *int
	}{
		{name: "more modifiers than a type takes", spelling: "varchar(1, 2, 3)", wantCategory: sqlmodel.TypeCategoryText,
			wantEngineName: "text"},
		{name: "a word for a modifier", spelling: "decimal(max)", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric"},
		{name: "two signs", spelling: "decimal(+-5)", wantCategory: sqlmodel.TypeCategoryDecimal,
			wantEngineName: "numeric"},
		{name: "a list left open", spelling: "varchar(10", wantCategory: sqlmodel.TypeCategoryText, wantEngineName: "text"},
		{name: "modifiers passed alongside the name are the only ones read", spelling: "varchar(10)",
			modifiers: []int{20}, wantCategory: sqlmodel.TypeCategoryText, wantEngineName: "text", wantLength: new(20)},
		{name: "modifiers passed with a bare name", spelling: "decimal", modifiers: []int{8},
			wantCategory: sqlmodel.TypeCategoryDecimal, wantEngineName: "numeric", wantPrecision: new(8)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := New().NormaliseTypeName(testCase.spelling, testCase.modifiers...)

			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Nil(t, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}
