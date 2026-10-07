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

package postgres

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

type viewLikeExtension struct{}

type capturingExtension struct{}

func (viewLikeExtension) Classify(tokens []pgext.Token) statementKind {
	if len(tokens) < 2 || !tokens[0].IsKeyword(pgext.TokenIdentifier, "CREATE") || !tokens[1].IsKeyword(pgext.TokenIdentifier, "VIEWLIKE") {
		return 0
	}
	return statementKindFoobar
}

func (viewLikeExtension) Parse(p pgext.ParserContext, _ statementKind) (*ast.CatalogueMutation, error) {
	p.Advance()
	p.Advance()
	_, name, err := p.ParseQualifiedName()
	if err != nil {
		return nil, err
	}
	if expectErr := p.ExpectKeyword("AS"); expectErr != nil {
		return nil, expectErr
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, "", name)
	mutation.ViewDefinition, _ = p.AnalyseViewBody(nil)
	p.ConsumeRemainder()
	return mutation, nil
}

func (capturingExtension) Classify(tokens []pgext.Token) statementKind {
	if len(tokens) >= 2 && tokens[0].IsKeyword(pgext.TokenIdentifier, "CAPTURE") {
		return statementKindFoobar
	}
	return 0
}

func (capturingExtension) Parse(p pgext.ParserContext, _ statementKind) (*ast.CatalogueMutation, error) {
	if err := p.ExpectKeyword("CAPTURE"); err != nil {
		return nil, err
	}
	tokens := p.Tokens()
	tokens[1].Value = "tampered"
	current := p.CurrentToken()
	head := p.EngineSpecificFromTokens(1, 3)
	body := p.ConsumeRemainderAsText()
	return &ast.CatalogueMutation{
		Kind:      ast.MutationCreateTable,
		TableName: current.Value,
		EngineSpecific: map[string]string{
			"HEAD":   head,
			"BODY":   body,
			"QUOTED": strconv.FormatBool(current.Quoted),
			"RAW":    current.Raw,
		},
	}, nil
}

func contextOver(t *testing.T, sql string) *parserContext {
	t.Helper()

	engine := New()
	parsed := parsedStatementOf(t, sql)
	return newParserContext(engine.newStatementParser(dialectkit.WalkGuard(t.Context(), len(parsed.tokens)), parsed), engine)
}

func TestParseColumnListReadsAParenthesisedListOfNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantErr     string
		wantNames   []string
		wantCurrent string
	}{
		{name: "bare, quoted and string names", sql: `SELECT ("Mixed", plain, 'text') rest`, wantNames: []string{"Mixed", "plain", "text"}, wantCurrent: "rest"},
		{name: "an empty list", sql: "SELECT () rest", wantNames: nil, wantCurrent: "rest"},
		{name: "no opening parenthesis", sql: "SELECT plain", wantErr: "expected '('"},
		{name: "names with no comma between", sql: "SELECT (a b)", wantErr: "expected ')'"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)
			parserContext.Advance()

			names, err := parserContext.ParseColumnList()

			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantNames, names)
			assert.Equal(t, testCase.wantCurrent, parserContext.CurrentToken().Value)
		})
	}
}

func TestMatchIfNotExistsConsumesTheWholeClauseOrNothing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMatched bool
		wantCurrent string
	}{
		{name: "the whole clause", sql: "SELECT IF NOT EXISTS widgets", wantMatched: true, wantCurrent: "widgets"},
		{name: "IF EXISTS alone", sql: "SELECT IF EXISTS widgets", wantMatched: false, wantCurrent: "if"},
		{name: "IF NOT without EXISTS", sql: "SELECT IF NOT widgets", wantMatched: false, wantCurrent: "if"},
		{name: "no clause", sql: "SELECT widgets", wantMatched: false, wantCurrent: "widgets"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)
			parserContext.Advance()

			matched := parserContext.MatchIfNotExists()

			assert.Equal(t, testCase.wantMatched, matched)
			assert.Equal(t, testCase.wantCurrent, parserContext.CurrentToken().Value)
		})
	}
}

func TestParseQualifiedNameReadsTheSchemaAndName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantSchema string
		wantName   string
		wantErr    bool
	}{
		{name: "a bare name", sql: "SELECT widgets", wantSchema: "", wantName: "widgets"},
		{name: "a schema-qualified name", sql: "SELECT stock.widgets", wantSchema: "stock", wantName: "widgets"},
		{name: "a database-qualified name drops the database", sql: "SELECT shop.stock.widgets", wantSchema: "stock", wantName: "widgets"},
		{name: "a quoted name keeps its case", sql: `SELECT "Stock"."Widgets"`, wantSchema: "Stock", wantName: "Widgets"},
		{name: "a dot with no name after it", sql: "SELECT stock.", wantErr: true},
		{name: "no name", sql: "SELECT ,", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)
			parserContext.Advance()

			schema, name, err := parserContext.ParseQualifiedName()

			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantSchema, schema)
			assert.Equal(t, testCase.wantName, name)
		})
	}
}

func TestAnalyseViewBodyAnalysesTheBodyWithoutConsumingIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		columnNames    []string
		wantOutputs    []string
		wantUnanalysed int
		wantErr        bool
	}{
		{name: "a query", sql: "SELECT 1 AS a, 2 AS b", wantOutputs: []string{"a", "b"}},
		{name: "a query whose columns the declared names rename", sql: "SELECT 1 AS a, 2 AS b", columnNames: []string{"x"}, wantOutputs: []string{"x", "b"}},
		{name: "a body that is no query", sql: "EXECUTE prepared_rows", wantUnanalysed: 1},
		{name: "a body nested beyond the depth limit", sql: "SELECT " + strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300), wantUnanalysed: 1},
		{name: "a malformed body", sql: "SELECT (1", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)

			analysis, err := parserContext.AnalyseViewBody(testCase.columnNames)

			assert.Equal(t, 0, parserContext.p.Position)
			assert.Len(t, parserContext.p.unanalysedParts, testCase.wantUnanalysed)
			if testCase.wantErr {
				require.Error(t, err)
				assert.Nil(t, analysis)
				assert.Equal(t, err, parserContext.p.syntax.err, "a malformed body fails the statement")
				return
			}
			require.NoError(t, err)
			if testCase.wantOutputs == nil {
				assert.Nil(t, analysis)
				return
			}
			require.NotNil(t, analysis)
			assert.Equal(t, testCase.wantOutputs, outputNames(analysis))
		})
	}
}

func TestAnExtensionsViewBodyIsReportedAsTheBuiltInViewsIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantUnanalysed int
		wantDefinition bool
		wantParseError bool
	}{
		{name: "a body the parser analyses", sql: "CREATE VIEWLIKE v AS SELECT 1 AS a", wantDefinition: true},
		{name: "a body nested beyond the depth limit", sql: "CREATE VIEWLIKE v AS SELECT " + strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300), wantUnanalysed: 1},
		{name: "a malformed body fails though the extension drops the error", sql: "CREATE VIEWLIKE v AS SELECT (1", wantParseError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(WithStatementExtensions(viewLikeExtension{})), testCase.sql)

			if testCase.wantParseError {
				assert.Nil(t, mutation)
				_, isParseError := errors.AsType[*dialectkit.ParseError](err)
				require.True(t, isParseError, "got %v", err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantDefinition, mutation.ViewDefinition != nil)
			assert.Len(t, mutation.UnanalysedParts, testCase.wantUnanalysed)
		})
	}
}

func TestParserContext_MatchIfExists_PartialMatchRestoresCursor(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("IF NOT EXISTS table")
	require.NoError(t, err)
	parserInstance := newParser(tokens)
	parserContextInstance := newParserContext(parserInstance, New())

	startPosition := parserInstance.Position
	matched := parserContextInstance.MatchIfExists()
	assert.False(t, matched, "IF NOT EXISTS must not satisfy MatchIfExists")
	assert.Equal(t, startPosition, parserInstance.Position,
		"cursor must rewind to IF on a partial match")
	assert.True(t, parserInstance.IsKeyword("IF"),
		"cursor should be back on the IF token so the next matcher can re-read it")
}

func TestParserContext_ParseReloptionList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want     map[string]string
		name     string
		sql      string
		wantErr  string
		consumed bool
	}{
		{
			name:     "key with a value",
			sql:      "(fillfactor = 70)",
			want:     map[string]string{"fillfactor": "70"},
			consumed: true,
		},
		{
			name:     "bare key means true",
			sql:      "(timescaledb.continuous)",
			want:     map[string]string{"timescaledb.continuous": "true"},
			consumed: true,
		},
		{
			name:     "bare key mixed with valued keys",
			sql:      "(timescaledb.continuous, timescaledb.materialized_only = false)",
			want:     map[string]string{"timescaledb.continuous": "true", "timescaledb.materialized_only": "false"},
			consumed: true,
		},
		{
			name:    "key followed by an unexpected token",
			sql:     "(fillfactor 70)",
			wantErr: "expected '=' after reloption key",
		},
		{
			name:    "missing open paren",
			sql:     "fillfactor = 70",
			wantErr: "expected '('",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)
			parserInstance := newParser(tokens)

			options, err := newParserContext(parserInstance, New()).ParseReloptionList()

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, options)
			assert.Equal(t, testCase.consumed, parserInstance.AtEnd())
		})
	}
}

func TestParserContext_ExpectKeyword(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{name: "matching keyword is consumed", sql: "create table", wantErr: false},
		{name: "other keyword is an error", sql: "DROP table", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)
			parserInstance := newParser(tokens)

			err = newParserContext(parserInstance, New()).ExpectKeyword("CREATE")

			if !testCase.wantErr {
				require.NoError(t, err)
				assert.Equal(t, 1, parserInstance.Position)
				return
			}
			require.Error(t, err)
			assert.Same(t, err, parserInstance.syntax.err, "the mismatch fails the statement even if the extension drops it")
		})
	}
}

func TestParserContextCapturesSourceTextAsWritten(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(capturingExtension{}))
	sql := `CAPTURE "Device Id"   =  'it''s',  E'a\nb', $tag$ x $$ y $tag$, "select"`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)

	assert.Equal(t, "Device Id", mutation.TableName, "Tokens returns a copy the caller cannot use to change the parser")
	assert.Equal(t, "true", mutation.EngineSpecific["QUOTED"])
	assert.Equal(t, `"Device Id"`, mutation.EngineSpecific["RAW"])
	assert.Equal(t, `"Device Id"   =`, mutation.EngineSpecific["HEAD"])
	assert.Equal(t, `"Device Id"   =  'it''s',  E'a\nb', $tag$ x $$ y $tag$, "select"`, mutation.EngineSpecific["BODY"])
}

func TestParserContextKeywordMatchingRejectsQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	engine := New(WithStatementExtensions(foobarExtension{}))
	statements, err := engine.ParseStatements(t.Context(), `CREATE FOOBAR "TABLE"`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "TABLE", mutation.TableName)

	statements, err = engine.ParseStatements(t.Context(), `CREATE "FOOBAR" widgets`)
	require.NoError(t, err)
	raw, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok)
	assert.NotEqual(t, statementKindFoobar, raw.kind, "a quoted word is a name, not the extension's keyword")
}

func TestParseTableElementsReadsTheListAsCreateTableDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		elements string
	}{
		{name: "serial and identity columns", elements: "(id serial, big bigserial, ident bigint GENERATED BY DEFAULT AS IDENTITY (START WITH 10), always int GENERATED ALWAYS AS IDENTITY)"},
		{name: "a stored generated column", elements: "(price numeric, tax numeric GENERATED ALWAYS AS (price * 0.2) STORED NOT NULL)"},
		{name: "defaults followed by constraints", elements: "(ts timestamptz DEFAULT now() NOT NULL, n int DEFAULT (1 + 2) CHECK (n > 0) NOT NULL, label text DEFAULT NULL)"},
		{name: "foreign keys with their actions", elements: "(owner int REFERENCES users (id) MATCH FULL ON DELETE SET NULL (owner) ON UPDATE CASCADE DEFERRABLE INITIALLY DEFERRED NOT NULL, other int REFERENCES things ON DELETE NO ACTION)"},
		{name: "table constraints", elements: "(a int, b int, CONSTRAINT pk PRIMARY KEY (a, b) INCLUDE (c) WITH (fillfactor = 70), UNIQUE NULLS NOT DISTINCT (b), CHECK (a < b) NO INHERIT, FOREIGN KEY (b) REFERENCES other (id) ON DELETE RESTRICT, EXCLUDE USING gist (a WITH =))"},
		{name: "a LIKE element including everything", elements: "(LIKE parent INCLUDING ALL, extra text)"},
		{name: "a key over a column the list does not declare", elements: "(LIKE parent, PRIMARY KEY (id))"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			want := applyDDL(t, "CREATE TABLE analytics.readings "+testCase.elements)
			parserContext := contextOver(t, "SELECT "+testCase.elements)
			parserContext.Advance()

			got, err := parserContext.ParseTableElements("analytics", "readings")

			require.NoError(t, err)
			assert.True(t, parserContext.AtEnd())
			assert.Equal(t, want, got)
		})
	}
}

func TestParseTableElementsFailsTheStatementOnAMalformedList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		elements    string
		wantMessage string
	}{
		{name: "no opening parenthesis", elements: "id int", wantMessage: "expected '(' to open the column list"},
		{name: "a list cut short", elements: "(id int, name", wantMessage: "expected ',' or ')' after the table element"},
		{name: "an unbalanced check", elements: "(id int CHECK ((id > 0)", wantMessage: "unmatched parenthesis"},
		{name: "a generated column without its expression", elements: "(id int GENERATED ALWAYS AS)", wantMessage: "expected '('"},
		{name: "a foreign key action that is no action", elements: "(id int REFERENCES t ON DELETE EXPLODE)", wantMessage: "expected a referential action"},
		{name: "a primary key without its key word", elements: "(id int, PRIMARY (id))", wantMessage: "expected ',' or ')' after the table element"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, "SELECT "+testCase.elements)
			parserContext.Advance()

			mutation, err := parserContext.ParseTableElements("", "readings")

			require.Nil(t, mutation)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
			assert.Equal(t, err, parserContext.p.syntax.err)
		})
	}
}

func TestSkipParenthesisedConsumesTheWholeGroup(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantErr     string
		wantCurrent string
	}{
		{name: "a flat group", sql: "SELECT (a, b) rest", wantCurrent: "rest"},
		{name: "nested groups", sql: "SELECT (a, (b, (c)), ')') rest", wantCurrent: "rest"},
		{name: "nesting deeper than any parse depth", sql: "SELECT " + strings.Repeat("(", 5_000) + strings.Repeat(")", 5_000) + " rest", wantCurrent: "rest"},
		{name: "no opening parenthesis", sql: "SELECT rest", wantErr: "expected '('"},
		{name: "a group that is never closed", sql: "SELECT (a, (b)", wantErr: "unmatched parenthesis"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)
			parserContext.Advance()

			err := parserContext.SkipParenthesised()

			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.Equal(t, err, parserContext.p.syntax.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantCurrent, parserContext.CurrentToken().Value)
		})
	}
}

func TestCaptureUntilBoundaryStopsAtTheListsOwnCommaOrParenthesis(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantErr     string
		wantText    string
		wantCurrent pgext.TokenKind
	}{
		{name: "up to a comma", sql: "SELECT INTERVAL  '1 day', next", wantText: "INTERVAL  '1 day'", wantCurrent: pgext.TokenComma},
		{name: "up to a closing parenthesis", sql: "SELECT now() - x) rest", wantText: "now() - x", wantCurrent: pgext.TokenRightParen},
		{name: "past the commas of a nested call", sql: "SELECT f(a, g(b, c)), next", wantText: "f(a, g(b, c))", wantCurrent: pgext.TokenComma},
		{name: "to the end of the statement", sql: `SELECT E'a\nb' || "Quoted"`, wantText: `E'a\nb' || "Quoted"`, wantCurrent: pgext.TokenEOF},
		{name: "nothing at a boundary", sql: "SELECT , next", wantText: "", wantCurrent: pgext.TokenComma},
		{name: "a group that is never closed", sql: "SELECT f(a, (b", wantErr: "unmatched parenthesis"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parserContext := contextOver(t, testCase.sql)
			parserContext.Advance()

			text, err := parserContext.CaptureUntilBoundary()

			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.Equal(t, err, parserContext.p.syntax.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantText, text)
			assert.Equal(t, testCase.wantCurrent, parserContext.CurrentToken().Kind)
		})
	}
}
