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

package analyse

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/check"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// keywordFrom is the embed keyword argument that names a FROM-clause alias.
	keywordFrom = "from"

	// keywordAs is the embed keyword argument that names the generated field and type.
	keywordAs = "as"

	// maxInlineEmbedMarkers is the most inline embed markers read from one query block; the
	// rest are ignored and reported once.
	maxInlineEmbedMarkers = 256

	// maxInlineEmbedNameRunes is the longest table name an inline embed marker may hold.
	maxInlineEmbedNameRunes = 128

	// maxQuotedEmbedNameRunes is how much of a rejected inline name a diagnostic quotes.
	maxQuotedEmbedNameRunes = 32

	// blockCommentOpener opens the block comment an inline embed marker is written in.
	blockCommentOpener = "/*"

	// blockCommentCloser closes a block comment.
	blockCommentCloser = "*/"

	// embedMarkerSuffix follows the directive prefix in an inline embed marker.
	embedMarkerSuffix = "." + directive.RoleEmbed
)

// embedInput holds what binding one query's embeds needs.
type embedInput struct {
	// directiveBlock holds the parsed header embed directives, the canonical source.
	directiveBlock *directive.DirectiveBlock

	// scope holds the query's table references, keyed by alias.
	scope *resolve.ScopeChain

	// errorBuilder builds diagnostics located in the query file.
	errorBuilder diagbuild.ErrorBuilder

	// tokens returns the lexical tokens of the query block, whose block comments are read
	// for inline embed markers when the header declares no embed; nil reads none.
	tokens func() []sqllex.Token

	// sql holds the raw SQL text.
	sql string

	// lines places offsets of the SQL text in the query file, for diagnostics about an
	// inline marker.
	lines *check.SourceLines

	// prefixes names the directive prefixes rendered in diagnostics.
	prefixes namespace.Set

	// markerPrefixes lists the directive prefixes recognised in inline markers.
	markerPrefixes []string

	// groupByKeys holds the query header's group_by keys, such as orders.id; nil when the
	// query is not grouped.
	groupByKeys []string

	// startLine is the line the query block starts on, which locates diagnostics about an
	// inline marker.
	startLine int
}

// embedRequest is one embed declaration, from a header directive or an inline marker.
type embedRequest struct {
	// table is the table or view the embed names.
	table string

	// from is the FROM-clause alias the embed names, or empty to take the alias of the
	// table's only reference.
	from string

	// as is the name the embed asks for, or empty to use the table.
	as string

	// span locates the declaration.
	span diagbuild.TextSpan

	// fromSpan locates the from: value, or is zero when from is empty.
	fromSpan diagbuild.TextSpan

	// asSpan locates the as: value, or is zero when as is empty.
	asSpan diagbuild.TextSpan
}

// inlineMarker is one inline embed marker read from a block comment.
type inlineMarker struct {
	// name is the table name the marker holds, trimmed of spaces.
	name string

	// comment is the block comment holding the marker.
	comment sqllex.Token

	// closed is false when the comment ends before the marker's closing parenthesis.
	closed bool
}

// embedBinding is an embed request bound to the one table reference whose columns it
// holds.
type embedBinding struct {
	// reference is the table reference whose columns the embed holds.
	reference *resolve.ScopedTable

	// table is the table or view the embed directive names.
	table string

	// name is the embed's name: its as: value, or the table when as: is absent.
	name string
}

// resolveEmbedDirectives binds each embed of a query to one table reference and marks
// that reference's output columns as embedded.
//
// The header directives are the source of embeds; the inline /* sgl.embed(table) */
// markers are read only when the header declares none. An embed binds to the reference
// whose alias its from: value names, or, without from:, to the table's only reference. A
// column is embedded when the alias that qualifies it is the bound reference's alias, so
// on a self-join only the named side is embedded, and the embed is outer when that
// reference's own join is.
//
// Takes input (embedInput) which holds the directives, scope and diagnostic context.
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the resolved output columns
// to annotate.
//
// Returns []sqlmodel.OutputColumn which holds the columns with embed annotations applied.
// Returns []diag.SourceError which holds a diagnostic for each embed that could not be
// bound.
func resolveEmbedDirectives(input embedInput, outputColumns []sqlmodel.OutputColumn) ([]sqlmodel.OutputColumn, []diag.SourceError) {
	requests := headerEmbedRequests(input.directiveBlock)
	var diagnostics []diag.SourceError
	if len(requests) == 0 {
		requests, diagnostics = inlineEmbedRequests(input)
	}
	if len(requests) == 0 {
		return outputColumns, diagnostics
	}

	bindings, bindDiagnostics := bindEmbeds(input, requests)
	diagnostics = append(diagnostics, bindDiagnostics...)
	for columnIndex := range outputColumns {
		applyEmbedBinding(&outputColumns[columnIndex], bindings)
	}
	if len(bindings) > 0 {
		diagnostics = append(diagnostics, partialGroupedEmbedDiagnostics(input, outputColumns)...)
	}
	return outputColumns, diagnostics
}

// partialGroupedEmbedDiagnostics reports, for a grouped query that embeds some tables,
// each other table whose columns it leaves unembedded.
//
// Grouping folds the rows of a group into one, so such a column would keep only the
// group's first row's value instead of becoming part of a list. The key table, whose
// columns the group shares, is not reported.
//
// Takes input (embedInput) which holds the group_by keys and diagnostic context.
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the columns with embeds
// applied.
//
// Returns []diag.SourceError which holds one Q015 warning per unembedded table.
func partialGroupedEmbedDiagnostics(input embedInput, outputColumns []sqlmodel.OutputColumn) []diag.SourceError {
	if len(input.groupByKeys) == 0 {
		return nil
	}
	keyQualifier, _, qualified := strings.Cut(input.groupByKeys[0], ".")
	if !qualified {
		return nil
	}
	var reported []string
	var diagnostics []diag.SourceError
	for index := range outputColumns {
		column := &outputColumns[index]
		qualifier := column.SourceQualifier
		if qualifier == "" {
			qualifier = column.SourceTable
		}
		if column.IsEmbedded || qualifier == "" || strings.EqualFold(qualifier, keyQualifier) ||
			strings.EqualFold(column.SourceTable, keyQualifier) || slices.Contains(reported, qualifier) {
			continue
		}
		reported = append(reported, qualifier)
		diagnostic := input.errorBuilder.At(
			diagbuild.TextSpan{Line: input.startLine, Column: 1, EndLine: 0, EndColumn: 0},
			diag.CodeGroupByMissingEmbed,
			fmt.Sprintf("group_by: %s folds each group into one row, but the columns of %s are not embedded, so each group keeps only its first row's values; add %s(%s)",
				input.groupByKeys[0], qualifier, input.prefixes.Qualify(directive.RoleEmbed), column.SourceTable),
		)
		diagnostic.Severity = diag.SeverityWarning
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

// headerEmbedRequests returns the embeds declared by `-- sgl.embed(table, ...)` header
// directives on the parsed block.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed embed
// declarations.
//
// Returns []embedRequest which holds the declared embeds in source order, or nil when no
// embeds are present.
func headerEmbedRequests(directiveBlock *directive.DirectiveBlock) []embedRequest {
	if directiveBlock == nil || len(directiveBlock.Embeds) == 0 {
		return nil
	}
	requests := make([]embedRequest, 0, len(directiveBlock.Embeds))
	for _, embed := range directiveBlock.Embeds {
		if embed == nil || embed.Table == "" {
			continue
		}
		requests = append(requests, embedRequest{
			table:    embed.Table,
			from:     embed.From,
			as:       embed.As,
			span:     embed.Span,
			fromSpan: embed.FromSpan,
			asSpan:   embed.AsSpan,
		})
	}
	return requests
}

// inlineEmbedRequests returns the embeds named by inline /* sgl.embed(table) */ markers
// in the block comments of the SQL text, in source order. An inline marker names only a
// table, and every request is located at the start of the query block.
//
// A marker whose comment ends before its closing parenthesis, or whose name is not an
// optionally schema-qualified table name of at most maxInlineEmbedNameRunes, is reported
// at its comment and requests nothing. Markers past maxInlineEmbedMarkers are ignored and
// reported once.
//
// Takes input (embedInput) which holds the block's tokens, the prefixes and the
// diagnostic context.
//
// Returns []embedRequest which holds one request per accepted marker, or nil when there
// are none.
// Returns []diag.SourceError which holds the problems with the markers.
func inlineEmbedRequests(input embedInput) ([]embedRequest, []diag.SourceError) {
	if input.tokens == nil || !strings.Contains(input.sql, blockCommentOpener) {
		return nil, nil
	}
	markers, truncated := scanInlineEmbedMarkers(input.tokens(), input.markerPrefixes)
	span := diagbuild.TextSpan{Line: input.startLine, Column: 1, EndLine: 0, EndColumn: 0}
	directiveName := input.prefixes.Qualify(directive.RoleEmbed)
	var requests []embedRequest
	var diagnostics []diag.SourceError
	for index := range markers {
		marker := &markers[index]
		markerSpan := input.lines.Span(marker.comment.Start, marker.comment.End)
		switch {
		case !marker.closed:
			diagnostics = append(diagnostics, input.errorBuilder.Unclosed(markerSpan, directiveName+"("))
		case marker.name == "":
			continue
		case !validInlineEmbedName(marker.name):
			diagnostics = append(diagnostics, input.errorBuilder.At(markerSpan, diag.CodeInvalidKeywordArgumentValue, fmt.Sprintf(
				"%s(...): %q is not a table name; write a table, or a schema and a table, of at most %d characters",
				directiveName, sqllex.Excerpt(marker.name, maxQuotedEmbedNameRunes), maxInlineEmbedNameRunes)))
		default:
			requests = append(requests, embedRequest{
				table:    marker.name,
				from:     "",
				as:       "",
				span:     span,
				fromSpan: span,
				asSpan:   span,
			})
		}
	}
	if truncated {
		diagnostics = append(diagnostics, input.errorBuilder.At(span, diag.CodeDirectiveLineLimit, fmt.Sprintf(
			"the query holds more than %d inline %s markers; the rest were ignored", maxInlineEmbedMarkers, directiveName)))
	}
	return requests, diagnostics
}

// bindEmbeds binds each embed request to a table reference, in declaration order, and
// rejects any request whose name or reference another embed already holds.
//
// Takes input (embedInput) which holds the scope and diagnostic context.
// Takes requests ([]embedRequest) which holds the embeds to bind.
//
// Returns []embedBinding which holds the bound embeds in declaration order.
// Returns []diag.SourceError which holds a diagnostic for each request left unbound.
func bindEmbeds(input embedInput, requests []embedRequest) ([]embedBinding, []diag.SourceError) {
	references := sortedTableReferences(input.scope)
	bindings := make([]embedBinding, 0, len(requests))
	var diagnostics []diag.SourceError
	for index := range requests {
		request := &requests[index]
		reference, problem := bindEmbedReference(input, request, references)
		if problem != nil {
			diagnostics = append(diagnostics, *problem)
			continue
		}
		if reference == nil {
			continue
		}
		name := request.as
		if name == "" {
			_, name = splitEmbedTable(request.table)
		}
		if clash := embedNameClash(input, request, name, bindings); clash != nil {
			diagnostics = append(diagnostics, *clash)
			continue
		}
		if clash := embedReferenceClash(input, request, reference, bindings); clash != nil {
			diagnostics = append(diagnostics, *clash)
			continue
		}
		bindings = append(bindings, embedBinding{reference: reference, table: request.table, name: name})
	}
	return bindings, diagnostics
}

// bindEmbedReference finds the table reference an embed request holds the columns of.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed to bind.
// Takes references ([]*resolve.ScopedTable) which holds the query's table references in
// alias order.
//
// Returns *resolve.ScopedTable which is the bound reference, or nil when the table is not
// referenced or the request cannot be bound.
// Returns *diag.SourceError which explains why the request cannot be bound, or nil.
func bindEmbedReference(input embedInput, request *embedRequest, references []*resolve.ScopedTable) (*resolve.ScopedTable, *diag.SourceError) {
	if request.from != "" {
		return bindNamedAlias(input, request, references)
	}
	return bindSoleReference(input, request, references)
}

// bindNamedAlias binds an embed request to the reference whose alias its from: value
// names, and checks that the reference reads the table the request names.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed to bind.
// Takes references ([]*resolve.ScopedTable) which holds the query's table references in
// alias order.
//
// Returns *resolve.ScopedTable which is the named reference, or nil on a problem.
// Returns *diag.SourceError which reports an alias that names no reference, or one of
// another table, or nil.
func bindNamedAlias(input embedInput, request *embedRequest, references []*resolve.ScopedTable) (*resolve.ScopedTable, *diag.SourceError) {
	directiveName := input.prefixes.Qualify(directive.RoleEmbed)
	for _, reference := range references {
		if !strings.EqualFold(reference.Alias, request.from) {
			continue
		}
		if !aliasedTableDiffers(reference, request.table) {
			return reference, nil
		}
		diagnostic := input.errorBuilder.At(request.fromSpan, diag.CodeInvalidKeywordArgumentValue, fmt.Sprintf(
			"%s(%s): keyword argument %q names alias %q, which refers to table %q, not %q",
			directiveName, request.table, keywordFrom, reference.Alias, reference.Name, request.table,
		))
		return nil, &diagnostic
	}
	diagnostic := input.errorBuilder.InvalidEnum(request.fromSpan, keywordFrom, request.from, referenceAliases(references))
	diagnostic.Message = fmt.Sprintf("%s(%s): %s", directiveName, request.table, diagnostic.Message)
	return nil, &diagnostic
}

// bindSoleReference binds an embed request without from: to the only reference of its
// table, so the reference's alias is taken from the FROM clause.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed to bind.
// Takes references ([]*resolve.ScopedTable) which holds the query's table references in
// alias order.
//
// Returns *resolve.ScopedTable which is the table's only reference, or nil when the table
// is referenced never or more than once.
// Returns *diag.SourceError which reports a table the query does not read, or reads more
// than once, or nil.
func bindSoleReference(input embedInput, request *embedRequest, references []*resolve.ScopedTable) (*resolve.ScopedTable, *diag.SourceError) {
	schema, table := splitEmbedTable(request.table)
	var matches []*resolve.ScopedTable
	for _, reference := range references {
		if schema != "" && !strings.EqualFold(reference.Schema, schema) {
			continue
		}
		if strings.EqualFold(reference.Name, table) || (schema == "" && strings.EqualFold(reference.Alias, table)) {
			matches = append(matches, reference)
		}
	}
	switch len(matches) {
	case 0:
		return nil, unreadEmbedTable(input, request, references)
	case 1:
		return matches[0], nil
	}
	directiveName := input.prefixes.Qualify(directive.RoleEmbed)
	return nil, new(input.errorBuilder.At(request.span, diag.CodeMissingRequired, fmt.Sprintf(
		"missing required keyword argument %q on %s(%s): the table is referenced more than once, as %s; "+
			"name the reference to embed with %s(%s, from: <alias>)",
		keywordFrom, directiveName, request.table, strings.Join(referenceAliases(matches), ", "),
		directiveName, request.table,
	)))
}

// unreadEmbedTable reports an embed naming a table the query does not read, which would
// embed nothing.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed.
// Takes references ([]*resolve.ScopedTable) which holds the query's table references.
//
// Returns *diag.SourceError which reports the table and names those the query reads.
func unreadEmbedTable(input embedInput, request *embedRequest, references []*resolve.ScopedTable) *diag.SourceError {
	tables := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.Name != "" && !slices.Contains(tables, reference.Name) {
			tables = append(tables, reference.Name)
		}
	}
	message := fmt.Sprintf("%s(%s) names no table the query reads, so it would embed nothing", input.prefixes.Qualify(directive.RoleEmbed), request.table)
	if len(tables) > 0 {
		message += "; name one of " + strings.Join(tables, ", ")
	}
	diagnostic := input.errorBuilder.At(request.span, diag.CodeInvalidKeywordArgumentValue, message)
	diagnostic.Suggestion = diagbuild.ClosestSuggestion(request.table, tables)
	return &diagnostic
}

// splitEmbedTable splits the table an embed names into its schema, when the embed
// qualifies it, and its name.
//
// Takes qualified (string) which is the table as the embed names it, such as
// inventory.articles.
//
// Returns schema (string) which is the schema, or empty.
// Returns table (string) which is the table's name.
func splitEmbedTable(qualified string) (schema, table string) {
	if before, after, found := strings.CutLast(qualified, "."); found && before != "" {
		return before, after
	}
	return "", qualified
}

// embedNameClash reports an embed whose name another bound embed already holds, since the
// two would generate the same field and type.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed being bound.
// Takes name (string) which is the request's name.
// Takes bindings ([]embedBinding) which holds the embeds bound so far.
//
// Returns *diag.SourceError which reports the clash, or nil when the name is free.
func embedNameClash(input embedInput, request *embedRequest, name string, bindings []embedBinding) *diag.SourceError {
	held := slices.IndexFunc(bindings, func(binding embedBinding) bool {
		return strings.EqualFold(binding.name, name)
	})
	if held == -1 {
		return nil
	}
	directiveName := input.prefixes.Qualify(directive.RoleEmbed)
	if request.as == "" {
		diagnostic := input.errorBuilder.At(request.span, diag.CodeMissingRequired, fmt.Sprintf(
			"missing required keyword argument %q on %s(%s): another embed is already named %q",
			keywordAs, directiveName, request.table, bindings[held].name,
		))
		return &diagnostic
	}
	diagnostic := input.errorBuilder.At(request.asSpan, diag.CodeInvalidKeywordArgumentValue, fmt.Sprintf(
		"%s(%s): invalid value %q for keyword argument %q: another embed is already named %q",
		directiveName, request.table, request.as, keywordAs, bindings[held].name,
	))
	return &diagnostic
}

// embedReferenceClash reports an embed bound to a reference another embed already holds,
// since each column belongs to one embed only.
//
// Takes input (embedInput) which holds the diagnostic context.
// Takes request (*embedRequest) which is the embed being bound.
// Takes reference (*resolve.ScopedTable) which is the reference the request binds to.
// Takes bindings ([]embedBinding) which holds the embeds bound so far.
//
// Returns *diag.SourceError which reports the clash, or nil when the reference is free.
func embedReferenceClash(input embedInput, request *embedRequest, reference *resolve.ScopedTable, bindings []embedBinding) *diag.SourceError {
	held := slices.IndexFunc(bindings, func(binding embedBinding) bool {
		return binding.reference == reference
	})
	if held == -1 {
		return nil
	}
	directiveName := input.prefixes.Qualify(directive.RoleEmbed)
	if request.from == "" {
		diagnostic := input.errorBuilder.At(request.span, diag.CodeInvalidKeywordArgumentValue, fmt.Sprintf(
			"%s(%s): the table's only reference, alias %q, is already held by the embed %q",
			directiveName, request.table, reference.Alias, bindings[held].name,
		))
		return &diagnostic
	}
	diagnostic := input.errorBuilder.At(request.fromSpan, diag.CodeInvalidKeywordArgumentValue, fmt.Sprintf(
		"%s(%s): keyword argument %q names alias %q, which the embed %q already holds",
		directiveName, request.table, keywordFrom, request.from, bindings[held].name,
	))
	return &diagnostic
}

// applyEmbedBinding marks a column as embedded when the alias that qualifies it is the
// alias of a bound embed's reference.
//
// Takes column (*sqlmodel.OutputColumn) which is the column to annotate.
// Takes bindings ([]embedBinding) which holds the bound embeds in declaration order.
func applyEmbedBinding(column *sqlmodel.OutputColumn, bindings []embedBinding) {
	if column.SourceQualifier == "" {
		return
	}
	for index := range bindings {
		binding := &bindings[index]
		if !strings.EqualFold(column.SourceQualifier, binding.reference.Alias) {
			continue
		}
		column.IsEmbedded = true
		column.EmbedTable = binding.table
		column.EmbedName = binding.name
		column.EmbedIsOuter = isOuterJoinKind(binding.reference.JoinKind)
		return
	}
}

// sortedTableReferences returns the scope's table references ordered by alias, so binding
// never depends on map iteration order.
//
// Takes scope (*resolve.ScopeChain) which holds the table references keyed by alias.
//
// Returns []*resolve.ScopedTable which holds the references in alias order.
func sortedTableReferences(scope *resolve.ScopeChain) []*resolve.ScopedTable {
	if scope == nil {
		return nil
	}
	references := make([]*resolve.ScopedTable, 0, len(scope.Tables))
	for _, alias := range slices.Sorted(maps.Keys(scope.Tables)) {
		references = append(references, scope.Tables[alias])
	}
	return references
}

// referenceAliases returns the aliases of the given references, in order.
//
// Takes references ([]*resolve.ScopedTable) which holds the references.
//
// Returns []string which holds one alias per reference.
func referenceAliases(references []*resolve.ScopedTable) []string {
	aliases := make([]string, 0, len(references))
	for _, reference := range references {
		aliases = append(aliases, reference.Alias)
	}
	return aliases
}

// aliasedTableDiffers reports whether a reference that renames a known table reads a
// table other than the one an embed names.
//
// A reference whose name is unknown (a derived table) or equal to its alias (an unaliased
// table, or a common table expression, whose scope entry carries the alias as its name)
// is never reported, since its table cannot be compared. A schema qualifier on the
// embed's table is ignored.
//
// Takes reference (*resolve.ScopedTable) which is the reference the embed's alias names.
// Takes table (string) which is the table the embed names.
//
// Returns bool which is true when the reference reads a different table.
func aliasedTableDiffers(reference *resolve.ScopedTable, table string) bool {
	if reference.Name == "" || strings.EqualFold(reference.Name, reference.Alias) {
		return false
	}
	unqualified := table
	if _, after, found := strings.CutLast(table, "."); found {
		unqualified = after
	}
	if strings.EqualFold(reference.Name, unqualified) || strings.EqualFold(reference.Alias, table) {
		return false
	}
	return true
}

// isOuterJoinKind reports whether a table introduced by the given join can be absent from
// a result row, so its embed is generated as a pointer.
//
// Takes joinKind (ast.JoinKind) which is the join that introduced the table.
//
// Returns bool which is true for a join that can leave a side without a matching row,
// such as an outer join.
func isOuterJoinKind(joinKind ast.JoinKind) bool {
	return joinKind.Nullable != 0
}

// scanInlineEmbedMarkers reads the inline embed markers the block comments of a query
// hold, in source order.
//
// A marker opens its comment, after any spaces, with an accepted prefix, ".embed" and an
// opening parenthesis, with optional spaces before the parenthesis, as a formatter may
// write /* sgl.embed (table) */. Its name runs to the first closing parenthesis or
// comment closer of its own comment. Markers in strings, line comments or executable
// comments are never read.
//
// Takes tokens ([]sqllex.Token) which are the block's lexical tokens.
// Takes prefixes ([]string) which are the directive prefixes to recognise.
//
// Returns []inlineMarker which holds at most maxInlineEmbedMarkers markers.
// Returns bool which is true when the block holds more markers than that.
func scanInlineEmbedMarkers(tokens []sqllex.Token, prefixes []string) ([]inlineMarker, bool) {
	var markers []inlineMarker
	for index := range tokens {
		token := tokens[index]
		if token.Kind != sqllex.KindComment || !strings.HasPrefix(token.Raw, blockCommentOpener) {
			continue
		}
		argument, found := embedMarkerArgument(token.Raw[len(blockCommentOpener):], prefixes)
		if !found {
			continue
		}
		if len(markers) == maxInlineEmbedMarkers {
			return markers, true
		}
		markers = append(markers, readInlineMarker(token, argument))
	}
	return markers, false
}

// embedMarkerArgument returns the text after an inline embed marker's opening
// parenthesis, when a comment's text opens with a marker.
//
// Takes text (string) which is the comment's text after its opener.
// Takes prefixes ([]string) which are the directive prefixes to recognise.
//
// Returns string which is the text after the opening parenthesis.
// Returns bool which is true when the comment opens with a marker.
func embedMarkerArgument(text string, prefixes []string) (string, bool) {
	text = strings.TrimLeft(text, " \t\r\n")
	for _, prefix := range prefixes {
		rest, found := strings.CutPrefix(text, prefix+embedMarkerSuffix)
		if !found {
			continue
		}
		if argument, opened := strings.CutPrefix(strings.TrimLeft(rest, " \t\r\n"), "("); opened {
			return argument, true
		}
	}
	return "", false
}

// readInlineMarker reads the name an inline embed marker holds: the text up to the first
// closing parenthesis of its comment, unless the comment closes first.
//
// Takes comment (sqllex.Token) which is the block comment holding the marker.
// Takes argument (string) which is the comment's text after the marker's opening
// parenthesis.
//
// Returns inlineMarker which holds the trimmed name, or no name when the comment closes
// before the parenthesis does.
func readInlineMarker(comment sqllex.Token, argument string) inlineMarker {
	closing := strings.IndexByte(argument, ')')
	commentEnd := strings.Index(argument, blockCommentCloser)
	if closing == -1 || (commentEnd != -1 && commentEnd < closing) {
		return inlineMarker{name: "", comment: comment, closed: false}
	}
	return inlineMarker{name: strings.TrimSpace(argument[:closing]), comment: comment, closed: true}
}

// validInlineEmbedName reports whether an inline embed marker's name is a table name, or
// a schema and a table name joined by a dot, each written as a directive identifier, of
// at most maxInlineEmbedNameRunes.
//
// Takes name (string) which is the trimmed name.
//
// Returns bool which is true for a name an embed can bind.
func validInlineEmbedName(name string) bool {
	if utf8.RuneCountInString(name) > maxInlineEmbedNameRunes {
		return false
	}
	schema, table, qualified := strings.Cut(name, ".")
	if !qualified {
		return validInlineEmbedIdentifier(schema)
	}
	return validInlineEmbedIdentifier(schema) && validInlineEmbedIdentifier(table)
}

// validInlineEmbedIdentifier reports whether a part of an inline embed marker's name is a
// directive identifier: an ASCII letter or underscore followed by letters, digits and
// underscores.
//
// Takes part (string) which is the part.
//
// Returns bool which is true for an identifier.
func validInlineEmbedIdentifier(part string) bool {
	if part == "" || !syntax.IsIdentifierStart(part[0]) {
		return false
	}
	for index := 1; index < len(part); index++ {
		if !syntax.IsIdentifierPart(part[index]) {
			return false
		}
	}
	return true
}
