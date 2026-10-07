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

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// returningQueryName is the query the RETURNING check analyses.
	returningQueryName = "ChangeItem"

	// returningQueryFile is the query file the RETURNING check writes.
	returningQueryFile = "returning.sql"
)

var (
	// returningStatements lists the statements the RETURNING check writes, one per kind of
	// row change. They use literals rather than placeholders, so they read the same in every
	// dialect's syntax.
	returningStatements = []struct {
		sql    string
		change ast.DataChange
	}{
		{change: ast.DataChangeInsert, sql: "INSERT INTO items (id, name) VALUES (1, 'one') RETURNING id;"},
		{change: ast.DataChangeUpdate, sql: "UPDATE items SET name = 'two' WHERE id = 1 RETURNING id;"},
		{change: ast.DataChangeDelete, sql: "DELETE FROM items WHERE id = 1 RETURNING id;"},
	}
)

// checkReturningFollowsInfo verifies RETURNING support against Info.Returning.
//
// Supported forms must generate output columns; unsupported forms must produce the
// dedicated diagnostic.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkReturningFollowsInfo(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	supported := engine.Info().Returning
	var problems []string
	for _, statement := range returningStatements {
		query := directiveLine(engine, returningQueryName, "one") + statement.sql
		emitter, result, err := runGenerate(ctx, engine,
			map[string]string{"001_items.up.sql": fixture.BaseMigration},
			map[string]string{returningQueryFile: query})
		if err != nil {
			return failed(generationFatalFormat, err)
		}
		outcome := returningReported(statement.change, result)
		if supported.Includes(statement.change) {
			outcome = returningAccepted(statement.change, emitter, result)
		}
		problems = append(problems, outcome.Problems...)
	}
	return outcomeOf(problems)
}

// returningAccepted checks a statement the dialect accepts RETURNING on: the query
// generates without an error and returns exactly the one column its clause names.
//
// Takes change (ast.DataChange) which is the statement's kind.
// Takes emitter (*recordingEmitter) which captured the analysed queries.
// Takes result (*app.Result) which holds the run's diagnostics.
//
// Returns checkOutcome which holds any problem found.
func returningAccepted(change ast.DataChange, emitter *recordingEmitter, result *app.Result) checkOutcome {
	if hasErrorDiagnostic(result, returningQueryFile) {
		return failed("Info.Returning includes %s, so its RETURNING clause must generate without an error, got %s",
			change, formatDiagnostics(result.Diagnostics))
	}
	query := findQuery(emitter.queries, returningQueryName)
	if query == nil || len(query.OutputColumns) != 1 {
		return failed("Info.Returning includes %s, so its RETURNING clause must become the %s query's one output "+
			"column, got diagnostics %s", change, returningQueryName, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// returningReported checks a statement the dialect does not accept RETURNING on: the
// clause produces the dedicated diagnostic.
//
// Takes change (ast.DataChange) which is the statement's kind.
// Takes result (*app.Result) which holds the run's diagnostics.
//
// Returns checkOutcome which holds any problem found.
func returningReported(change ast.DataChange, result *app.Result) checkOutcome {
	if !hasDiagnostic(result, returningQueryFile, diag.CodeReturningNotSupported) {
		return failed("Info.Returning does not include %s, so its RETURNING clause must produce a %s diagnostic, got %s",
			change, diag.CodeReturningNotSupported, formatDiagnostics(result.Diagnostics))
	}
	return passed
}
