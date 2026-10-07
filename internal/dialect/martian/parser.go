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

package martian

import (
	"context"
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// maximumExpressionDepth bounds expression nesting so hostile input cannot exhaust the
	// stack.
	maximumExpressionDepth = 200

	// keywordAs introduces an alias.
	keywordAs = "AS"

	// keywordFrom introduces the table a SELECT or DELETE reads.
	keywordFrom = "FROM"

	// keywordWhere introduces a filter.
	keywordWhere = "WHERE"

	// keywordNull is the null literal.
	keywordNull = "NULL"

	// keywordNot negates IS NULL and NOT NULL.
	keywordNot = "NOT"

	// keywordPrimary opens PRIMARY KEY.
	keywordPrimary = "PRIMARY"

	// keywordKey closes PRIMARY KEY.
	keywordKey = "KEY"

	// keywordConvert opens CONVERT(value TO type), Martian's conversion.
	keywordConvert = "CONVERT"

	// keywordTo separates the converted value from its type in CONVERT.
	keywordTo = "TO"

	// symbolComma separates list items.
	symbolComma = ","

	// symbolOpen opens a parenthesised group.
	symbolOpen = "("

	// symbolClose closes a parenthesised group.
	symbolClose = ")"

	// symbolTerminator ends a statement.
	symbolTerminator = ";"
)

// reservedWords lists the keywords that end a clause, so they are never taken as an
// implicit alias.
var reservedWords = []string{
	keywordAs, keywordFrom, keywordWhere, "ORDER", "BY", "SET", "VALUES", "AND", "OR",
	keywordNot, "IS", "IN", keywordNull, "ASC", "DESC",
}

// parser builds Martian statements from tokens.
type parser struct {
	// input is the text the tokens were read from; their offsets index into it.
	input string

	// tokens holds the tokens of the whole input, ending with tokenEnd.
	tokens []token

	// position is the index of the current token.
	position int

	// depth is the current expression nesting.
	depth int
}

// current returns the current token.
//
// Returns token which is the token at the current position.
func (p *parser) current() token {
	return p.tokens[p.position]
}

// next returns the token after the current one, or the current one at the end.
//
// Returns token which is the following token.
func (p *parser) next() token {
	if p.position+1 < len(p.tokens) {
		return p.tokens[p.position+1]
	}
	return p.tokens[p.position]
}

// previousEnd returns the offset one past the last byte of the token before the current
// one.
//
// Returns int which is the offset, or zero at the start of the input.
func (p *parser) previousEnd() int {
	if p.position == 0 {
		return 0
	}
	return p.tokens[p.position-1].end
}

// spanFrom returns the span from an offset to the end of the token before the current
// one.
//
// Takes start (int) which is the offset the span starts at.
//
// Returns ast.Span which covers the tokens read since.
func (p *parser) spanFrom(start int) ast.Span {
	return ast.Span{Start: start, End: p.previousEnd()}
}

// advance moves past the current token and returns it.
//
// Returns token which is the token moved past.
func (p *parser) advance() token {
	current := p.tokens[p.position]
	if current.kind != tokenEnd {
		p.position++
	}
	return current
}

// acceptKeyword moves past the current token when it is the given keyword.
//
// Takes keyword (string) which is the upper-case keyword.
//
// Returns bool which is true when the keyword was there.
func (p *parser) acceptKeyword(keyword string) bool {
	if p.current().isKeyword(keyword) {
		p.advance()
		return true
	}
	return false
}

// acceptSymbol moves past the current token when it is the given symbol.
//
// Takes symbol (string) which is the symbol.
//
// Returns bool which is true when the symbol was there.
func (p *parser) acceptSymbol(symbol string) bool {
	if p.current().isSymbol(symbol) {
		p.advance()
		return true
	}
	return false
}

// expectKeyword moves past the given keyword.
//
// Takes keyword (string) which is the upper-case keyword.
//
// Returns error when the current token is not the keyword.
func (p *parser) expectKeyword(keyword string) error {
	if !p.acceptKeyword(keyword) {
		return p.unexpected(keyword)
	}
	return nil
}

// expectSymbol moves past the given symbol.
//
// Takes symbol (string) which is the symbol.
//
// Returns error when the current token is not the symbol.
func (p *parser) expectSymbol(symbol string) error {
	if !p.acceptSymbol(symbol) {
		return p.unexpected(symbol)
	}
	return nil
}

// expectName reads an identifier, unquoted or delimited.
//
// Returns string which is the folded or delimited name.
// Returns error when the current token is not a name.
func (p *parser) expectName() (string, error) {
	if !p.current().isName() {
		return "", p.unexpected("a name")
	}
	return p.advance().value, nil
}

// unexpected builds the error for a token the grammar does not allow.
//
// Takes wanted (string) which describes what the grammar expected.
//
// Returns error which names the offending token and says where it is.
func (p *parser) unexpected(wanted string) error {
	current := p.current()
	if current.kind == tokenEnd {
		return newSyntaxError(p.input, current.offset, current.end, "expected "+wanted+" but the input ended")
	}
	return newSyntaxError(p.input, current.offset, current.end, "expected "+wanted)
}

// parseStatement parses one statement, dispatching on its leading keyword.
//
// Returns *statement which is the parsed statement.
// Returns error when the statement is not valid Martian.
func (p *parser) parseStatement() (*statement, error) {
	leading := p.current()
	switch {
	case leading.isKeyword("CREATE"):
		created, err := p.parseCreateTable()
		return &statement{source: "", tokens: nil, createTable: created, selection: nil, insertion: nil, update: nil, deletion: nil}, err
	case leading.isKeyword("SELECT"):
		selected, err := p.parseSelect()
		return &statement{source: "", tokens: nil, createTable: nil, selection: selected, insertion: nil, update: nil, deletion: nil}, err
	case leading.isKeyword("INSERT"):
		inserted, err := p.parseInsert()
		return &statement{source: "", tokens: nil, createTable: nil, selection: nil, insertion: inserted, update: nil, deletion: nil}, err
	case leading.isKeyword("UPDATE"):
		updated, err := p.parseUpdate()
		return &statement{source: "", tokens: nil, createTable: nil, selection: nil, insertion: nil, update: updated, deletion: nil}, err
	case leading.isKeyword("DELETE"):
		deleted, err := p.parseDelete()
		return &statement{source: "", tokens: nil, createTable: nil, selection: nil, insertion: nil, update: nil, deletion: deleted}, err
	default:
		return nil, p.unexpected("CREATE, SELECT, INSERT, UPDATE or DELETE")
	}
}

// parseQualifiedName reads a name with an optional schema, such as dbo.ITEMS.
//
// Returns qualifiedName which is the parsed name.
// Returns error when the name is malformed.
func (p *parser) parseQualifiedName() (qualifiedName, error) {
	first, err := p.expectName()
	if err != nil {
		return qualifiedName{}, err
	}
	if !p.acceptSymbol(".") {
		return qualifiedName{schema: "", name: first}, nil
	}
	second, err := p.expectName()
	if err != nil {
		return qualifiedName{}, err
	}
	return qualifiedName{schema: first, name: second}, nil
}

// parseTableReference reads a table name and an optional alias.
//
// Returns tableReference which is the parsed reference.
// Returns error when the reference is malformed.
func (p *parser) parseTableReference() (tableReference, error) {
	table, err := p.parseQualifiedName()
	if err != nil {
		return tableReference{}, err
	}
	alias, err := p.parseOptionalAlias()
	if err != nil {
		return tableReference{}, err
	}
	return tableReference{call: nil, table: table, alias: alias}, nil
}

// parseFromItem reads the item a SELECT reads from: a function call, such as UPPER('a'),
// or a table, each with an optional alias.
//
// Returns tableReference which is the parsed item.
// Returns error when the item is malformed.
func (p *parser) parseFromItem() (tableReference, error) {
	if p.current().kind != tokenIdentifier || !p.next().isSymbol(symbolOpen) {
		return p.parseTableReference()
	}
	parsed, err := p.parseFunctionArguments(p.advance())
	if err != nil {
		return tableReference{}, err
	}
	call, isCall := parsed.(*functionExpression)
	if !isCall {
		return tableReference{}, p.unexpected("a function call")
	}
	alias, err := p.parseOptionalAlias()
	if err != nil {
		return tableReference{}, err
	}
	return tableReference{call: call, table: qualifiedName{schema: "", name: ""}, alias: alias}, nil
}

// parseOptionalAlias reads an AS alias or an implicit alias.
//
// Returns string which is the alias, or empty when there is none.
// Returns error when AS is not followed by a name.
func (p *parser) parseOptionalAlias() (string, error) {
	if p.acceptKeyword(keywordAs) {
		return p.expectName()
	}
	current := p.current()
	if current.kind == tokenQuotedIdentifier || (current.kind == tokenIdentifier && !slices.Contains(reservedWords, current.value)) {
		return p.advance().value, nil
	}
	return "", nil
}

// parseNameList reads a parenthesised, comma-separated list of names.
//
// Returns []string which holds the names.
// Returns error when the list is malformed.
func (p *parser) parseNameList() ([]string, error) {
	if err := p.expectSymbol(symbolOpen); err != nil {
		return nil, err
	}
	var names []string
	for {
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	return names, p.expectSymbol(symbolClose)
}

// parseIntegerList reads a parenthesised, comma-separated list of integers, such as a
// type's modifiers.
//
// Returns []int which holds the integers.
// Returns error when the list is malformed.
func (p *parser) parseIntegerList() ([]int, error) {
	if err := p.expectSymbol(symbolOpen); err != nil {
		return nil, err
	}
	var values []int
	for {
		if p.current().kind != tokenNumber {
			return nil, p.unexpected("a number")
		}
		modifier := p.advance()
		value, err := strconv.Atoi(modifier.value)
		if err != nil {
			return nil, newSyntaxError(p.input, modifier.offset, modifier.end, "type modifier: "+err.Error())
		}
		values = append(values, value)
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	return values, p.expectSymbol(symbolClose)
}

// parseStatements parses every statement of a Martian input, stopping before the next
// statement once the context is done.
//
// Takes input (string) which is the SQL text.
//
// Returns []ast.ParsedStatement which holds one entry per statement.
// Returns error which is the context's cause once the context is done, or reports input
// that is not valid Martian.
func parseStatements(ctx context.Context, input string) ([]ast.ParsedStatement, error) {
	tokens, err := tokenise(input)
	if err != nil {
		return nil, err
	}
	state := &parser{input: input, tokens: tokens, position: 0, depth: 0}
	var statements []ast.ParsedStatement
	for state.current().kind != tokenEnd {
		if state.current().isSymbol(symbolTerminator) {
			state.advance()
			continue
		}
		if cause := context.Cause(ctx); cause != nil {
			return nil, cause
		}
		first := state.position
		start := state.current().offset
		parsed, parseError := state.parseStatement()
		if parseError != nil {
			return nil, parseError
		}
		if !state.current().isSymbol(symbolTerminator) && state.current().kind != tokenEnd {
			return nil, state.unexpected("the end of the statement")
		}
		parsed.source = input
		parsed.tokens = tokens[first:state.position:state.position]
		statements = append(statements, ast.ParsedStatement{
			Raw:      parsed,
			Location: start,
			Length:   state.previousEnd() - start,
		})
	}
	return statements, nil
}
