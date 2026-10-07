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
	"strconv"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// insertShapePass reports INSERT statements with invalid column counts or unknown target
// columns. It checks VALUES rows and SELECT projections against the target columns.
type insertShapePass struct {
	// catalogue holds the tables an INSERT writes.
	catalogue *sqlmodel.Catalogue

	// dialect holds what the pass reads of the dialect, the columns every table has without
	// declaring them; nil when there is no dialect.
	dialect *dialectFacts
}

// Analyse checks the INSERT a query holds.
//
// Takes subject (*DiagnosticContext) which carries the query and its raw analysis.
//
// Returns []diag.SourceError which holds a Q064 error per row or select list of the wrong
// width and a Q001 warning per unknown target column, or nil for any other statement.
func (p *insertShapePass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if subject == nil || subject.RawAnalysis == nil || subject.Query == nil || subject.RawAnalysis.InsertTable == "" {
		return nil
	}
	raw := subject.RawAnalysis
	target := insertTarget{table: p.insertedTable(raw.InsertTarget), columns: raw.InsertColumns}
	reporter := insertReporter{
		catalogue: p.catalogue,
		implicit:  p.implicitColumns(),
		reported:  newReportIndex(subject.ExistingDiagnostics),
		problems:  newQueryReporter(subject.Filename, subject.Query.SQL, subject.Query.Line),
	}
	reporter.unknownTargetColumns(target)
	reporter.valueRowWidths(raw.Spans.Values, target)
	reporter.selectWidth(raw.InsertSelect, target)
	return reporter.problems.finish()
}

// implicitColumns lists the columns every table has without declaring them, as the
// dialect names them: its row identifier aliases and its system columns.
//
// Returns []string which holds the names, or nil when there is no dialect.
func (p *insertShapePass) implicitColumns() []string {
	if p.dialect == nil {
		return nil
	}
	return slices.Concat(p.dialect.info.RowIDAliases, p.dialect.info.SystemColumns)
}

// insertedTable finds the table an INSERT writes.
//
// Takes reference (ast.TableReference) which names it.
//
// Returns *sqlmodel.Table which is the table, or nil when the catalogue does not hold it.
func (p *insertShapePass) insertedTable(reference ast.TableReference) *sqlmodel.Table {
	if p.catalogue == nil || reference.Name == "" {
		return nil
	}
	return p.catalogue.LookupTable(reference.Schema, reference.Name)
}

// insertTarget is what an INSERT writes: the table, when the catalogue holds it, and the
// columns the statement lists.
type insertTarget struct {
	// table is the table the INSERT writes, or nil when the catalogue does not hold it.
	table *sqlmodel.Table

	// columns lists the target columns the statement names, or nothing when it names none.
	columns []string
}

// insertReporter collects the problems of one INSERT.
type insertReporter struct {
	// catalogue holds the table's columns.
	catalogue *sqlmodel.Catalogue

	// problems places the problems found in the query.
	problems *queryReporter

	// reported indexes the diagnostics the query had before the pass ran.
	reported reportIndex

	// implicit lists the columns every table has without declaring them.
	implicit []string
}

// unknownTargetColumns reports each listed target column the table does not have, unless
// the query reports it already, as the parameter resolver does for a placeholder assigned
// to it. A column every table has without declaring it, such as SQLite's rowid, is no
// unknown column.
//
// Takes target (insertTarget) which is the table and the listed columns.
func (r *insertReporter) unknownTargetColumns(target insertTarget) {
	if target.table == nil {
		return
	}
	for _, name := range target.columns {
		if r.catalogue.FindColumn(target.table.Schema, target.table.Name, name) != nil ||
			slices.ContainsFunc(r.implicit, func(candidate string) bool { return r.catalogue.Identifiers.Equal(candidate, name) }) {
			continue
		}
		message := fmt.Sprintf("unknown column %q in table %q", name, target.table.Name)
		if r.reported.has(diag.CodeUnknownColumn, message) {
			continue
		}
		r.problems.add(ast.Span{}, diag.CodeUnknownColumn, diag.SeverityWarning, message, "")
	}
}

// valueRowWidths reports each VALUES row that gives another number of values than the
// statement lists target columns, or, where it lists none, more values than the table has
// columns.
//
// Takes rows ([]ast.TupleSpan) which are the VALUES rows.
// Takes target (insertTarget) which is the table and the listed columns.
func (r *insertReporter) valueRowWidths(rows []ast.TupleSpan, target insertTarget) {
	for index, row := range rows {
		values := len(row.Items)
		switch {
		case len(target.columns) > 0 && values != len(target.columns):
			r.problems.add(row.Span, diag.CodeInsertShape, diag.SeverityError, fmt.Sprintf(
				"row %d of the INSERT gives %s for the %s it lists, which the engine refuses",
				index+1, countOf(values, "value"), countOf(len(target.columns), "target column")), "")
		case len(target.columns) == 0 && target.table != nil && values > writableColumns(target.table):
			r.problems.add(row.Span, diag.CodeInsertShape, diag.SeverityError, fmt.Sprintf(
				"row %d of the INSERT gives %s for the %s table %s has, which the engine refuses",
				index+1, countOf(values, "value"), countOf(writableColumns(target.table), "column"), target.table.Name), "")
		}
	}
}

// selectWidth compares an INSERT's SELECT projection width with the listed target
// columns. Projections containing a star are skipped.
//
// Takes inserted (*ast.RawQueryAnalysis) which is the SELECT the INSERT writes, or nil.
// Takes target (insertTarget) which is the table and the listed columns.
func (r *insertReporter) selectWidth(inserted *ast.RawQueryAnalysis, target insertTarget) {
	if inserted == nil || len(target.columns) == 0 || len(inserted.OutputColumns) == 0 {
		return
	}
	for _, column := range inserted.OutputColumns {
		if column.IsStar {
			return
		}
	}
	if width := len(inserted.OutputColumns); width != len(target.columns) {
		r.problems.add(ast.Span{}, diag.CodeInsertShape, diag.SeverityError, fmt.Sprintf(
			"the SELECT of the INSERT gives %s for the %s it lists, which the engine refuses",
			countOf(width, "column"), countOf(len(target.columns), "target column")), "")
	}
}

// writableColumns counts the columns an INSERT that lists none writes: every column the
// table declares, a hidden one, such as an engine's implicit row identifier, left out.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns int which is the count.
func writableColumns(table *sqlmodel.Table) int {
	count := 0
	for index := range table.Columns {
		if !table.Columns[index].Hidden {
			count++
		}
	}
	return count
}

// countOf writes a count of things, the noun plural unless the count is one.
//
// Takes count (int) which is the count.
// Takes noun (string) which names one thing.
//
// Returns string which is the count and the noun, such as "2 values".
func countOf(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}
