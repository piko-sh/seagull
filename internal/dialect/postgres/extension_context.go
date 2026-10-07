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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parserContext is the concrete pgext.ParserContext implementation; it wraps a *parser
// and exposes only the methods declared on the interface.
type parserContext struct {
	// p is the wrapped parser whose cursor the context drives.
	p *parser

	// engine carries the Dialect so extensions can reuse its type normalisation.
	engine *Dialect
}

// newParserContext wraps a *parser for handing to extension code.
//
// The wrapper is single-use; do not retain references across the ApplyDDL boundary. The
// engine is carried so extensions can reuse the engine's type normalisation
// (ParseColumnType) instead of reimplementing it.
//
// Takes p (*parser) which is the parser to wrap.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns *parserContext which is the single-use wrapper.
func newParserContext(p *parser, engine *Dialect) *parserContext {
	return &parserContext{p: p, engine: engine}
}

// ParseColumnType parses and normalises the column type at the cursor, consuming the type
// tokens. See the ParserContext interface for details.
//
// Returns sqlmodel.SQLType which is the normalised column type.
// Returns int which is the array dimension count, 0 for a non-array type.
func (c *parserContext) ParseColumnType() (sqlmodel.SQLType, int) {
	return c.p.parseColumnType(c.engine)
}

// ExpectKeyword consumes the next token as the named keyword, recording a syntax error
// when it does not match.
//
// Takes name (string) which is the required keyword.
//
// Returns error when the token does not match the keyword.
func (c *parserContext) ExpectKeyword(name string) error { return c.p.requireKeyword(name) }

// MatchKeyword consumes the current token when it matches the named keyword and returns
// whether a match occurred.
//
// Takes name (string) which is the keyword to match.
//
// Returns bool which is true when the token was consumed.
func (c *parserContext) MatchKeyword(name string) bool { return c.p.MatchKeyword(name) }

// MatchIfNotExists consumes a trailing `IF NOT EXISTS` clause if present.
//
// The NOT token is required; callers that need `IF EXISTS` semantics must use
// MatchIfExists.
//
// Returns bool which is true when the clause was consumed.
func (c *parserContext) MatchIfNotExists() bool {
	startPosition := c.p.Position
	if !c.p.MatchKeyword("IF") {
		return false
	}
	if !c.p.MatchKeyword("NOT") {
		c.p.Position = startPosition
		return false
	}
	if !c.p.MatchKeyword("EXISTS") {
		c.p.Position = startPosition
		return false
	}
	return true
}

// MatchIfExists consumes a trailing `IF EXISTS` clause if present.
//
// A partial match (IF present but EXISTS missing) restores the cursor to the position
// before IF so subsequent matchers see the original token stream; without the restore an
// `IF NOT EXISTS` lookahead would leave the cursor on NOT and break later keyword
// matching.
//
// Returns bool which is true when the clause was consumed.
func (c *parserContext) MatchIfExists() bool {
	startPosition := c.p.Position
	if !c.p.MatchKeyword("IF") {
		return false
	}
	if !c.p.MatchKeyword("EXISTS") {
		c.p.Position = startPosition
		return false
	}
	return true
}

// CurrentToken returns the token at the cursor without consuming it.
//
// Returns pgext.Token which is the cursor token, or an EOF sentinel past the end.
func (c *parserContext) CurrentToken() pgext.Token { return c.p.Current() }

// Peek returns the token one past the cursor without consuming it.
//
// Returns pgext.Token which is the lookahead token, or an EOF sentinel out of range.
func (c *parserContext) Peek() pgext.Token { return c.p.Peek() }

// Advance consumes and returns the current token.
//
// Returns pgext.Token which is the consumed token.
func (c *parserContext) Advance() pgext.Token { return c.p.Advance() }

// AtEnd reports whether the cursor has reached end of input.
//
// Returns bool which is true when the cursor is at end of input.
func (c *parserContext) AtEnd() bool { return c.p.AtEnd() }

// ParseQualifiedName parses an optionally schema-qualified name.
//
// Returns schema (string) which is the qualifying schema, empty when absent.
// Returns name (string) which is the object name.
// Returns err (error) when the name cannot be parsed.
func (c *parserContext) ParseQualifiedName() (schema string, name string, err error) {
	return c.p.parseSchemaQualifiedName()
}

// ParseColumnList parses a `(col1, col2, ...)` identifier list.
//
// It accepts bare and double-quoted identifiers, both emitted as TokenIdentifier, as well
// as TokenString values.
//
// Returns []string which is the parsed column names.
// Returns error when the opening or closing paren is missing.
func (c *parserContext) ParseColumnList() ([]string, error) {
	if c.p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(c.p.Current(), "expected '('")
	}
	c.p.Advance()
	var names []string
	for !c.p.AtEnd() {
		current := c.p.Current()
		if current.Kind != tokenIdentifier && current.Kind != tokenString {
			break
		}
		names = append(names, current.Value)
		c.p.Advance()
		if c.p.Current().Kind == tokenComma {
			c.p.Advance()
			continue
		}
		break
	}
	if c.p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(c.p.Current(), "expected ')'")
	}
	c.p.Advance()
	return names, nil
}

// ParseReloptionList parses `( key [= value], ... )` and returns the captured key-value
// pairs.
//
// Values that are single-quoted strings have their quotes stripped; other values are
// captured as their source text. A key written without `= value`, such as
// `timescaledb.continuous`, is a boolean option set to true, as in PostgreSQL itself. A
// key written twice keeps its last value.
//
// Returns map[string]string which is the captured key-value pairs.
// Returns error when the body is malformed or a value cannot be parsed.
func (c *parserContext) ParseReloptionList() (map[string]string, error) {
	options, err := c.p.parseReloptions()
	if err != nil {
		return nil, err
	}
	pairs := make(map[string]string, len(options))
	for _, option := range options {
		pairs[option.key] = option.value
	}
	return pairs, nil
}

// ParseTableElements parses the parenthesised column and constraint list at the cursor
// exactly as CREATE TABLE does and returns the table it declares. See the ParserContext
// interface for details.
//
// Takes schema (string) which is the table's schema, empty when the name has none.
// Takes name (string) which is the table's name.
//
// Returns *ast.CatalogueMutation which creates the table the list declares.
// Returns error when the list is missing or malformed, recorded as the statement's.
func (c *parserContext) ParseTableElements(schema, name string) (*ast.CatalogueMutation, error) {
	body, err := c.p.parseCreateTableElements(c.engine)
	if err != nil {
		return nil, c.p.recordSyntaxError(err)
	}
	return newCreateTableMutation(schema, name, &body), nil
}

// SkipParenthesised consumes the parenthesised group at the cursor, nested groups
// included.
//
// Returns error when the cursor is not on '(' or the group is not closed, recorded as the
// statement's.
func (c *parserContext) SkipParenthesised() error {
	return c.p.requireSkipParenthesised()
}

// CaptureUntilBoundary consumes the tokens up to the next comma or closing parenthesis
// outside the groups they open, or the end of the statement, and returns their source
// text exactly as written.
//
// Returns string which is the captured text, empty at a boundary.
// Returns error when a group the tokens open is not closed, recorded as the statement's.
func (c *parserContext) CaptureUntilBoundary() (string, error) {
	start := c.p.Position
	if err := c.p.skipToListElementEnd(); err != nil {
		return "", err
	}
	return c.p.sourceText(start, c.p.Position), nil
}

// ConsumeRemainder advances the cursor to end of input in O(1) by jumping straight to the
// end of the token slice. Used by extensions that capture the rest of the statement
// opaquely after their own header parsing has completed.
func (c *parserContext) ConsumeRemainder() {
	c.p.Position = len(c.p.Tokens)
}

// ConsumeRemainderAsText returns the source text from the cursor to the end of the
// statement, exactly as written, and advances to the end.
//
// Returns string which is the remaining source text.
func (c *parserContext) ConsumeRemainderAsText() string {
	text := c.p.sourceText(c.p.Position, c.p.statementEnd())
	c.ConsumeRemainder()
	return text
}

// Tokens returns a copy of the statement's tokens, so a hook can inspect the statement's
// shape after the built-in handler has consumed it without being able to change what the
// parser reads.
//
// Returns []pgext.Token which is a copy of the statement's tokens.
func (c *parserContext) Tokens() []pgext.Token {
	return slices.Clone(c.p.Tokens)
}

// AnalyseViewBody analyses the query body at the cursor without consuming any tokens.
//
// It delegates to the same view-body analyser the built-in CREATE VIEW handler uses, so
// an extension's `AS SELECT` body is typed identically to a plain view: a body it does
// not analyse is reported on the statement's mutation, and a malformed one fails the
// statement.
//
// Takes columnNames ([]string) which optionally overlay the inferred projection names.
//
// Returns *ast.RawQueryAnalysis which is the analysed body, or nil when it was not
// analysed.
// Returns error when the body is missing or malformed, or its analysis stops.
func (c *parserContext) AnalyseViewBody(columnNames []string) (*ast.RawQueryAnalysis, error) {
	return c.p.analyseViewBody(columnNames)
}

// EngineSpecificFromTokens returns the source text spanning the tokens from startIndex up
// to but excluding endIndex, exactly as written.
//
// Takes startIndex (int) which is the first token index, inclusive.
// Takes endIndex (int) which is the index one past the last token.
//
// Returns string which is the source text, empty when the range is invalid.
func (c *parserContext) EngineSpecificFromTokens(startIndex, endIndex int) string {
	if startIndex < 0 || endIndex > len(c.p.Tokens) || startIndex >= endIndex {
		return ""
	}
	return c.p.sourceText(startIndex, endIndex)
}

// adjustParenthesisDepth updates a paren nesting counter for one token, as used by the
// parenthesised-group collector.
//
// Other token kinds leave the depth untouched. It is extracted so the surrounding loop
// does not need a switch with side-effecting cases and can keep its body shallow.
//
// Takes kind (tokenKind) which is the cursor token's classification.
// Takes depth (int) which is the current paren nesting depth.
//
// Returns int which is the updated depth.
func adjustParenthesisDepth(kind tokenKind, depth int) int {
	switch kind {
	case tokenLeftParen:
		return depth + 1
	case tokenRightParen:
		return depth - 1
	default:
		return depth
	}
}
