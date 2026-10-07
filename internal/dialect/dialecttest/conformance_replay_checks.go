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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// replayMigration names the migration file the replay checks run after the base one.
	replayMigration = "002_replay.up.sql"

	// keyColumnPrefix starts the name of every column ReplayFixture.KeyedTables declares in
	// a primary key.
	keyColumnPrefix = "left_id"
)

// ReplayFixture supplies migrations in the dialect's syntax for the checks that DDL
// replays into the catalogue as the engine applies it. A field left empty skips its
// check, for a dialect without the form.
type ReplayFixture struct {
	// Idempotent repeats the base migration using supported IF EXISTS and IF NOT EXISTS
	// forms. It must leave items, and items_view where supported, with only the id and name
	// columns.
	Idempotent string

	// KeyedTables creates tables with the columns left_id, right_id and note, none declared
	// NOT NULL, whose primary key (left_id, right_id) is declared at table level or, where
	// the dialect can, added by a later ALTER TABLE.
	KeyedTables string

	// Placement creates the table placed and then adds, moves or changes its columns with
	// the dialect's FIRST, BEFORE or AFTER placements, leaving PlacedColumns.
	Placement string

	// Renames runs after Fixture.BaseMigration and renames items, moving it to another
	// schema too where the dialect can, and renames any view or type it creates, leaving
	// RenamedRelations.
	Renames string

	// StarViews runs after Fixture.BaseMigration and creates the views star_view, as SELECT
	// * FROM items, and qualified_view, as SELECT items.* FROM items, and, where the dialect
	// lets a view declare its column names, named_view (item_id, item_name) as SELECT * FROM
	// items.
	StarViews string

	// ColumnAddition runs after StarViews and adds the column extra to items, with the
	// dialect's ALTER TABLE ... ADD COLUMN.
	ColumnAddition string

	// DerivedRelations runs after Fixture.BaseMigration and creates relations a query may
	// read that are no plain table or view, such as partitions and sequences, where the
	// dialect has them.
	DerivedRelations string

	// DerivedQueries reads each relation DerivedRelations creates; every query must analyse
	// without a diagnostic, its columns typed.
	DerivedQueries string

	// PlacedColumns lists the columns of placed after Placement, in the order the engine
	// keeps them.
	PlacedColumns []string

	// AcceptedForms lists migrations, each run after Fixture.BaseMigration on its own, that
	// the engine accepts, such as its CREATE OR REPLACE forms, names qualified by their
	// database, settings of the search path and constraint clauses; each must replay without
	// an error, as a refused migration stops generation.
	AcceptedForms []string

	// RenamedRelations lists the tables and views Renames leaves, each holding the columns
	// id and name; items must be gone.
	RenamedRelations []string

	// KeyColumnsNullable is true when the engine lets a primary-key column of the tables
	// KeyedTables creates hold NULL, as a SQLite rowid table does.
	KeyColumnsNullable bool
}

// checkIfExistsFormsReplayAsTheEngineDoes checks that a repeated CREATE IF NOT EXISTS,
// ADD COLUMN IF NOT EXISTS and ADD VALUE IF NOT EXISTS change nothing, and that an ALTER
// IF EXISTS of a missing table is no error.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkIfExistsFormsReplayAsTheEngineDoes(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.Idempotent == "" {
		return notApplicable("the fixture names no IF [NOT] EXISTS forms")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.Idempotent)
	if problem != "" {
		return failedWith(problem)
	}
	var problems []string
	if columns := catalogueColumnNames(catalogue, "items"); !slices.Equal(columns, []string{"id", "name"}) {
		problems = append(problems, fmt.Sprintf("items must keep exactly the columns [id name], got %v", columns))
	}
	if columns := relationColumnNames(catalogue, "items_view"); columns != nil && !slices.Equal(columns, []string{"id", "name"}) {
		problems = append(problems, fmt.Sprintf("items_view must keep the columns [id name] it was first created with, got %v", columns))
	}
	for _, schema := range catalogue.Schemas {
		for _, enum := range schema.Enums {
			if duplicate := firstDuplicate(enum.Values); duplicate != "" {
				problems = append(problems, fmt.Sprintf("the enum %s must hold %q once, got %v", enum.Name, duplicate, enum.Values))
			}
		}
	}
	return outcomeOf(problems)
}

// checkPrimaryKeyColumnsFollowTheEngine checks that the columns of a primary key declared
// at table level, or added later, are NOT NULL exactly where the engine makes them so.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkPrimaryKeyColumnsFollowTheEngine(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.KeyedTables == "" {
		return notApplicable("the fixture creates no table with a table-level primary key")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.KeyedTables)
	if problem != "" {
		return failedWith(problem)
	}
	var problems []string
	keyed := 0
	for _, schema := range catalogue.Schemas {
		for _, table := range schema.Tables {
			if !slices.ContainsFunc(table.Columns, func(column sqlmodel.Column) bool { return strings.EqualFold(column.Name, keyColumnPrefix) }) {
				continue
			}
			keyed++
			problems = append(problems, keyNullabilityProblems(table, fixture.Replay.KeyColumnsNullable)...)
		}
	}
	if keyed == 0 {
		problems = append(problems, "KeyedTables must create at least one table with the column left_id")
	}
	return outcomeOf(problems)
}

// keyNullabilityProblems describes how a keyed table's columns differ from the engine's
// nullability.
//
// Takes table (*sqlmodel.Table) which is the table.
// Takes keyNullable (bool) which is whether the engine lets a key column hold NULL.
//
// Returns []string which describes each difference.
func keyNullabilityProblems(table *sqlmodel.Table, keyNullable bool) []string {
	var problems []string
	for index := range table.Columns {
		column := &table.Columns[index]
		want := keyNullable
		if strings.EqualFold(column.Name, "note") {
			want = true
		}
		if column.Nullable != want {
			problems = append(problems, fmt.Sprintf("the column %s.%s must be nullable=%t, as the engine makes it, got nullable=%t",
				table.Name, column.Name, want, column.Nullable))
		}
	}
	return problems
}

// checkColumnPlacementsReplayAsTheEngineDoes checks that FIRST, BEFORE and AFTER place
// added, moved and changed columns where the engine does, so SELECT * scans in table
// order.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkColumnPlacementsReplayAsTheEngineDoes(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.Placement == "" {
		return notApplicable("the dialect has no column placements")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.Placement)
	if problem != "" {
		return failedWith(problem)
	}
	if columns := catalogueColumnNames(catalogue, "placed"); !slices.Equal(columns, fixture.Replay.PlacedColumns) {
		return failed("placed must hold the columns %v in that order, as the engine keeps them, got %v",
			fixture.Replay.PlacedColumns, columns)
	}
	return passed
}

// checkRenamesReplayAsTheEngineDoes checks that renamed and moved relations are known by
// their new names alone.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkRenamesReplayAsTheEngineDoes(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.Renames == "" {
		return notApplicable("the fixture names no renames")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.Renames)
	if problem != "" {
		return failedWith(problem)
	}
	var problems []string
	if relationColumnNames(catalogue, "items") != nil {
		problems = append(problems, "items must be gone once it is renamed")
	}
	for _, name := range fixture.Replay.RenamedRelations {
		if columns := relationColumnNames(catalogue, name); !slices.Equal(columns, []string{"id", "name"}) {
			problems = append(problems, fmt.Sprintf("the relation %s must hold the columns [id name] after the renames, got %v", name, columns))
		}
	}
	return outcomeOf(problems)
}

// checkViewStarsExpandToTheirColumns checks that a view defined with SELECT * or a
// qualified star holds the columns the star stands for, under the names a declared column
// list gives them.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkViewStarsExpandToTheirColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.StarViews == "" {
		return notApplicable("the dialect has no views")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.StarViews)
	if problem != "" {
		return failedWith(problem)
	}
	var problems []string
	for _, name := range []string{"star_view", "qualified_view"} {
		if columns := relationColumnNames(catalogue, name); !slices.Equal(columns, []string{"id", "name"}) {
			problems = append(problems, fmt.Sprintf("the view %s must hold the columns [id name] its star stands for, got %v", name, columns))
		}
	}
	if columns := relationColumnNames(catalogue, "named_view"); columns != nil && !slices.Equal(columns, []string{"item_id", "item_name"}) {
		problems = append(problems, fmt.Sprintf("the view named_view must hold the declared columns [item_id item_name], got %v", columns))
	}
	return outcomeOf(append(problems, untypedViewColumns(catalogue)...))
}

// checkViewsFollowTheirTablesAsTheDialectSays checks Info.ViewsFollowTheirTables: once a
// column is added to items, the views star_view and qualified_view, which read it through
// a star, hold the new column where the engine reads a view's definition anew for every
// query, and keep the columns the star stood for when they were created otherwise.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkViewsFollowTheirTablesAsTheDialectSays(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.StarViews == "" || fixture.Replay.ColumnAddition == "" {
		return notApplicable("the fixture adds no column under a view")
	}
	catalogue, problem := replayCatalogue(ctx, fixture, fixture.Replay.StarViews+"\n"+fixture.Replay.ColumnAddition)
	if problem != "" {
		return failedWith(problem)
	}
	want := []string{"id", "name"}
	if fixture.NewDialect().Info().ViewsFollowTheirTables {
		want = append(want, "extra")
	}
	var problems []string
	for _, name := range []string{"star_view", "qualified_view"} {
		if columns := relationColumnNames(catalogue, name); !slices.Equal(columns, want) {
			problems = append(problems, fmt.Sprintf("the view %s must hold the columns %v once items gains extra, as Info.ViewsFollowTheirTables "+
				"is %v, got %v", name, want, fixture.NewDialect().Info().ViewsFollowTheirTables, columns))
		}
	}
	return outcomeOf(append(problems, untypedViewColumns(catalogue)...))
}

// untypedViewColumns describes each view column whose type is unknown, as a column a star
// stands for has the type of the column it stands for.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the views.
//
// Returns []string which describes each such column.
func untypedViewColumns(catalogue *sqlmodel.Catalogue) []string {
	var problems []string
	for _, schema := range catalogue.Schemas {
		for _, view := range schema.Views {
			for index := range view.Columns {
				if view.Columns[index].SQLType.Category == sqlmodel.TypeCategoryUnknown {
					problems = append(problems, fmt.Sprintf("the column %s.%s must take the type of the column its star stands for",
						view.Name, view.Columns[index].Name))
				}
			}
		}
	}
	return problems
}

// checkAcceptedDDLReplaysWithoutAnError checks that every migration the engine accepts,
// as the fixture lists them, replays without an error diagnostic.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkAcceptedDDLReplaysWithoutAnError(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || len(fixture.Replay.AcceptedForms) == 0 {
		return notApplicable("the fixture lists no DDL forms the engine accepts")
	}
	var problems []string
	for _, form := range fixture.Replay.AcceptedForms {
		if _, problem := replayCatalogue(ctx, fixture, form); problem != "" {
			problems = append(problems, problem)
		}
	}
	return outcomeOf(problems)
}

// checkDerivedRelationsAreQueryable checks that the relations DDL derives, such as
// partitions and sequences, can be read by a query, with typed columns and no diagnostic.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkDerivedRelationsAreQueryable(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Replay == nil || fixture.Replay.DerivedRelations == "" {
		return notApplicable("the dialect derives no relations from DDL")
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{baseMigrationFile: fixture.BaseMigration, replayMigration: fixture.Replay.DerivedRelations},
		map[string]string{"derived.sql": fixture.Replay.DerivedQueries})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("every query reading a derived relation must analyse without a diagnostic, got %s", formatDiagnostics(result.Diagnostics))
	}
	var problems []string
	for _, query := range emitter.queries {
		for index := range query.OutputColumns {
			if query.OutputColumns[index].SQLType.Category == sqlmodel.TypeCategoryUnknown {
				problems = append(problems, fmt.Sprintf("the column %s of %s must be typed", query.OutputColumns[index].Name, query.Name))
			}
		}
	}
	if len(emitter.queries) == 0 {
		problems = append(problems, "the queries reading the derived relations must reach the emitter")
	}
	return outcomeOf(problems)
}

// failedWith reports a check that found one problem, described in full.
//
// Takes problem (string) which describes it.
//
// Returns checkOutcome which holds the problem.
func failedWith(problem string) checkOutcome {
	return outcomeOf([]string{problem})
}

// checkMissingSemicolonIsReported checks that two migration statements run together, the
// semicolon between them missing, are reported as the engine rejects them, rather than
// read as one statement that silently drops the second.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkMissingSemicolonIsReported(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	joined := strings.Replace(fixture.SplitterMigration, ";", "", 1)
	_, result, err := runGenerate(ctx, fixture.NewDialect(), map[string]string{baseMigrationFile: joined}, nil)
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if !hasErrorDiagnostic(result, baseMigrationFile) {
		return failed("the migration %q, whose first semicolon is missing, must produce an error diagnostic, got %s",
			joined, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// replayCatalogue generates over Fixture.BaseMigration followed by a replay migration and
// returns the catalogue.
//
// Takes fixture (*Fixture) which supplies the dialect and the base migration.
// Takes migration (string) which is the replay migration.
//
// Returns *sqlmodel.Catalogue which is the catalogue the migrations build.
// Returns string which describes why the catalogue is unusable, or is empty.
func replayCatalogue(ctx context.Context, fixture *Fixture, migration string) (*sqlmodel.Catalogue, string) {
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{baseMigrationFile: fixture.BaseMigration, replayMigration: migration}, nil)
	if err != nil {
		return nil, "generation itself should not fail fatally: " + err.Error()
	}
	if hasErrorDiagnostic(result, "") {
		return nil, "the migration " + fmt.Sprintf("%q", migration) + " is valid on the engine and must replay without an error, got " +
			formatDiagnostics(result.Diagnostics)
	}
	if emitter.catalogue == nil {
		return nil, "generation must reach the emitter with a catalogue"
	}
	return emitter.catalogue, ""
}

// relationColumnNames returns the lower-cased column names of a table or view, in order,
// looked up case-insensitively across every schema.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
// Takes name (string) which is the relation's name.
//
// Returns []string which holds the column names, or nil when no relation has the name.
func relationColumnNames(catalogue *sqlmodel.Catalogue, name string) []string {
	if columns := catalogueColumnNames(catalogue, name); columns != nil {
		return columns
	}
	for _, schema := range catalogue.Schemas {
		for viewName, view := range schema.Views {
			if !strings.EqualFold(viewName, name) || view == nil {
				continue
			}
			columns := make([]string, 0, len(view.Columns))
			for index := range view.Columns {
				columns = append(columns, strings.ToLower(view.Columns[index].Name))
			}
			return columns
		}
	}
	return nil
}

// firstDuplicate returns the first value that appears more than once.
//
// Takes values ([]string) which are the values.
//
// Returns string which is the duplicated value, or empty when every value is distinct.
func firstDuplicate(values []string) string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			return value
		}
		seen[value] = struct{}{}
	}
	return ""
}
