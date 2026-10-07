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

	"seagull.sh/seagull/internal/dialect/ast"
)

// checkGroupingModifiersLeaveKeysOut checks grouping keys and their output nullability.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkGroupingModifiersLeaveKeysOut(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.GroupingQuery == "" {
		return notApplicable("the dialect has no grouping modifier")
	}
	engine := fixture.NewDialect()
	analysis, err := analyseOneStatement(ctx, engine, fixture.GroupingQuery)
	if err != nil {
		return failed("%q must analyse: %v", fixture.GroupingQuery, err)
	}
	if analysis.GroupingSets == nil || len(analysis.GroupingSets.Keys) == 0 {
		return failed("%q must report its grouping modifier and the key it leaves out of some rows", fixture.GroupingQuery)
	}
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"subtotals.sql": directiveLine(engine, "Subtotals", "many") + fixture.GroupingQuery})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q must analyse without diagnostics, got %s", fixture.GroupingQuery, formatDiagnostics(result.Diagnostics))
	}
	query := findQuery(emitter.queries, "Subtotals")
	if query == nil || len(query.OutputColumns) != 2 {
		return failed("the query Subtotals must be analysed into the columns id and total")
	}
	return outcomeOf(groupingProblems(analysis.GroupingSets, query.OutputColumns[0].Nullable, query.OutputColumns[1].Nullable))
}

// groupingProblems describes how the nullability of a grouping query's key and aggregate
// differ from what its grouping modifier makes them.
//
// Takes sets (*ast.GroupingSets) which is the modifier as the dialect reports it.
// Takes keyNullable (bool) which is the nullability of the key column.
// Takes totalNullable (bool) which is the nullability of the counting column.
//
// Returns []string which holds the problems.
func groupingProblems(sets *ast.GroupingSets, keyNullable, totalNullable bool) []string {
	var problems []string
	switch {
	case sets.FillsDefaults && keyNullable:
		problems = append(problems, "the key id must keep its nullability where the engine fills a left-out key with its default value")
	case !sets.FillsDefaults && !keyNullable:
		problems = append(problems, "the key id must be nullable: the subtotal rows hold NULL in its place")
	}
	if totalNullable {
		problems = append(problems, "the count total is never NULL, a grouping modifier or not")
	}
	return problems
}
