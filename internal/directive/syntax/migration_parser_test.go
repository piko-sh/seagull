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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

type wantDiagnostic struct {
	code       string
	message    string
	suggestion string
	line       int
	column     int
	severity   diag.ErrorSeverity
}

func TestParseMigrationReportsMalformedDirectives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string
		want    []wantDiagnostic
		check   func(t *testing.T, directives MigrationDirectives)
	}{
		{
			name:    "well-formed directives",
			content: "-- sgl.migration(no_transaction: true)\n-- sgl.column(users.email, nullable: false, go_type: \"net/mail.Address\")\nCREATE TABLE users (email text);\n",
			want:    nil,
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.True(t, directives.NoTransaction)
				require.Len(t, directives.ColumnOverrides, 1)
				assert.Equal(t, "net/mail.Address", directives.ColumnOverrides[0].GoType)
				assert.Equal(t, new(false), directives.ColumnOverrides[0].Nullable)
			},
		},
		{
			name:    "a misspelt keyword argument",
			content: "-- sgl.migration(no_transacton: true)\nCREATE INDEX CONCURRENTLY i ON t (c);\n",
			want: []wantDiagnostic{{
				code: diag.CodeUnknownKeywordArgument, line: 1, column: 18, severity: diag.SeverityError, suggestion: "no_transaction",
				message: `unknown keyword argument "no_transacton" on sgl.migration; did you mean "no_transaction"?`,
			}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.False(t, directives.NoTransaction)
			},
		},
		{
			name:    "a read-only value that is not a boolean",
			content: "-- sgl.migration(readonly: maybe)\nCREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n",
			want:    []wantDiagnostic{{code: diag.CodeInvalidKeywordArgumentValue, line: 1, column: 28, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Empty(t, directives.ReadOnlyOverrides)
			},
		},
		{
			name:    "a column without its table",
			content: "-- sgl.column(email, nullable: true)\n",
			want: []wantDiagnostic{{
				code: diag.CodeInvalidKeywordArgumentValue, line: 1, column: 15, severity: diag.SeverityError,
				message: `invalid value for keyword argument "name": expected a column qualified by its table, such as users.email, ` +
					`since sgl.column in a migration names the table it changes`,
			}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Empty(t, directives.ColumnOverrides)
			},
		},
		{
			name:    "a column named by an empty string",
			content: "-- sgl.column('', nullable: true)\n",
			want: []wantDiagnostic{{
				code: diag.CodeInvalidKeywordArgumentValue, line: 1, column: 15, severity: diag.SeverityError,
				message: `invalid value for keyword argument "name": expected a column qualified by its table, such as users.email, ` +
					`since sgl.column in a migration names the table it changes`,
			}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Empty(t, directives.ColumnOverrides)
			},
		},
		{
			name:    "a nullability that is not a boolean",
			content: "-- sgl.column(users.email, nullable: yes, type: citext)\n",
			want:    []wantDiagnostic{{code: diag.CodeInvalidKeywordArgumentValue, line: 1, column: 38, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				require.Len(t, directives.ColumnOverrides, 1)
				assert.Nil(t, directives.ColumnOverrides[0].Nullable)
				assert.Equal(t, "citext", directives.ColumnOverrides[0].SQLType)
			},
		},
		{
			name:    "a misspelt column keyword argument",
			content: "-- sgl.column(users.id, go_typ: \"github.com/google/uuid.UUID\", nullable: false)\n",
			want:    []wantDiagnostic{{code: diag.CodeUnknownKeywordArgument, line: 1, column: 25, severity: diag.SeverityError, suggestion: "go_type"}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				require.Len(t, directives.ColumnOverrides, 1)
				assert.Empty(t, directives.ColumnOverrides[0].GoType)
				assert.Equal(t, new(false), directives.ColumnOverrides[0].Nullable)
			},
		},
		{
			name:    "an unclosed call",
			content: "-- sgl.migration(no_transaction: true\nCREATE INDEX CONCURRENTLY i ON t (c);\n",
			want:    []wantDiagnostic{{code: diag.CodeUnclosedDirective, line: 1, column: 38, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.False(t, directives.NoTransaction)
			},
		},
		{
			name:    "a call continued on the next line",
			content: "-- sgl.migration(readonly: true,\n--   no_transaction: true)\nCREATE INDEX CONCURRENTLY i ON t (c);\n",
			want: []wantDiagnostic{{
				code: diag.CodeUnclosedDirective, line: 1, column: 17, severity: diag.SeverityError,
				message: "sgl.migration(...) continues on the next line, but a migration file reads each directive from its own line; " +
					"write the whole call on one line",
			}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.False(t, directives.NoTransaction)
			},
		},
		{
			name:    "text after the call",
			content: "-- sgl.migration(no_transaction: true) please\n",
			want:    []wantDiagnostic{{code: diag.CodeDirectiveSyntax, line: 1, column: 40, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.False(t, directives.NoTransaction)
			},
		},
		{
			name:    "a bare import path",
			content: "-- sgl.column(menu.id, go_type: example.com/my-menu/v2.Pence)\n",
			want:    nil,
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				require.Len(t, directives.ColumnOverrides, 1)
				assert.Equal(t, "example.com/my-menu/v2.Pence", directives.ColumnOverrides[0].GoType)
			},
		},
		{
			name:    "a value that is not one word",
			content: "-- sgl.column(menu.id, type: double precision)\n",
			want:    []wantDiagnostic{{code: diag.CodeDirectiveSyntax, line: 1, column: 37, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Empty(t, directives.ColumnOverrides)
			},
		},
		{
			name:    "a string left open",
			content: "-- sgl.column(users.id, go_type: \"uuid.UUID)\n",
			want:    []wantDiagnostic{{code: diag.CodeUnterminatedString, line: 1, column: 34, severity: diag.SeverityError}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Empty(t, directives.ColumnOverrides)
			},
		},
		{
			name:    "a misspelt directive",
			content: "-- sgl.migraton(no_transaction: true)\n",
			want:    []wantDiagnostic{{code: diag.CodeUnknownDirective, line: 1, column: 4, severity: diag.SeverityError, suggestion: "sgl.migration"}},
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.False(t, directives.NoTransaction)
			},
		},
		{
			name:    "a call to the bare prefix",
			content: "-- sgl(no_transaction: true)\n",
			want:    []wantDiagnostic{{code: diag.CodeUnknownDirective, line: 1, column: 4, severity: diag.SeverityError, message: `unknown directive "sgl"`}},
			check:   nil,
		},
		{
			name:    "a query directive",
			content: "-- sgl.embed(orders, from: o)\n",
			want: []wantDiagnostic{{
				code: diag.CodeDirectiveWrongContext, line: 1, column: 4, severity: diag.SeverityWarning,
				message: "sgl.embed is a query directive and is ignored in a migration file; use sgl.migration or sgl.column for migration directives",
			}},
			check: nil,
		},
		{
			name:    "a query header, which the misplaced query lines list",
			content: "\n-- sgl.query(GetUser, one)\n",
			want:    nil,
			check: func(t *testing.T, directives MigrationDirectives) {
				t.Helper()
				assert.Equal(t, []int{2}, directives.MisplacedQueryLines)
			},
		},
		{
			name:    "prose that mentions the prefix",
			content: "-- sgl keeps this migration small\n-- sgl.migration is documented elsewhere\n",
			want:    nil,
			check:   nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directives, diagnostics := ParseMigration(testCase.content, NeutralLexicon(), namespace.Default(), "001_init.up.sql")

			require.Len(t, diagnostics, len(testCase.want), "%v", diagnostics)
			for index, want := range testCase.want {
				got := diagnostics[index]
				assert.Equal(t, "001_init.up.sql", got.Filename)
				assert.Equal(t, want.code, got.Code, got.Message)
				assert.Equal(t, want.line, got.Line, got.Message)
				assert.Equal(t, want.column, got.Column, got.Message)
				assert.Equal(t, want.severity, got.Severity, got.Message)
				if want.message != "" {
					assert.Equal(t, want.message, got.Message)
				}
				if want.suggestion != "" {
					assert.Equal(t, want.suggestion, got.Suggestion)
				}
			}
			if testCase.check != nil {
				testCase.check(t, directives)
			}
			assert.Equal(t, directives, ScanMigration(testCase.content, NeutralLexicon(), namespace.Default()))
		})
	}
}

func TestParseMigrationAcceptsAliasPrefixesAndReportsForeignOnes(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: []string{"other"}}
	content := "-- acmedb.migration(no_transaction: true)\n-- other.migration(readonly: true)\n-- acmedb.column(users.id, nullable: maybe)\n"

	directives, diagnostics := ParseMigration(content, NeutralLexicon(), prefixes, "002.up.sql")

	assert.True(t, directives.NoTransaction)
	assert.Equal(t, []ForeignDirective{{Prefix: "other", Name: "other.migration", Line: 2}}, directives.Foreign)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostics[0].Code)
	assert.Equal(t, 3, diagnostics[0].Line)
}

func TestParseMigrationReportsTextLeftUnread(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("SELECT 1; /* open\n", maxLexicalRecoveries+1) + "-- sgl.migration(no_transaction: true)\n"

	directives, diagnostics := ParseMigration(content, Lexicon{LinePrefix: "--", Rules: NeutralLexicon().Rules}, namespace.Default(), "003.up.sql")

	assert.False(t, directives.NoTransaction)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeDirectiveLineLimit, diagnostics[0].Code)
	assert.Equal(t, maxLexicalRecoveries+1, diagnostics[0].Line)
	assert.Contains(t, diagnostics[0].Message, fmt.Sprintf("from line %d on were not read", maxLexicalRecoveries+1))
}

func TestParseMigrationCapsTheDiagnosticsOfOneFile(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("-- sgl.migration(bogus: true)\n", maxMigrationDiagnostics+10)

	_, diagnostics := ParseMigration(content, NeutralLexicon(), namespace.Default(), "004.up.sql")

	require.Len(t, diagnostics, maxMigrationDiagnostics+1)
	notice := diagnostics[maxMigrationDiagnostics]
	assert.Equal(t, diag.CodeDiagnosticLimit, notice.Code)
	assert.Equal(t, diag.SeverityError, notice.Severity)
	assert.Equal(t, "and 10 more diagnostics were left out", notice.Message)
	assert.Equal(t, 1, notice.Line)
}

func TestRequestsNoTransactionRefusesADirectiveItCannotReadForCertain(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		content       string
		wantMessage   string
		wantRequested bool
		wantRefused   bool
	}{
		{name: "no directive", content: "CREATE INDEX i ON t (a);\n"},
		{name: "a well-formed request", content: "-- sgl.migration(no_transaction: true)\nCREATE INDEX i ON t (a);\n", wantRequested: true},
		{name: "a well-formed refusal of the request", content: "-- sgl.migration(no_transaction: false)\n"},
		{name: "a misspelt key", content: "-- sgl.migration(no_transacton: true)\n", wantRefused: true, wantMessage: "line 1"},
		{name: "a value that is not true or false", content: "-- sgl.migration(no_transaction: yes)\n", wantRefused: true},
		{name: "a call left unclosed", content: "-- sgl.migration(no_transaction: true\n", wantRefused: true},
		{name: "text after the call", content: "-- sgl.migration(no_transaction: true) please\n", wantRefused: true},
		{
			name:        "a call continued on the next line",
			content:     "-- sgl.migration(no_transaction: true,\n-- readonly: true)\n",
			wantRefused: true,
		},
		{name: "a string left open", content: "-- sgl.migration(no_transaction: 'true)\n", wantRefused: true},
		{name: "a misspelt role", content: "SELECT 1;\n\n-- sgl.migraton(no_transaction: true)\n", wantRefused: true, wantMessage: "line 3"},
		{name: "a read-only value that is not true or false", content: "-- sgl.migration(readonly: maybe)\n", wantRefused: true},
		{name: "a malformed column directive does not decide the transaction", content: "-- sgl.column(email, nullable: maybe)\n"},
		{name: "a query-only directive is only a warning", content: "-- sgl.param(id)\n"},
		{
			name:        "several problems are counted",
			content:     "-- sgl.migration(no_transacton: true)\n-- sgl.migration(readonly: maybe)\n",
			wantRefused: true,
			wantMessage: "(and 1 more)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requested, err := RequestsNoTransaction(testCase.content, NeutralLexicon(), namespace.Default())

			if !testCase.wantRefused {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantRequested, requested)
				return
			}
			require.ErrorIs(t, err, ErrMalformedMigrationDirective)
			assert.False(t, requested)
			assert.Contains(t, err.Error(), testCase.wantMessage)
		})
	}
}

func TestParseMigrationTokensReadsWhatParseMigrationReads(t *testing.T) {
	t.Parallel()

	content := "-- sgl.migration(readonly: true)\nCREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n" +
		"-- sgl.column(users.Email, go_typ: string)\n-- other.query(x)\n-- sgl.migration(no_transaction: true)\n"
	lexicon := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{Quotes: sqllex.QuoteRules{DollarQuoting: true}}}
	prefixes := namespace.Set{Canonical: "sgl", Aliases: nil, Foreign: []string{"other"}}

	wantDirectives, wantDiagnostics := ParseMigration(content, lexicon, prefixes, "001.up.sql")
	directives, diagnostics := ParseMigrationTokens(content, ScanTokens(lexicon.Rules, content), lexicon, prefixes, "001.up.sql")

	assert.Equal(t, wantDirectives, directives)
	assert.Equal(t, wantDiagnostics, diagnostics)
	assert.NotEmpty(t, diagnostics)
	assert.Len(t, directives.ReadOnlyOverrides, 1)
	assert.Len(t, directives.Foreign, 1)
	assert.True(t, directives.NoTransaction)
}

func TestParseMigrationKeepsTheCaseAndQuotesOfAColumnDirectivesTarget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		directive  string
		wantTable  string
		wantColumn string
		wantParts  []NamePart
	}{
		{
			name:       "an unquoted mixed-case name",
			directive:  "-- sgl.column(events.UserID, nullable: true)",
			wantTable:  "events",
			wantColumn: "UserID",
			wantParts:  []NamePart{{Name: "events", Written: "events", Quoted: false}, {Name: "UserID", Written: "UserID", Quoted: false}},
		},
		{
			name:       "a quoted column",
			directive:  `-- sgl.column(events."UserID", nullable: true)`,
			wantTable:  "events",
			wantColumn: `"UserID"`,
			wantParts:  []NamePart{{Name: "events", Written: "events", Quoted: false}, {Name: "UserID", Written: `"UserID"`, Quoted: true}},
		},
		{
			name:       "a schema-qualified quoted table",
			directive:  `-- sgl.column(app."Events".id, nullable: true)`,
			wantTable:  `app."Events"`,
			wantColumn: "id",
			wantParts: []NamePart{
				{Name: "app", Written: "app", Quoted: false},
				{Name: "Events", Written: `"Events"`, Quoted: true},
				{Name: "id", Written: "id", Quoted: false},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directives, diagnostics := ParseMigration(testCase.directive+"\n", NeutralLexicon(), namespace.Default(), "001.up.sql")

			require.Empty(t, diagnostics)
			require.Len(t, directives.ColumnOverrides, 1)
			override := directives.ColumnOverrides[0]
			assert.Equal(t, testCase.wantTable, override.Table)
			assert.Equal(t, testCase.wantColumn, override.Column)
			parts, split := SplitQualifiedName(override.Table + "." + override.Column)
			require.True(t, split)
			assert.Equal(t, testCase.wantParts, parts)
		})
	}
}
