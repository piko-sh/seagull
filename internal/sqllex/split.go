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
	"strings"
)

const (
	// LeadingWordLimit is the most leading words a ProceduralBlockFunc is given.
	LeadingWordLimit = 32

	// delimiterCommand is the word that starts a client-side DELIMITER line.
	delimiterCommand = "DELIMITER"

	// metaCommandMarker starts a client-side meta-command line, such as psql's \connect.
	metaCommandMarker = '\\'
)

// Statement is one statement of a script and where it sits in the source.
type Statement struct {
	// Text is the statement's source text, input[Start:End]: from its first significant
	// token to the end of its last, without the terminator or the whitespace and comments
	// around it. Comments inside the statement are kept.
	Text string

	// Start is the byte offset of the statement's first significant token.
	Start int

	// End is the byte offset just past the statement's last significant token.
	End int

	// LeadingStart is the byte offset where the whitespace and comments directly before the
	// statement begin, after the previous terminator or DELIMITER line, so
	// input[LeadingStart:End] keeps a statement's leading comments, such as directive lines.
	// It equals Start when nothing precedes the statement.
	LeadingStart int

	// Line is the 1-based line of Start.
	Line int

	// Column is the 1-based column of Start, counted in runes.
	Column int
}

// span is a half-open byte range of the input.
type span struct {
	// start is the offset of the first byte.
	start int

	// end is the offset just past the last byte.
	end int
}

// splitResult is everything Split found. The statements, separators and noise between
// them cover the input exactly once.
type splitResult struct {
	// statements holds the statements in order.
	statements []Statement

	// separators holds the terminators, DELIMITER lines and meta-command lines in order.
	separators []span

	// noise holds the runs of whitespace and comments outside every statement.
	noise []span
}

// splitter is the state of one Split call.
type splitter struct {
	// err is the first error a rules callback raised, or nil. Splitting stops once it is
	// set.
	err error

	// input is the script.
	input string

	// terminator is the terminator in force.
	terminator string

	// result collects the statements, separators and noise.
	result splitResult

	// words holds the current statement's leading words in upper case, for the
	// procedural-block predicate.
	words []string

	// lines turns statement offsets into lines and columns.
	lines lineTracker

	// scanner reads the script's tokens.
	scanner Scanner

	// codeStart is the offset of the current statement's first significant token.
	codeStart int

	// codeEnd is the offset just past the current statement's last significant token.
	codeEnd int

	// leadingStart is where the current statement's leading noise begins.
	leadingStart int

	// noiseStart is where the open run of noise began, or -1 when none is open.
	noiseStart int

	// blockStart is the offset of the BEGIN that opened the outermost open block.
	blockStart int

	// blockDepth counts open procedural blocks.
	blockDepth int

	// caseDepth counts open CASE constructs inside a block.
	caseDepth int

	// parenthesisDepth counts open parentheses when they hold the terminator back.
	parenthesisDepth int

	// hasCode is true once the current statement has a significant token.
	hasCode bool

	// custom is true while a DELIMITER line has replaced the rules' terminator.
	custom bool
}

// newSplitter returns a splitter at the start of text.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes text (string) which is the script.
//
// Returns *splitter which is ready to run.
func newSplitter(rules Rules, text string) *splitter {
	return &splitter{
		err:              nil,
		input:            text,
		terminator:       rules.terminator(),
		result:           splitResult{statements: nil, separators: nil, noise: nil},
		words:            nil,
		scanner:          Scanner{err: nil, input: text, rules: rules, position: 0},
		lines:            lineTracker{input: text, offset: 0, line: 1, lineStart: 0},
		codeStart:        0,
		codeEnd:          0,
		leadingStart:     0,
		noiseStart:       -1,
		blockStart:       0,
		blockDepth:       0,
		caseDepth:        0,
		parenthesisDepth: 0,
		hasCode:          false,
		custom:           false,
	}
}

// consume handles one token.
//
// Takes token (Token) which is the token the scanner read.
func (s *splitter) consume(token Token) {
	if token.Kind.IsNoise() {
		if s.noiseStart < 0 {
			s.noiseStart = token.Start
		}
		return
	}
	if !s.hasCode && s.scanner.rules.DelimiterCommand && token.IsWord(delimiterCommand) && s.delimiterLine(token) {
		return
	}
	if !s.hasCode && s.scanner.rules.MetaCommands && s.metaCommandLine(token) {
		return
	}
	if offset := s.terminatorWithin(token); offset >= 0 {
		if offset > token.Start {
			s.markCode(token.Start, offset)
		}
		s.terminate(offset)
		return
	}
	wasCode := s.hasCode
	s.markCode(token.Start, token.End)
	switch token.Kind {
	case KindWord:
		s.trackWord(token, wasCode)
	case KindPunctuation:
		s.trackParenthesis(token)
	default:
	}
}

// terminatorWithin returns where the terminator starts inside a code token, when it ends
// the statement there.
//
// Takes token (Token) which is a significant token.
//
// Returns int which is the terminator's offset, or -1 when the token holds none that ends
// the statement.
func (s *splitter) terminatorWithin(token Token) int {
	switch token.Kind {
	case KindWord, KindNumber, KindParameter, KindPunctuation:
	default:
		return -1
	}
	if !s.custom && (s.blockDepth > 0 || s.parenthesisDepth > 0) {
		return -1
	}
	for offset := token.Start; offset < token.End; offset++ {
		if s.terminatorAt(offset) {
			return offset
		}
	}
	return -1
}

// terminatorAt reports whether the terminator in force starts at offset.
//
// Takes offset (int) which is the byte offset to test.
//
// Returns bool which is true when the input continues with the terminator there.
func (s *splitter) terminatorAt(offset int) bool {
	return offset < len(s.input) && strings.HasPrefix(s.input[offset:], s.terminator)
}

// terminate ends the current statement at a terminator starting at offset and moves the
// scanner past it.
//
// Takes offset (int) which is the terminator's offset.
func (s *splitter) terminate(offset int) {
	end := offset + len(s.terminator)
	s.endStatement(offset)
	s.result.separators = append(s.result.separators, span{start: offset, end: end})
	s.scanner.seek(end)
}

// markCode records significant text from start to end in the current statement, starting
// the statement when it is the first.
//
// Takes start (int) which is the offset of the code's first byte.
// Takes end (int) which is the offset just past it.
func (s *splitter) markCode(start, end int) {
	if !s.hasCode {
		s.hasCode = true
		s.codeStart = start
		s.leadingStart = start
		if s.noiseStart >= 0 {
			s.leadingStart = s.noiseStart
		}
		s.closeNoise(start)
	}
	s.noiseStart = -1
	s.codeEnd = end
}

// closeNoise records the open run of noise as outside every statement, ending at end.
//
// Takes end (int) which is the offset just past the run.
func (s *splitter) closeNoise(end int) {
	if s.noiseStart >= 0 && end > s.noiseStart {
		s.result.noise = append(s.result.noise, span{start: s.noiseStart, end: end})
	}
	s.noiseStart = -1
}

// endStatement records the current statement, if any, and resets the per-statement state.
//
// Takes at (int) which is the offset where the statement's region ends: a terminator, a
// DELIMITER line or the end of the input.
func (s *splitter) endStatement(at int) {
	s.closeNoise(at)
	if s.hasCode {
		line, column := s.lines.at(s.codeStart)
		s.result.statements = append(s.result.statements, Statement{
			Text:         s.input[s.codeStart:s.codeEnd],
			Start:        s.codeStart,
			End:          s.codeEnd,
			LeadingStart: s.leadingStart,
			Line:         line,
			Column:       column,
		})
	}
	s.hasCode = false
	s.words = s.words[:0]
	s.blockDepth = 0
	s.caseDepth = 0
	s.parenthesisDepth = 0
}

// finish ends the last statement at the end of the input.
//
// Returns error which is an *Error wrapping ErrUnterminatedBlock when a procedural block
// is still open.
func (s *splitter) finish() error {
	if s.hasCode && (s.blockDepth > 0 || s.caseDepth > 0) {
		return newError(s.input, s.blockStart, ErrUnterminatedBlock)
	}
	s.endStatement(len(s.input))
	return nil
}

// delimiterLine handles a DELIMITER line starting with token: the word, a space or tab,
// and an argument that becomes the terminator. The rest of the line is ignored.
//
// Takes token (Token) which is the DELIMITER word at the start of a statement.
//
// Returns bool which is true when the line was a DELIMITER command; false leaves the word
// to be read as code.
func (s *splitter) delimiterLine(token Token) bool {
	rest := s.input[token.End:]
	argument := strings.TrimLeft(rest, " \t")
	if len(argument) == len(rest) {
		return false
	}
	argumentStart := token.End + len(rest) - len(argument)
	argumentEnd := argumentStart
	for argumentEnd < len(s.input) && !isWhitespace(s.input[argumentEnd]) {
		argumentEnd++
	}
	if argumentEnd == argumentStart {
		return false
	}
	end := lineEnd(s.input, argumentEnd)
	s.closeNoise(token.Start)
	s.result.separators = append(s.result.separators, span{start: token.Start, end: end})
	s.setTerminator(s.input[argumentStart:argumentEnd])
	s.scanner.seek(end)
	return true
}

// metaCommandLine handles a client-side meta-command line starting with token, as psql
// reads one: a backslash where a statement could begin, and the rest of its line. The
// line belongs to no statement, so it is neither replayed nor sent to the server.
//
// Takes token (Token) which is a significant token at the start of a statement.
//
// Returns bool which is true when the token opens a meta-command line; false leaves the
// token to be read as code.
func (s *splitter) metaCommandLine(token Token) bool {
	if token.Kind != KindPunctuation || s.input[token.Start] != metaCommandMarker {
		return false
	}
	end := lineEnd(s.input, token.Start)
	s.closeNoise(token.Start)
	s.result.separators = append(s.result.separators, span{start: token.Start, end: end})
	s.scanner.seek(end)
	return true
}

// setTerminator puts terminator in force. The rules' own terminator turns the custom mode
// off again.
//
// Takes terminator (string) which is the DELIMITER argument.
func (s *splitter) setTerminator(terminator string) {
	s.terminator = terminator
	s.custom = terminator != s.scanner.rules.terminator()
}

// Split cuts text into statements under the rules.
//
// A terminator ends a statement unless it sits inside a quoted token or a comment, inside
// a procedural block the rules recognise, or, when the rules ask, inside parentheses. A
// stretch holding only whitespace, ordinary comments or terminators is not a statement,
// so stray and trailing terminators and ordinary-comment-only tails produce nothing.
// Executable comments are significant and can form statements on their own. With
// DelimiterCommand set, DELIMITER lines change the terminator and belong to no statement.
// With MetaCommands set, client meta-command lines, such as psql's \restrict, are also
// omitted from statements. A panic in the rules' ProceduralBlock callback stops the split
// and is returned as an error.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes text (string) which is the script.
//
// Returns []Statement which holds the statements in source order, with offsets into text.
// Returns error which is an *Error when a quoted token, comment, dollar-quoted string or
// procedural block is left unterminated, or wraps a *CallbackError, and so
// ErrRulesCallback, when the ProceduralBlock callback panics.
func Split(rules Rules, text string) ([]Statement, error) {
	result, err := split(rules, text)
	if err != nil {
		return nil, err
	}
	return result.statements, nil
}

// split cuts text into statements and records the separators and noise between them.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes text (string) which is the script.
//
// Returns splitResult which holds the statements, separators and noise.
// Returns error which is an *Error for an unterminated construct, or wraps a
// *CallbackError when a rules callback panics.
func split(rules Rules, text string) (splitResult, error) {
	splitter := newSplitter(rules, text)
	for {
		if splitter.custom && splitter.terminatorAt(splitter.scanner.Offset()) {
			splitter.terminate(splitter.scanner.Offset())
			continue
		}
		token, ok := splitter.scanner.Next()
		if !ok {
			break
		}
		splitter.consume(token)
		if splitter.err != nil {
			return splitResult{}, splitter.err
		}
	}
	if err := splitter.scanner.Err(); err != nil {
		return splitResult{}, err
	}
	if err := splitter.finish(); err != nil {
		return splitResult{}, err
	}
	return splitter.result, nil
}
