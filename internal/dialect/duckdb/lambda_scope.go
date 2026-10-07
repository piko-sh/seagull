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

package duckdb

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// maxLambdaHeadTokens limits the tokens in a lambda's head, counting names and the commas
// between them.
//
// A longer list before an arrow or a colon is not read as a lambda head. Asking whether a
// lambda declares a name therefore reads at most this many tokens for each lambda around
// it.
const maxLambdaHeadTokens = 64

// lambdaPresence records whether a statement holds anything that may declare a lambda or
// list comprehension name, once that is known.
type lambdaPresence uint8

const (
	// lambdaPresenceUnknown is a statement not looked at yet.
	lambdaPresenceUnknown lambdaPresence = iota

	// lambdaPresenceAbsent is a statement with no arrow, colon or FOR, so no name in it is a
	// lambda's.
	lambdaPresenceAbsent

	// lambdaPresencePossible is a statement holding an arrow, a colon or a FOR, which may
	// head a lambda or a list comprehension.
	lambdaPresencePossible
)

// lambdaDeclaration is one place in a statement that declares names over part of it: a
// lambda's head, whose parameters are bound over the rest of the argument it heads, or a
// list comprehension, whose variable is bound over its element and condition.
type lambdaDeclaration struct {
	// outer is the declaration visible where this one is made, or nil.
	outer *lambdaDeclaration

	// comprehension locates the parts of a list comprehension, read in full once its closing
	// bracket is reached; its list is -1 for a lambda or for a bracket group that is no
	// comprehension.
	comprehension comprehension

	// first is the index of the first token of a lambda's head that may name a parameter.
	first int

	// end is one past the index of the last such token.
	end int
}

// lambdaScopeLevel is one bracket group the build of a lambdaScopeIndex is inside: what
// was visible where it opened, and where its current argument started.
type lambdaScopeLevel struct {
	// opened is what was visible where the group opened, restored when it closes.
	opened *lambdaDeclaration

	// argument is what was visible where the group's current argument started, restored at
	// each comma.
	argument *lambdaDeclaration

	// comprehension is the declaration a square bracket group makes, read as the group's
	// words are reached; nil for any other group.
	comprehension *lambdaDeclaration
}

// lambdaScopeIndex answers which lambdas and list comprehensions declare a name at each
// token of a parser's tokens, built in one pass over them.
type lambdaScopeIndex struct {
	// visible holds, for each token index and for the end of the tokens, the innermost
	// declaration visible there, each linking to the next one out.
	visible []*lambdaDeclaration
}

// comprehension locates the parts of a list comprehension, [element FOR name IN list IF
// condition], by the indexes of its tokens.
type comprehension struct {
	// variable is the name the comprehension declares.
	variable string

	// list is the index of its IN keyword.
	list int

	// listEnd is the index of its IF keyword, or of its closing bracket when it has no
	// condition.
	listEnd int
}

// lambdaBindsReference reports whether a reference belongs to a lambda or comprehension
// binding.
//
// Bindings take precedence over columns, including qualified field accesses.
//
// Takes position (int) which is the index of a token inside the reference's expression.
// Takes reference (*ast.ColumnReference) which is the reference; nil names nothing.
//
// Returns bool which is true when a lambda binds the reference's name.
func (p *parser) lambdaBindsReference(position int, reference *ast.ColumnReference) bool {
	if reference == nil {
		return false
	}
	name := reference.ColumnName
	if reference.TableAlias != "" {
		name = reference.TableAlias
	}
	return p.lambdaDeclares(position, name)
}

// insideLambdaBody reports whether a position lies in the body of a lambda, or in the
// element or condition of a list comprehension, which stands for a value computed per
// element rather than for the value of the expression around the lambda.
//
// Takes position (int) which is the index of the token asked about.
//
// Returns bool which is true when a lambda's body holds it.
func (p *parser) insideLambdaBody(position int) bool {
	return p.lambdaDeclares(position, "")
}

// lambdaDeclares reports whether a lambda heading the argument that holds a position, in
// any group around it, or a list comprehension around it, declares a name.
//
// A statement holding no arrow, colon or FOR answers at once; otherwise the parser's
// lambdaScopeIndex is built on first use, so each answer walks only the declarations
// visible at the position. Each declaration visible there nests the position one level
// deeper, so a position with more of them than the parse depth limit, which the parse
// refuses anyway, is answered as bound after that many.
//
// Takes position (int) which is the index of the token asked about.
// Takes name (string) which is the name, or empty to ask about any name.
//
// Returns bool which is true when one does.
func (p *parser) lambdaDeclares(position int, name string) bool {
	if position <= 0 || !p.statement.mayHoldLambdas() {
		return false
	}
	if p.lambdaScopes == nil {
		p.lambdaScopes = p.buildLambdaScopeIndex()
	}
	position = min(position, len(p.Tokens))
	walked := 0
	for declaration := p.lambdaScopes.visible[position]; declaration != nil; declaration = declaration.outer {
		if walked >= p.maxParseDepth || p.declarationBinds(declaration, position, name) {
			return true
		}
		walked++
	}
	return false
}

// declarationBinds reports whether a declaration binds a name at a position: a list
// comprehension whose variable it is, when the position is not in the comprehension's
// list, or a lambda's head that names it.
//
// Takes declaration (*lambdaDeclaration) which is the declaration.
// Takes position (int) which is the index of the token asked about.
// Takes name (string) which is the name, or empty to ask about any name.
//
// Returns bool which is true when it does.
func (p *parser) declarationBinds(declaration *lambdaDeclaration, position int, name string) bool {
	if found := declaration.comprehension; found.list >= 0 {
		return declares(found.variable, name) && (position < found.list || position > found.listEnd)
	}
	for index := declaration.first; index < declaration.end; index++ {
		if p.Tokens[index].Kind == tokenIdentifier && declares(p.Tokens[index].Value, name) {
			return true
		}
	}
	return false
}

// buildLambdaScopeIndex records, in one pass over the parser's tokens, the declarations
// visible at each token.
//
// Walking forwards, it keeps what is visible: a lambda's head adds its declaration for
// the rest of the argument it heads, a comma drops the declarations its argument made, an
// opening bracket starts a group, which a square bracket makes a candidate list
// comprehension, and a closing bracket restores what was visible where its group opened.
// A closing bracket that opens no group hides everything before it.
//
// Returns *lambdaScopeIndex which answers lambdaDeclares.
func (p *parser) buildLambdaScopeIndex() *lambdaScopeIndex {
	index := &lambdaScopeIndex{visible: make([]*lambdaDeclaration, len(p.Tokens)+1)}
	levels := []lambdaScopeLevel{{opened: nil, argument: nil, comprehension: nil}}
	var current *lambdaDeclaration
	for position := range p.Tokens {
		index.visible[position] = current
		level := &levels[len(levels)-1]
		switch p.Tokens[position].Kind {
		case tokenLeftParen, tokenLeftBrace:
			levels = append(levels, lambdaScopeLevel{opened: current, argument: current, comprehension: nil})
		case tokenLeftBracket:
			candidate := &lambdaDeclaration{outer: current, comprehension: noComprehension(), first: 0, end: 0}
			levels = append(levels, lambdaScopeLevel{opened: current, argument: candidate, comprehension: candidate})
			current = candidate
		case tokenRightParen, tokenRightBracket, tokenRightBrace:
			current = p.closeLambdaScopeLevel(&levels, position)
		case tokenComma:
			current = level.argument
		case tokenIdentifier:
			if level.comprehension != nil {
				level.comprehension.comprehension = p.noteComprehensionKeyword(level.comprehension.comprehension, position)
			}
		default:
			if first, end, heads := p.lambdaHead(position); heads {
				current = &lambdaDeclaration{outer: current, comprehension: noComprehension(), first: first, end: end}
			}
		}
	}
	index.visible[len(p.Tokens)] = current
	for _, unclosed := range levels[1:] {
		if unclosed.comprehension != nil {
			unclosed.comprehension.comprehension = noComprehension()
		}
	}
	return index
}

// closeLambdaScopeLevel leaves the group a closing bracket closes, finishing reading it
// as a list comprehension when it is a square bracket group, and returns what was visible
// where the group opened. A closing bracket that closes no group hides everything before
// it.
//
// Takes levels (*[]lambdaScopeLevel) which holds the open groups, outermost first, the
// statement's own level at the bottom.
// Takes closing (int) which is the index of the closing bracket.
//
// Returns *lambdaDeclaration which is what is visible after it.
func (p *parser) closeLambdaScopeLevel(levels *[]lambdaScopeLevel, closing int) *lambdaDeclaration {
	if len(*levels) == 1 {
		(*levels)[0].argument = nil
		return nil
	}
	closed := (*levels)[len(*levels)-1]
	*levels = (*levels)[:len(*levels)-1]
	if closed.comprehension != nil {
		found, read := p.closeComprehension(closed.comprehension.comprehension, closing)
		if !read {
			found = noComprehension()
		}
		closed.comprehension.comprehension = found
	}
	return closed.opened
}

// lambdaHead reports whether the token at an index ends the head of a lambda: the arrow
// of x -> or (x, y) -> written where a call's argument starts, or the colon of LAMBDA x,
// y:.
//
// Takes index (int) which is the index of the token.
//
// Returns first (int) which is the index of the head's first token that may name a
// parameter.
// Returns end (int) which is one past the index of its last.
// Returns heads (bool) which is true when the token ends a lambda's head.
func (p *parser) lambdaHead(index int) (first int, end int, heads bool) {
	current := p.Tokens[index]
	switch {
	case current.Kind == tokenArrow:
		return p.arrowLambdaHead(index)
	case current.Kind == tokenOperator && current.Value == operatorColon:
		return p.keywordLambdaHead(index)
	default:
		return 0, 0, false
	}
}

// arrowLambdaHead reads the head of a lambda an arrow ends, x -> or (x, y) ->, written
// where a call's argument starts, holding at most maxLambdaHeadTokens tokens.
//
// Takes arrow (int) which is the index of the arrow.
//
// Returns first (int) which is the index of the head's first name.
// Returns end (int) which is one past the index of its last.
// Returns heads (bool) which is true when the arrow ends such a head.
func (p *parser) arrowLambdaHead(arrow int) (first int, end int, heads bool) {
	if arrow < 1 {
		return 0, 0, false
	}
	if p.Tokens[arrow-1].Kind == tokenIdentifier {
		return arrow - 1, arrow, p.startsArgument(arrow - 1)
	}
	if p.Tokens[arrow-1].Kind != tokenRightParen {
		return 0, 0, false
	}
	index := arrow - 2
	for ; index >= 0 && p.Tokens[index].Kind != tokenLeftParen; index-- {
		if kind := p.Tokens[index].Kind; kind != tokenIdentifier && kind != tokenComma || arrow-1-index > maxLambdaHeadTokens {
			return 0, 0, false
		}
	}
	return index + 1, arrow - 1, index >= 0 && p.startsArgument(index)
}

// keywordLambdaHead reads the head of a lambda written with the LAMBDA keyword, LAMBDA x,
// y:, that a colon ends, holding at most maxLambdaHeadTokens tokens.
//
// Takes colon (int) which is the index of the colon.
//
// Returns first (int) which is the index of the head's first name.
// Returns end (int) which is one past the index of its last.
// Returns heads (bool) which is true when the colon ends such a head holding at least one
// token.
func (p *parser) keywordLambdaHead(colon int) (first int, end int, heads bool) {
	for index := colon - 1; index >= 0; index-- {
		current := p.Tokens[index]
		switch {
		case current.IsKeyword(tokenIdentifier, keywordLAMBDA):
			return index + 1, colon, index < colon-1
		case current.Kind != tokenIdentifier && current.Kind != tokenComma, colon-index > maxLambdaHeadTokens:
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// startsArgument reports whether the token at an index starts an argument of a call: it
// follows the call's opening parenthesis or a comma.
//
// Takes index (int) which is the index of the token.
//
// Returns bool which is true when it does.
func (p *parser) startsArgument(index int) bool {
	if index < 1 {
		return false
	}
	previous := p.Tokens[index-1].Kind
	return previous == tokenLeftParen || previous == tokenComma
}

// noteComprehensionKeyword records the FOR name IN, or the IF after it, a token at a
// comprehension's own level starts.
//
// Takes found (comprehension) which holds the parts found so far.
// Takes index (int) which is the index of the token.
//
// Returns comprehension which holds the parts found with this token.
func (p *parser) noteComprehensionKeyword(found comprehension, index int) comprehension {
	current := p.Tokens[index]
	switch {
	case found.list < 0 && current.IsKeyword(tokenIdentifier, keywordFOR) && index+2 < len(p.Tokens) &&
		p.Tokens[index+1].Kind == tokenIdentifier && p.Tokens[index+2].IsKeyword(tokenIdentifier, keywordIN):
		found.variable, found.list = p.Tokens[index+1].Value, index+2
	case found.list >= 0 && found.listEnd < 0 && current.IsKeyword(tokenIdentifier, "IF"):
		found.listEnd = index
	}
	return found
}

// closeComprehension finishes reading a comprehension at its closing bracket.
//
// Takes found (comprehension) which holds the parts found.
// Takes closing (int) which is the index of the closing bracket.
//
// Returns comprehension which locates its parts.
// Returns bool which is false when the group held no FOR name IN.
func (*parser) closeComprehension(found comprehension, closing int) (comprehension, bool) {
	if found.list < 0 {
		return found, false
	}
	if found.listEnd < 0 {
		found.listEnd = closing
	}
	return found, true
}

// mayHoldLambdas reports whether the statement holds an arrow, a colon operator or a FOR,
// without which no lambda or list comprehension declares anything in it, working it out
// on first use.
//
// Returns bool which is true when it holds one.
func (state *statementState) mayHoldLambdas() bool {
	if state.lambdas == lambdaPresenceUnknown {
		state.lambdas = lambdaPresenceAbsent
		if slices.ContainsFunc(state.tokens, headsLambdaOrComprehension) {
			state.lambdas = lambdaPresencePossible
		}
	}
	return state.lambdas == lambdaPresencePossible
}

// declares reports whether a name a lambda declares is the name asked about, compared
// regardless of case as DuckDB compares names; an empty name asked about matches every
// declared name.
//
// Takes declared (string) which is the declared name.
// Takes wanted (string) which is the name asked about, or empty.
//
// Returns bool which is true when they match.
func declares(declared, wanted string) bool {
	return wanted == "" || strings.EqualFold(declared, wanted)
}

// noComprehension returns the parts of a group that is not, or not yet known to be, a
// list comprehension.
//
// Returns comprehension which holds no parts.
func noComprehension() comprehension {
	return comprehension{variable: "", list: -1, listEnd: -1}
}

// headsLambdaOrComprehension reports whether a token may end a lambda's head or open a
// list comprehension's FOR: an arrow, a colon operator or the keyword FOR.
//
// Takes candidate (token) which is the token.
//
// Returns bool which is true when it may.
func headsLambdaOrComprehension(candidate token) bool {
	return candidate.Kind == tokenArrow ||
		candidate.Kind == tokenOperator && candidate.Value == operatorColon ||
		candidate.IsKeyword(tokenIdentifier, keywordFOR)
}
