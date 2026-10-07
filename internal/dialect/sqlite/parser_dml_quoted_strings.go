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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// columnNames holds visible column names from catalogue relations, built-in table
// functions, rowid names and statement aliases or column lists. SQLite treats
// double-quoted names absent from this set as strings.
type columnNames struct {
	// catalogue is the schema the statement runs against, or nil.
	catalogue *sqlmodel.Catalogue

	// known holds the lower-cased names, gathered on first use.
	known map[string]bool

	// tokens are the statement's tokens.
	tokens []token
}

// newColumnNames returns the names a statement's double-quoted names may name a column
// by.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema the statement runs against,
// or nil.
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns *columnNames which gathers the names when first asked.
func newColumnNames(catalogue *sqlmodel.Catalogue, tokens []token) *columnNames {
	return &columnNames{catalogue: catalogue, known: nil, tokens: tokens}
}

// names reports whether a name may name a column, ignoring letter case as SQLite does.
//
// Takes name (string) which is the name.
//
// Returns bool which is true when some column or declared alias has the name.
func (names *columnNames) names(name string) bool {
	if names.known == nil {
		names.known = names.gather()
	}
	return names.known[strings.ToLower(name)]
}

// gather collects the lower-cased names a column may have in the statement.
//
// Returns map[string]bool which holds the names.
func (names *columnNames) gather() map[string]bool {
	known := make(map[string]bool)
	for _, alias := range rowIDAliases {
		known[alias] = true
	}
	for _, columns := range tableValuedFunctionColumns {
		for index := range columns {
			known[strings.ToLower(columns[index].Name)] = true
		}
	}
	names.gatherCatalogue(known)
	names.gatherDeclared(known)
	return known
}

// gatherCatalogue adds the columns of the catalogue's tables and views.
//
// Takes known (map[string]bool) which receives the lower-cased names.
func (names *columnNames) gatherCatalogue(known map[string]bool) {
	if names.catalogue == nil {
		return
	}
	for _, schemaName := range slices.Sorted(maps.Keys(names.catalogue.Schemas)) {
		schema := names.catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if table != nil {
				addColumnNames(known, table.Columns)
			}
		}
		for _, view := range schema.Views {
			if view != nil {
				addColumnNames(known, view.Columns)
			}
		}
	}
}

// gatherDeclared adds the names the statement declares: the name after each AS, and the
// names in a list just before an AS that opens a common table expression's body, as in t
// (a, b) AS (...), which name its columns.
//
// Takes known (map[string]bool) which receives the lower-cased names.
func (names *columnNames) gatherDeclared(known map[string]bool) {
	for index, current := range names.tokens {
		if !current.IsKeyword(tokenIdentifier, keywordAS) || index+1 >= len(names.tokens) {
			continue
		}
		next := names.tokens[index+1]
		switch {
		case opensCommonTableBody(next):
			names.gatherColumnList(known, index-1)
		case next.Kind == tokenIdentifier:
			known[strings.ToLower(next.Value)] = true
		}
	}
}

// gatherColumnList adds the names of the parenthesised column list that ends at a token,
// as in t (a, b) AS (...), when the token closes one.
//
// Takes known (map[string]bool) which receives the lower-cased names.
// Takes closing (int) which is the index of the token that may close the list.
func (names *columnNames) gatherColumnList(known map[string]bool, closing int) {
	if closing < 0 || names.tokens[closing].Kind != tokenRightParen {
		return
	}
	for before := closing - 1; before >= 0 && names.tokens[before].Kind != tokenLeftParen; before-- {
		if names.tokens[before].Kind == tokenIdentifier {
			known[strings.ToLower(names.tokens[before].Value)] = true
		}
	}
}

// readsAsString reports whether SQLite reads a double-quoted name, just consumed, as a
// string: a name that is not qualified, calls nothing and names no column, which SQLite
// takes for a string literal rather than report. The parser warns of each such name.
//
// Takes name (token) which is the name's token.
//
// Returns bool which is true when the name is a string.
func (p *parser) readsAsString(name token) bool {
	if !name.Quoted || !strings.HasPrefix(name.Raw, `"`) || p.columnNames == nil ||
		p.Current().Kind == tokenDot || p.Current().Kind == tokenLeftParen || p.columnNames.names(name.Value) {
		return false
	}
	quoted := "'" + strings.ReplaceAll(name.Value, "'", "''") + "'"
	p.warn(name, "SQLite reads "+name.Raw+" as the string "+quoted+" because it names no column; write "+quoted+
		" instead, since SQLite built without double-quoted strings rejects it")
	return true
}

// warn records a warning about the SQL a token starts, once however often the token is
// parsed.
//
// Takes at (token) which is the token the warning is about.
// Takes message (string) which says how the engine reads it and how to write it instead.
func (p *parser) warn(at token, message string) {
	if p.warnings == nil {
		return
	}
	p.warnings[at.Start] = ast.Warning{Message: message, Span: ast.Span{Start: at.Start, End: at.End}}
}

// sortedWarnings returns the warnings the parser and its children recorded, in source
// order.
//
// Returns []ast.Warning which holds the warnings, or nil when there are none.
func (p *parser) sortedWarnings() []ast.Warning {
	if len(p.warnings) == 0 {
		return nil
	}
	offsets := slices.Sorted(maps.Keys(p.warnings))
	warnings := make([]ast.Warning, 0, len(offsets))
	for _, offset := range offsets {
		warnings = append(warnings, p.warnings[offset])
	}
	return warnings
}

// opensCommonTableBody reports whether the token after an AS starts a common table
// expression's body: its opening parenthesis, or MATERIALIZED or NOT MATERIALIZED before
// it, rather than an alias.
//
// Takes next (token) which is the token after AS.
//
// Returns bool which is true when the AS introduces a body.
func opensCommonTableBody(next token) bool {
	return next.Kind == tokenLeftParen || next.IsKeyword(tokenIdentifier, keywordNOT) ||
		next.IsKeyword(tokenIdentifier, "MATERIALIZED")
}

// addColumnNames adds the lower-cased names of columns.
//
// Takes known (map[string]bool) which receives the names.
// Takes columns ([]sqlmodel.Column) which are the columns.
func addColumnNames(known map[string]bool, columns []sqlmodel.Column) {
	for index := range columns {
		known[strings.ToLower(columns[index].Name)] = true
	}
}
