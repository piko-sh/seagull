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

package sqllex

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// keywordBegin opens a procedural block.
	keywordBegin = "BEGIN"

	// keywordCase opens a CASE construct inside a block.
	keywordCase = "CASE"

	// keywordEnd closes a block, a CASE or an inner control structure.
	keywordEnd = "END"
)

// lineTracker turns increasing byte offsets into lines and columns without rescanning the
// input from its start each time.
type lineTracker struct {
	// input is the text.
	input string

	// offset is how far the tracker has counted.
	offset int

	// line is the 1-based line at offset.
	line int

	// lineStart is the offset where that line begins.
	lineStart int
}

// at returns the line and rune column of offset, which must not be before the last offset
// asked for.
//
// Takes offset (int) which is the byte offset.
//
// Returns line (int) which is the 1-based line.
// Returns column (int) which is the 1-based column in runes.
func (tracker *lineTracker) at(offset int) (line, column int) {
	stretch := tracker.input[tracker.offset:offset]
	if newlines := strings.Count(stretch, "\n"); newlines > 0 {
		tracker.line += newlines
		tracker.lineStart = tracker.offset + strings.LastIndexByte(stretch, '\n') + 1
	}
	tracker.offset = offset
	return tracker.line, utf8.RuneCountInString(tracker.input[tracker.lineStart:offset]) + 1
}

// trackWord updates the block and CASE depths for a word in the current statement and
// records it among the leading words.
//
// Takes token (Token) which is the word.
// Takes wasCode (bool) which is true when the statement had started before this word.
func (s *splitter) trackWord(token Token, wasCode bool) {
	procedural := s.scanner.rules.ProceduralBlock
	if procedural == nil || s.custom {
		return
	}
	switch {
	case token.IsWord(keywordBegin):
		if wasCode && s.opensBlock(procedural, token) {
			if s.blockDepth == 0 {
				s.blockStart = token.Start
			}
			s.blockDepth++
		}
	case token.IsWord(keywordCase):
		if s.blockDepth > 0 {
			s.caseDepth++
		}
	case token.IsWord(keywordEnd):
		s.closeWithEnd()
	}
	s.recordWord(token)
}

// opensBlock asks the rules' ProceduralBlock callback whether a BEGIN opens a procedural
// block. A panic in the callback is kept as the splitter's error, located at the BEGIN,
// and the BEGIN is then read as an ordinary word.
//
// Takes procedural (ProceduralBlockFunc) which is the rules' callback.
// Takes token (Token) which is the BEGIN word.
//
// Returns bool which is true when the callback says the BEGIN opens a block.
func (s *splitter) opensBlock(procedural ProceduralBlockFunc, token Token) bool {
	opens, err := callPredicate(proceduralBlockCallback, func() bool { return procedural(s.words) })
	if err == nil {
		return opens
	}
	if s.err == nil {
		line, column := lineAndColumn(s.input, token.Start)
		s.err = fmt.Errorf("checking %s at line %d, column %d (byte %d): %w", token.Raw, line, column, token.Start, err)
	}
	return false
}

// recordWord adds a word to the leading words while there is room.
//
// Takes token (Token) which is the word.
func (s *splitter) recordWord(token Token) {
	if len(s.words) < LeadingWordLimit {
		s.words = append(s.words, upperWord(token.Raw))
	}
}

// closeWithEnd decides which procedural construct an END closes.
//
// Qualified endings close their matching construct. A bare END closes an open CASE
// expression before the surrounding block.
func (s *splitter) closeWithEnd() {
	if s.blockDepth == 0 {
		return
	}
	lookahead := s.scanner
	next, ok := lookahead.NextSignificant()
	nextWord := ""
	if ok && next.Kind == KindWord {
		nextWord = upperWord(next.Raw)
	}
	switch nextWord {
	case "IF", "LOOP", "WHILE", "REPEAT":
		return
	case keywordCase:
		if s.caseDepth > 0 {
			s.caseDepth--
		}
		s.scanner = lookahead
		s.markCode(next.Start, next.End)
		s.recordWord(next)
		return
	}
	if s.caseDepth > 0 {
		s.caseDepth--
		return
	}
	s.blockDepth--
}

// trackParenthesis updates the parenthesis depth when the rules hold the terminator back
// inside parentheses.
//
// Takes token (Token) which is a punctuation token.
func (s *splitter) trackParenthesis(token Token) {
	if !s.scanner.rules.TerminatorOutsideParentheses || s.custom {
		return
	}
	switch token.Raw {
	case "(":
		s.parenthesisDepth++
	case ")":
		if s.parenthesisDepth > 0 {
			s.parenthesisDepth--
		}
	}
}
