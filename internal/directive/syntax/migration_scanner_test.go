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

package syntax

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestScanMigrationReadOnlyOverrideValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected      map[string]*bool
		name          string
		content       string
		commentPrefix string
	}{
		{
			name:          "no directives returns empty map",
			content:       "SELECT 1;",
			commentPrefix: "--",
			expected:      map[string]*bool{},
		},
		{
			name: "sgl.migration(readonly: true) before CREATE FUNCTION keys the statement true",
			content: `-- sgl.migration(readonly: true)
CREATE FUNCTION my_func() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION my_func": new(true),
			},
		},
		{
			name: "sgl.migration(readonly: false) before CREATE FUNCTION keys the statement false",
			content: `-- sgl.migration(readonly: false)
CREATE FUNCTION my_writer() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION my_writer": new(false),
			},
		},
		{
			name: "directive not followed by CREATE FUNCTION is ignored",
			content: `-- sgl.migration(readonly: true)
SELECT 1;
CREATE FUNCTION after_select() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected:      map[string]*bool{},
		},
		{
			name: "multiple functions with overrides returns all",
			content: `-- sgl.migration(readonly: true)
CREATE FUNCTION reader() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;
-- sgl.migration(readonly: false)
CREATE FUNCTION writer() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION reader": new(true),
				"CREATE FUNCTION writer": new(false),
			},
		},
		{
			name: "an overload of the same name without a directive is not keyed",
			content: `-- sgl.migration(readonly: true)
CREATE FUNCTION touch(integer) RETURNS void AS $$ SELECT 1 $$ LANGUAGE sql;
CREATE FUNCTION touch(text) RETURNS void AS $$ DELETE FROM t $$ LANGUAGE sql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION touch(integer)": new(true),
			},
		},
		{
			name: "intervening comment clears the pending override",
			content: `-- sgl.migration(readonly: true)
-- an unrelated descriptive comment
CREATE FUNCTION distant() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected:      map[string]*bool{},
		},
		{
			name: "blank line between directive and function preserves the override",
			content: `-- sgl.migration(readonly: true)

CREATE FUNCTION spaced() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION spaced": new(true),
			},
		},
		{
			name: "later readonly directive after an intervening comment still applies",
			content: `-- sgl.migration(readonly: true)
-- unrelated comment that breaks the first directive
-- sgl.migration(readonly: false)
CREATE FUNCTION rebound() RETURNS void AS $$ BEGIN END; $$ LANGUAGE plpgsql;`,
			commentPrefix: "--",
			expected: map[string]*bool{
				"CREATE FUNCTION rebound": new(false),
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			expected := make(map[int]*bool, len(testCase.expected))
			for statement, value := range testCase.expected {
				offset := strings.Index(testCase.content, statement)
				require.GreaterOrEqual(t, offset, 0, "statement %q", statement)
				expected[offset] = value
			}
			result := ScanMigration(testCase.content, Lexicon{LinePrefix: testCase.commentPrefix, Rules: NeutralLexicon().Rules}, namespace.Default()).ReadOnlyOverrides
			assert.Equal(t, expected, result)
		})
	}
}

func TestCreatedFunctionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{name: "CREATE FUNCTION returns the lower-case name", sql: "CREATE FUNCTION myFunc() RETURNS void", expected: "myfunc"},
		{name: "CREATE OR REPLACE FUNCTION", sql: "CREATE OR REPLACE FUNCTION myFunc() RETURNS void", expected: "myfunc"},
		{name: "CREATE PROCEDURE", sql: "CREATE PROCEDURE archive_rows() LANGUAGE sql", expected: "archive_rows"},
		{name: "a schema-qualified name drops the schema", sql: "CREATE FUNCTION myschema.myFunc() RETURNS void", expected: "myfunc"},
		{name: "a quoted name drops the quotes", sql: `CREATE FUNCTION "MyFunc"(integer) RETURNS void`, expected: "myfunc"},
		{name: "a name on the next line", sql: "CREATE FUNCTION\n  next_line() RETURNS void", expected: "next_line"},
		{name: "a trigger that executes a function is not a function", sql: "CREATE TRIGGER t AFTER INSERT ON x EXECUTE FUNCTION f()", expected: ""},
		{name: "not a CREATE statement", sql: "SELECT * FROM users", expected: ""},
		{name: "a semicolon ends the statement", sql: "CREATE; FUNCTION next_statement() RETURNS void", expected: ""},
		{name: "every modifier and a three-part name", sql: "CREATE OR REPLACE TEMPORARY FUNCTION db.app.calc() RETURNS int", expected: "calc"},
		{name: "a head longer than is read", sql: "CREATE OR OR OR OR OR OR OR OR OR FUNCTION too_far() RETURNS int", expected: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := Tokens(sqllex.Rules{}, testCase.sql)
			start := 0
			for tokens[start].Kind.IsNoise() {
				start++
			}
			assert.Equal(t, testCase.expected, createdFunctionName(statementHead(tokens, start, make([]sqllex.Token, 0, maxCreateFunctionHeadTokens))))
		})
	}
}

func TestScanMigrationReadsDirectivesUnderTheLexicon(t *testing.T) {
	t.Parallel()

	content := "-- sgl.migration(no_transaction: true)\n" +
		"CREATE FUNCTION body() RETURNS text AS $$\n" +
		"-- sgl.query(Inside, one)\n" +
		"-- sgl.column(items.id, nullable: true)\n" +
		"-- sgl.migration(readonly: true)\n" +
		"SELECT 'x'\n" +
		"$$ LANGUAGE sql;\n" +
		"-- sgl.query(Outside, one)\n" +
		"-- sgl.column(items.name, nullable: true)\n"
	dollar := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{Quotes: sqllex.QuoteRules{DollarQuoting: true}}}

	scan := ScanMigration(content, dollar, namespace.Default())

	assert.True(t, scan.NoTransaction)
	assert.Equal(t, []int{8}, scan.MisplacedQueryLines)
	require.Len(t, scan.ColumnOverrides, 1)
	assert.Equal(t, "name", scan.ColumnOverrides[0].Column)
	assert.Equal(t, 9, scan.ColumnOverrides[0].GoTypeSpan.Line)
	assert.Empty(t, scan.ReadOnlyOverrides)
}

func TestScanMigrationReadsAColumnsElementNullability(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		want   *bool
		name   string
		clause string
	}{
		{name: "declared non-null", clause: ", non_null_elements: true", want: new(true)},
		{name: "declared nullable", clause: ", non_null_elements: false", want: new(false)},
		{name: "not declared", clause: "", want: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			content := "-- sgl.column(posts.tags" + testCase.clause + ")\nCREATE TABLE posts (tags text[]);\n"

			scan := ScanMigration(content, Lexicon{LinePrefix: "--", Rules: sqllex.Rules{}}, namespace.Default())

			require.Len(t, scan.ColumnOverrides, 1)
			require.Equal(t, testCase.want, scan.ColumnOverrides[0].NonNullElements)
		})
	}
}

func TestNoTransactionRequiredAsksTheDialect(t *testing.T) {
	t.Parallel()

	vacuum := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{RequiresNoTransaction: func(tokens []sqllex.Token) bool {
		return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
	}}}

	assert.True(t, NoTransactionRequired("VACUUM;", vacuum, namespace.Default()))
	assert.False(t, NoTransactionRequired("CREATE INDEX CONCURRENTLY i ON t (c);", vacuum, namespace.Default()))
	assert.False(t, NoTransactionRequired("SELECT 'VACUUM';", vacuum, namespace.Default()))
	assert.True(t, NoTransactionRequired("-- sgl.migration(no_transaction: true)\nSELECT 1;", vacuum, namespace.Default()))
	assert.False(t, NoTransactionRequired("SELECT '\n-- sgl.migration(no_transaction: true)\n';", vacuum, namespace.Default()))
}

func TestNoTransactionRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "plain statement runs transactionally",
			content:  "CREATE TABLE foo (id int);",
			expected: false,
		},
		{
			name:     "explicit no_transaction directive forces non-transactional",
			content:  "-- sgl.migration(no_transaction: true)\nCREATE TABLE foo (id int);",
			expected: true,
		},
		{
			name:     "CREATE INDEX CONCURRENTLY is auto-detected",
			content:  "CREATE INDEX CONCURRENTLY idx ON users (email);",
			expected: true,
		},
		{
			name:     "lower-case create index concurrently is auto-detected",
			content:  "create index concurrently idx on users (email);",
			expected: true,
		},
		{
			name:     "leading VACUUM is auto-detected",
			content:  "VACUUM ANALYZE users;",
			expected: true,
		},
		{
			name:     "CONCURRENTLY inside a string literal does not strip transaction safety",
			content:  "INSERT INTO audit (note) VALUES ('processed CONCURRENTLY');",
			expected: false,
		},
		{
			name:     "CONCURRENTLY inside a double-quoted identifier does not strip transaction safety",
			content:  `INSERT INTO "run CONCURRENTLY" (id) VALUES (1);`,
			expected: false,
		},
		{
			name: "VACUUM beginning a dollar-quoted body line does not strip transaction safety",
			content: "CREATE FUNCTION housekeeping() RETURNS void AS $$\n" +
				"BEGIN\n" +
				"VACUUM things;\n" +
				"END;\n" +
				"$$ LANGUAGE plpgsql;",
			expected: false,
		},
		{
			name: "CONCURRENTLY inside a dollar-quoted body does not strip transaction safety",
			content: "CREATE FUNCTION note() RETURNS text AS $tag$\n" +
				"SELECT 'rebuild CONCURRENTLY please';\n" +
				"$tag$ LANGUAGE sql;",
			expected: false,
		},
		{
			name:     "column name containing concurrently substring is not matched",
			content:  "CREATE TABLE jobs (run_concurrently boolean);",
			expected: false,
		},
		{
			name:     "VACUUMING leading word is not matched as VACUUM",
			content:  "UPDATE machines SET state = 'vacuuming' WHERE id = 1;",
			expected: false,
		},
		{
			name:     "CONCURRENTLY in a line comment does not strip transaction safety",
			content:  "-- rebuild the index CONCURRENTLY later\nCREATE TABLE foo (id int);",
			expected: false,
		},
		{
			name:     "CONCURRENTLY in a block comment does not strip transaction safety",
			content:  "/* run CONCURRENTLY when convenient */\nCREATE TABLE foo (id int);",
			expected: false,
		},
		{
			name:     "real CONCURRENTLY after a string literal statement is still detected",
			content:  "INSERT INTO t (note) VALUES ('concurrently');\nCREATE INDEX CONCURRENTLY idx ON t (note);",
			expected: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := NoTransactionRequired(testCase.content, neutralTransactionLexicon(), namespace.Default())
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestNoTransactionRequiredWithMalformedSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{name: "an unterminated string before the keyword", content: "SELECT 'open; CREATE INDEX CONCURRENTLY i ON t (c);", expected: false},
		{name: "an unterminated string with an explicit directive", content: "-- sgl.migration(no_transaction: true)\nSELECT 'open", expected: true},
		{name: "a hash comment hides the keyword", content: "SELECT 1; # CREATE INDEX CONCURRENTLY i ON t (c)\n", expected: false},
		{name: "a backslash-escaped quote keeps the string open", content: `SELECT 'a\'; CREATE INDEX CONCURRENTLY i ON t (c);'`, expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, NoTransactionRequired(testCase.content, neutralTransactionLexicon(), namespace.Default()))
		})
	}
}

func TestCreatedFunctionNameStopsAtTheName(t *testing.T) {
	t.Parallel()

	for sql, expected := range map[string]string{
		"CREATE FUNCTION times_two AS x -> x * 2":         "times_two",
		"CREATE FUNCTION add_up AS (x, y) -> x + y":       "add_up",
		"CREATE FUNCTION `quoted` AS x -> x":              "quoted",
		"CREATE FUNCTION db.scoped AS x -> x":             "scoped",
		"CREATE OR REPLACE FUNCTION \"Mixed\".\"Case\"()": "case",
	} {
		tokens := Tokens(sqllex.Rules{Quotes: sqllex.QuoteRules{Backticks: true}}, sql)
		assert.Equal(t, expected, createdFunctionName(statementHead(tokens, 0, make([]sqllex.Token, 0, maxCreateFunctionHeadTokens))), sql)
	}
}

func neutralTransactionLexicon() Lexicon {
	lexicon := NeutralLexicon()
	lexicon.Rules.Quotes.SingleQuoteEscape = sqllex.EscapeDoubledOrBackslash
	lexicon.Rules.Quotes.DoubleQuoteEscape = sqllex.EscapeDoubledOrBackslash
	lexicon.Rules.Quotes.Backticks = false
	lexicon.Rules.Comments.HashLineComments = true
	lexicon.Rules.RequiresNoTransaction = func(tokens []sqllex.Token) bool {
		if len(tokens) > 0 && tokens[0].IsWord("VACUUM") {
			return true
		}
		return slices.ContainsFunc(tokens, func(token sqllex.Token) bool { return token.IsWord("CONCURRENTLY") })
	}
	return lexicon
}

func TestScanMigrationReadsManyReadOnlyDirectivesInLinearTime(t *testing.T) {
	t.Parallel()

	block := "-- sgl.migration(readonly: true)\nDROP TABLE x;\n"
	smaller := strings.Repeat(block, 24_000/len(block))
	larger := strings.Repeat(block, 4*(24_000/len(block)))
	audited := strings.Repeat(block, 376_000/len(block))

	scaling.Within(t, 200*time.Millisecond, func(context.Context) {
		directives := ScanMigration(audited, NeutralLexicon(), namespace.Default())
		assert.Empty(t, directives.ReadOnlyOverrides)
	})
	scaling.Linear(t,
		func() { ScanMigration(smaller, NeutralLexicon(), namespace.Default()) },
		func() { ScanMigration(larger, NeutralLexicon(), namespace.Default()) })
}

func TestRequiresNoTransactionReturnsAPanickingPredicateAsAnError(t *testing.T) {
	t.Parallel()

	panicking := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{RequiresNoTransaction: func([]sqllex.Token) bool { panic("boom") }}}
	vacuum := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{RequiresNoTransaction: func(tokens []sqllex.Token) bool {
		return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
	}}}
	testCases := []struct {
		name         string
		content      string
		lexicon      Lexicon
		wantRequired bool
		wantPanic    bool
	}{
		{name: "a predicate that panics", content: "VACUUM;", lexicon: panicking, wantRequired: false, wantPanic: true},
		{name: "a directive decides before the predicate runs", content: "-- sgl.migration(no_transaction: true)\nVACUUM;", lexicon: panicking, wantRequired: true, wantPanic: false},
		{name: "a statement the predicate detects", content: "VACUUM;", lexicon: vacuum, wantRequired: true, wantPanic: false},
		{name: "malformed text is not detected", content: "VACUUM 'open", lexicon: vacuum, wantRequired: false, wantPanic: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			required, err := RequiresNoTransaction(testCase.content, testCase.lexicon, namespace.Default())

			assert.Equal(t, testCase.wantRequired, required)
			assert.Equal(t, testCase.wantRequired, NoTransactionRequired(testCase.content, testCase.lexicon, namespace.Default()))
			if !testCase.wantPanic {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, sqllex.ErrRulesCallback)
			assert.Contains(t, err.Error(), "boom")
		})
	}
}

func TestScanMigrationColumnOverrideSpansTheGoTypeValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		content  string
		wantType string
		wantSpan diagbuild.TextSpan
	}{
		{
			name:     "quoted value",
			content:  "CREATE TABLE menu (id INTEGER);\n-- sgl.column(menu.id, go_type: \"bad type\")\n",
			wantType: "bad type",
			wantSpan: diagbuild.TextSpan{Line: 2, Column: 33, EndLine: 2, EndColumn: 43},
		},
		{
			name:     "bare value after other keywords",
			content:  "  -- sgl.column(menu.id, nullable: true, go_type: example.com/menu.Pence)\nCREATE TABLE menu (id INTEGER);",
			wantType: "example.com/menu.Pence",
			wantSpan: diagbuild.TextSpan{Line: 1, Column: 51, EndLine: 1, EndColumn: 73},
		},
		{
			name:     "quoted value after other keywords",
			content:  "  -- sgl.column(menu.id, nullable: true, go_type: 'example.com/menu.Pence')\nCREATE TABLE menu (id INTEGER);",
			wantType: "example.com/menu.Pence",
			wantSpan: diagbuild.TextSpan{Line: 1, Column: 51, EndLine: 1, EndColumn: 75},
		},
		{
			name:     "a bare word",
			content:  "-- sgl.column(menu.id, go_type: Pence)\n",
			wantType: "Pence",
			wantSpan: diagbuild.TextSpan{Line: 1, Column: 33, EndLine: 1, EndColumn: 38},
		},
		{
			name:     "no go type",
			content:  "-- sgl.column(menu.id, nullable: true)\n",
			wantType: "",
			wantSpan: diagbuild.TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 1},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			overrides := ScanMigration(testCase.content, NeutralLexicon(), namespace.Default()).ColumnOverrides

			require.Len(t, overrides, 1)
			require.Equal(t, "menu", overrides[0].Table)
			require.Equal(t, "id", overrides[0].Column)
			require.Equal(t, testCase.wantType, overrides[0].GoType)
			require.Equal(t, testCase.wantSpan, overrides[0].GoTypeSpan)
		})
	}
}

func TestScanMigrationColumnOverrideReadsStructTags(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		content  string
		want     string
		wantText string
	}{
		{
			name:     "single-quoted tags",
			content:  "-- sgl.column(users.email, struct_tags: 'validate:\"required,email\" db:\"email\"')\n",
			want:     `validate:"required,email" db:"email"`,
			wantText: "struct_tags: 'validate:\"required,email\" db:\"email\"'",
		},
		{
			name:     "beside a Go type",
			content:  "-- sgl.column(users.id, go_type: \"github.com/google/uuid.UUID\", struct_tags: 'json:\"-\"')\n",
			want:     `json:"-"`,
			wantText: "struct_tags: 'json:\"-\"'",
		},
		{
			name:     "a doubled quote is one quote",
			content:  "-- sgl.column(users.note, struct_tags: 'example:\"it''s\"')\n",
			want:     `example:"it's"`,
			wantText: "struct_tags: 'example:\"it''s\"'",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			overrides := ScanMigration(testCase.content, NeutralLexicon(), namespace.Default()).ColumnOverrides

			require.Len(t, overrides, 1)
			require.Equal(t, testCase.want, overrides[0].StructTags)
			span := overrides[0].StructTagsSpan
			require.Equal(t, 1, span.Line)
			require.Equal(t, testCase.wantText, testCase.content[span.Column-1:span.EndColumn-1])
		})
	}

	overrides := ScanMigration("-- sgl.column(users.email, nullable: true)\n", NeutralLexicon(), namespace.Default()).ColumnOverrides
	require.Empty(t, overrides[0].StructTags)
	require.Equal(t, diagbuild.TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 1}, overrides[0].StructTagsSpan)
}
