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

package analyse

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/check"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const (
	selfJoinSQL = "SELECT e.id, m.id FROM employees e LEFT JOIN employees m ON m.id = e.manager_id"
)

func embedTestScope(tables ...*resolve.ScopedTable) *resolve.ScopeChain {
	scope := resolve.NewScopeChain(resolve.ScopeKindQuery, nil)
	for _, table := range tables {
		scope.Tables[table.Alias] = table
	}
	return scope
}

func selfJoinScope() *resolve.ScopeChain {
	return embedTestScope(
		&resolve.ScopedTable{Name: "employees", Alias: "e", JoinKind: ast.JoinInner},
		&resolve.ScopedTable{Name: "employees", Alias: "m", JoinKind: ast.JoinLeft},
	)
}

func qualifiedColumn(name, table, qualifier string) sqlmodel.OutputColumn {
	column := resolve.NewOutputColumn(name, sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, false)
	column.SourceTable = table
	column.SourceColumn = name
	column.SourceQualifier = qualifier
	return column
}

func embedded(column sqlmodel.OutputColumn, table, name string, isOuter bool) sqlmodel.OutputColumn {
	column.IsEmbedded = true
	column.EmbedTable = table
	column.EmbedName = name
	column.EmbedIsOuter = isOuter
	return column
}

func embedBlock(embeds ...*directive.EmbedDirective) *directive.DirectiveBlock {
	return &directive.DirectiveBlock{Embeds: embeds}
}

func embedTestInput(block *directive.DirectiveBlock, sql string, scope *resolve.ScopeChain) embedInput {
	return embedInput{
		directiveBlock: block,
		scope:          scope,
		errorBuilder:   diagbuild.NewErrorBuilder("employees.sql"),
		tokens:         func() []sqllex.Token { return syntax.Tokens(sqllex.Rules{}, sql) },
		sql:            sql,
		lines:          check.NewSourceLines(sql, 7),
		prefixes:       namespace.Default(),
		markerPrefixes: namespace.Default().Accepted(),
		startLine:      7,
	}
}

func TestResolveEmbedDirectivesBindsEachEmbedToOneReference(t *testing.T) {
	t.Parallel()

	usersScope := embedTestScope(
		&resolve.ScopedTable{Name: "users", Alias: "u", JoinKind: ast.JoinInner},
		&resolve.ScopedTable{Name: "addresses", Alias: "a", JoinKind: ast.JoinLeft},
	)
	userID := qualifiedColumn("id", "users", "u")
	street := qualifiedColumn("street", "addresses", "a")
	reportID := qualifiedColumn("id", "employees", "e")
	managerID := qualifiedColumn("id", "employees", "m")

	testCases := []struct {
		name     string
		block    *directive.DirectiveBlock
		sql      string
		scope    *resolve.ScopeChain
		columns  []sqlmodel.OutputColumn
		expected []sqlmodel.OutputColumn
	}{
		{
			name:     "from binds only the left-joined side of a self-join",
			block:    embedBlock(&directive.EmbedDirective{Table: "employees", From: "m", As: "Manager"}),
			sql:      selfJoinSQL,
			scope:    selfJoinScope(),
			columns:  []sqlmodel.OutputColumn{reportID, managerID},
			expected: []sqlmodel.OutputColumn{reportID, embedded(managerID, "employees", "Manager", true)},
		},
		{
			name:     "from binds the preserved side of a self-join as an inner embed",
			block:    embedBlock(&directive.EmbedDirective{Table: "employees", From: "E", As: ""}),
			sql:      selfJoinSQL,
			scope:    selfJoinScope(),
			columns:  []sqlmodel.OutputColumn{reportID, managerID},
			expected: []sqlmodel.OutputColumn{embedded(reportID, "employees", "employees", false), managerID},
		},
		{
			name: "both sides of a self-join bind under their own names",
			block: embedBlock(
				&directive.EmbedDirective{Table: "employees", From: "e", As: "Report"},
				&directive.EmbedDirective{Table: "employees", From: "m", As: "Manager"},
			),
			sql:     selfJoinSQL,
			scope:   selfJoinScope(),
			columns: []sqlmodel.OutputColumn{reportID, managerID},
			expected: []sqlmodel.OutputColumn{
				embedded(reportID, "employees", "Report", false),
				embedded(managerID, "employees", "Manager", true),
			},
		},
		{
			name:     "without from the table's only reference is bound",
			block:    embedBlock(&directive.EmbedDirective{Table: "Addresses", From: "", As: ""}),
			sql:      "SELECT u.id, a.street FROM users u LEFT JOIN addresses a ON a.user_id = u.id",
			scope:    usersScope,
			columns:  []sqlmodel.OutputColumn{userID, street},
			expected: []sqlmodel.OutputColumn{userID, embedded(street, "Addresses", "Addresses", true)},
		},
		{
			name:     "an inline marker binds the table's only reference",
			block:    embedBlock(),
			sql:      "SELECT u.id, /* sgl.embed(addresses) */ a.street FROM users u LEFT JOIN addresses a ON a.user_id = u.id",
			scope:    usersScope,
			columns:  []sqlmodel.OutputColumn{userID, street},
			expected: []sqlmodel.OutputColumn{userID, embedded(street, "addresses", "addresses", true)},
		},
		{
			name:     "header directives take precedence over inline markers",
			block:    embedBlock(&directive.EmbedDirective{Table: "users", From: "u", As: "Owner"}),
			sql:      "SELECT u.id, /* sgl.embed(addresses) */ a.street FROM users u LEFT JOIN addresses a ON a.user_id = u.id",
			scope:    usersScope,
			columns:  []sqlmodel.OutputColumn{userID, street},
			expected: []sqlmodel.OutputColumn{embedded(userID, "users", "Owner", false), street},
		},
		{
			name:  "a column without a qualifier is never embedded",
			block: embedBlock(&directive.EmbedDirective{Table: "users", From: "", As: ""}),
			sql:   "SELECT count(*) AS cnt FROM users u",
			scope: usersScope,
			columns: []sqlmodel.OutputColumn{
				resolve.NewOutputColumn("cnt", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, false),
			},
			expected: []sqlmodel.OutputColumn{
				resolve.NewOutputColumn("cnt", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, false),
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := append([]sqlmodel.OutputColumn(nil), testCase.columns...)
			got, diagnostics := resolveEmbedDirectives(embedTestInput(testCase.block, testCase.sql, testCase.scope), columns)

			assert.Empty(t, diagnostics)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestResolveEmbedDirectivesReportsAnEmbedItCannotBind(t *testing.T) {
	t.Parallel()

	directiveSpan := diagbuild.TextSpan{Line: 2, Column: 4, EndLine: 2, EndColumn: 40}
	valueSpan := diagbuild.TextSpan{Line: 2, Column: 30, EndLine: 2, EndColumn: 31}

	testCases := []struct {
		name           string
		block          *directive.DirectiveBlock
		sql            string
		wantCode       string
		wantMessage    string
		wantSuggestion string
		wantSpan       diagbuild.TextSpan
		wantEmbedNames []string
	}{
		{
			name: "a self-join without from",
			block: embedBlock(&directive.EmbedDirective{
				Table: "employees", From: "", As: "Manager", Span: directiveSpan, FromSpan: diagbuild.TextSpan{}, AsSpan: valueSpan,
			}),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeMissingRequired,
			wantMessage:    `missing required keyword argument "from" on sgl.embed(employees): the table is referenced more than once, as e, m`,
			wantSuggestion: "",
			wantSpan:       directiveSpan,
			wantEmbedNames: []string{"", ""},
		},
		{
			name:           "an inline marker on a self-join",
			block:          embedBlock(),
			sql:            "SELECT /* sgl.embed(employees) */ e.id, m.id FROM employees e LEFT JOIN employees m ON m.id = e.manager_id",
			wantCode:       diag.CodeMissingRequired,
			wantMessage:    `missing required keyword argument "from" on sgl.embed(employees)`,
			wantSuggestion: "",
			wantSpan:       diagbuild.TextSpan{Line: 7, Column: 1, EndLine: 0, EndColumn: 0},
			wantEmbedNames: []string{"", ""},
		},
		{
			name: "from naming no reference",
			block: embedBlock(&directive.EmbedDirective{
				Table: "employees", From: "n", As: "Manager", Span: directiveSpan, FromSpan: valueSpan, AsSpan: diagbuild.TextSpan{},
			}),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(employees): invalid value "n" for keyword argument "from"`,
			wantSuggestion: "e",
			wantSpan:       valueSpan,
			wantEmbedNames: []string{"", ""},
		},
		{
			name: "from naming another table's alias",
			block: embedBlock(&directive.EmbedDirective{
				Table: "departments", From: "m", As: "", Span: directiveSpan, FromSpan: valueSpan, AsSpan: diagbuild.TextSpan{},
			}),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(departments): keyword argument "from" names alias "m", which refers to table "employees", not "departments"`,
			wantSuggestion: "",
			wantSpan:       valueSpan,
			wantEmbedNames: []string{"", ""},
		},
		{
			name: "two embeds of one table without as",
			block: embedBlock(
				&directive.EmbedDirective{Table: "employees", From: "e", As: "", Span: diagbuild.TextSpan{}},
				&directive.EmbedDirective{Table: "employees", From: "m", As: "", Span: directiveSpan},
			),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeMissingRequired,
			wantMessage:    `missing required keyword argument "as" on sgl.embed(employees): another embed is already named "employees"`,
			wantSuggestion: "",
			wantSpan:       directiveSpan,
			wantEmbedNames: []string{"employees", ""},
		},
		{
			name: "two embeds of one alias",
			block: embedBlock(
				&directive.EmbedDirective{Table: "employees", From: "m", As: "Manager", Span: diagbuild.TextSpan{}},
				&directive.EmbedDirective{Table: "employees", From: "M", As: "Boss", Span: directiveSpan, FromSpan: valueSpan},
			),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(employees): keyword argument "from" names alias "M", which the embed "Manager" already holds`,
			wantSuggestion: "",
			wantSpan:       valueSpan,
			wantEmbedNames: []string{"", "Manager"},
		},
		{
			name: "a table the query does not read",
			block: embedBlock(&directive.EmbedDirective{
				Table: "departments", From: "", As: "", Span: directiveSpan, FromSpan: diagbuild.TextSpan{}, AsSpan: diagbuild.TextSpan{},
			}),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(departments) names no table the query reads, so it would embed nothing; name one of employees`,
			wantSuggestion: "",
			wantSpan:       directiveSpan,
			wantEmbedNames: []string{"", ""},
		},
		{
			name:           "an inline marker naming a table the query does not read",
			block:          embedBlock(),
			sql:            "SELECT /* sgl.embed (departments) */ e.id, m.id FROM employees e LEFT JOIN employees m ON m.id = e.manager_id",
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(departments) names no table the query reads`,
			wantSuggestion: "",
			wantSpan:       diagbuild.TextSpan{Line: 7, Column: 1, EndLine: 0, EndColumn: 0},
			wantEmbedNames: []string{"", ""},
		},
		{
			name: "a misspelt table",
			block: embedBlock(&directive.EmbedDirective{
				Table: "employes", From: "", As: "", Span: directiveSpan, FromSpan: diagbuild.TextSpan{}, AsSpan: diagbuild.TextSpan{},
			}),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(employes) names no table the query reads`,
			wantSuggestion: "employees",
			wantSpan:       directiveSpan,
			wantEmbedNames: []string{"", ""},
		},
		{
			name: "two embeds given one name in different case",
			block: embedBlock(
				&directive.EmbedDirective{Table: "employees", From: "e", As: "person", Span: diagbuild.TextSpan{}},
				&directive.EmbedDirective{Table: "employees", From: "m", As: "Person", Span: directiveSpan, AsSpan: valueSpan},
			),
			sql:            selfJoinSQL,
			wantCode:       diag.CodeInvalidKeywordArgumentValue,
			wantMessage:    `sgl.embed(employees): invalid value "Person" for keyword argument "as": another embed is already named "person"`,
			wantSuggestion: "",
			wantSpan:       valueSpan,
			wantEmbedNames: []string{"person", ""},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := []sqlmodel.OutputColumn{
				qualifiedColumn("id", "employees", "e"),
				qualifiedColumn("id", "employees", "m"),
			}
			got, diagnostics := resolveEmbedDirectives(embedTestInput(testCase.block, testCase.sql, selfJoinScope()), columns)

			require.Len(t, diagnostics, 1, "diagnostics: %v", diagnostics)
			assert.Equal(t, testCase.wantCode, diagnostics[0].Code)
			assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			assert.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			assert.Equal(t, testCase.wantSuggestion, diagnostics[0].Suggestion)
			assert.Equal(t, "employees.sql", diagnostics[0].Filename)
			assert.Equal(t, testCase.wantSpan.Line, diagnostics[0].Line)
			assert.Equal(t, testCase.wantSpan.Column, diagnostics[0].Column)
			embedNames := make([]string, 0, len(got))
			for index := range got {
				embedNames = append(embedNames, got[index].EmbedName)
			}
			assert.Equal(t, testCase.wantEmbedNames, embedNames, "an embed that cannot bind embeds nothing")
		})
	}
}

func TestResolveEmbedDirectivesBindsASchemaQualifiedTable(t *testing.T) {
	t.Parallel()

	scope := embedTestScope(
		&resolve.ScopedTable{Name: "books", Alias: "books", JoinKind: ast.JoinInner},
		&resolve.ScopedTable{Name: "articles", Schema: "inventory", Alias: "articles", JoinKind: ast.JoinInner},
	)
	block := embedBlock(&directive.EmbedDirective{Table: "inventory.articles"})
	columns := []sqlmodel.OutputColumn{
		qualifiedColumn("id", "books", "books"),
		qualifiedColumn("id", "articles", "articles"),
	}

	got, diagnostics := resolveEmbedDirectives(embedTestInput(block, "SELECT books.id, articles.id FROM books JOIN inventory.articles", scope), columns)

	require.Empty(t, diagnostics)
	require.Len(t, got, 2)
	assert.False(t, got[0].IsEmbedded)
	assert.True(t, got[1].IsEmbedded)
	assert.Equal(t, "articles", got[1].EmbedName)
}

func TestResolveEmbedDirectivesIgnoresScopeOrder(t *testing.T) {
	t.Parallel()

	block := embedBlock(&directive.EmbedDirective{Table: "employees", From: "m", As: "Manager"})
	first, _ := resolveEmbedDirectives(embedTestInput(block, selfJoinSQL, selfJoinScope()), []sqlmodel.OutputColumn{
		qualifiedColumn("id", "employees", "e"),
		qualifiedColumn("id", "employees", "m"),
	})

	for range 50 {
		got, diagnostics := resolveEmbedDirectives(embedTestInput(block, selfJoinSQL, selfJoinScope()), []sqlmodel.OutputColumn{
			qualifiedColumn("id", "employees", "e"),
			qualifiedColumn("id", "employees", "m"),
		})
		require.Empty(t, diagnostics)
		require.Equal(t, first, got)
	}
}

func TestIsOuterJoinKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		kind     ast.JoinKind
		expected bool
	}{
		{name: "inner", kind: ast.JoinInner, expected: false},
		{name: "left", kind: ast.JoinLeft, expected: true},
		{name: "right", kind: ast.JoinRight, expected: true},
		{name: "full", kind: ast.JoinFull, expected: true},
		{name: "cross", kind: ast.JoinCross, expected: false},
		{name: "an engine's join that pads both sides", kind: ast.JoinKind{Keyword: "POSITIONAL", Nullable: ast.JoinSideLeft | ast.JoinSideRight, Filters: false}, expected: true},
		{name: "an engine's filtering join", kind: ast.JoinKind{Keyword: "SEMI", Nullable: 0, Filters: true}, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, isOuterJoinKind(testCase.kind))
		})
	}
}

func TestAliasedTableDiffers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		reference *resolve.ScopedTable
		table     string
		expected  bool
	}{
		{name: "aliased table of the same name", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: "Employees", expected: false},
		{name: "aliased table under a schema qualifier", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: "hr.employees", expected: false},
		{name: "embed naming the alias itself", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: "m", expected: false},
		{name: "aliased table of another name", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: "departments", expected: true},
		{name: "common table expression", reference: &resolve.ScopedTable{Name: "recent", Alias: "recent"}, table: "orders", expected: false},
		{name: "derived table", reference: &resolve.ScopedTable{Name: "", Alias: "d"}, table: "orders", expected: false},
		{name: "table under a nested qualifier", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: "db.hr.employees", expected: false},
		{name: "table after a bare dot", reference: &resolve.ScopedTable{Name: "employees", Alias: "m"}, table: ".employees", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, aliasedTableDiffers(testCase.reference, testCase.table))
		})
	}
}

func TestSplitEmbedTableSplitsAtTheLastDot(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		qualified  string
		wantSchema string
		wantTable  string
	}{
		{name: "an unqualified table", qualified: "articles", wantSchema: "", wantTable: "articles"},
		{name: "a schema-qualified table", qualified: "inventory.articles", wantSchema: "inventory", wantTable: "articles"},
		{name: "a nested qualifier keeps all but the last part", qualified: "db.inventory.articles", wantSchema: "db.inventory", wantTable: "articles"},
		{name: "a leading dot names no schema", qualified: ".articles", wantSchema: "", wantTable: ".articles"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			schema, table := splitEmbedTable(testCase.qualified)

			assert.Equal(t, testCase.wantSchema, schema)
			assert.Equal(t, testCase.wantTable, table)
		})
	}
}

func TestScanInlineEmbedMarkersReadsTheBlockCommentsOnly(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []inlineMarker
	}{
		{
			name:     "no embed markers",
			sql:      "SELECT u.id, u.name FROM users u",
			expected: nil,
		},
		{
			name:     "one marker",
			sql:      "SELECT u.id, /* sgl.embed(addresses) */ a.* FROM users u JOIN addresses a ON a.user_id = u.id",
			expected: []inlineMarker{{name: "addresses", closed: true}},
		},
		{
			name:     "markers in source order",
			sql:      "SELECT u.id, /* sgl.embed(addresses) */ a.*, /* sgl.embed(orders) */ o.* FROM users u",
			expected: []inlineMarker{{name: "addresses", closed: true}, {name: "orders", closed: true}},
		},
		{
			name:     "spaces around the name",
			sql:      "SELECT /* sgl.embed(  roles  ) */ r.* FROM roles r",
			expected: []inlineMarker{{name: "roles", closed: true}},
		},
		{
			name:     "a space before the parenthesis",
			sql:      "SELECT /* sgl.embed (roles) */ r.* FROM roles r",
			expected: []inlineMarker{{name: "roles", closed: true}},
		},
		{
			name:     "no space after the comment opener",
			sql:      "SELECT /*sgl.embed(roles)*/ r.* FROM roles r",
			expected: []inlineMarker{{name: "roles", closed: true}},
		},
		{
			name:     "an empty marker",
			sql:      "SELECT /* sgl.embed() */ * FROM users",
			expected: []inlineMarker{{name: "", closed: true}},
		},
		{
			name:     "a comment that closes before the marker does",
			sql:      "SELECT /* sgl.embed(roles */ r.* FROM roles r WHERE (r.id = 1)",
			expected: []inlineMarker{{name: "", closed: false}},
		},
		{
			name:     "a comment that is never closed",
			sql:      "SELECT /* sgl.embed(broken FROM users",
			expected: nil,
		},
		{
			name:     "a marker inside a string literal",
			sql:      "SELECT '/* sgl.embed(roles) */' AS note FROM roles r",
			expected: nil,
		},
		{
			name:     "a marker inside a line comment",
			sql:      "SELECT r.* -- /* sgl.embed(roles) */\nFROM roles r",
			expected: nil,
		},
		{
			name:     "a comment that names another directive",
			sql:      "SELECT /* sgl.embedded(roles) */ r.* FROM roles r",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			markers, truncated := scanInlineEmbedMarkers(syntax.Tokens(sqllex.Rules{}, testCase.sql), namespace.Default().Accepted())

			require.False(t, truncated)
			got := make([]inlineMarker, 0, len(markers))
			for _, marker := range markers {
				require.Equal(t, sqllex.KindComment, marker.comment.Kind)
				got = append(got, inlineMarker{name: marker.name, closed: marker.closed})
			}
			if testCase.expected == nil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestResolveEmbedDirectivesReportsAMalformedInlineMarker(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantCode    string
		wantMessage string
		wantColumn  int
	}{
		{
			name:        "a comment that closes before the marker does",
			sql:         "SELECT /* sgl.embed(roles */ r.id FROM roles r WHERE (r.id = 1)",
			wantCode:    diag.CodeUnclosedDirective,
			wantMessage: "unclosed sgl.embed(; expected matching ')'",
			wantColumn:  8,
		},
		{
			name:        "a name that is not a table",
			sql:         "SELECT /* sgl.embed(roles r) */ r.id FROM roles r",
			wantCode:    diag.CodeInvalidKeywordArgumentValue,
			wantMessage: `sgl.embed(...): "roles r" is not a table name; write a table, or a schema and a table, of at most 128 characters`,
			wantColumn:  8,
		},
		{
			name:        "a name too long to be a table",
			sql:         "SELECT /* sgl.embed(" + strings.Repeat("r", 200) + ") */ r.id FROM roles r",
			wantCode:    diag.CodeInvalidKeywordArgumentValue,
			wantMessage: `sgl.embed(...): "` + strings.Repeat("r", 32) + `..." is not a table name; write a table, or a schema and a table, of at most 128 characters`,
			wantColumn:  8,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := embedTestScope(&resolve.ScopedTable{Name: "roles", Alias: "r", JoinKind: ast.JoinInner})
			columns := []sqlmodel.OutputColumn{qualifiedColumn("id", "roles", "r")}

			got, diagnostics := resolveEmbedDirectives(embedTestInput(embedBlock(), testCase.sql, scope), columns)

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantCode, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, testCase.wantMessage, diagnostics[0].Message)
			require.Equal(t, 7, diagnostics[0].Line)
			require.Equal(t, testCase.wantColumn, diagnostics[0].Column)
			require.False(t, got[0].IsEmbedded)
		})
	}
}

func TestResolveEmbedDirectivesBoundsTheInlineMarkersItReads(t *testing.T) {
	t.Parallel()

	const markers = 4_000
	testCases := []struct {
		name   string
		marker string
	}{
		{name: "markers that share one closing parenthesis", marker: "/* sgl.embed(t */ "},
		{name: "markers naming no table the query reads", marker: "/* sgl.embed(t) */ "},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql := "SELECT " + strings.Repeat(testCase.marker, markers) + "r.id FROM roles r WHERE (r.id = 1)"
			scope := embedTestScope(&resolve.ScopedTable{Name: "roles", Alias: "r", JoinKind: ast.JoinInner})

			var diagnostics []diag.SourceError
			scaling.Within(t, 500*time.Millisecond, func(_ context.Context) {
				_, diagnostics = resolveEmbedDirectives(embedTestInput(embedBlock(), sql, scope),
					[]sqlmodel.OutputColumn{qualifiedColumn("id", "roles", "r")})
			})

			messageBytes := 0
			for _, diagnostic := range diagnostics {
				messageBytes += len(diagnostic.Message)
			}
			require.Less(t, messageBytes, 1<<20)
			require.LessOrEqual(t, len(diagnostics), maxInlineEmbedMarkers+1)
			require.True(t, slices.ContainsFunc(diagnostics, func(diagnostic diag.SourceError) bool {
				return diagnostic.Code == diag.CodeDirectiveLineLimit
			}), "the markers left unread are reported")
		})
	}
}

func TestResolveEmbedDirectivesReportsAPartialGroupedEmbed(t *testing.T) {
	t.Parallel()

	scope := embedTestScope(
		&resolve.ScopedTable{Name: "orders", Alias: "orders", JoinKind: ast.JoinInner},
		&resolve.ScopedTable{Name: "order_lines", Alias: "order_lines", JoinKind: ast.JoinLeft},
	)
	columns := func() []sqlmodel.OutputColumn {
		return []sqlmodel.OutputColumn{
			qualifiedColumn("id", "orders", "orders"),
			qualifiedColumn("customer", "orders", "orders"),
			qualifiedColumn("id", "order_lines", "order_lines"),
			qualifiedColumn("dish", "order_lines", "order_lines"),
		}
	}

	testCases := []struct {
		name        string
		embeds      []*directive.EmbedDirective
		groupByKeys []string
		wantQ015    bool
	}{
		{
			name:        "the detail table left unembedded is reported",
			embeds:      []*directive.EmbedDirective{{Table: "orders"}},
			groupByKeys: []string{"orders.id"},
			wantQ015:    true,
		},
		{
			name:        "every detail table embedded is not reported",
			embeds:      []*directive.EmbedDirective{{Table: "orders"}, {Table: "order_lines"}},
			groupByKeys: []string{"orders.id"},
			wantQ015:    false,
		},
		{
			name:        "the key table left unembedded is not reported",
			embeds:      []*directive.EmbedDirective{{Table: "order_lines"}},
			groupByKeys: []string{"orders.id"},
			wantQ015:    false,
		},
		{
			name:        "without group_by nothing is reported",
			embeds:      []*directive.EmbedDirective{{Table: "orders"}},
			groupByKeys: nil,
			wantQ015:    false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			input := embedTestInput(embedBlock(testCase.embeds...), "", scope)
			input.groupByKeys = testCase.groupByKeys
			_, diagnostics := resolveEmbedDirectives(input, columns())

			var partial []diag.SourceError
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == diag.CodeGroupByMissingEmbed {
					partial = append(partial, diagnostic)
				}
			}
			if !testCase.wantQ015 {
				require.Empty(t, partial)
				return
			}
			require.Len(t, partial, 1)
			require.Contains(t, partial[0].Message, "order_lines")
			require.Equal(t, 7, partial[0].Line)
		})
	}
}
