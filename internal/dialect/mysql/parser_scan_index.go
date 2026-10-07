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

package mysql

import (
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// argumentScanIndex answers, for any token position of one statement, which argument slot
// of its enclosing parenthesised group the token sits in, and which '(' each ')' closes.
//
// Both answers are computed in one pass over the tokens, so each lookup takes constant
// time where a walk from the group's '(' to the token would make the scans that type
// every placeholder of a statement quadratic in their number.
type argumentScanIndex struct {
	// ordinals holds, per position, how many commas stand at the position's own nesting
	// level between the '(' that opened that level and the position.
	ordinals []int

	// matchingOpen holds, per position of a ')', the index of the '(' it closes, and -1 for
	// every other position and for a ')' that closes nothing.
	matchingOpen []int
}

// newArgumentScanIndex reads the tokens once and records, for every position, its
// argument ordinal within the enclosing group and, for every ')', its matching '('.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns *argumentScanIndex which answers lookups in constant time.
func newArgumentScanIndex(tokens []token) *argumentScanIndex {
	count := len(tokens)
	index := &argumentScanIndex{ordinals: make([]int, count+1), matchingOpen: make([]int, count)}
	levelOffset := count + 1
	commasAtLevel := make([]int, 2*levelOffset+1)
	openParens := make([]int, 0, min(count, dialectkit.DefaultMaxParseDepth))
	level := 0
	for position := range tokens {
		index.ordinals[position] = commasAtLevel[level+levelOffset]
		index.matchingOpen[position] = -1
		switch tokens[position].Kind {
		case tokenLeftParen:
			level++
			commasAtLevel[level+levelOffset] = 0
			openParens = append(openParens, position)
		case tokenRightParen:
			level--
			if last := len(openParens) - 1; last >= 0 {
				index.matchingOpen[position] = openParens[last]
				openParens = openParens[:last]
			}
		case tokenComma:
			commasAtLevel[level+levelOffset]++
		default:
		}
	}
	index.ordinals[count] = commasAtLevel[level+levelOffset]
	return index
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

// argumentScan returns the statement's argument scan index, building it on first use so
// statements without parameters never pay for it.
//
// Returns *argumentScanIndex which answers argument-ordinal and matching-parenthesis
// lookups in constant time.
func (p *parser) argumentScan() *argumentScanIndex {
	if p.argumentIndex == nil {
		p.argumentIndex = newArgumentScanIndex(p.Tokens)
	}
	return p.argumentIndex
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
	if isLogicalOperator(current) {
		return dialectkit.TokenClassBoundary
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
