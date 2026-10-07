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

const (
	// DefaultTerminator is the statement terminator used when Rules.Terminator is empty.
	DefaultTerminator = ";"
)

// EscapeMode says how a quote character is written inside a quoted token.
type EscapeMode uint8

const (
	// EscapeDoubled escapes the closing quote by doubling it, as standard SQL does. It is
	// the zero value.
	//
	// For example:
	//
	// 	'it''s'
	EscapeDoubled EscapeMode = iota

	// EscapeBackslash makes a backslash escape the byte after it, so \' does not close the
	// token. A doubled quote closes the token and opens a new one.
	EscapeBackslash

	// EscapeDoubledOrBackslash accepts both forms: a doubled quote and a backslash escape.
	EscapeDoubledOrBackslash
)

const (
	// DoubleQuoteIdentifier reads "..." as a quoted identifier, as standard SQL does. It is
	// the zero value.
	DoubleQuoteIdentifier DoubleQuoteMeaning = iota

	// DoubleQuoteString reads "..." as a string literal.
	DoubleQuoteString
)

// String returns the escape mode's name.
//
// Returns string which is the name, such as "doubled".
func (mode EscapeMode) String() string {
	switch mode {
	case EscapeDoubled:
		return "doubled"
	case EscapeBackslash:
		return "backslash"
	case EscapeDoubledOrBackslash:
		return "doubled-or-backslash"
	default:
		return "unknown"
	}
}

// allowsDoubling reports whether a doubled quote stands for one quote character.
//
// Returns bool which is true for EscapeDoubled and EscapeDoubledOrBackslash.
func (mode EscapeMode) allowsDoubling() bool {
	return mode != EscapeBackslash
}

// allowsBackslash reports whether a backslash escapes the byte after it.
//
// Returns bool which is true for EscapeBackslash and EscapeDoubledOrBackslash.
func (mode EscapeMode) allowsBackslash() bool {
	return mode == EscapeBackslash || mode == EscapeDoubledOrBackslash
}

// DoubleQuoteMeaning says what text inside double quotes is.
type DoubleQuoteMeaning uint8

// String returns the meaning's name.
//
// Returns string which is "identifier", "string" or "unknown" for an unrecognised value.
func (meaning DoubleQuoteMeaning) String() string {
	switch meaning {
	case DoubleQuoteIdentifier:
		return "identifier"
	case DoubleQuoteString:
		return "string"
	default:
		return "unknown"
	}
}

// ProceduralBlockFunc decides whether a statement can hold a procedural body whose BEGIN
// ... END block keeps statement terminators inside the statement.
//
// The splitter calls it when it meets the word BEGIN after the statement has already
// started, passing the statement's leading unquoted words in upper case, up to (and not
// including) that BEGIN and at most LeadingWordLimit of them. When it returns true, the
// BEGIN opens a block: terminators stay inside the statement until the matching END, CASE
// ... END expressions inside the block are balanced, and END IF, END LOOP, END WHILE and
// END REPEAT close inner constructs without closing the block. When it returns false the
// BEGIN is an ordinary word, such as a column named begin. The slice is reused between
// calls, so the function must not keep it. A panic in the function stops the split, and
// Split returns it as an error wrapping a *CallbackError.
//
// Takes leadingWords ([]string) which holds the upper-cased words before the BEGIN.
//
// Returns bool which is true when the BEGIN opens a procedural block.
type ProceduralBlockFunc func(leadingWords []string) bool

// StatementPredicate decides a property of one statement from its significant tokens: its
// words, quoted identifiers, strings, numbers, parameters, punctuation and executable
// comments, without whitespace or ordinary comments. A panic in the predicate is returned
// by RequiresNoTransaction as an error wrapping a *CallbackError.
//
// Takes tokens ([]Token) which holds the statement's significant tokens in order.
//
// Returns bool which is true when the statement has the property.
type StatementPredicate func(tokens []Token) bool

// Rules describes how an engine writes SQL text. The scanner and splitter read only these
// rules, so every lexical difference between engines is a value here rather than a branch
// in the code.
//
// The zero value describes standard SQL: '...' strings with doubled quotes, "..." quoted
// identifiers, -- and non-nesting /* */ comments, no parameter sigils, and ";" as the
// terminator with no procedural blocks.
type Rules struct {
	// ProceduralBlock decides when BEGIN opens a procedural body. Nil means BEGIN, CASE and
	// END never affect where a statement ends.
	ProceduralBlock ProceduralBlockFunc

	// RequiresNoTransaction identifies statements that cannot run inside a transaction. Nil
	// imposes no restriction.
	RequiresNoTransaction StatementPredicate

	// Terminator ends a statement. Empty means DefaultTerminator.
	Terminator string

	// Quotes describes string literals and quoted identifiers.
	Quotes QuoteRules

	// Parameters describes bind-parameter placeholders.
	Parameters ParameterRules

	// Comments describes line and block comments.
	Comments CommentRules

	// DelimiterCommand enables client-side DELIMITER lines to change the statement
	// terminator. These lines are omitted from statements, and custom terminators apply
	// outside quotes and comments.
	DelimiterCommand bool

	// MetaCommands recognises client-side backslash commands at statement boundaries. Their
	// lines are omitted from statements sent to the server.
	MetaCommands bool

	// TerminatorOutsideParentheses makes a terminator end a statement only when every
	// parenthesis opened in the statement has been closed, so a parenthesised list of
	// statements stays one statement.
	TerminatorOutsideParentheses bool

	// DollarInIdentifiers lets an unquoted identifier continue with '$' after its first
	// character, so name$suffix is one word and never opens a dollar-quoted string.
	DollarInIdentifiers bool
}

// terminator returns the effective statement terminator.
//
// Returns string which is Terminator, or DefaultTerminator when it is empty.
func (rules *Rules) terminator() string {
	if rules.Terminator == "" {
		return DefaultTerminator
	}
	return rules.Terminator
}

// QuoteRules describes the quoted forms an engine accepts.
type QuoteRules struct {
	// StringPrefixes lists the letters, in upper case, that may sit directly before a
	// single-quoted string as part of the same literal, such as X'00ff' or B'1010'. The
	// prefixed literal follows SingleQuoteEscape.
	StringPrefixes string

	// SingleQuoteEscape says how a quote is escaped inside a '...' string.
	SingleQuoteEscape EscapeMode

	// DoubleQuote says whether "..." is a quoted identifier or a string.
	DoubleQuote DoubleQuoteMeaning

	// DoubleQuoteEscape says how a quote is escaped inside "...".
	DoubleQuoteEscape EscapeMode

	// BacktickEscape says how a backtick is escaped inside `...` when Backticks is set.
	BacktickEscape EscapeMode

	// Backticks reads `...` as a quoted identifier.
	Backticks bool

	// Brackets reads [...] as a quoted identifier closed by the first ']'.
	Brackets bool

	// DollarQuoting reads $$...$$ and $tag$...$tag$ as strings. A tag starts with a letter
	// or underscore and continues with letters, digits and underscores.
	DollarQuoting bool

	// EscapeStrings reads E'...' (either case) as a string in which a backslash escapes the
	// byte after it and a doubled quote stands for one quote.
	EscapeStrings bool
}

// CommentRules describes the comment forms an engine accepts. Every engine accepts --
// line comments and /* */ block comments; these rules refine them.
type CommentRules struct {
	// DoubleDashRequiresWhitespace makes -- start a comment only when a space, control
	// character or the end of the input follows it, so a--b is an expression.
	DoubleDashRequiresWhitespace bool

	// HashLineComments makes # start a comment that runs to the end of the line.
	HashLineComments bool

	// NestedBlockComments makes /* */ comments nest, so each inner /* needs its own */.
	NestedBlockComments bool

	// ExecutableComments keeps server-executed block comments as significant tokens within
	// statements or as standalone statements.
	ExecutableComments bool
}

// ParameterRules describes the bind-parameter placeholders an engine accepts. A
// placeholder is scanned as one KindParameter token.
type ParameterRules struct {
	// NamedSigils lists the bytes that start a named parameter when a letter or underscore
	// follows, such as ":" for :name. A doubled sigil, such as ::, is punctuation.
	NamedSigils string

	// BracedLiteralWords lists case-insensitive literal words excluded from braced
	// placeholder detection. A brace with a literal word as both its name and type is
	// punctuation.
	BracedLiteralWords []string

	// NoNamedParameterAfterOperand treats a named sigil directly after an operand as
	// punctuation, distinguishing lambdas and slices from placeholders.
	NoNamedParameterAfterOperand bool

	// QuestionMark reads ? as an anonymous positional parameter.
	QuestionMark bool

	// NumberedQuestionMark recognises question marks followed by digits as numbered
	// parameters. It requires QuestionMark.
	NumberedQuestionMark bool

	// DollarNumbers reads $ followed by digits, such as $1, as a numbered parameter.
	DollarNumbers bool

	// Braced recognises {name:Type} as a typed parameter when both names are valid and the
	// braces close on one line without nested braces. Other opening braces remain
	// punctuation.
	Braced bool
}
