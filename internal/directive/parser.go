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

package directive

import (
	"fmt"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// literalTrue is the canonical boolean-true marker used in directive value validation.
	// The parser emits this exact string when normalising keyword argument values, so
	// concentrating it here keeps the emitted text consistent across producers and
	// consumers.
	literalTrue = "true"

	// literalFalse is the boolean-false counterpart to literalTrue.
	literalFalse = "false"

	// asKeywordLookahead is the number of bytes the parameter-binding detector reads past
	// the start of " AS " before deciding the line is a binding rather than free-form prose.
	asKeywordLookahead = 3

	// maxLogicalLineCount caps the logical directive lines a query block may produce.
	//
	// The collector is otherwise bounded only by block.sql size, so this defence-in-depth
	// ceiling stops a pathological or hostile input from forcing unbounded parsing work. The
	// cap is well above any realistic directive header.
	maxLogicalLineCount = 10_000

	// maxContinuationLineCount caps the continuation lines a logical line may absorb.
	//
	// The cap applies while the logical line's paren depth stays open. Without it, an
	// unclosed paren would pull every following comment line into one logical line, so the
	// cap mirrors maxLogicalLineCount as a per-line defence-in-depth ceiling. The limit is
	// well above any realistic multi-line directive header.
	maxContinuationLineCount = 10_000
)

// PrefixPolicy configures the directive prefixes a Parser recognises and how it reports
// them.
type PrefixPolicy struct {
	// Prefixes holds the canonical prefix, its aliases and the foreign prefixes.
	Prefixes namespace.Set

	// RequireCanonical reports every directive written with an alias prefix as a Q050 hint,
	// so a codebase can converge on the canonical spelling.
	RequireCanonical bool
}

// Parser parses directive comment blocks into structured representations.
//
// The function-call grammar covers a top header such as "-- sgl.query(name: Foo, command:
// one)", a parameter binding such as "-- $1 as sgl.param(status)", and an embed such as
// "-- sgl.embed(orders, from: o)". Every shape is accepted with the canonical prefix or
// any alias; a foreign prefix is reported and otherwise ignored. Multi-line continuation
// is honoured for any directive call: while paren depth remains open, subsequent
// comment-prefixed lines are joined onto the same logical directive.
type Parser struct {
	// sigils lists the dialect's parameter forms an anchor may take.
	sigils []dialect.PlaceholderSigil

	// sigilForms lists the same forms in the shape the directive lexer scans.
	sigilForms []syntax.SigilForm

	// commentStyle holds the comment syntax used by the SQL engine.
	commentStyle ast.CommentStyle

	// policy holds the directive prefixes the parser recognises.
	policy PrefixPolicy
}

// NewParser constructs a parser bound to the engine's parameter prefixes, comment style
// and the run's directive prefixes.
//
// Takes sigils ([]dialect.PlaceholderSigil) which are the dialect's parameter forms a
// binding's anchor may take.
// Takes commentStyle (ast.CommentStyle) which is the engine's comment syntax.
// Takes policy (PrefixPolicy) which names the directive prefixes to recognise.
//
// Returns *Parser which is the configured parser.
func NewParser(
	sigils []dialect.PlaceholderSigil,
	commentStyle ast.CommentStyle,
	policy PrefixPolicy,
) *Parser {
	forms := make([]syntax.SigilForm, 0, len(sigils))
	for _, sigil := range sigils {
		forms = append(forms, syntax.SigilForm{Prefix: sigil.Prefix, Suffix: sigil.Suffix})
	}
	return &Parser{
		commentStyle: commentStyle,
		sigils:       sigils,
		sigilForms:   forms,
		policy:       policy,
	}
}

// Parse turns a query block's directive comment lines into a structured DirectiveBlock
// plus any diagnostics produced along the way.
//
// The block is read under its lexical rules. The directive header is every line comment
// that begins its line with the engine comment prefix before the block's first SQL token;
// those are parsed. A directive call after the SQL has started is not parsed: an unknown
// one, such as a misspelt query header, is reported as an unknown directive, and a known
// one as ignored. When the header's first directive is unknown, the missing name and
// command are not reported as well, since the unknown directive is most likely the
// misspelt query header.
//
// Takes block (sqlfile.QueryBlock) which holds the SQL text and its starting line number.
// Takes filename (string) which is the source file name used for diagnostics.
//
// Returns *DirectiveBlock which is the parsed directive header.
// Returns []diag.SourceError which are the diagnostics gathered during parsing.
func (p *Parser) Parse(
	block sqlfile.QueryBlock,
	filename string,
) (*DirectiveBlock, []diag.SourceError) {
	result := &DirectiveBlock{}
	errorBuilder := diagbuild.NewErrorBuilder(filename)
	comments := p.readBlockComments(block)
	logicalLines, limits := p.collectLogicalLines(&comments)
	diagnostics := limitDiagnostics(limits, block.StartLine, errorBuilder)
	sawTopCall := false
	unknownOpener := false
	firstLine, lastLine := -1, -1

	for index, logical := range logicalLines {
		if firstLine == -1 {
			firstLine = logical.StartLine
		}
		lastLine = logical.EndLine

		reported := len(diagnostics)
		if p.parseLogicalLine(logical, result, &diagnostics, errorBuilder) {
			sawTopCall = true
		}
		if index == 0 && reportsCode(diagnostics[reported:], diag.CodeUnknownDirective) {
			unknownOpener = true
		}
	}
	diagnostics = append(diagnostics, p.trailingDirectiveDiagnostics(&comments, errorBuilder)...)

	result.Description = p.description(&comments)
	setBlockSpan(result, firstLine, lastLine, block)
	if !sawTopCall && !unknownOpener {
		validateRequiredDirectives(result, &diagnostics, errorBuilder, block.StartLine, p.policy.Prefixes.Canonical)
	}
	return result, diagnostics
}

// parseLogicalLine dispatches one logical directive line to the appropriate sub-parser
// based on its leading token shape.
//
// Takes logical (syntax.LogicalLine) which is the directive line to dispatch.
// Takes result (*DirectiveBlock) which accumulates the parsed directives.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns bool which is true when the line was a top-level query call, so the outer loop
// can suppress the redundant required-keyword argument validation that fires when no top
// call was parsed at all.
func (p *Parser) parseLogicalLine(
	logical syntax.LogicalLine,
	result *DirectiveBlock,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) bool {
	if logical.UnterminatedStringOffset >= 0 {
		lexer := syntax.NewLexer(logical)
		span := lexer.SpanAt(logical.UnterminatedStringOffset, logical.UnterminatedStringOffset+1)
		*diagnostics = append(*diagnostics, errorBuilder.UnterminatedString(span, string(logical.UnterminatedStringQuote)))
		return false
	}

	lexer := syntax.NewLexer(logical, p.sigilForms...)
	first := lexer.Peek()
	if first.Kind == syntax.TokenEOF {
		return false
	}

	if sigil, ok := p.anchorSigil(first.Lexeme); ok && p.looksLikeParameterBinding(logical.Content) {
		p.parseParameterBinding(lexer, sigil, result, diagnostics, errorBuilder)
		return false
	}

	if first.Kind != syntax.TokenIdentifier {
		return false
	}
	switch p.policy.Prefixes.Match(first.Lexeme) {
	case namespace.KindCanonical, namespace.KindAlias:
		return p.parsePrefixedCall(lexer, result, diagnostics, errorBuilder)
	case namespace.KindForeign:
		p.reportForeignCall(lexer, lexer.Next(), diagnostics, errorBuilder)
	case namespace.KindNone:
	}
	return false
}

// looksLikeParameterBinding peeks at the content to confirm the line resembles
// `<sigil><tail> as sgl.X(...)` rather than free-form prose.
//
// Takes content (string) which is the logical line content to inspect.
//
// Returns bool which is true when the line resembles a parameter binding.
func (*Parser) looksLikeParameterBinding(content string) bool {
	upper := strings.ToUpper(content)
	for index := 0; index+asKeywordLookahead < len(upper); index++ {
		if upper[index] == ' ' && upper[index+1] == 'A' && upper[index+2] == 'S' &&
			(upper[index+asKeywordLookahead] == ' ' || upper[index+asKeywordLookahead] == '\t') {
			return true
		}
	}
	return false
}

// anchorSigil returns the dialect's parameter form that a line's first token is written
// in, choosing the form with the longest prefix the token opens with. When a numbered or
// anonymous form and a named form share that prefix, a digit after it chooses the former
// and anything else the latter.
//
// Takes lexeme (string) which is the first token's text.
//
// Returns dialect.PlaceholderSigil which is the matching form.
// Returns bool which is false when the token opens no form.
func (p *Parser) anchorSigil(lexeme string) (dialect.PlaceholderSigil, bool) {
	var chosen dialect.PlaceholderSigil
	found := false
	for _, sigil := range p.sigils {
		if sigil.Prefix == "" || !strings.HasPrefix(lexeme, sigil.Prefix) {
			continue
		}
		if !found || len(sigil.Prefix) > len(chosen.Prefix) ||
			(len(sigil.Prefix) == len(chosen.Prefix) && fitsAnchor(sigil, lexeme) && !fitsAnchor(chosen, lexeme)) {
			chosen, found = sigil, true
		}
	}
	return chosen, found
}

// looksLikeDirective reports whether a stripped comment line begins a directive.
//
// collectLogicalLines uses it as the single gate deciding whether a leading comment line
// is parsed as a directive (scanned for paren depth, joined with continuation lines,
// checked for an unterminated string) or skipped as a free-form SQL comment owned by the
// SQL author.
//
// A directive takes one of two shapes. The first is a prefixed call: the leading token is
// a bare prefix identifier (canonical, alias or foreign) that continues with "(" or ".",
// as in sgl(...) or sgl.embed(...). A comment that only starts with the word "sgl" (such
// as "sgl keeps this fast") does not continue that way and is prose. The second is a
// parameter binding: the leading token is written in one of the dialect's parameter forms
// and the line carries an " as " keyword, as in "$1 as sgl.param(status)". Foreign calls
// are collected so the dispatcher can report them. The check reuses the lexer and the
// predicates the dispatcher applies, so a line accepted here is the same line
// parseLogicalLine would route to a sub-parser.
//
// Takes content (string) which is one comment line with its prefix already stripped.
//
// Returns bool reporting whether the line should be parsed as a directive.
func (p *Parser) looksLikeDirective(content string) bool {
	lexer := syntax.NewLexer(syntax.LogicalLine{
		Content:                  content,
		Segments:                 nil,
		StartLine:                0,
		EndLine:                  0,
		UnterminatedStringOffset: 0,
		UnterminatedStringQuote:  0,
	}, p.sigilForms...)
	first := lexer.Peek()
	if first.Kind == syntax.TokenEOF {
		return false
	}
	if first.Kind == syntax.TokenIdentifier && p.policy.Prefixes.Match(first.Lexeme) != namespace.KindNone {
		second := lexer.PeekN(1)
		return second.Kind == syntax.TokenLParen || second.Kind == syntax.TokenDot
	}
	if _, isSigil := p.anchorSigil(first.Lexeme); isSigil && p.looksLikeParameterBinding(content) {
		return true
	}
	return false
}

// parsePrefixedCall parses a header-level directive call written with the canonical
// prefix or an alias.
//
// It handles forms such as `-- sgl.query(...)` and `-- sgl.embed(...)`. The top-level
// header is `sgl.query`; a bare `sgl(...)` (no `.role` suffix) is not a registered
// directive and surfaces an UnknownDirective diagnostic. Diagnostics always render the
// canonical prefix, whichever prefix the source used.
//
// Takes lexer (*syntax.Lexer) which supplies the directive tokens.
// Takes result (*DirectiveBlock) which accumulates the parsed directives.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns bool which is true when the call resolved to the top-level header, letting the
// caller suppress redundant missing-required diagnostics from validateRequiredDirectives.
func (p *Parser) parsePrefixedCall(
	lexer *syntax.Lexer,
	result *DirectiveBlock,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) bool {
	prefixes := p.policy.Prefixes
	nameToken := lexer.Next()
	p.reportAliasPrefix(nameToken, diagnostics, errorBuilder)
	role := ""
	directiveName := prefixes.Canonical
	directiveSpan := nameToken.Span

	if dot := lexer.Peek(); dot.Kind == syntax.TokenDot {
		lexer.Next()
		roleToken := lexer.Next()
		if roleToken.Kind != syntax.TokenIdentifier {
			*diagnostics = append(*diagnostics, errorBuilder.DirectiveSyntax(roleToken.Span, "expected directive name after '"+prefixes.Canonical+".'"))
			return false
		}
		role = roleToken.Lexeme
		directiveName = prefixes.Qualify(role)
		directiveSpan = syntax.MergeSpan(nameToken.Span, roleToken.Span)
	}

	spec, found := lookupDirective(role)
	if !found {
		*diagnostics = append(*diagnostics, errorBuilder.UnknownDirective(directiveSpan, directiveName, directiveNames(prefixes)))
		return false
	}

	call, callErrors := syntax.ParseCallArguments(lexer, errorBuilder, directiveName)
	*diagnostics = append(*diagnostics, callErrors...)
	if call == nil {
		return false
	}
	reportTrailingText(lexer, diagnostics, errorBuilder, directiveName)

	specErrors := validateCall(spec, call, errorBuilder, directiveName)
	*diagnostics = append(*diagnostics, specErrors...)

	switch spec.Role {
	case DirectiveRoleTop:
		applyTopCall(result, call)
		return true
	case DirectiveRoleHeader:
		applyHeaderCall(result, spec, call, errorBuilder, diagnostics, directiveName)
	case DirectiveRoleMigration:
		wrongContext := errorBuilder.At(directiveSpan, diag.CodeDirectiveWrongContext,
			directiveName+" is a migration directive and is ignored in a query file; use "+prefixes.Qualify(RoleQuery)+" for query headers")
		wrongContext.Severity = diag.SeverityWarning
		*diagnostics = append(*diagnostics, wrongContext)
	case DirectiveRoleParam:
	}
	return false
}

// parseParameterBinding parses `-- $N as sgl.X(name, kw: v, ...)`.
//
// Takes lexer (*syntax.Lexer) which supplies the directive tokens.
// Takes sigil (dialect.PlaceholderSigil) which is the form of the anchor.
// Takes result (*DirectiveBlock) which accumulates the parsed directives.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
func (p *Parser) parseParameterBinding(
	lexer *syntax.Lexer,
	sigil dialect.PlaceholderSigil,
	result *DirectiveBlock,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) {
	anchorToken := lexer.Next()
	tail, number, anchorOk := parseParameterAnchor(anchorToken, sigil, diagnostics, errorBuilder)
	if !anchorOk {
		return
	}

	prefixToken, asOk := p.consumeAsPrefixDot(lexer, diagnostics, errorBuilder)
	if !asOk {
		return
	}

	prefixes := p.policy.Prefixes
	roleToken, roleOk := consumeRoleToken(lexer, diagnostics, errorBuilder, prefixes)
	if !roleOk {
		return
	}
	directiveName := prefixes.Qualify(roleToken.Lexeme)
	kindSpan := syntax.MergeSpan(prefixToken.Span, roleToken.Span)

	spec, found := lookupDirective(roleToken.Lexeme)
	if !found || spec.Role != DirectiveRoleParam {
		*diagnostics = append(*diagnostics, errorBuilder.UnknownDirective(roleToken.Span, directiveName, paramDirectiveNames(prefixes)))
		return
	}

	named := sigil.Kind == dialect.PlaceholderNamed
	call, callErrors := resolveParameterCall(lexer, named, spec, anchorToken, tail, errorBuilder, directiveName)
	*diagnostics = append(*diagnostics, callErrors...)
	if call == nil {
		return
	}
	reportTrailingText(lexer, diagnostics, errorBuilder, directiveName)

	specErrors := validateCall(spec, call, errorBuilder, directiveName)
	*diagnostics = append(*diagnostics, specErrors...)

	if number == 0 && named {
		bound := 0
		for _, existing := range result.Parameters {
			if existing.Kind != sqlmodel.ParameterDirectiveSortable {
				bound++
			}
		}
		number = bound + 1
	}
	directive := buildParameterDirective(spec, call, anchorToken, kindSpan, named, number)
	if directive == nil {
		return
	}
	if named {
		directive.Anchor = tail
	}
	result.Parameters = append(result.Parameters, directive)
}

// consumeAsPrefixDot reads the `as sgl.` token sequence that follows a parameter anchor,
// accepting the canonical prefix or any alias.
//
// A foreign prefix after `as` is reported as a Q051 warning rather than a syntax error,
// and an alias is reported as a Q050 hint when the policy requires the canonical prefix.
//
// Takes lexer (*syntax.Lexer) which supplies the directive tokens.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns syntax.Token which is the consumed prefix token, used to build a `sgl.X` kind
// span.
// Returns bool which is false the moment the stream diverges from `as <prefix>.`.
func (p *Parser) consumeAsPrefixDot(lexer *syntax.Lexer, diagnostics *[]diag.SourceError, errorBuilder diagbuild.ErrorBuilder) (syntax.Token, bool) {
	prefixes := p.policy.Prefixes
	asToken := lexer.Next()
	if asToken.Kind != syntax.TokenIdentifier || !strings.EqualFold(asToken.Lexeme, "as") {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(asToken.Span, "'as' after parameter anchor", asToken.Lexeme, []string{"as"}))
		return syntax.Token{}, false
	}
	prefixToken := lexer.Next()
	if prefixToken.Kind == syntax.TokenIdentifier && prefixes.Match(prefixToken.Lexeme) == namespace.KindForeign {
		p.reportForeignCall(lexer, prefixToken, diagnostics, errorBuilder)
		return syntax.Token{}, false
	}
	if prefixToken.Kind != syntax.TokenIdentifier || !prefixes.Accepts(prefixToken.Lexeme) {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(prefixToken.Span, "'"+prefixes.Canonical+"' after 'as'", prefixToken.Lexeme, prefixes.Accepted()))
		return syntax.Token{}, false
	}
	p.reportAliasPrefix(prefixToken, diagnostics, errorBuilder)
	dotToken := lexer.Next()
	if dotToken.Kind != syntax.TokenDot {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(dotToken.Span, "'.' after '"+prefixes.Canonical+"'", dotToken.Lexeme, nil))
		return syntax.Token{}, false
	}
	return prefixToken, true
}

// reportAliasPrefix appends a Q050 hint when token is an alias prefix and the policy
// requires the canonical prefix.
//
// Takes token (syntax.Token) which is the prefix token as written.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
func (p *Parser) reportAliasPrefix(token syntax.Token, diagnostics *[]diag.SourceError, errorBuilder diagbuild.ErrorBuilder) {
	if !p.policy.RequireCanonical || p.policy.Prefixes.Match(token.Lexeme) != namespace.KindAlias {
		return
	}
	*diagnostics = append(*diagnostics, errorBuilder.AliasPrefix(token.Span, token.Lexeme, p.policy.Prefixes.Canonical))
}

// reportForeignCall appends a Q051 warning for a directive written with a foreign prefix.
//
// The role, when the prefix is followed by a dot and an identifier, is included in the
// reported name.
//
// Takes lexer (*syntax.Lexer) which supplies the tokens after the prefix.
// Takes prefixToken (syntax.Token) which is the foreign prefix already read from lexer.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
func (p *Parser) reportForeignCall(
	lexer *syntax.Lexer,
	prefixToken syntax.Token,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) {
	name := prefixToken.Lexeme
	span := prefixToken.Span
	if lexer.Peek().Kind == syntax.TokenDot && lexer.PeekN(1).Kind == syntax.TokenIdentifier {
		lexer.Next()
		roleToken := lexer.Next()
		name += "." + roleToken.Lexeme
		span = syntax.MergeSpan(prefixToken.Span, roleToken.Span)
	}
	*diagnostics = append(*diagnostics, errorBuilder.ForeignPrefix(span, p.commentStyle.LinePrefix, name, prefixToken.Lexeme))
}

// ExtractQueryDirectives folds the MetadataDirective entries on a DirectiveBlock, and its
// description, into the QueryDirectives the analyser reads.
//
// Takes block (*DirectiveBlock) whose Metadata entries are folded.
//
// Returns *QueryDirectives which is the assembled directive set.
func ExtractQueryDirectives(block *DirectiveBlock) *QueryDirectives {
	directives := &QueryDirectives{}
	directives.Description = block.Description
	for _, metadata := range block.Metadata {
		switch metadata.Directive {
		case "group_by":
			directives.GroupByKeys = append(directives.GroupByKeys, metadata.Value)
		case "nullable":
			setBoolOverride(&directives.NullableOverride, metadata.Value)
		case "readonly":
			setBoolOverride(&directives.ReadOnlyOverride, metadata.Value)
		case "optional":
			directives.Optional = strings.EqualFold(strings.TrimSpace(metadata.Value), literalTrue)
		case KeywordModel:
			directives.Model = strings.TrimSpace(metadata.Value)
		case KeywordRow:
			directives.Row = strings.TrimSpace(metadata.Value)
		case "runtime":

			switch strings.ToLower(strings.TrimSpace(metadata.Value)) {
			case literalTrue:
				directives.DynamicRuntime = true
			case literalFalse:
				directives.DynamicRuntime = false
			}
		case "dynamic":
			if strings.EqualFold(strings.TrimSpace(metadata.Value), "runtime") {
				directives.DynamicRuntime = true
			}
		}
	}
	return directives
}

// reportsCode reports whether any of the diagnostics carries a code.
//
// Takes diagnostics ([]diag.SourceError) which are the diagnostics to search.
// Takes code (string) which is the code to look for.
//
// Returns bool which is true when one of them carries the code.
func reportsCode(diagnostics []diag.SourceError, code string) bool {
	for index := range diagnostics {
		if diagnostics[index].Code == code {
			return true
		}
	}
	return false
}

// reportTrailingText reports the text a directive line holds after its call, such as the
// int in `-- $1 as sgl.param(id) int`, which would otherwise be silently ignored (Q007).
//
// Takes lexer (*syntax.Lexer) which is positioned just after the call.
// Takes diagnostics (*[]diag.SourceError) which collects the diagnostic.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs it.
// Takes directiveName (string) which names the directive in the message.
func reportTrailingText(lexer *syntax.Lexer, diagnostics *[]diag.SourceError, errorBuilder diagbuild.ErrorBuilder, directiveName string) {
	if trailing, found := syntax.TrailingTextDiagnostic(lexer, errorBuilder, directiveName); found {
		*diagnostics = append(*diagnostics, trailing)
	}
}

// parseParameterAnchor extracts the identifier tail and number from a parameter anchor.
//
// For a named anchor (`:email`, or `{email:String}` for a form that encloses the name and
// type) the tail is the identifier; for a numbered or anonymous form (`$1`, `?1`, `@p1`)
// the tail must be a positive integer naming the placeholder by its position.
//
// Takes anchorToken (syntax.Token) which is the parameter-anchor token.
// Takes sigil (dialect.PlaceholderSigil) which is the form of the anchor.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns string which is the identifier tail.
// Returns int which is the parsed number, zero for named anchors.
// Returns bool which is false when a diagnostic was emitted and the binding should be
// abandoned.
func parseParameterAnchor(
	anchorToken syntax.Token,
	sigil dialect.PlaceholderSigil,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) (string, int, bool) {
	anchorLex := anchorToken.Lexeme
	if !strings.HasPrefix(anchorLex, sigil.Prefix) {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(anchorToken.Span, "parameter prefix", anchorLex, nil))
		return "", 0, false
	}
	tail := anchorLex[len(sigil.Prefix):]
	if sigil.Kind == dialect.PlaceholderNamed {
		return parseNamedAnchor(anchorToken, sigil, tail, diagnostics, errorBuilder)
	}
	parsed, parseErr := strconv.Atoi(tail)
	if parseErr != nil || parsed <= 0 {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(anchorToken.Span, fmt.Sprintf("positive integer after %q", sigil.Prefix), tail, nil))
		return "", 0, false
	}
	return tail, parsed, true
}

// parseNamedAnchor extracts the identifier of a named anchor from the text after its
// prefix, dropping the form's suffix and, for a form that carries the parameter's type,
// everything from the type separator on.
//
// Takes anchorToken (syntax.Token) which is the parameter-anchor token.
// Takes sigil (dialect.PlaceholderSigil) which is the named form of the anchor.
// Takes tail (string) which is the anchor's text after the prefix.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns string which is the identifier.
// Returns int which is always zero, as a named anchor has no number.
// Returns bool which is false when a diagnostic was emitted and the binding should be
// abandoned.
func parseNamedAnchor(
	anchorToken syntax.Token,
	sigil dialect.PlaceholderSigil,
	tail string,
	diagnostics *[]diag.SourceError,
	errorBuilder diagbuild.ErrorBuilder,
) (string, int, bool) {
	if sigil.Suffix == "" && sigil.TypeSeparator == "" {
		if tail == "" {
			*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(anchorToken.Span, "identifier after parameter prefix", anchorToken.Lexeme, nil))
			return "", 0, false
		}
		return tail, 0, true
	}
	name := strings.TrimSuffix(tail, sigil.Suffix)
	if sigil.TypeSeparator != "" {
		name, _, _ = strings.Cut(name, sigil.TypeSeparator)
	}
	name = strings.TrimSpace(name)
	if !isLegalParameterName(name) {
		expected := "identifier in '" + sigil.Render(0, "name", "Type") + "'"
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(anchorToken.Span, expected, anchorToken.Lexeme, nil))
		return "", 0, false
	}
	return name, 0, true
}

// isLegalParameterName reports whether name is a valid bare parameter identifier (ASCII
// letter or underscore start, then letters/digits/underscores). Used to reject a
// malformed enclosing anchor, such as a brace form, before its corrupted text reaches
// name conversion.
//
// Takes name (string) which is the candidate parameter identifier.
//
// Returns bool which is true when name is a legal identifier.
func isLegalParameterName(name string) bool {
	if name == "" || !syntax.IsIdentifierStart(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !syntax.IsIdentifierPart(name[index]) {
			return false
		}
	}
	return true
}

// consumeRoleToken reads the role identifier that completes `sgl.X`.
//
// Takes lexer (*syntax.Lexer) which supplies the directive tokens.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
// Takes prefixes (namespace.Set) whose canonical prefix the diagnostics render.
//
// Returns syntax.Token which is the role token when present.
// Returns bool which is false when the role slot did not contain an identifier.
func consumeRoleToken(lexer *syntax.Lexer, diagnostics *[]diag.SourceError, errorBuilder diagbuild.ErrorBuilder, prefixes namespace.Set) (syntax.Token, bool) {
	roleToken := lexer.Next()
	if roleToken.Kind != syntax.TokenIdentifier {
		*diagnostics = append(*diagnostics, errorBuilder.ParameterBindingSyntax(roleToken.Span, "role name after '"+prefixes.Canonical+".'", roleToken.Lexeme, paramDirectiveNames(prefixes)))
		return syntax.Token{}, false
	}
	return roleToken, true
}

// resolveParameterCall returns the parsed call arguments for a parameter binding.
//
// A named placeholder does not have to restate its identifier when the spec's first
// positional is the name. When the source omits the parenthesised argument list a minimal
// call is synthesised from the anchor tail; when the list is present but gives no name,
// positionally or as `name:`, the anchor tail is inserted as the name positional, so
// `:title as sgl.param(optional: true)` names the parameter title as a bare `:title as
// sgl.param` does.
//
// Takes lexer (*syntax.Lexer) which supplies the directive tokens.
// Takes named (bool) which is true when the anchor is a named form.
// Takes spec (*DirectiveSpec) which describes the directive being parsed.
// Takes anchorToken (syntax.Token) which is the parameter-anchor token.
// Takes tail (string) which is the anchor tail used for the synthesised name positional.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
// Takes directiveName (string) which names the directive for diagnostics.
//
// Returns *syntax.CallArguments which is the parsed or synthesised call arguments.
// Returns []diag.SourceError which are the diagnostics from parsing the call.
func resolveParameterCall(
	lexer *syntax.Lexer,
	named bool,
	spec *DirectiveSpec,
	anchorToken syntax.Token,
	tail string,
	errorBuilder diagbuild.ErrorBuilder,
	directiveName string,
) (*syntax.CallArguments, []diag.SourceError) {
	infersName := named && len(spec.Positionals) > 0 && spec.Positionals[0].Name == keywordName
	if lexer.Peek().Kind != syntax.TokenLParen && infersName {
		return &syntax.CallArguments{
			Positionals:      []syntax.ParsedPositional{anchorNamePositional(anchorToken, tail)},
			OpenSpan:         anchorToken.Span,
			CloseSpan:        anchorToken.Span,
			Closed:           true,
			KeywordArguments: nil,
		}, nil
	}
	call, diagnostics := syntax.ParseCallArguments(lexer, errorBuilder, directiveName)
	if call != nil && infersName && !callNamesParameter(call) {
		call.Positionals = append([]syntax.ParsedPositional{anchorNamePositional(anchorToken, tail)}, call.Positionals...)
	}
	return call, diagnostics
}

// anchorNamePositional builds the name positional a named placeholder implies, spanning
// the anchor so a diagnostic about the name points at the placeholder.
//
// Takes anchorToken (syntax.Token) which is the parameter-anchor token.
// Takes tail (string) which is the anchor's identifier.
//
// Returns syntax.ParsedPositional which is the synthesised name positional.
func anchorNamePositional(anchorToken syntax.Token, tail string) syntax.ParsedPositional {
	return syntax.ParsedPositional{
		Value: syntax.ParsedValue{
			Raw:      tail,
			AsList:   nil,
			ListSpan: diagbuild.TextSpan{},
			IsList:   false,
		},
		Span:   anchorToken.Span,
		Failed: false,
	}
}

// callNamesParameter reports whether a parsed call names its parameter, through a first
// positional or a `name:` keyword argument.
//
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
//
// Returns bool which is true when the call supplies a name.
func callNamesParameter(call *syntax.CallArguments) bool {
	if len(call.Positionals) > 0 {
		return true
	}
	for index := range call.KeywordArguments {
		if call.KeywordArguments[index].Key == keywordName {
			return true
		}
	}
	return false
}

// setBlockSpan stretches result.Span to cover the inclusive physical line range spanned
// by the directive header.
//
// Takes result (*DirectiveBlock) whose Span is widened in place.
// Takes firstLine (int) which is the first directive line, or -1 when none exist.
// Takes lastLine (int) which is the last directive line.
// Takes block (sqlfile.QueryBlock) which supplies the SQL text for end-column resolution.
func setBlockSpan(result *DirectiveBlock, firstLine, lastLine int, block sqlfile.QueryBlock) {
	if firstLine == -1 {
		return
	}
	endLineIndex := lastLine - block.StartLine
	endColumn := 1
	index := 0
	for line := range strings.SplitSeq(block.SQL, "\n") {
		if index == endLineIndex {
			endColumn = len(line) + 1
			break
		}
		index++
	}
	result.Span = diagbuild.TextSpan{
		Line:      firstLine,
		Column:    1,
		EndLine:   lastLine,
		EndColumn: endColumn,
	}
}

// validateRequiredDirectives emits the missing-name and missing-command diagnostics when
// no top-level `sgl.query(...)` call appeared in the block.
//
// The diagnostics are anchored to the block's first line so the user sees the file
// location the directive header was expected to start at.
//
// Takes result (*DirectiveBlock) which is inspected for the required fields.
// Takes diagnostics (*[]diag.SourceError) which collects emitted diagnostics.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
// Takes startLine (int) which is the block's first line used to anchor the diagnostics.
// Takes canonical (string) which is the canonical prefix the messages render.
func validateRequiredDirectives(result *DirectiveBlock, diagnostics *[]diag.SourceError, errorBuilder diagbuild.ErrorBuilder, startLine int, canonical string) {
	span := diagbuild.TextSpan{Line: startLine, Column: 1, EndLine: startLine, EndColumn: 1}
	if result.Name == nil {
		*diagnostics = append(*diagnostics, errorBuilder.At(span, diag.CodeMissingDirective, "missing 'name' keyword argument on "+canonical+"(...)"))
	}
	if result.Command == nil {
		*diagnostics = append(*diagnostics, errorBuilder.At(span, diag.CodeMissingDirective, "missing 'command' keyword argument on "+canonical+"(...)"))
	}
}

// parseCommandValue maps a directive's raw command identifier to its QueryCommand value.
//
// It handles values such as "one" and "many", and returns an error for values the spec
// does not recognise so the caller can decide whether to surface it as a diagnostic or
// fall back to defaults.
//
// Takes raw (string) which is the command identifier from the directive.
//
// Returns sqlmodel.QueryCommand which is the mapped command value.
// Returns error when the command identifier is not recognised.
func parseCommandValue(raw string) (sqlmodel.QueryCommand, error) {
	switch strings.ToLower(raw) {
	case "one":
		return sqlmodel.QueryCommandOne, nil
	case "many":
		return sqlmodel.QueryCommandMany, nil
	case "exec":
		return sqlmodel.QueryCommandExec, nil
	case "execresult":
		return sqlmodel.QueryCommandExecResult, nil
	case "execrows":
		return sqlmodel.QueryCommandExecRows, nil
	case "batch":
		return sqlmodel.QueryCommandBatch, nil
	case "stream":
		return sqlmodel.QueryCommandStream, nil
	case "copyfrom":
		return sqlmodel.QueryCommandCopyFrom, nil
	case "asyncexec":
		return sqlmodel.QueryCommandAsyncExec, nil
	default:
		return 0, fmt.Errorf("unknown command %q", raw)
	}
}

// setBoolOverride parses a "true" / "false" string into a *bool pointer, leaving the
// pointer nil for any other value.
//
// The comparison is case-insensitive to match the validator (which lowercases the value
// before checking), so `nullable: TRUE` is honoured rather than silently dropped.
//
// Takes target (**bool) which receives a pointer to the parsed boolean.
// Takes value (string) which is the raw override text.
func setBoolOverride(target **bool, value string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case literalTrue:
		*target = new(true)
	case literalFalse:
		*target = new(false)
	}
}

// paramDirectiveNames returns the names of every directive whose role is Param, qualified
// with the canonical prefix, used to populate the "did you mean" candidates emitted when
// an unknown directive is referenced from a parameter binding.
//
// Takes prefixes (namespace.Set) whose canonical prefix qualifies each name.
//
// Returns []string which are the qualified param-role directive names.
func paramDirectiveNames(prefixes namespace.Set) []string {
	var names []string
	for _, spec := range directiveSpecs {
		if spec.Role == DirectiveRoleParam {
			names = append(names, prefixes.Qualify(spec.Name))
		}
	}
	return names
}

// fitsAnchor reports whether the text after a form's prefix suits the form: a digit for a
// numbered or anonymous form, anything else for a named one.
//
// Takes sigil (dialect.PlaceholderSigil) which is the form.
// Takes lexeme (string) which is the anchor's text, opening with the form's prefix.
//
// Returns bool which is true when the text suits the form.
func fitsAnchor(sigil dialect.PlaceholderSigil, lexeme string) bool {
	rest := lexeme[len(sigil.Prefix):]
	startsWithDigit := rest != "" && syntax.IsDigit(rest[0])
	return startsWithDigit == (sigil.Kind != dialect.PlaceholderNamed)
}
