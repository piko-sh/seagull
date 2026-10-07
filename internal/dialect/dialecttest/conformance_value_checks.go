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

package dialecttest

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// predicateProbe selects one predicate of each kind the analyser types from the
	// fixture's items table.
	predicateProbe = "SELECT id = 1 AS compared, name IS NULL AS missing, id IN (1, 2) AS listed, " +
		"id BETWEEN 1 AND 2 AS ranged, id = 1 AND id = 2 AS joined FROM items;"

	// rowValueProbe names the fixture's items table where a value is expected.
	rowValueProbe = "SELECT items FROM items;"

	// fixtureTable is the table BaseMigration creates.
	fixtureTable = "items"

	// selectListUnknownProbe names two columns the fixture's items table lacks inside the
	// expressions of its select list, each once.
	selectListUnknownProbe = "SELECT missing_column = 1 AS compared, coalesce(other_missing, 1) AS fallback FROM items;"
)

// checkPredicatesHaveTheStatedType checks that every predicate is typed as the dialect
// states: as its PredicateTyperPort types it, which must be a boolean or an integer of a
// known width, or, for a dialect without one, as its core boolean, never NULL over
// operands that are never NULL.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which lists every predicate typed otherwise.
func checkPredicatesHaveTheStatedType(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"predicates.sql": directiveLine(engine, "Predicates", "many") + predicateProbe})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q must analyse without diagnostics, got %s", predicateProbe, formatDiagnostics(result.Diagnostics))
	}
	query := findQuery(emitter.queries, "Predicates")
	if query == nil {
		return failed("the query Predicates was not analysed")
	}
	_, typed := engine.(dialect.PredicateTyperPort)
	core := engine.CoreTypes().Boolean
	var problems []string
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		switch {
		case !typed && (column.SQLType.Category != core.Category || column.SQLType.EngineName != core.EngineName || column.Nullable):
			problems = append(problems, fmt.Sprintf("the predicate %s is %s, nullable %v: a dialect that is no PredicateTyperPort "+
				"types every predicate as its core boolean %s, never NULL over operands never NULL", column.Name, describeType(&column.SQLType),
				column.Nullable, describeType(&core)))
		case !isStatedPredicateType(&column.SQLType):
			problems = append(problems, fmt.Sprintf("the predicate %s is %s, which is neither a boolean nor an integer of a known width",
				column.Name, describeType(&column.SQLType)))
		}
	}
	return outcomeOf(problems)
}

// isStatedPredicateType reports whether a type can be a predicate's: a boolean, or an
// integer of a known width, as engines without a boolean type return.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a boolean or a sized integer.
func isStatedPredicateType(sqlType *sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryBoolean || isSizedInteger(sqlType)
}

// describeType renders a type for a problem: its category and its engine name.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns string which is the description.
func describeType(sqlType *sqlmodel.SQLType) string {
	return fmt.Sprintf("%s %q", sqlType.Category, sqlType.EngineName)
}

// checkRowValuesFollowTheDialect checks relation-row expressions against the dialect's
// typing rules.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkRowValuesFollowTheDialect(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	queries := map[string]string{"rows.sql": directiveLine(engine, "Rows", "many") + rowValueProbe}
	toJSON := engine.Info().ToJSON
	typer, typed := engine.(dialect.RowValueTyperPort)
	if typed && toJSON != "" {
		queries["documents.sql"] = directiveLine(engine, "Documents", "many") + "SELECT " + toJSON + "(items) AS document FROM items;"
	}
	emitter, result, err := runGenerate(ctx, engine, map[string]string{"001_items.up.sql": fixture.BaseMigration}, queries)
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if !typed {
		if !hasDiagnostic(result, "rows.sql", diag.CodeUnknownColumn) {
			return failed("a dialect that is no RowValueTyperPort must report %q as an unknown column, got %s", rowValueProbe,
				formatDiagnostics(result.Diagnostics))
		}
		return passed
	}
	if len(result.Diagnostics) > 0 {
		return failed("a RowValueTyperPort's queries naming a relation as a value must analyse without diagnostics, got %s",
			formatDiagnostics(result.Diagnostics))
	}
	return outcomeOf(rowValueProblems(emitter, typer, engine.CoreTypes().JSON))
}

// rowValueProblems describes how the analysed row value queries differ from what the
// dialect states: the row typed as the RowValueTyperPort types the items table's row, and
// the JSON rendering of it typed as JSON.
//
// Takes emitter (*recordingEmitter) which holds the catalogue and the analysed queries.
// Takes typer (dialect.RowValueTyperPort) which types the row.
// Takes document (sqlmodel.SQLType) which is the dialect's JSON type.
//
// Returns []string which holds the problems.
func rowValueProblems(emitter *recordingEmitter, typer dialect.RowValueTyperPort, document sqlmodel.SQLType) []string {
	var problems []string
	rows := findQuery(emitter.queries, "Rows")
	table := findCatalogueTable(emitter.catalogue, fixtureTable)
	if rows == nil || len(rows.OutputColumns) != 1 || table == nil {
		return []string{"the query Rows must be analysed into one column over the items table"}
	}
	relation := dialect.RowRelation{
		Name:        table.Name,
		Schema:      table.Schema,
		Columns:     make([]ast.ScopedColumn, len(table.Columns)),
		Function:    false,
		OuterJoined: false,
	}
	for index := range table.Columns {
		relation.Columns[index] = ast.ScopedColumn{Name: table.Columns[index].Name, SQLType: table.Columns[index].ValueType(),
			Nullable: table.Columns[index].Nullable}
	}
	want := typer.RowValueType(relation)
	got := rows.OutputColumns[0].SQLType
	if got.Category != want.Category || got.EngineName != want.EngineName || len(got.StructFields) != len(want.StructFields) {
		problems = append(problems, fmt.Sprintf("the row of items is %s with %d fields, but the dialect types it as %s with %d fields",
			describeType(&got), len(got.StructFields), describeType(&want), len(want.StructFields)))
	}
	if documents := findQuery(emitter.queries, "Documents"); documents != nil {
		if len(documents.OutputColumns) != 1 || documents.OutputColumns[0].SQLType.Category != document.Category {
			problems = append(problems, "the JSON rendering of the row of items must be typed as the dialect's JSON type")
		}
	}
	return problems
}

// findCatalogueTable finds a table by name, compared without regard to case, in any
// schema of a catalogue.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
// Takes name (string) which is the table name.
//
// Returns *sqlmodel.Table which is the table, or nil when no schema holds it.
func findCatalogueTable(catalogue *sqlmodel.Catalogue, name string) *sqlmodel.Table {
	if catalogue == nil {
		return nil
	}
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for tableName, table := range schema.Tables {
			if strings.EqualFold(tableName, name) {
				return table
			}
		}
	}
	return nil
}

// checkUnknownColumnInSelectListReportedOnce checks that an unknown column written inside
// an expression of the select list, compared or passed to a function, is reported, and
// reported once.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkUnknownColumnInSelectListReportedOnce(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	_, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"unknown.sql": directiveLine(engine, "Unknown", "many") + selectListUnknownProbe})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	var problems []string
	for _, column := range []string{"missing_column", "other_missing"} {
		count := 0
		for index := range result.Diagnostics {
			if result.Diagnostics[index].Code == diag.CodeUnknownColumn && quotesName(result.Diagnostics[index].Message, column) {
				count++
			}
		}
		if count != 1 {
			problems = append(problems, fmt.Sprintf("the unknown column %s of %q must be reported once, got %s", column,
				selectListUnknownProbe, formatDiagnostics(result.Diagnostics)))
		}
	}
	return outcomeOf(problems)
}
