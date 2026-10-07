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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// parenthesisGroups records, for every token position of one statement, the innermost "("
// still open there and how many commas that group holds at its own level before the
// position.
//
// The index is built in one forward pass. Parameter-context scans can then skip groups,
// find a call's arity and locate a placeholder's argument slot in constant time, without
// rescanning the statement for every placeholder.
type parenthesisGroups struct {
	// opens holds, for each position, the index of the innermost "(" open there, or -1.
	opens []int

	// commas holds, for each position, how many commas of that group come before it at the
	// group's own level.
	commas []int
}

// newParenthesisGroups builds the groups of a statement's tokens in one pass, matching
// each ")" to the nearest "(" still open before it and leaving a ")" that closes nothing
// unmatched.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns *parenthesisGroups which answers lookups for every position from 0 to
// len(tokens) inclusive.
func newParenthesisGroups(tokens []token) *parenthesisGroups {
	groups := &parenthesisGroups{opens: make([]int, len(tokens)+1), commas: make([]int, len(tokens)+1)}
	var openStack, commaStack []int
	for index, current := range tokens {
		groups.record(index, openStack, commaStack)
		switch current.Kind {
		case tokenLeftParen:
			openStack = append(openStack, index)
			commaStack = append(commaStack, 0)
		case tokenRightParen:
			if len(openStack) > 0 {
				openStack = openStack[:len(openStack)-1]
				commaStack = commaStack[:len(commaStack)-1]
			}
		case tokenComma:
			if len(commaStack) > 0 {
				commaStack[len(commaStack)-1]++
			}
		default:
		}
	}
	groups.record(len(tokens), openStack, commaStack)
	return groups
}

// record notes the group open at a position: the top of the open stack and its commas.
//
// Takes position (int) which is the token position.
// Takes openStack ([]int) which holds the indexes of the "(" still open, innermost last.
// Takes commaStack ([]int) which holds each open group's comma count, innermost last.
func (g *parenthesisGroups) record(position int, openStack, commaStack []int) {
	if len(openStack) == 0 {
		g.opens[position] = -1
		return
	}
	g.opens[position] = openStack[len(openStack)-1]
	g.commas[position] = commaStack[len(commaStack)-1]
}

// opening returns the "(" of the innermost group open at a position; for a ")" that is
// the "(" it closes.
//
// Takes position (int) which is the token position.
//
// Returns int which is the index of the "(", or -1 when no group is open there or the
// position is out of range.
func (g *parenthesisGroups) opening(position int) int {
	if position < 0 || position >= len(g.opens) {
		return -1
	}
	return g.opens[position]
}

// commaCount returns how many commas of the innermost group open at a position come
// before it at the group's own level: the 0-based argument slot of a call argument there,
// or for a ")" one less than the call's argument count.
//
// Takes position (int) which is the token position.
//
// Returns int which is the comma count, or zero when the position is out of range.
func (g *parenthesisGroups) commaCount(position int) int {
	if position < 0 || position >= len(g.commas) {
		return 0
	}
	return g.commas[position]
}

// parenthesisScanIndex returns the statement's parenthesis scan index, building it on
// first use so statements without parameters never pay for it.
//
// Returns *dialectkit.ParenthesisScanIndex which answers enclosing-group and
// enclosing-LIKE lookups in constant time.
func (p *parser) parenthesisScanIndex() *dialectkit.ParenthesisScanIndex {
	if p.parenthesisIndex == nil {
		p.parenthesisIndex = dialectkit.NewParenthesisScanIndex(len(p.Tokens), p.classifyScanToken)
	}
	return p.parenthesisIndex
}

// parenthesisGroups returns the statement's parenthesis groups, building them on first
// use so statements without parameters never pay for them.
//
// Returns *parenthesisGroups which answers group lookups in constant time.
func (p *parser) parenthesisGroups() *parenthesisGroups {
	if p.groups == nil {
		p.groups = newParenthesisGroups(p.Tokens)
	}
	return p.groups
}

// castTypeStop returns the first token after a position that is a ")" or the keyword AS,
// where the search for the type of the CAST around a placeholder stops.
//
// The answers for every position are built in one backward pass on first use, so the
// placeholders of a long CAST operand do not each scan to its AS.
//
// Takes position (int) which is the placeholder's token index.
//
// Returns int which is the index of the ")" or AS, or -1 when neither follows or the
// position is out of range.
func (p *parser) castTypeStop(position int) int {
	if p.castTypeStops == nil {
		stops := make([]int, len(p.Tokens)+1)
		next := -1
		stops[len(p.Tokens)] = next
		for index := len(p.Tokens) - 1; index >= 0; index-- {
			stops[index] = next
			if p.Tokens[index].Kind == tokenRightParen || p.Tokens[index].IsKeyword(tokenIdentifier, keywordAS) {
				next = index
			}
		}
		p.castTypeStops = stops
	}
	if position < 0 || position >= len(p.castTypeStops) {
		return -1
	}
	return p.castTypeStops[position]
}

// likeOperatorColumn returns the column a LIKE-style operator's left-hand side names, as
// resolveLikeOperatorColumn finds it, working it out once per operator so every
// placeholder of a long pattern shares the answer.
//
// Takes likePosition (int) which is the operator's token index.
//
// Returns *ast.ColumnReference which is a copy of the column, or nil when none is found.
func (p *parser) likeOperatorColumn(likePosition int) *ast.ColumnReference {
	if p.likeColumns == nil {
		p.likeColumns = make(map[int]*ast.ColumnReference)
	}
	column, known := p.likeColumns[likePosition]
	if !known {
		column = p.resolveLikeOperatorColumn(likePosition)
		p.likeColumns[likePosition] = column
	}
	if column == nil {
		return nil
	}
	return new(*column)
}

// classifyScanToken reports the role the token at index plays in the parenthesis-aware
// parameter-context scans.
//
// Takes index (int) which is the token's position.
//
// Returns dialectkit.TokenClass which is the token's role; clause boundaries take
// precedence over pattern operators.
func (p *parser) classifyScanToken(index int) dialectkit.TokenClass {
	current := p.Tokens[index]
	if current.Kind == tokenLeftParen {
		return dialectkit.TokenClassLeftParen
	}
	if current.Kind == tokenRightParen {
		return dialectkit.TokenClassRightParen
	}
	if current.Kind != tokenIdentifier {
		return dialectkit.TokenClassOther
	}
	keyword := current.Keyword(tokenIdentifier)
	if isLikeBoundaryKeyword(keyword) {
		return dialectkit.TokenClassBoundary
	}
	if isLikePatternKeyword(keyword) {
		return dialectkit.TokenClassPattern
	}
	return dialectkit.TokenClassOther
}
