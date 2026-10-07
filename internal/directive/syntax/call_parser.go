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

package syntax

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

const (
	// maxDirectiveValueDepth bounds nested list-literal recursion so a deeply nested
	// `[[[...]]]` value cannot overflow the stack with a fatal, non-recoverable error.
	maxDirectiveValueDepth = 256
)

// CallArguments is the parsed contents of a directive call: a list of positional values
// followed by any number of keyword arguments.
//
// Multi-positional is supported; keyword arguments whose key matches a positional slot
// are resolved to that slot during validation.
type CallArguments struct {
	// Positionals holds the parsed positional arguments in source order.
	Positionals []ParsedPositional

	// KeywordArguments holds the parsed keyword arguments in source order.
	KeywordArguments []ParsedKeywordArgument

	// OpenSpan covers the opening parenthesis of the call.
	OpenSpan diagbuild.TextSpan

	// CloseSpan covers the closing parenthesis of the call.
	CloseSpan diagbuild.TextSpan

	// Closed reports whether the closing parenthesis was consumed.
	Closed bool
}

// span returns the span of the call: from its opening parenthesis through its closing
// one, or the opening parenthesis alone when the call is unclosed.
//
// Returns diagbuild.TextSpan which locates the call.
func (args *CallArguments) span() diagbuild.TextSpan {
	if !args.Closed {
		return args.OpenSpan
	}
	return MergeSpan(args.OpenSpan, args.CloseSpan)
}

// markLastFailed marks the argument parsed last as failed.
func (args *CallArguments) markLastFailed() {
	positionalCount := len(args.Positionals)
	keywordCount := len(args.KeywordArguments)
	switch {
	case keywordCount > 0 && (positionalCount == 0 || keywordArgumentFollows(args)):
		args.KeywordArguments[keywordCount-1].Failed = true
	case positionalCount > 0:
		args.Positionals[positionalCount-1].Failed = true
	}
}

// ParsedPositional is one positional argument with its value and the span covering the
// value tokens for diagnostics.
type ParsedPositional struct {
	// Value is the parsed positional value.
	Value ParsedValue

	// Span covers the value tokens for diagnostics.
	Span diagbuild.TextSpan

	// Failed reports that a value followed the argument where a comma or the closing
	// parenthesis belongs, as when a keyword argument's colon is missing. The syntax error
	// has been reported, so validation reports nothing more about the argument.
	Failed bool
}

// ParsedKeywordArgument is one keyword argument with its key, value, and the spans
// covering the key, value, and the whole key-value pair respectively.
type ParsedKeywordArgument struct {
	// Key is the keyword argument name.
	Key string

	// Value is the parsed keyword argument value.
	Value ParsedValue

	// Span covers the whole key-value pair.
	Span diagbuild.TextSpan

	// KeySpan covers the key token.
	KeySpan diagbuild.TextSpan

	// ValueSpan covers the value tokens.
	ValueSpan diagbuild.TextSpan

	// Failed reports that a value followed the argument where a comma or the closing
	// parenthesis belongs. The syntax error has been reported, so validation reports nothing
	// more about the argument.
	Failed bool
}

// ParsedValue carries the raw value text plus optional list-shape information.
//
// The raw field is always set; the list fields are set only when the value was parsed
// from a `[a, b, c]` literal.
type ParsedValue struct {
	// Raw is the value text, always set.
	Raw string

	// AsList holds the list elements when the value was a list literal.
	AsList []string

	// ListSpan covers the list literal when the value was a list.
	ListSpan diagbuild.TextSpan

	// IsList reports whether the value was parsed from a list literal.
	IsList bool
}

// ParseCallArguments reads the parenthesised argument list of a directive call.
//
// The opening paren must be the next token; on success the closing paren is consumed and
// args.Closed is set to true. At most maxDiagnosticsPerCall diagnostics are reported,
// followed by one Q066 diagnostic on the call when any were left out.
//
// Takes lexer (*Lexer) which supplies the call tokens.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which names the directive for messages.
//
// Returns *CallArguments which holds the parsed arguments, or nil when the opening paren
// is missing.
// Returns []diag.SourceError which holds any diagnostics produced.
func ParseCallArguments(lexer *Lexer, errorBuilder diagbuild.ErrorBuilder, directiveName string) (*CallArguments, []diag.SourceError) {
	openToken := lexer.Next()
	if openToken.Kind != TokenLParen {
		return nil, []diag.SourceError{errorBuilder.DirectiveSyntax(openToken.Span, fmt.Sprintf("expected '(' after %s", directiveName))}
	}
	arguments := &CallArguments{
		OpenSpan:         openToken.Span,
		Positionals:      nil,
		KeywordArguments: nil,
		CloseSpan:        diagbuild.TextSpan{},
		Closed:           false,
	}
	limiter := diagbuild.NewLimiter(maxDiagnosticsPerCall)
	parseArgumentList(lexer, arguments, errorBuilder, directiveName, limiter)
	return arguments, limiter.Finish(callNoticePlace(errorBuilder, arguments.span()))
}

// parseArgumentList reads the arguments of a call whose opening parenthesis has been
// read, through its closing parenthesis or the end of the input.
//
// Takes lexer (*Lexer) which supplies the call tokens.
// Takes arguments (*CallArguments) which accumulates the parsed arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which names the directive for messages.
// Takes limiter (*diagbuild.Limiter) which receives the diagnostics.
func parseArgumentList(lexer *Lexer, arguments *CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string, limiter *diagbuild.Limiter) {
	if next := lexer.Peek(); next.Kind == tokenRParen {
		arguments.CloseSpan = lexer.Next().Span
		arguments.Closed = true
		return
	}

	for {
		current := lexer.Peek()
		if current.Kind == TokenEOF {
			limiter.Add(errorBuilder.Unclosed(current.Span, directiveName))
			return
		}
		if current.Kind == tokenRParen {
			arguments.CloseSpan = lexer.Next().Span
			arguments.Closed = true
			return
		}

		limiter.Add(parseSingleArgument(lexer, arguments, errorBuilder)...)
		separatorDiagnostics, done := consumeSeparator(lexer, arguments, errorBuilder, directiveName)
		limiter.Add(separatorDiagnostics...)
		if done {
			return
		}
	}
}

// consumeSeparator reads what follows an argument: a comma, the closing parenthesis, the
// end of the input, or text that belongs to neither.
//
// Punctuation where a separator belongs is reported and read as one. A value there, as
// when a keyword argument's colon is missing, is reported, the argument before it is
// marked as failed, and the rest of the malformed argument is skipped up to the comma or
// closing parenthesis that ends it.
//
// Takes lexer (*Lexer) which supplies the tokens.
// Takes arguments (*CallArguments) which holds the arguments parsed so far.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes directiveName (string) which names the directive for messages.
//
// Returns []diag.SourceError which holds any diagnostics produced.
// Returns bool which is true when the call has ended, closed or not.
func consumeSeparator(lexer *Lexer, arguments *CallArguments, errorBuilder diagbuild.ErrorBuilder, directiveName string) ([]diag.SourceError, bool) {
	separator := lexer.Peek()
	switch separator.Kind {
	case tokenComma:
		lexer.Next()
		return nil, false
	case tokenRParen:
		arguments.CloseSpan = lexer.Next().Span
		arguments.Closed = true
		return nil, true
	case TokenEOF:
		return []diag.SourceError{errorBuilder.Unclosed(separator.Span, directiveName)}, true
	default:
	}
	diagnostics := []diag.SourceError{errorBuilder.DirectiveSyntax(separator.Span, fmt.Sprintf("expected ',' or ')', got %q", separator.Lexeme))}
	if !startsValue(separator.Kind) {
		lexer.Next()
		return diagnostics, false
	}
	arguments.markLastFailed()
	skipToArgumentBoundary(lexer)
	if lexer.Peek().Kind == tokenComma {
		lexer.Next()
	}
	return diagnostics, false
}

// startsValue reports whether a token kind can start an argument value.
//
// Takes kind (tokenKind) which is the token's kind.
//
// Returns bool which is true for an identifier, number, string, sigil or list.
func startsValue(kind tokenKind) bool {
	switch kind {
	case TokenIdentifier, tokenNumber, tokenString, tokenSigil, tokenLBracket:
		return true
	default:
		return false
	}
}

// keywordArgumentFollows reports whether the last keyword argument was parsed after the
// last positional, judged by where their spans start.
//
// Takes arguments (*CallArguments) which holds at least one positional and one keyword
// argument.
//
// Returns bool which is true when the keyword argument came later.
func keywordArgumentFollows(arguments *CallArguments) bool {
	keyword := arguments.KeywordArguments[len(arguments.KeywordArguments)-1].Span
	positional := arguments.Positionals[len(arguments.Positionals)-1].Span
	return keyword.Line > positional.Line || (keyword.Line == positional.Line && keyword.Column > positional.Column)
}

// skipToArgumentBoundary consumes the tokens of a malformed argument up to, but not
// including, the comma or closing parenthesis that ends it at its own nesting level, or
// the end of the input.
//
// Takes lexer (*Lexer) which supplies the tokens.
func skipToArgumentBoundary(lexer *Lexer) {
	depth := 0
	for {
		token := lexer.Peek()
		switch token.Kind {
		case TokenEOF:
			return
		case tokenComma, tokenRParen:
			if depth == 0 {
				return
			}
			if token.Kind == tokenRParen {
				depth--
			}
		case TokenLParen, tokenLBracket:
			depth++
		case tokenRBracket:
			depth = max(depth-1, 0)
		default:
		}
		lexer.Next()
	}
}

// parseSingleArgument parses one argument in a directive call.
//
// Positional arguments must precede any keyword argument; once a keyword argument is
// seen, any subsequent positional is reported as a syntax error.
//
// Takes lexer (*Lexer) which supplies the argument tokens.
// Takes arguments (*CallArguments) which accumulates the parsed argument.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
//
// Returns []diag.SourceError which holds any diagnostics produced.
func parseSingleArgument(lexer *Lexer, arguments *CallArguments, errorBuilder diagbuild.ErrorBuilder) []diag.SourceError {
	var diagnostics []diag.SourceError
	keyOrValue := lexer.Peek()

	if keyOrValue.Kind == TokenIdentifier && lexer.PeekN(1).Kind == tokenColon {
		keyToken := lexer.Next()
		lexer.Next()
		value, valueSpan, valueDiagnostics := parseValue(lexer, errorBuilder, 0)
		diagnostics = append(diagnostics, valueDiagnostics...)
		arguments.KeywordArguments = append(arguments.KeywordArguments, ParsedKeywordArgument{
			Key:       keyToken.Lexeme,
			Value:     value,
			Span:      MergeSpan(keyToken.Span, valueSpan),
			KeySpan:   keyToken.Span,
			ValueSpan: valueSpan,
			Failed:    false,
		})
		return diagnostics
	}

	if len(arguments.KeywordArguments) > 0 {
		diagnostics = append(diagnostics, errorBuilder.DirectiveSyntax(keyOrValue.Span, "positional arguments must precede keyword arguments; later arguments must be 'key: value'"))
		_, _, valueDiagnostics := parseValue(lexer, errorBuilder, 0)
		diagnostics = append(diagnostics, valueDiagnostics...)
		return diagnostics
	}

	value, valueSpan, valueDiagnostics := parseValue(lexer, errorBuilder, 0)
	diagnostics = append(diagnostics, valueDiagnostics...)
	arguments.Positionals = append(arguments.Positionals, ParsedPositional{Value: value, Span: valueSpan, Failed: false})
	return diagnostics
}

// parseValue reads one value token (string, number, identifier, or list literal).
//
// Dotted identifier chains are joined into a single raw value so `sgl.foo.bar` survives
// intact. The depth argument tracks list-literal nesting so adversarial input cannot
// overflow the stack.
//
// Takes lexer (*Lexer) which supplies the value tokens.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes depth (int) which is the current list-literal nesting depth.
//
// Returns ParsedValue which is the parsed value.
// Returns diagbuild.TextSpan which covers the value tokens.
// Returns []diag.SourceError which holds any diagnostics produced.
func parseValue(lexer *Lexer, errorBuilder diagbuild.ErrorBuilder, depth int) (ParsedValue, diagbuild.TextSpan, []diag.SourceError) {
	var diagnostics []diag.SourceError
	current := lexer.Peek()
	if depth >= maxDirectiveValueDepth {
		lexer.Next()
		diagnostics = append(diagnostics, errorBuilder.DirectiveSyntax(current.Span, "value nesting too deep"))
		return ParsedValue{
			Raw:      current.Lexeme,
			AsList:   nil,
			ListSpan: diagbuild.TextSpan{},
			IsList:   false,
		}, current.Span, diagnostics
	}
	switch current.Kind {
	case tokenLBracket:
		return parseListValue(lexer, errorBuilder, depth)
	case tokenString:
		lexer.Next()
		if lexer.Peek().Kind == TokenDot {
			return parseDottedValue(lexer, errorBuilder, current.Span, quoteNamePart(current.Lexeme))
		}
		return ParsedValue{
			Raw:      current.Lexeme,
			AsList:   nil,
			ListSpan: diagbuild.TextSpan{},
			IsList:   false,
		}, current.Span, diagnostics
	case tokenNumber, TokenIdentifier:
		lexer.Next()
		return parseDottedValue(lexer, errorBuilder, current.Span, current.Lexeme)
	default:
		lexer.Next()
		diagnostics = append(diagnostics, errorBuilder.DirectiveSyntax(current.Span, fmt.Sprintf("unexpected token %q in value position", current.Lexeme)))
		return ParsedValue{
			Raw:      current.Lexeme,
			AsList:   nil,
			ListSpan: diagbuild.TextSpan{},
			IsList:   false,
		}, current.Span, diagnostics
	}
}

// parseListValue reads a `[a, b, c]` list literal.
//
// Unterminated lists and mismatched closers (e.g. `[a, b)`) reset the result so
// downstream validation does not see a partial list; the caller still receives the value
// with IsList=false so the value-shape diagnostic at the validator level keeps producing
// the expected Q-coded message.
//
// Takes lexer (*Lexer) which supplies the list tokens.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes depth (int) which is the current list-literal nesting depth.
//
// Returns ParsedValue which is the parsed list value.
// Returns diagbuild.TextSpan which covers the list literal.
// Returns []diag.SourceError which holds any diagnostics produced.
func parseListValue(lexer *Lexer, errorBuilder diagbuild.ErrorBuilder, depth int) (ParsedValue, diagbuild.TextSpan, []diag.SourceError) {
	var diagnostics []diag.SourceError
	openToken := lexer.Next()
	value := ParsedValue{
		IsList:   true,
		Raw:      "",
		AsList:   nil,
		ListSpan: diagbuild.TextSpan{},
	}

	if lexer.Peek().Kind == tokenRBracket {
		closeToken := lexer.Next()
		value.ListSpan = MergeSpan(openToken.Span, closeToken.Span)
		return value, value.ListSpan, diagnostics
	}
	for {
		item, _, itemDiagnostics := parseValue(lexer, errorBuilder, depth+1)
		diagnostics = append(diagnostics, itemDiagnostics...)
		if item.IsList {
			diagnostics = append(diagnostics, errorBuilder.InvalidListLiteral(item.ListSpan, "nested list"))
			value.IsList = false
			value.AsList = nil
			value.ListSpan = MergeSpan(openToken.Span, drainEnclosingList(lexer))
			return value, value.ListSpan, diagnostics
		}
		value.AsList = append(value.AsList, item.Raw)
		separator := lexer.Peek()
		switch separator.Kind {
		case tokenComma:
			lexer.Next()
			continue
		case tokenRBracket:
			closeToken := lexer.Next()
			value.ListSpan = MergeSpan(openToken.Span, closeToken.Span)
			return value, value.ListSpan, diagnostics
		case TokenEOF:
			diagnostics = append(diagnostics, errorBuilder.Unclosed(separator.Span, "list"))
			value.IsList = false
			value.AsList = nil
			value.ListSpan = MergeSpan(openToken.Span, separator.Span)
			return value, value.ListSpan, diagnostics
		case TokenIdentifier, tokenNumber, tokenString, TokenLParen, tokenRParen,
			tokenLBracket, tokenColon, TokenDot, tokenSigil, tokenInvalid:
		}

		diagnostics = append(diagnostics, errorBuilder.InvalidListLiteral(separator.Span, separator.Lexeme))
		value.IsList = false
		value.AsList = nil
		value.ListSpan = MergeSpan(openToken.Span, separator.Span)
		consumeUntilListBoundary(lexer)
		if lexer.Peek().Kind == tokenRBracket {
			lexer.Next()
		}
		return value, value.ListSpan, diagnostics
	}
}

// drainEnclosingList consumes the remainder of the list literal open at depth one,
// balancing nested brackets.
//
// It returns the span of the closing bracket, or the last token seen at end-of-input. It
// recovers after a nested-list element is rejected so the error does not cascade into the
// tokens that follow the outer list.
//
// Takes lexer (*Lexer) which is positioned just after the rejected element.
//
// Returns diagbuild.TextSpan which is the span of the closing bracket or final token.
func drainEnclosingList(lexer *Lexer) diagbuild.TextSpan {
	depth := 1
	lastSpan := lexer.Peek().Span
	for depth > 0 {
		current := lexer.Next()
		lastSpan = current.Span
		switch current.Kind {
		case tokenLBracket:
			depth++
		case tokenRBracket:
			depth--
		case TokenEOF:
			return lastSpan
		default:
		}
	}
	return lastSpan
}

// consumeUntilListBoundary advances the lexer through unrecognised tokens until it finds
// a list separator, terminator, or end-of-input.
//
// It is used as a recovery hook after parseListValue rejects a bad literal so subsequent
// calls keep producing useful diagnostics rather than cascading errors.
//
// Takes lexer (*Lexer) which is advanced to the next list boundary.
func consumeUntilListBoundary(lexer *Lexer) {
	for {
		peek := lexer.Peek()
		if peek.Kind == TokenEOF || peek.Kind == tokenRBracket || peek.Kind == tokenComma || peek.Kind == tokenRParen {
			return
		}
		lexer.Next()
	}
}

// parseDottedValue reads the rest of a value that may continue as a dotted chain, such as
// public.events."UserID", joining its parts into one raw value.
//
// String parts keep their double quotes, with embedded quotes doubled, so readers can
// distinguish quoted names from unquoted ones.
//
// Takes lexer (*Lexer) which is positioned just after the chain's first part.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostics.
// Takes firstSpan (diagbuild.TextSpan) which covers the first part.
// Takes first (string) which is the first part as the raw value writes it.
//
// Returns ParsedValue which is the parsed value.
// Returns diagbuild.TextSpan which covers the value tokens.
// Returns []diag.SourceError which holds a diagnostic for a dot not followed by a part.
func parseDottedValue(lexer *Lexer, errorBuilder diagbuild.ErrorBuilder, firstSpan diagbuild.TextSpan, first string) (ParsedValue, diagbuild.TextSpan, []diag.SourceError) {
	var diagnostics []diag.SourceError
	var raw strings.Builder
	raw.WriteString(first)
	span := firstSpan
	for lexer.Peek().Kind == TokenDot {
		lexer.Next()
		tail := lexer.Next()
		switch tail.Kind {
		case TokenIdentifier, tokenNumber:
			raw.WriteByte('.')
			raw.WriteString(tail.Lexeme)
		case tokenString:
			raw.WriteByte('.')
			raw.WriteString(quoteNamePart(tail.Lexeme))
		default:
			diagnostics = append(diagnostics, errorBuilder.DirectiveSyntax(tail.Span, "expected identifier after '.'"))
			return ParsedValue{Raw: raw.String(), AsList: nil, ListSpan: diagbuild.TextSpan{}, IsList: false}, span, diagnostics
		}
		span = MergeSpan(span, tail.Span)
	}
	return ParsedValue{Raw: raw.String(), AsList: nil, ListSpan: diagbuild.TextSpan{}, IsList: false}, span, diagnostics
}

// quoteNamePart writes a part of a dotted name in double quotes, doubling any quote
// inside it, as SplitQualifiedName reads a quoted part.
//
// Takes name (string) which is the part's text.
//
// Returns string which is the quoted part.
func quoteNamePart(name string) string {
	return string(identifierQuote) + strings.ReplaceAll(name, string(identifierQuote), `""`) + string(identifierQuote)
}
