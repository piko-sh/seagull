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
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// usingJoinTable names the second table the USING check derives from the base migration.
	usingJoinTable = "item_labels"

	// usingJoinColumnCount is the number of columns SELECT * returns over items JOIN
	// item_labels USING (id): id once, then each table's name.
	usingJoinColumnCount = 3
)

// checkUsingJoinColumnAppearsOnce checks that a join with a USING clause merges the
// column it names: SELECT * returns it once, so the generated row has as many fields as
// the database returns columns, and naming it without a qualifier is not ambiguous.
//
// The check joins the base migration's items table with a copy of it named item_labels,
// both holding id and name.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkUsingJoinColumnAppearsOnce(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.BaseMigration == "" || !strings.Contains(fixture.BaseMigration, "items") {
		return notApplicable("the fixture sets no BaseMigration creating items")
	}
	engine := fixture.NewDialect()
	migration := fixture.BaseMigration + "\n" + strings.Replace(fixture.BaseMigration, "items", usingJoinTable, 1)
	queries := directiveLine(engine, "ItemsWithLabels", "many") +
		"SELECT * FROM items JOIN " + usingJoinTable + " USING (id);\n\n" +
		directiveLine(engine, "ItemLabelIDs", "many") +
		"SELECT id FROM items JOIN " + usingJoinTable + " USING (id);\n"
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": migration},
		map[string]string{"using_join.sql": queries})
	if err != nil {
		return failed(generationFailedFormat, err)
	}

	var problems []string
	if star := findQuery(emitter.queries, "ItemsWithLabels"); star != nil {
		problems = append(problems, starColumnProblems(star.OutputColumns)...)
	} else {
		problems = append(problems, "the ItemsWithLabels query should be analysed, got diagnostics "+
			formatDiagnostics(result.Diagnostics))
	}
	if findQuery(emitter.queries, "ItemLabelIDs") == nil {
		problems = append(problems, "the ItemLabelIDs query should be analysed, got diagnostics "+
			formatDiagnostics(result.Diagnostics))
	}
	if hasDiagnostic(result, "using_join.sql", diag.CodeAmbiguousColumn) {
		problems = append(problems, "the merged column id should not be ambiguous: "+formatDiagnostics(result.Diagnostics))
	}
	return outcomeOf(problems)
}

// starColumnProblems describes how a SELECT * over items JOIN item_labels USING (id)
// differs from the three columns the database returns: id once, then each table's name.
//
// Takes columns ([]sqlmodel.OutputColumn) which are the query's output columns.
//
// Returns []string which describes each problem found.
func starColumnProblems(columns []sqlmodel.OutputColumn) []string {
	names := make([]string, len(columns))
	identifiers := 0
	for index := range columns {
		names[index] = strings.ToLower(columns[index].Name)
		if names[index] == "id" {
			identifiers++
		}
	}
	var problems []string
	if len(columns) != usingJoinColumnCount {
		problems = append(problems, fmt.Sprintf("SELECT * over a USING join should return %d columns, got %d: %v",
			usingJoinColumnCount, len(columns), names))
	}
	if identifiers != 1 {
		problems = append(problems, fmt.Sprintf("SELECT * over a USING join should return id once, got %d times: %v", identifiers, names))
	}
	return problems
}
