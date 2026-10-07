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

package directive

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

func newTestParser() *Parser {
	return NewParser(
		[]dialect.PlaceholderSigil{
			{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
		},
		ast.CommentStyle{LinePrefix: "--"},
		PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false},
	)
}

func newTestParserWithNamedPrefix() *Parser {
	return NewParser(
		[]dialect.PlaceholderSigil{
			{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			{Prefix: "@", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
		},
		ast.CommentStyle{LinePrefix: "--"},
		PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false},
	)
}

func parseSQL(t *testing.T, parser *Parser, sql string) (*DirectiveBlock, []diag.SourceError) {
	t.Helper()
	return parser.Parse(sqlfile.QueryBlock{SQL: sql, StartLine: 1}, "test.sql")
}

func TestDirectiveParser_TopCallNameCommand(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(name: GetUser, command: one)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "GetUser", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandOne, block.Command.Command)
}

func TestDirectiveParser_TopCallAllKeywordArguments(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(name: SearchOrders, command: many, dynamic: runtime, readonly: false, group_by: orders.id)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)

	directives := ExtractQueryDirectives(block)
	assert.True(t, directives.DynamicRuntime)
	require.NotNil(t, directives.ReadOnlyOverride)
	assert.False(t, *directives.ReadOnlyOverride)
	assert.Equal(t, []string{"orders.id"}, directives.GroupByKeys)
}

func TestDirectiveParser_MultiLineTopCall(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(\n" +
		"--   name: SearchOrders,\n" +
		"--   command: many,\n" +
		"--   dynamic: runtime,\n" +
		"-- )\n" +
		"SELECT 1;"

	block, diagnostics := parseSQL(t, newTestParser(), input)

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "SearchOrders", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandMany, block.Command.Command)
}

func TestDirectiveParser_ContinuationLineSegmentOffsets(t *testing.T) {
	t.Parallel()

	parser := newTestParser()
	lines := []string{
		"-- sgl.query(",
		"--   name: GetUser,",
		"--   command: one",
		"-- )",
	}
	comments := parser.readBlockComments(sqlfile.QueryBlock{SQL: strings.Join(lines, "\n") + "\nSELECT 1;", StartLine: 1})

	logical, next, capped := comments.buildLogicalLine(0)

	require.False(t, capped)
	require.Equal(t, 4, next, "all four physical lines should contribute")
	require.Len(t, logical.Segments, 4)
	require.Equal(t, "sgl.query(   name: GetUser,   command: one )", logical.Content)

	for index := 1; index < len(logical.Segments); index++ {
		segment := logical.Segments[index]
		assert.Equal(t, comments.header[index].Body, logical.Content[segment.ContentStart:segment.ContentEnd])
		assert.Equal(t, index+1, segment.PhysicalLine)
		assert.Equal(t, 4, segment.ColumnOffset)
	}
}

func TestDirectiveParser_ContinuationLineCap(t *testing.T) {
	t.Parallel()

	parser := newTestParser()

	lines := make([]string, 0, maxContinuationLineCount+100)
	lines = append(lines, "-- sgl.query(")
	for range maxContinuationLineCount + 50 {
		lines = append(lines, "--   name: GetUser,")
	}
	sql := strings.Join(lines, "\n") + "\nSELECT 1;"

	comments := parser.readBlockComments(sqlfile.QueryBlock{SQL: sql, StartLine: 1})
	logical, next, capped := comments.buildLogicalLine(0)

	assert.True(t, capped)
	assert.LessOrEqual(t, len(logical.Segments), maxContinuationLineCount+1,
		"continuation segments must be bounded by the per-line cap")
	assert.Equal(t, maxContinuationLineCount+1, next,
		"the builder must stop absorbing lines once the cap is reached")

	_, diagnostics := parseSQL(t, parser, sql)
	require.True(t, reportsCode(diagnostics, diag.CodeDirectiveLineLimit),
		"a directive cut short by the continuation cap must be reported, not parsed silently")
}

func TestDirectiveParser_ParamBindings(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: SearchOrders, command: many)\n" +
		"-- $1 as sgl.param(status, optional: true)\n" +
		"-- $2 as sgl.param(ids, kind: slice, type: int)\n" +
		"-- sgl.sortable(order_by, columns: [name, total, placed_at])\n" +
		"-- $3 as sgl.param(page_size, default: 10, max: 100)\n" +
		"-- $4 as sgl.param(skip)\n" +
		"SELECT 1;"

	block, diagnostics := parseSQL(t, newTestParser(), input)

	require.Empty(t, diagnostics)
	require.Len(t, block.Parameters, 5)

	assert.Equal(t, sqlmodel.ParameterDirectiveParam, block.Parameters[0].Kind)
	assert.Equal(t, "status", block.Parameters[0].Name)
	assert.True(t, block.Parameters[0].IsOptional)

	assert.True(t, block.Parameters[1].IsSlice)
	require.NotNil(t, block.Parameters[1].TypeHint)
	assert.Equal(t, "int", *block.Parameters[1].TypeHint)

	assert.Equal(t, sqlmodel.ParameterDirectiveSortable, block.Parameters[2].Kind)
	assert.Equal(t, []string{"name", "total", "placed_at"}, block.Parameters[2].Columns)

	assert.Equal(t, sqlmodel.ParameterDirectiveParam, block.Parameters[3].Kind)
	require.NotNil(t, block.Parameters[3].DefaultValue)
	assert.Equal(t, 10, *block.Parameters[3].DefaultValue)
	require.NotNil(t, block.Parameters[3].MaxValue)
	assert.Equal(t, 100, *block.Parameters[3].MaxValue)

	assert.Equal(t, "skip", block.Parameters[4].Name)
}

func TestDirectiveParser_NamedAnchor(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: GetUser, command: one)\n" +
		"-- :email as sgl.param(user_email, type: text)\n" +
		"SELECT 1;"

	block, diagnostics := parseSQL(t, newTestParserWithNamedPrefix(), input)

	require.Empty(t, diagnostics)
	require.Len(t, block.Parameters, 1)
	assert.True(t, block.Parameters[0].IsNamed)
	assert.Equal(t, "user_email", block.Parameters[0].Name)
}

func TestDirectiveParser_EmbedHeader(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: GetBookWithAuthor, command: one)\n" +
		"-- sgl.embed(authors, from: a)\n" +
		"SELECT 1;"

	block, diagnostics := parseSQL(t, newTestParser(), input)

	require.Empty(t, diagnostics)
	require.Len(t, block.Embeds, 1)
	assert.Equal(t, "authors", block.Embeds[0].Table)
	assert.Equal(t, "a", block.Embeds[0].From)
}

func TestDirectiveParser_UnknownDirectiveSuggestion(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: G, command: one)\n" +
		"-- $1 as sgl.parm(status)\n" +
		"SELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	require.NotEmpty(t, diagnostics)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnknownDirective {
			assert.Equal(t, "sgl.param", diagnostic.Suggestion)
			found = true
		}
	}
	assert.True(t, found, "expected an unknown-directive diagnostic with Suggestion=sgl.param")
}

func TestDirectiveParser_UnknownKeywordArgumentSuggestion(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(naem: G, command: one)\nSELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	require.NotEmpty(t, diagnostics)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnknownKeywordArgument {
			assert.Equal(t, "name", diagnostic.Suggestion)
			found = true
		}
	}
	assert.True(t, found, "expected an unknown-keywordArgument diagnostic with Suggestion=name")
}

func TestDirectiveParser_InvalidEnumSuggestion(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: G, command: onee)\nSELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	require.NotEmpty(t, diagnostics)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeInvalidKeywordArgumentValue {
			assert.Equal(t, "one", diagnostic.Suggestion)
			found = true
		}
	}
	assert.True(t, found, "expected invalid-keywordArgument-value diagnostic with Suggestion=one")
}

func TestDirectiveParser_DuplicateKeywordArgument(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: G, name: H, command: one)\nSELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDuplicateKeywordArgument {
			found = true
		}
	}
	assert.True(t, found, "expected duplicate-keywordArgument diagnostic")
}

func TestDirectiveParser_MissingRequired(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(command: one)\nSELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	hasMissingName := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeMissingRequired && diagnostic.Message == "missing required name on sgl.query" {
			hasMissingName = true
		}
	}
	assert.True(t, hasMissingName)
}

func TestDirectiveParser_UnclosedCall(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: G, command: one\nSELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnclosedDirective {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed-directive diagnostic")
}

func TestDirectiveParser_EmptyInput(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(), "")

	require.NotNil(t, block)
	assert.Nil(t, block.Name)
	assert.Nil(t, block.Command)

	assert.Len(t, diagnostics, 2)
}

func TestDirectiveParser_PreservesGroupByMultiple(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: G, command: many, group_by: orders.id)\nSELECT 1;"

	block, diagnostics := parseSQL(t, newTestParser(), input)

	require.Empty(t, diagnostics)
	directives := ExtractQueryDirectives(block)
	assert.Equal(t, []string{"orders.id"}, directives.GroupByKeys)
}

func TestDirectiveParser_TopCallPositional(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "GetUser", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandOne, block.Command.Command)
}

func TestDirectiveParser_TopCallPositionalWithKeywordArguments(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(SearchOrders, many, dynamic: runtime, group_by: orders.id)\nSELECT 1;")

	require.Empty(t, diagnostics)
	assert.Equal(t, "SearchOrders", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandMany, block.Command.Command)

	directives := ExtractQueryDirectives(block)
	assert.True(t, directives.DynamicRuntime)
	assert.Equal(t, []string{"orders.id"}, directives.GroupByKeys)
}

func TestDirectiveParser_KeywordArgumentAsPositional(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(command: one, name: GetUser)\nSELECT 1;")

	require.Empty(t, diagnostics)
	assert.Equal(t, "GetUser", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandOne, block.Command.Command)
}

func TestDirectiveParser_KeywordArgumentAsPositionalDuplicate(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one, name: Other)\nSELECT 1;")

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDuplicateKeywordArgument {
			found = true
		}
	}
	assert.True(t, found, "expected duplicate diagnostic when positional and keywordArgument-as-positional fill the same slot")
}

func TestDirectiveParser_TooManyPositionals(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one, extra)\nSELECT 1;")

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDirectiveSyntax {
			found = true
		}
	}
	assert.True(t, found, "expected directive-syntax diagnostic for extra positional")
}

func TestDirectiveParser_PositionalParameterBinding(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, many)\n-- $1 as sgl.param(status, optional: true)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.Parameters, 1)
	assert.Equal(t, "status", block.Parameters[0].Name)
	assert.Equal(t, sqlmodel.ParameterDirectiveParam, block.Parameters[0].Kind)
	assert.True(t, block.Parameters[0].IsOptional)
}

func TestDirectiveParser_SortablePositionalColumns(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetItems, many)\n-- sgl.sortable(order_by, [name, price])\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.Parameters, 1)
	assert.Equal(t, "order_by", block.Parameters[0].Name)
	assert.Equal(t, []string{"name", "price"}, block.Parameters[0].Columns)
}

func TestDirectiveParser_EmbedPositional(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetBook, one)\n-- sgl.embed(authors, from: a)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.Embeds, 1)
	assert.Equal(t, "authors", block.Embeds[0].Table)
	assert.Equal(t, "a", block.Embeds[0].From)
}

func TestDirectiveParser_ParameterBindingExpectedPrefix(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, many)\n-- $1 as sgk.optional(status)\nSELECT 1;")

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeParameterBindingSyntax {
			assert.Equal(t, "sgl", diagnostic.Suggestion)
			found = true
		}
	}
	assert.True(t, found, "expected Q033 with Suggestion=sgl")
}

func TestDirectiveParser_InvalidListLiteralBracket(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, many)\n-- sgl.sortable(order_by, columns: [a, b, c))\nSELECT 1;")

	foundList := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeInvalidListLiteral {
			foundList = true
		}
	}
	assert.True(t, foundList, "expected Q034 for mismatched list closer")
}

func TestDirectiveParser_UnterminatedString(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(name: \"open, command: many)\nSELECT 1;")

	foundUnterminated := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnterminatedString {
			foundUnterminated = true
		}
	}
	assert.True(t, foundUnterminated, "expected Q035 for unterminated string")
}

func TestDirectiveParser_ProseCommentWithApostropheIgnored(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(name: VerifyEmail, command: one)\n"+
			"-- Marks an account's email as verified\n"+
			"SELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "VerifyEmail", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandOne, block.Command.Command)
}

func TestDirectiveParser_ProseCommentBeforeDirectiveIgnored(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- Marks an account's email as verified\n"+
			"-- sgl.query(name: VerifyEmail, command: one)\n"+
			"SELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	assert.Equal(t, "VerifyEmail", block.Name.Value)
}

func TestDirectiveParser_ProseStartingWithPrefixWordIgnored(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl keeps this query fast\n"+
			"-- sgl.query(name: GetUser, command: one)\n"+
			"SELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	assert.Equal(t, "GetUser", block.Name.Value)
}

func TestDirectiveParser_ProseWithUnbalancedParenthesisDoesNotSwallowDirective(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- returns the latest row (if present\n"+
			"-- sgl.query(name: GetLatest, command: one)\n"+
			"SELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "GetLatest", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandOne, block.Command.Command)
}

func TestDirectiveParser_ProseWithUnbalancedBracketDoesNotSwallowDirective(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- see the list [item one for context\n"+
			"-- sgl.query(name: ListThings, command: many)\n"+
			"SELECT 1;")

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "ListThings", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandMany, block.Command.Command)
}

func TestDirectiveParser_MultiLineCallWithSurroundingProse(t *testing.T) {
	t.Parallel()

	input := "-- Fetch a user's orders (most recent first)\n" +
		"-- sgl.query(\n" +
		"--   name: SearchOrders,\n" +
		"--   command: many,\n" +
		"-- )\n" +
		"-- ordering is handled by the caller's cursor\n" +
		"SELECT 1;"

	block, diagnostics := parseSQL(t, newTestParser(), input)

	require.Empty(t, diagnostics)
	require.NotNil(t, block.Name)
	require.NotNil(t, block.Command)
	assert.Equal(t, "SearchOrders", block.Name.Value)
	assert.Equal(t, sqlmodel.QueryCommandMany, block.Command.Command)
}

func TestDirectiveParser_GenuineUnterminatedStringInMultiLineCall(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(\n" +
		"--   name: GetUser,\n" +
		"--   command: 'one\n" +
		"-- )\n" +
		"SELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	foundUnterminated := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnterminatedString {
			foundUnterminated = true
		}
	}
	assert.True(t, foundUnterminated, "expected Q035 for an unterminated string inside a real directive")
}

func TestDirectiveParser_GenuineUnterminatedStringInParamBinding(t *testing.T) {
	t.Parallel()

	input := "-- sgl.query(name: SearchOrders, command: many)\n" +
		"-- $1 as sgl.param('status\n" +
		"SELECT 1;"

	_, diagnostics := parseSQL(t, newTestParser(), input)

	foundUnterminated := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnterminatedString {
			foundUnterminated = true
		}
	}
	assert.True(t, foundUnterminated, "expected Q035 for an unterminated string inside a parameter binding")
}

func TestDirectiveParser_ProseOnlyHeaderStillRequiresDirective(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- describes the user's most recent activity\n"+
			"SELECT 1;")

	require.NotEmpty(t, diagnostics, "a prose-only header must still report missing directives")
	for _, diagnostic := range diagnostics {
		assert.NotEqual(t, diag.CodeUnterminatedString, diagnostic.Code,
			"a prose apostrophe must not raise Q035")
	}
}

func TestDirectiveParser_ColumnOverrideTypeOnly(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\n-- sgl.column(email_lower, type: text)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.ColumnOverrides, 1)
	assert.Equal(t, "email_lower", block.ColumnOverrides[0].Name)
	assert.Equal(t, "text", block.ColumnOverrides[0].SQLType)
	assert.Equal(t, "", block.ColumnOverrides[0].GoType)
	assert.Nil(t, block.ColumnOverrides[0].Nullable)
}

func TestDirectiveParser_ColumnOverrideGoTypeOnly(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\n-- sgl.column(id, go_type: \"github.com/google/uuid.UUID\")\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.ColumnOverrides, 1)
	assert.Equal(t, "id", block.ColumnOverrides[0].Name)
	assert.Equal(t, "github.com/google/uuid.UUID", block.ColumnOverrides[0].GoType)
}

func TestDirectiveParser_ColumnOverrideNullable(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\n-- sgl.column(id, nullable: true)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.ColumnOverrides, 1)
	require.NotNil(t, block.ColumnOverrides[0].Nullable)
	assert.True(t, *block.ColumnOverrides[0].Nullable)
}

func TestDirectiveParser_ColumnOverrideNonNullElements(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "declared non-null", value: "true", want: true},
		{name: "declared nullable", value: "false", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(),
				"-- sgl.query(GetUser, one)\n-- sgl.column(tags, non_null_elements: "+testCase.value+")\nSELECT 1;")

			require.Empty(t, diagnostics)
			require.Len(t, block.ColumnOverrides, 1)
			require.NotNil(t, block.ColumnOverrides[0].NonNullElements)
			require.Equal(t, testCase.want, *block.ColumnOverrides[0].NonNullElements)
			require.Nil(t, block.ColumnOverrides[0].Nullable)
		})
	}
}

func TestDirectiveParser_ParameterNonNullElements(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		want  *bool
		name  string
		param string
	}{
		{name: "declared non-null", param: "-- $1 as sgl.param(tags, non_null_elements: true)", want: new(true)},
		{name: "declared nullable", param: "-- $1 as sgl.param(tags, non_null_elements: false)", want: new(false)},
		{name: "not declared", param: "-- $1 as sgl.param(tags)", want: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(),
				"-- sgl.query(SetTags, exec)\n"+testCase.param+"\nUPDATE posts SET tags = $1;")

			require.Empty(t, diagnostics)
			require.Len(t, block.Parameters, 1)
			require.Equal(t, testCase.want, block.Parameters[0].NonNullElements)
		})
	}
}

func TestDirectiveParser_RowTypeKeywords(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		header    string
		wantModel string
		wantRow   string
		wantCodes []string
	}{
		{name: "a table's model", header: "-- sgl.query(ListRoles, many, model: roles)", wantModel: "roles"},
		{name: "a schema-qualified table's model", header: "-- sgl.query(ListRoles, many, model: auth.roles)", wantModel: "auth.roles"},
		{name: "a named row", header: "-- sgl.query(ListRoles, many, row: RoleSummary)", wantRow: "RoleSummary"},
		{name: "a snake_case row name", header: "-- sgl.query(ListRoles, many, row: role_summary)", wantRow: "role_summary"},
		{
			name: "a model and a row together", header: "-- sgl.query(ListRoles, many, model: roles, row: RoleSummary)",
			wantModel: "roles", wantRow: "RoleSummary", wantCodes: []string{diag.CodeMutuallyExclusiveKeywordArgument},
		},
		{
			name: "a row name that cannot become a Go name", header: "-- sgl.query(ListRoles, many, row: _hidden)",
			wantRow: "_hidden", wantCodes: []string{diag.CodeInvalidKeywordArgumentValue},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), testCase.header+"\nSELECT * FROM roles;")

			codes := make([]string, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				codes = append(codes, diagnostic.Code)
			}
			require.ElementsMatch(t, testCase.wantCodes, codes)
			directives := ExtractQueryDirectives(block)
			require.Equal(t, testCase.wantModel, directives.Model)
			require.Equal(t, testCase.wantRow, directives.Row)
		})
	}
}

func TestDirectiveParser_ColumnOverrideMutuallyExclusive(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\n-- sgl.column(id, type: text, go_type: \"foo.Bar\")\nSELECT 1;")

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeMutuallyExclusiveKeywordArgument {
			found = true
		}
	}
	assert.True(t, found, "expected Q038 mutually-exclusive diagnostic")
}

func TestDirectiveParser_ColumnOverrideMultiple(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetUser, one)\n-- sgl.column(id, type: int8)\n-- sgl.column(name, nullable: true)\nSELECT 1;")

	require.Empty(t, diagnostics)
	require.Len(t, block.ColumnOverrides, 2)
	assert.Equal(t, "id", block.ColumnOverrides[0].Name)
	assert.Equal(t, "int8", block.ColumnOverrides[0].SQLType)
	assert.Equal(t, "name", block.ColumnOverrides[1].Name)
	require.NotNil(t, block.ColumnOverrides[1].Nullable)
}

func TestDirectiveParser_NegativeNumberLiteral(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetItems, many)\n-- $1 as sgl.param(page_size, default: -5)\nSELECT 1;")

	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeInvalidKeywordArgumentValue, diagnostics[0].Code)
	assert.Equal(t, 40, diagnostics[0].Column)
	require.Len(t, block.Parameters, 1)
	require.NotNil(t, block.Parameters[0].DefaultValue)
	assert.Equal(t, -5, *block.Parameters[0].DefaultValue)
}

func TestDirectiveParser_MissingColonReportsOnlyTheSyntaxError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
	}{
		{name: "a keyword argument without its colon", input: "-- sgl.query(GetAuthor, one, readonly true)\nSELECT 1;"},
		{name: "a keyword argument without its colon on a binding", input: "-- sgl.query(GetAuthor, one)\n-- $1 as sgl.param(author_id, nullable true)\nSELECT 1;"},
		{name: "two values without their colon", input: "-- sgl.query(GetAuthor, one, readonly true false)\nSELECT 1;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), testCase.input)

			require.Len(t, diagnostics, 1, "only the real syntax error is reported: %v", diagnostics)
			assert.Equal(t, diag.CodeDirectiveSyntax, diagnostics[0].Code)
			assert.Contains(t, diagnostics[0].Message, "expected ',' or ')'")
			require.NotNil(t, block.Name)
			assert.Equal(t, "GetAuthor", block.Name.Value)
			require.NotNil(t, block.Command)
		})
	}
}

func TestDirectiveParser_QueryNameMustBeAnExportableGoName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		header    string
		wantValid bool
	}{
		{name: "a PascalCase name", header: "-- sgl.query(GetUser, one)", wantValid: true},
		{name: "a snake_case name", header: "-- sgl.query(get_user, one)", wantValid: true},
		{name: "a name given by keyword", header: "-- sgl.query(name: list_users2, command: many)", wantValid: true},
		{name: "a quoted name with a non-ASCII capital", header: `-- sgl.query("Ärger", one)`, wantValid: true},
		{name: "a digit", header: "--sgl.query(0,mAnY)", wantValid: false},
		{name: "a leading digit by keyword", header: `-- sgl.query(name: "2fa_codes", command: many)`, wantValid: false},
		{name: "a dotted name", header: "-- sgl.query(name.A, one)", wantValid: false},
		{name: "a leading underscore", header: "-- sgl.query(_hidden, one)", wantValid: false},
		{name: "a quoted name with a space", header: `-- sgl.query("Get User", one)`, wantValid: false},
		{name: "a quoted name with no capital form", header: `-- sgl.query("名字", one)`, wantValid: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := parseSQL(t, newTestParser(), testCase.header+"\nSELECT 1;")

			invalid := diagnosticsWithCode(diagnostics, diag.CodeInvalidKeywordArgumentValue)
			if testCase.wantValid {
				require.Empty(t, diagnostics)
				return
			}
			require.Len(t, invalid, 1, "%v", diagnostics)
			assert.Equal(t, 1, invalid[0].Line)
			assert.Contains(t, invalid[0].Message, "starts with a letter")
		})
	}
}

func TestDirectiveParserReportsTextAfterTheCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		wantColumn int
		wantText   string
	}{
		{name: "a binding with a trailing type", sql: "-- sgl.query(GetJob, one)\n-- $1 as sgl.param(jobID) int\nSELECT $1;", wantColumn: 27, wantText: `"int"`},
		{name: "a header with trailing prose", sql: "-- sgl.query(GetJob, one) returns the job\nSELECT 1;", wantColumn: 27, wantText: `"returns"`},
		{name: "an embed with a trailing word", sql: "-- sgl.query(GetJob, one)\n-- sgl.embed(owners) please\nSELECT 1;", wantColumn: 22, wantText: `"please"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := parseSQL(t, newTestParser(), testCase.sql)
			var trailing []diag.SourceError
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == diag.CodeDirectiveSyntax {
					trailing = append(trailing, diagnostic)
				}
			}
			require.Len(t, trailing, 1, "diagnostics: %+v", diagnostics)
			assert.Contains(t, trailing[0].Message, testCase.wantText)
			assert.Equal(t, testCase.wantColumn, trailing[0].Column)
		})
	}

	_, clean := parseSQL(t, newTestParser(), "-- sgl.query(GetJob, one)\n-- $1 as sgl.param(jobID, nullable: false)\nSELECT $1;")
	assert.Empty(t, clean)
}

func TestDirectiveParser_DescriptionIsTheHeadersProse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "lines above the header",
			sql:  "-- GetUser reads one user.\n-- It fails when none matches.\n-- sgl.query(GetUser, one)\nSELECT 1;",
			want: "GetUser reads one user.\nIt fails when none matches.",
		},
		{
			name: "lines below the header and its bindings",
			sql:  "-- sgl.query(GetUser, one)\n-- $1 as sgl.param(user_id)\n-- Reads one user.\nSELECT $1;",
			want: "Reads one user.",
		},
		{
			name: "a blank or empty comment line starts a paragraph",
			sql:  "-- First paragraph.\n--\n-- Second paragraph.\n-- sgl.query(GetUser, one)\n\n-- Third paragraph.\nSELECT 1;",
			want: "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.",
		},
		{
			name: "indentation is kept and trailing spaces dropped",
			sql:  "-- Example:   \n--\n--   SELECT 1;\n-- sgl.query(GetUser, one)\nSELECT 1;",
			want: "Example:\n\n  SELECT 1;",
		},
		{
			name: "prose around the header is one paragraph",
			sql:  "-- Reads one user.\n-- sgl.query(GetUser, one)\n-- $1 as sgl.param(user_id)\n-- It fails when none matches.\nSELECT $1;",
			want: "Reads one user.\nIt fails when none matches.",
		},
		{
			name: "a continued directive is no prose",
			sql:  "-- sgl.query(GetUser, one,\n--   readonly: true)\nSELECT 1;",
			want: "",
		},
		{
			name: "comments after the SQL starts are no description",
			sql:  "-- sgl.query(GetUser, one)\nSELECT 1 -- trailing\n-- after\n;",
			want: "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParser(), testCase.sql)

			require.Empty(t, diagnostics)
			require.Equal(t, testCase.want, block.Description)
			require.Equal(t, testCase.want, ExtractQueryDirectives(block).Description)
		})
	}
}

func newPolicyParser(policy PrefixPolicy) *Parser {
	return NewParser(
		[]dialect.PlaceholderSigil{{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}},
		ast.CommentStyle{LinePrefix: "--"},
		policy,
	)
}

func acmePolicy(requireCanonical bool) PrefixPolicy {
	return PrefixPolicy{
		Prefixes:         namespace.Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: []string{"other"}},
		RequireCanonical: requireCanonical,
	}
}

func diagnosticsWithCode(diagnostics []diag.SourceError, code string) []diag.SourceError {
	var matched []diag.SourceError
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			matched = append(matched, diagnostic)
		}
	}
	return matched
}

func TestParserAcceptsAliasPrefixesAsCanonical(t *testing.T) {
	t.Parallel()

	sql := "-- acmedb.query(GetUser, one)\n-- $1 as acmedb.param(user_id)\n-- acmedb.embed(orders, from: o)\nSELECT 1;"
	canonicalSQL := "-- acme.query(GetUser, one)\n-- $1 as acme.param(user_id)\n-- acme.embed(orders, from: o)\nSELECT 1;"

	aliasBlock, aliasDiagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)), sql)
	canonicalBlock, canonicalDiagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)), canonicalSQL)

	require.Empty(t, aliasDiagnostics)
	require.Empty(t, canonicalDiagnostics)
	require.NotNil(t, aliasBlock.Name)
	assert.Equal(t, "GetUser", aliasBlock.Name.Value)
	require.Len(t, aliasBlock.Parameters, 1)
	assert.Equal(t, "user_id", aliasBlock.Parameters[0].Name)
	assert.Equal(t, "param", aliasBlock.Parameters[0].DirectiveName)
	require.Len(t, aliasBlock.Embeds, 1)
	assert.Equal(t, canonicalBlock.Embeds[0].Table, aliasBlock.Embeds[0].Table)
}

func TestParserReportsAliasPrefixOnlyWhenCanonicalRequired(t *testing.T) {
	t.Parallel()

	sql := "-- acmedb.query(GetUser, one)\n-- $1 as acmedb.param(user_id)\nSELECT 1;"

	_, relaxed := parseSQL(t, newPolicyParser(acmePolicy(false)), sql)
	assert.Empty(t, diagnosticsWithCode(relaxed, diag.CodeAliasPrefix))

	_, strict := parseSQL(t, newPolicyParser(acmePolicy(true)), sql)
	hints := diagnosticsWithCode(strict, diag.CodeAliasPrefix)
	require.Len(t, hints, 2)
	for _, hint := range hints {
		assert.Equal(t, diag.SeverityHint, hint.Severity)
		assert.Equal(t, "acme", hint.Suggestion)
		assert.Contains(t, hint.Message, `"acmedb"`)
	}

	_, canonical := parseSQL(t, newPolicyParser(acmePolicy(true)), "-- acme.query(GetUser, one)\nSELECT 1;")
	assert.Empty(t, diagnosticsWithCode(canonical, diag.CodeAliasPrefix))
}

func TestParserReportsForeignPrefixWithoutParsing(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)),
		"-- other.query(GetUser, one)\n-- acme.query(GetUser, one)\n-- $1 as other.param(user_id)\nSELECT 1;")

	warnings := diagnosticsWithCode(diagnostics, diag.CodeForeignPrefix)
	require.Len(t, warnings, 2)
	assert.Equal(t, diag.SeverityWarning, warnings[0].Severity)
	assert.Equal(t,
		"-- other.query is not enabled here; add other to directives.prefixes to accept it, then optionally format the directives to rewrite it to the canonical prefix",
		warnings[0].Message)
	assert.Contains(t, warnings[1].Message, "other.param")
	require.NotNil(t, block.Name)
	assert.Empty(t, block.Parameters)
}

func TestParserReportsBareForeignCall(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)), "-- other(name: Foo)\n-- acme.query(Foo, one)\nSELECT 1;")

	warnings := diagnosticsWithCode(diagnostics, diag.CodeForeignPrefix)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "-- other is not enabled here")
}

func TestParserTreatsForeignProseAsProse(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)),
		"-- acme.query(GetUser, one)\n-- other keeps this query fast\nSELECT 1;")

	assert.Empty(t, diagnostics)
}

func TestParserWithoutForeignPrefixesIgnoresOtherTools(t *testing.T) {
	t.Parallel()

	policy := PrefixPolicy{Prefixes: namespace.Set{Canonical: "acme", Aliases: nil, Foreign: nil}, RequireCanonical: false}
	_, diagnostics := parseSQL(t, newPolicyParser(policy), "-- other.query(GetUser, one)\n-- acme.query(GetUser, one)\nSELECT 1;")

	assert.Empty(t, diagnostics)
}

func TestParserRendersCanonicalPrefixInDiagnostics(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)), "-- acmedb.nope(GetUser, one)\nSELECT 1;")

	unknown := diagnosticsWithCode(diagnostics, diag.CodeUnknownDirective)
	require.Len(t, unknown, 1)
	assert.Contains(t, unknown[0].Message, "acme.nope")
	assert.NotContains(t, unknown[0].Message, "acmedb")

	assert.Empty(t, diagnosticsWithCode(diagnostics, diag.CodeMissingDirective),
		"an unknown first directive is most likely the misspelt header, so the missing name and command are not reported too")

	_, diagnostics = parseSQL(t, newPolicyParser(acmePolicy(false)), "-- acmedb.sortable(order_by, columns: [name])\nSELECT 1;")
	missing := diagnosticsWithCode(diagnostics, diag.CodeMissingDirective)
	require.NotEmpty(t, missing)
	assert.Contains(t, missing[0].Message, "acme(...)")
}

func TestParserBareCanonicalCallIsUnknownDirective(t *testing.T) {
	t.Parallel()

	_, diagnostics := parseSQL(t, newPolicyParser(acmePolicy(false)), "-- acme(name: Foo, command: one)\nSELECT 1;")

	unknown := diagnosticsWithCode(diagnostics, diag.CodeUnknownDirective)
	require.Len(t, unknown, 1)
	assert.Contains(t, unknown[0].Message, `"acme"`)
}
