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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// maxMigrationDiagnostics is the most diagnostics ParseMigration reports for one
	// migration file; the rest are summarised in one diagnostic that says how many were left
	// out.
	maxMigrationDiagnostics = 256
)

var (
	// queryOnlyRoles lists the directive roles that belong in a query file's header and mean
	// nothing in a migration, besides the query header itself, which
	// MigrationDirectives.MisplacedQueryLines lists.
	queryOnlyRoles = []string{"param", "sortable", "embed"}
)

// migrationParse is the state of one ParseMigration call.
type migrationParse struct {
	// pending is the read-only value a directive gave the next statement, or nil.
	pending *bool

	// limiter keeps the diagnostics found so far, up to maxMigrationDiagnostics.
	limiter *diagbuild.Limiter

	// transactionProblem is the first error found in a sgl.migration directive, or in a
	// directive whose role is unknown, either of which may have meant to say whether the
	// migration runs inside a transaction.
	transactionProblem *diag.SourceError

	// content is the migration file content.
	content string

	// prefixes names the directive prefixes to recognise.
	prefixes namespace.Set

	// lines indexes the content's lines.
	lines LineIndex

	// tokens holds the content's tokens.
	tokens []sqllex.Token

	// linePrefix opens a directive comment.
	linePrefix string

	// errorBuilder renders the diagnostics against the migration's file name.
	errorBuilder diagbuild.ErrorBuilder

	// result collects what the well-formed directives say.
	result MigrationDirectives

	// transactionProblems counts the errors transactionProblem is the first of.
	transactionProblems int
}

// comment reads one comment token, recording what a directive comment says and reporting
// what is wrong with it.
//
// Takes index (int) which is the index of the comment token.
func (parse *migrationParse) comment(index int) {
	parse.pending = nil
	token := parse.tokens[index]
	comment, isDirectiveComment := ReadDirectiveComment(parse.content, parse.lines, token, parse.linePrefix)
	if !isDirectiveComment {
		return
	}
	line := parse.lines.Line(token.Start)
	if foreign, isForeign := ForeignDirectiveCall(strings.TrimSpace(comment.Body), parse.prefixes); isForeign {
		foreign.Line = line
		parse.result.Foreign = append(parse.result.Foreign, foreign)
		return
	}
	prefix, role, rest := DirectiveName(comment.Body)
	kind := parse.prefixes.Match(prefix)
	if (kind != namespace.KindCanonical && kind != namespace.KindAlias) || !OpensCall(rest) {
		return
	}
	switch role {
	case roleQuery:
		parse.result.MisplacedQueryLines = append(parse.result.MisplacedQueryLines, line)
	case roleMigration:
		if call, parsed := parse.readCall(index, comment, migrationCallSpec, roleMigration); parsed {
			parse.applyMigrationCall(call)
		}
	case roleColumn:
		if call, parsed := parse.readCall(index, comment, columnCallSpec, roleColumn); parsed {
			parse.applyColumnCall(call, line)
		}
	default:
		parse.reportOtherRole(comment, role)
	}
}

// readCall parses and validates the directive call a comment holds.
//
// A call is read from its own line. A string left open on the line is reported, and so is
// a call left open whose directive continues on the line below, which a migration file
// does not read; neither is parsed further. Text after the call is reported too. Only a
// call that parsed cleanly is applied; one whose arguments are well formed but wrong is
// applied for the arguments that are right.
//
// Takes index (int) which is the index of the comment token.
// Takes comment (DirectiveComment) which is the directive comment.
// Takes spec (CallSpec) which declares the arguments the directive accepts.
// Takes role (string) which is the directive's role.
//
// Returns migrationCall which holds the parsed call.
// Returns bool which is false when the call could not be parsed cleanly, so it is not
// applied.
func (parse *migrationParse) readCall(index int, comment DirectiveComment, spec CallSpec, role string) (migrationCall, bool) {
	directiveName := parse.prefixes.Qualify(role)
	transaction := role == roleMigration
	lexer := NewLexer(parse.logicalLine(comment))
	depth, unterminated, quote := LineDepth(comment.Body)
	if unterminated >= 0 {
		parse.report(transaction, parse.errorBuilder.UnterminatedString(lexer.SpanAt(unterminated, unterminated+1), string(quote)))
		return migrationCall{call: nil, name: directiveName}, false
	}
	skipDirectiveName(lexer)
	if depth > 0 && parse.continuesBelow(index) {
		parse.report(transaction, parse.errorBuilder.At(lexer.Peek().Span, diag.CodeUnclosedDirective, fmt.Sprintf(
			"%s(...) continues on the next line, but a migration file reads each directive from its own line; "+
				"write the whole call on one line", directiveName)))
		return migrationCall{call: nil, name: directiveName}, false
	}
	call, callDiagnostics := ParseCallArguments(lexer, parse.errorBuilder, directiveName)
	parse.report(transaction, callDiagnostics...)
	if call == nil {
		return migrationCall{call: nil, name: directiveName}, false
	}
	trailing, trailed := TrailingTextDiagnostic(lexer, parse.errorBuilder, directiveName)
	if trailed {
		parse.report(transaction, trailing)
	}
	parse.report(transaction, ValidateCall(&spec, call, parse.errorBuilder, directiveName)...)
	return migrationCall{call: call, name: directiveName}, call.Closed && len(callDiagnostics) == 0 && !trailed
}

// report keeps diagnostics found in the migration's directives, noting the first error of
// a directive that may have meant to say whether the migration runs inside a transaction.
//
// Takes transaction (bool) which is true when the diagnostics are about a sgl.migration
// directive or a directive whose role is unknown.
// Takes diagnostics (...diag.SourceError) which are the diagnostics.
func (parse *migrationParse) report(transaction bool, diagnostics ...diag.SourceError) {
	parse.limiter.Add(diagnostics...)
	if !transaction {
		return
	}
	for index := range diagnostics {
		if diagnostics[index].Severity != diag.SeverityError {
			continue
		}
		if parse.transactionProblem == nil {
			parse.transactionProblem = new(diagnostics[index])
		}
		parse.transactionProblems++
	}
}

// logicalLine makes the logical line a directive comment's call is read from: the
// comment's text, mapped back to its line.
//
// Takes comment (DirectiveComment) which is the directive comment.
//
// Returns LogicalLine which holds the comment's text.
func (parse *migrationParse) logicalLine(comment DirectiveComment) LogicalLine {
	line := parse.lines.Line(comment.BodyStart)
	return LogicalLine{
		Content: comment.Body,
		Segments: []LineSegment{{
			PhysicalLine: line,
			ContentStart: 0,
			ContentEnd:   len(comment.Body),
			ColumnOffset: parse.lines.Column(comment.BodyStart),
		}},
		StartLine:                line,
		EndLine:                  line,
		UnterminatedStringOffset: -1,
		UnterminatedStringQuote:  0,
	}
}

// continuesBelow reports whether a directive comment is followed, on the line directly
// below it, by another directive comment, which would continue a call left open.
//
// Takes index (int) which is the index of the comment token.
//
// Returns bool which is true when the next line holds a directive comment.
func (parse *migrationParse) continuesBelow(index int) bool {
	next := index + 1
	if next < len(parse.tokens) && parse.tokens[next].Kind == sqllex.KindWhitespace {
		next++
	}
	if next >= len(parse.tokens) {
		return false
	}
	following, isDirectiveComment := ReadDirectiveComment(parse.content, parse.lines, parse.tokens[next], parse.linePrefix)
	return isDirectiveComment && parse.lines.Line(following.Token.Start) == parse.lines.Line(parse.tokens[index].Start)+1
}

// applyMigrationCall records what a migration directive's well-formed arguments say: a
// read-only value for the statement that follows, and whether the migration runs outside
// a transaction.
//
// Takes call (migrationCall) which is the parsed directive call.
func (parse *migrationParse) applyMigrationCall(call migrationCall) {
	if readOnly, found := firstBoolean(call.call, keywordReadOnly); found {
		parse.pending = &readOnly
	}
	if noTransaction, found := firstBoolean(call.call, keywordNoTransaction); found && noTransaction {
		parse.result.NoTransaction = true
	}
}

// applyColumnCall records the column override a column directive's well-formed arguments
// declare, reporting a column name that does not name its table.
//
// Takes call (migrationCall) which is the parsed directive call.
// Takes line (int) which is the 1-based line of the directive.
func (parse *migrationParse) applyColumnCall(call migrationCall, line int) {
	name, nameSpan, named := callName(call.call)
	if !named {
		if span, empty := emptyCallName(call.call); empty {
			parse.report(false, parse.errorBuilder.InvalidValueShape(span, keywordName,
				fmt.Sprintf("a column qualified by its table, such as users.email, since %s in a migration names the table it changes", call.name)))
		}
		return
	}
	parts, split := SplitQualifiedName(name)
	if !split || len(parts) < 2 {
		parse.report(false, parse.errorBuilder.InvalidValueShape(nameSpan, keywordName,
			fmt.Sprintf("a column qualified by its table, such as users.%s, since %s in a migration names the table it changes", name, call.name)))
		return
	}
	tableParts := make([]string, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		tableParts = append(tableParts, part.Written)
	}
	lineStart := diagbuild.TextSpan{Line: line, Column: 1, EndLine: line, EndColumn: 1}
	override := MigrationColumnOverride{
		Nullable:        nil,
		NonNullElements: nil,
		Table:           strings.Join(tableParts, "."),
		Column:          parts[len(parts)-1].Written,
		SQLType:         "",
		GoType:          "",
		StructTags:      "",
		GoTypeSpan:      lineStart,
		StructTagsSpan:  lineStart,
	}
	applyColumnArguments(call.call, &override)
	parse.result.ColumnOverrides = append(parse.result.ColumnOverrides, override)
}

// reportOtherRole reports a directive call whose role means nothing in a migration: a
// query directive, which is ignored, or an unknown one, which is most likely a misspelt
// migration or column directive.
//
// Takes comment (DirectiveComment) which is the directive comment.
// Takes role (string) which is the directive's role, empty for a bare prefix call.
func (parse *migrationParse) reportOtherRole(comment DirectiveComment, role string) {
	lexer := NewLexer(parse.logicalLine(comment))
	nameSpan := lexer.Next().Span
	if role != "" {
		lexer.Next()
		nameSpan = MergeSpan(nameSpan, lexer.Next().Span)
	}
	name := parse.prefixes.Canonical
	if role != "" {
		name = parse.prefixes.Qualify(role)
	}
	if slices.Contains(queryOnlyRoles, role) {
		warning := parse.errorBuilder.At(nameSpan, diag.CodeDirectiveWrongContext, name+" is a query directive and is ignored in a migration file; use "+
			parse.prefixes.Qualify(roleMigration)+" or "+parse.prefixes.Qualify(roleColumn)+" for migration directives")
		warning.Severity = diag.SeverityWarning
		parse.report(false, warning)
		return
	}
	parse.report(true, parse.errorBuilder.UnknownDirective(nameSpan, name,
		[]string{parse.prefixes.Qualify(roleMigration), parse.prefixes.Qualify(roleColumn)}))
}

// statementStart reads the first significant token after noise, applying a pending
// read-only value when the statement creates a function or procedure.
//
// Takes index (int) which is the index of the significant token.
func (parse *migrationParse) statementStart(index int) {
	if parse.pending == nil {
		return
	}
	var head [maxCreateFunctionHeadTokens]sqllex.Token
	if name := createdFunctionName(statementHead(parse.tokens, index, head[:0])); name != "" {
		parse.result.ReadOnlyOverrides[parse.tokens[index].Start] = parse.pending
	}
	parse.pending = nil
}

// migrationCall is one directive call read from a line of a migration file.
type migrationCall struct {
	// call holds the parsed arguments.
	call *CallArguments

	// name is the directive's name with the canonical prefix, such as sgl.column.
	name string
}

// ParseMigration reads the directive comments of a migration file under a dialect's
// lexicon, as ScanMigration does, and reports what is wrong with them.
//
// Each sgl.migration and sgl.column call is parsed with ParseCallArguments and checked
// with ValidateCall against the arguments its directive accepts, so an unknown keyword
// argument (Q027), a value that is not true or false where one is expected (Q028), a
// column not qualified by its table (Q028), a call left unclosed (Q032), a call continued
// on the next line (Q032), a string left open (Q035) and text after the call (Q007) are
// each reported on the directive's line. A directive with an unknown role is reported
// (Q026), and a query directive other than the query header, which MisplacedQueryLines
// lists, is reported as ignored (a Q044 warning). A text holding more unterminated
// constructs than are read is reported (Q039) where reading stopped. At most
// maxMigrationDiagnostics diagnostics are reported, followed by one Q066 diagnostic when
// any were left out.
//
// What a well-formed argument says is recorded even when another argument of its call is
// wrong, such as an unknown key or a value of the wrong shape; a call that does not
// parse, such as one left unclosed or followed by more text, records nothing.
//
// Takes content (string) which is the migration file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
// Takes filename (string) which names the migration file in the diagnostics.
//
// Returns MigrationDirectives which holds what the directives say.
// Returns []diag.SourceError which holds the problems found, in order.
func ParseMigration(content string, lexicon Lexicon, prefixes namespace.Set, filename string) (MigrationDirectives, []diag.SourceError) {
	return ParseMigrationTokens(content, ScanTokens(lexicon.Rules, content), lexicon, prefixes, filename)
}

// ParseMigrationTokens reads the directive comments of a migration file whose tokens a
// caller has already scanned, as ParseMigration does, so a caller that needs the tokens
// for more than the directives reads the text once.
//
// Takes content (string) which is the migration file content.
// Takes scan (TokenScan) which holds the content's tokens, as ScanTokens read them under
// the lexicon's rules.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
// Takes filename (string) which names the migration file in the diagnostics.
//
// Returns MigrationDirectives which holds what the directives say.
// Returns []diag.SourceError which holds the problems found, in order.
func ParseMigrationTokens(content string, scan TokenScan, lexicon Lexicon, prefixes namespace.Set, filename string) (MigrationDirectives, []diag.SourceError) {
	parse := readMigration(content, scan, lexicon, prefixes, filename)
	return parse.result, parse.limiter.Finish(callNoticePlace(parse.errorBuilder, diagbuild.TextSpan{Line: 1, Column: 1, EndLine: 1, EndColumn: 1}))
}

// RequestsNoTransaction reads whether a migration's sgl.migration directives ask for it
// to run outside a transaction, as the migration run time needs to know before it runs
// the migration.
//
// A directive it cannot read for certain is refused rather than read as not asking: a
// sgl.migration directive with an error, such as a misspelt key (no_transacton), a value
// that is not true or false, a call left unclosed or continued on the next line, or text
// after the call, and a directive whose role is unknown, such as a misspelt sgl.migraton.
// Running a migration inside a transaction it asked to run outside of, or the reverse,
// cannot be undone. Problems in other directives, such as sgl.column, do not affect how
// the migration runs and are left to the generator to report.
//
// Takes content (string) which is the migration file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns bool which is true when a well-formed sgl.migration(no_transaction: true)
// directive asks for the migration to run without BEGIN/COMMIT.
// Returns error which wraps ErrMalformedMigrationDirective, naming the line and the
// problem of the first such directive, when one is malformed.
func RequestsNoTransaction(content string, lexicon Lexicon, prefixes namespace.Set) (bool, error) {
	parse := readMigration(content, ScanTokens(lexicon.Rules, content), lexicon, prefixes, "")
	if parse.transactionProblem == nil {
		return parse.result.NoTransaction, nil
	}
	problem := parse.transactionProblem
	err := fmt.Errorf("%w on line %d: %s", ErrMalformedMigrationDirective, problem.Line, problem.Message)
	if more := parse.transactionProblems - 1; more > 0 {
		err = fmt.Errorf("%w (and %d more)", err, more)
	}
	return false, err
}

// TrailingTextDiagnostic reports the text a directive line holds after its call, such as
// the int in `-- $1 as sgl.param(id) int`, which would otherwise be silently ignored
// (Q007). The lexer is left at the end of the line.
//
// Takes lexer (*Lexer) which is positioned just after the call.
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
// Takes directiveName (string) which names the directive in the message.
//
// Returns diag.SourceError which is the diagnostic.
// Returns bool which is false when nothing follows the call.
func TrailingTextDiagnostic(lexer *Lexer, errorBuilder diagbuild.ErrorBuilder, directiveName string) (diag.SourceError, bool) {
	first := lexer.Peek()
	if first.Kind == TokenEOF {
		return diag.SourceError{}, false
	}
	last := first
	for token := lexer.Next(); token.Kind != TokenEOF; token = lexer.Next() {
		last = token
	}
	return errorBuilder.DirectiveSyntax(MergeSpan(first.Span, last.Span),
		fmt.Sprintf("unexpected %q after the %s(...) call; a directive line holds the call alone, so put "+
			"every setting inside the parentheses", first.Lexeme, directiveName)), true
}

// TruncatedTextDiagnostic reports that a text was read only up to a line, because it held
// more unterminated strings, quoted identifiers and comments than ScanTokens recovers
// from, so the directives after it were not read.
//
// Takes errorBuilder (diagbuild.ErrorBuilder) which renders the diagnostic.
// Takes line (int) which is the 1-based line where reading stopped.
// Takes subject (string) which names the text, such as "the migration".
//
// Returns diag.SourceError which is the Q039 diagnostic.
func TruncatedTextDiagnostic(errorBuilder diagbuild.ErrorBuilder, line int, subject string) diag.SourceError {
	span := diagbuild.TextSpan{Line: line, Column: 1, EndLine: line, EndColumn: 1}
	return errorBuilder.At(span, diag.CodeDirectiveLineLimit, fmt.Sprintf(
		"%s holds more than %d unterminated strings, quoted identifiers or comments, so its directives "+
			"from line %d on were not read; close them", subject, maxLexicalRecoveries, line))
}

// readMigration reads the directive comments of a migration file's tokens, recording what
// they say and the problems found.
//
// Takes content (string) which is the migration file content.
// Takes scan (TokenScan) which holds the content's tokens.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
// Takes filename (string) which names the migration file in the diagnostics.
//
// Returns *migrationParse which holds what the directives say and the problems found.
func readMigration(content string, scan TokenScan, lexicon Lexicon, prefixes namespace.Set, filename string) *migrationParse {
	parse := &migrationParse{
		pending:             nil,
		limiter:             diagbuild.NewLimiter(maxMigrationDiagnostics),
		transactionProblem:  nil,
		content:             content,
		prefixes:            prefixes,
		lines:               NewLineIndex(content),
		result:              MigrationDirectives{ReadOnlyOverrides: make(map[int]*bool), ColumnOverrides: nil, Foreign: nil, MisplacedQueryLines: nil, NoTransaction: false},
		tokens:              scan.Tokens,
		linePrefix:          lexicon.LinePrefix,
		errorBuilder:        diagbuild.NewErrorBuilder(filename),
		transactionProblems: 0,
	}
	for index, token := range parse.tokens {
		switch {
		case token.Kind == sqllex.KindWhitespace:
		case token.Kind.IsNoise():
			parse.comment(index)
		default:
			parse.statementStart(index)
		}
	}
	if scan.Truncated() {
		parse.report(false, TruncatedTextDiagnostic(parse.errorBuilder, parse.lines.Line(scan.TruncatedAt), "the migration"))
	}
	return parse
}

// skipDirectiveName reads the name a directive call opens with: its prefix, the dot and
// its role.
//
// Takes lexer (*Lexer) which is positioned at the start of the directive.
func skipDirectiveName(lexer *Lexer) {
	lexer.Next()
	lexer.Next()
	lexer.Next()
}

// firstBoolean returns the value of the first keyword argument of a key whose value is
// true or false.
//
// Takes call (*CallArguments) which holds the parsed keyword arguments.
// Takes key (string) which names the keyword argument.
//
// Returns value (bool) which is the value.
// Returns found (bool) which is false when no such argument holds a boolean.
func firstBoolean(call *CallArguments, key string) (value bool, found bool) {
	for index := range call.KeywordArguments {
		argument := &call.KeywordArguments[index]
		if argument.Key != key || argument.Failed {
			continue
		}
		if parsed, isBoolean := parseBoolean(argument.Value.Raw); isBoolean {
			return parsed, true
		}
	}
	return false, false
}

// callName returns the name a call gives its first positional slot: the first positional,
// or a name: keyword argument when there is none.
//
// Takes call (*CallArguments) which holds the parsed arguments.
//
// Returns string which is the name.
// Returns diagbuild.TextSpan which locates the name.
// Returns bool which is false when the call gives no name.
func callName(call *CallArguments) (string, diagbuild.TextSpan, bool) {
	if len(call.Positionals) > 0 {
		positional := &call.Positionals[0]
		return positional.Value.Raw, positional.Span, !positional.Failed && positional.Value.Raw != ""
	}
	for index := range call.KeywordArguments {
		argument := &call.KeywordArguments[index]
		if argument.Key == keywordName {
			return argument.Value.Raw, argument.ValueSpan, !argument.Failed && argument.Value.Raw != ""
		}
	}
	return "", diagbuild.TextSpan{}, false
}

// emptyCallName reports a name argument a call gives as an empty value, such as
// sgl.column(""), which parsed without error but names nothing.
//
// Takes call (*CallArguments) which holds the parsed arguments.
//
// Returns diagbuild.TextSpan which locates the empty value.
// Returns bool which is true when the call's name is given and empty.
func emptyCallName(call *CallArguments) (diagbuild.TextSpan, bool) {
	if len(call.Positionals) > 0 {
		positional := &call.Positionals[0]
		return positional.Span, !positional.Failed && positional.Value.Raw == ""
	}
	for index := range call.KeywordArguments {
		argument := &call.KeywordArguments[index]
		if argument.Key == keywordName {
			return argument.ValueSpan, !argument.Failed && argument.Value.Raw == ""
		}
	}
	return diagbuild.TextSpan{}, false
}

// applyColumnArguments writes the well-formed keyword arguments of a column directive
// onto its override, the first of each key; an argument whose value has the wrong shape,
// or whose key is unknown, was reported and is left out.
//
// Takes call (*CallArguments) which holds the parsed keyword arguments.
// Takes override (*MigrationColumnOverride) which receives the values.
func applyColumnArguments(call *CallArguments, override *MigrationColumnOverride) {
	seen := make(map[string]bool, len(call.KeywordArguments))
	for index := range call.KeywordArguments {
		argument := &call.KeywordArguments[index]
		if argument.Failed || seen[argument.Key] {
			continue
		}
		seen[argument.Key] = true
		switch argument.Key {
		case keywordType:
			override.SQLType = argument.Value.Raw
		case keywordGoType:
			override.GoType = argument.Value.Raw
			override.GoTypeSpan = argument.ValueSpan
		case keywordStructTags:
			override.StructTags = argument.Value.Raw
			override.StructTagsSpan = argument.Span
		case keywordNullable:
			override.Nullable = booleanPointer(argument.Value.Raw)
		case keywordNonNullElements:
			override.NonNullElements = booleanPointer(argument.Value.Raw)
		}
	}
}

// booleanPointer reads a true or false value, ignoring case.
//
// Takes value (string) which is the value as written.
//
// Returns *bool which holds the value, or nil when it is neither true nor false.
func booleanPointer(value string) *bool {
	parsed, isBoolean := parseBoolean(value)
	if !isBoolean {
		return nil
	}
	return &parsed
}
