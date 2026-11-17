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

package check

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// fullTextIndexPass warns about a full-text search whose columns no full-text index of
// their table covers exactly, which MySQL and MariaDB refuse when the query runs.
type fullTextIndexPass struct {
	// catalogue holds the tables and their indexes.
	catalogue *sqlmodel.Catalogue
}

// Analyse checks each full-text search the dialect recorded for the query.
//
// Takes subject (*DiagnosticContext) which holds the query, its analysis and its scope.
//
// Returns []diag.SourceError which holds a warning per search the engine would refuse.
func (p *fullTextIndexPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if subject.RawAnalysis == nil || p.catalogue == nil {
		return nil
	}
	searcher, searches := subject.RawAnalysis.Detail.(ast.FullTextSearcher)
	if !searches {
		return nil
	}
	line := 0
	if subject.Query != nil {
		line = subject.Query.Line
	}
	var diagnostics []diag.SourceError
	for _, search := range searcher.FullTextSearches() {
		if code, message := p.searchProblem(subject, search); message != "" {
			diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, line, code, diag.SeverityWarning, message))
		}
	}
	return diagnostics
}

// searchProblem finds a reason an engine would reject a full-text search.
//
// It reports an absent column (1054), columns from multiple tables (1210), or a missing
// FULLTEXT index on exactly the searched columns (1191). A nested search uses its own
// query's tables. Columns that may come from a derived or enclosing query are left to the
// engine.
//
// Takes subject (*DiagnosticContext) which resolves table aliases.
// Takes search (ast.FullTextSearch) which is the search.
//
// Returns code (string) which is the problem's code.
// Returns message (string) which says what is wrong, or is empty.
func (p *fullTextIndexPass) searchProblem(subject *DiagnosticContext, search ast.FullTextSearch) (code, message string) {
	var found *sqlmodel.Table
	columns := make([]string, 0, len(search.Columns))
	written := make([]string, 0, len(search.Columns))
	tables := 0
	sources := p.sourcesOf(subject, search)
	for _, reference := range search.Columns {
		written = append(written, writtenColumn(reference))
		table, unknown := tableOf(sources, reference)
		if unknown {
			return diag.CodeUnknownColumn, fmt.Sprintf("unknown column %q", writtenColumn(reference))
		}
		if table == nil {
			return "", ""
		}
		name, has := columnNamed(table, reference.ColumnName)
		if !has {
			return diag.CodeUnknownColumn, fmt.Sprintf("unknown column %q in table %q", reference.ColumnName, reference.TableAlias)
		}
		if found != table {
			tables++
		}
		found = table
		columns = append(columns, name)
	}
	switch {
	case found == nil:
		return "", ""
	case tables > 1:
		return diag.CodeFullTextWithoutIndex, fmt.Sprintf("MATCH (%s) searches columns of more than one table, "+
			"which no FULLTEXT index covers, so the engine refuses the query with error 1210", strings.Join(written, ", "))
	case hasFullTextIndex(found, columns):
		return "", ""
	default:
		return diag.CodeFullTextWithoutIndex, fmt.Sprintf("MATCH (%s) needs a FULLTEXT index of exactly those columns on %s, "+
			"which the migrations do not create, so the engine refuses the query with error 1191", strings.Join(columns, ", "), found.Name)
	}
}

// searchSources are the tables a search's columns may be read from: those of the query it
// is in, by the name each is referred to by.
type searchSources struct {
	// tables maps each lower-cased alias, or table name without one, to its table, nil for a
	// relation that is not a catalogue table.
	tables map[string]*sqlmodel.Table

	// enclosing holds the tables of the query a nested query sits in, whose columns the
	// nested query may read, or is nil for the query itself.
	enclosing map[string]*sqlmodel.Table

	// complete reports that every relation of the query is a catalogue table, so a column
	// none of them holds is unknown.
	complete bool
}

// sourcesOf gathers the tables a search reads: the query's scope for a search of the
// query itself, and the FROM clause of the nested query a search sits in otherwise.
//
// Takes subject (*DiagnosticContext) which holds the query and its scope.
// Takes search (ast.FullTextSearch) which is the search.
//
// Returns searchSources which holds the tables.
func (p *fullTextIndexPass) sourcesOf(subject *DiagnosticContext, search ast.FullTextSearch) searchSources {
	sources := searchSources{tables: make(map[string]*sqlmodel.Table), complete: search.Query != nil, enclosing: nil}
	if search.Query != nil && search.Query != subject.RawAnalysis {
		p.addNestedSources(&sources, search.Query)
		sources.enclosing = p.sourcesOf(subject, ast.FullTextSearch{Query: subject.RawAnalysis, Columns: nil}).tables
		return sources
	}
	if subject.Scope == nil {
		sources.complete = false
		return sources
	}
	for _, scoped := range subject.Scope.OrderedTables() {
		var table *sqlmodel.Table
		if scoped.Name != "" {
			table = p.catalogue.LookupTable(scoped.Schema, scoped.Name)
		}
		sources.complete = sources.complete && table != nil
		sources.tables[strings.ToLower(scoped.Alias)] = table
	}
	return sources
}

// addNestedSources adds the tables a nested query's FROM clause names to a search's
// sources.
//
// Takes sources (*searchSources) which receives the tables.
// Takes query (*ast.RawQueryAnalysis) which is the nested query.
func (p *fullTextIndexPass) addNestedSources(sources *searchSources, query *ast.RawQueryAnalysis) {
	references := slices.Clone(query.FromTables)
	for _, join := range query.JoinClauses {
		references = append(references, join.Table)
	}
	for _, reference := range references {
		name := reference.Alias
		if name == "" {
			name = reference.Name
		}
		table := p.catalogue.LookupTable(reference.Schema, reference.Name)
		sources.complete = sources.complete && table != nil
		sources.tables[strings.ToLower(name)] = table
	}
	sources.complete = sources.complete && len(query.RawDerivedTables) == 0 && len(query.RawTableValuedFunctions) == 0
}

// tableOf resolves the table a column reference names, through its alias, or the one
// table of the search's query holding a column of that name.
//
// Takes sources (searchSources) which are the search's tables.
// Takes reference (ast.ColumnReference) which is the column.
//
// Returns *sqlmodel.Table which is the table, or nil when it is not known for certain.
// Returns bool which is true when no table of a query made only of catalogue tables holds
// an unqualified column, which the engine refuses as unknown.
func tableOf(sources searchSources, reference ast.ColumnReference) (*sqlmodel.Table, bool) {
	if reference.TableAlias != "" {
		return sources.tables[strings.ToLower(reference.TableAlias)], false
	}
	var found *sqlmodel.Table
	for _, table := range sources.tables {
		if _, has := columnNamed(table, reference.ColumnName); table == nil || !has {
			continue
		}
		if found != nil && found != table {
			return nil, false
		}
		found = table
	}
	if found != nil || !sources.complete || len(sources.tables) == 0 {
		return found, false
	}
	for _, table := range sources.enclosing {
		if _, has := columnNamed(table, reference.ColumnName); table == nil || has {
			return nil, false
		}
	}
	return nil, true
}

// writtenColumn spells a column reference as the query wrote it, with its table alias.
//
// Takes reference (ast.ColumnReference) which is the column.
//
// Returns string which is the alias and column, or the column alone.
func writtenColumn(reference ast.ColumnReference) string {
	if reference.TableAlias == "" {
		return reference.ColumnName
	}
	return reference.TableAlias + "." + reference.ColumnName
}

// columnNamed finds a table's column by name, without regard to case, as MySQL compares
// column names.
//
// Takes table (*sqlmodel.Table) which is the table, or nil.
// Takes name (string) which is the name.
//
// Returns string which is the column's name as the table spells it.
// Returns bool which is false when the table has no such column.
func columnNamed(table *sqlmodel.Table, name string) (string, bool) {
	if table == nil {
		return "", false
	}
	for index := range table.Columns {
		if strings.EqualFold(table.Columns[index].Name, name) {
			return table.Columns[index].Name, true
		}
	}
	return "", false
}

// hasFullTextIndex reports whether a table has a full-text index of exactly the columns,
// in any order.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes columns ([]string) which are the searched columns.
//
// Returns bool which is true when such an index exists.
func hasFullTextIndex(table *sqlmodel.Table, columns []string) bool {
	wanted := lowerSorted(columns)
	return slices.ContainsFunc(table.Indexes, func(index sqlmodel.Index) bool {
		return index.IsFullText && slices.Equal(lowerSorted(index.Columns), wanted)
	})
}

// lowerSorted returns names lower-cased, sorted and without repeats.
//
// Takes names ([]string) which are the names.
//
// Returns []string which is the normalised set.
func lowerSorted(names []string) []string {
	lowered := make([]string, 0, len(names))
	for _, name := range names {
		lowered = append(lowered, strings.ToLower(name))
	}
	slices.Sort(lowered)
	return slices.Compact(lowered)
}
