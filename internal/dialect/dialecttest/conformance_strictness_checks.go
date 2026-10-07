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

	"seagull.sh/seagull/internal/diag"
)

const (
	// rejectedQueryFormat names the query file and query a rejection check writes for each
	// of its statements.
	rejectedQueryFormat = "Rejected%d"

	// baseMigrationFile is the name a check gives the fixture's BaseMigration.
	baseMigrationFile = "001_items.up.sql"
)

var (
	// unrecognisedStatements open with a word that starts no statement of any engine.
	unrecognisedStatements = []string{"SELEC id FROM items;"}

	// unbalancedConditions leave a parenthesis of their condition open.
	unbalancedConditions = []string{
		"SELECT id FROM items WHERE id = (;",
		"SELECT id FROM items WHERE (id = 1;",
	}

	// emptyConditions write a clause keyword with no condition after it.
	emptyConditions = []string{
		"SELECT id FROM items WHERE;",
		"SELECT id FROM items GROUP BY id HAVING;",
		"SELECT a.id FROM items a JOIN items b ON;",
	}

	// danglingOperators end their condition with an operator that has no right operand.
	danglingOperators = []string{
		"SELECT id FROM items WHERE id = 1 AND;",
		"SELECT id FROM items WHERE id = 1 OR;",
		"SELECT id FROM items WHERE id =;",
	}

	// conditionLeftovers follow a complete condition with a token the condition cannot hold,
	// which a scan that steps over a condition token by token would not notice.
	conditionLeftovers = []string{
		"SELECT id FROM items WHERE id = 1 name;",
		"SELECT id FROM items GROUP BY id HAVING count(*) > 1 name;",
		"SELECT a.id FROM items a JOIN items b ON a.id = b.id name;",
	}
)

// checkUnrecognisedStatement checks that a statement opening with a word that starts no
// statement is rejected with a parse error, rather than analysed as a statement with
// nothing to report.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkUnrecognisedStatement(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return rejectionOutcome(ctx, fixture, unrecognisedStatements)
}

// checkUnbalancedCondition checks that a condition leaving a parenthesis open is rejected
// with a parse error.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkUnbalancedCondition(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return rejectionOutcome(ctx, fixture, unbalancedConditions)
}

// checkEmptyCondition checks that a WHERE, HAVING or ON keyword with no condition after
// it is rejected with a parse error.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkEmptyCondition(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return rejectionOutcome(ctx, fixture, emptyConditions)
}

// checkDanglingOperator checks that a condition ending with an operator that has no right
// operand is rejected with a parse error.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkDanglingOperator(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return rejectionOutcome(ctx, fixture, danglingOperators)
}

// checkConditionLeftovers checks that a token left over after a complete WHERE, HAVING or
// ON condition is rejected with a parse error, so a dialect that steps over a condition
// token by token still reads all of it as one expression.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkConditionLeftovers(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return rejectionOutcome(ctx, fixture, conditionLeftovers)
}

// checkFromIsNeverASelectedColumn checks that SELECT followed directly by FROM and a
// table never reads the keyword FROM as a column: the statement is rejected, or, where
// the engine accepts an empty select list, it has no output columns.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkFromIsNeverASelectedColumn(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	const statement = "SELECT FROM items;"
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"select_from.sql": directiveLine(engine, "SelectFrom", "many") + statement})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if hasDiagnostic(result, "select_from.sql", diag.CodeParseError) {
		return passed
	}
	query := findQuery(emitter.queries, "SelectFrom")
	if query == nil {
		return failed("%q must be rejected with a %s parse error or analysed, got %s", statement, diag.CodeParseError,
			formatDiagnostics(result.Diagnostics))
	}
	if columns := outputColumnNames(query); len(columns) > 0 {
		return failed("%q must not read FROM as a selected column, got the output columns %v and diagnostics %s",
			statement, columns, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// rejectionOutcome generates each statement as a query of its own over the base migration
// and checks that each is rejected with a parse error.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
// Takes statements ([]string) which are the statements to reject.
//
// Returns checkOutcome which holds a problem for each statement not rejected.
func rejectionOutcome(ctx context.Context, fixture *Fixture, statements []string) checkOutcome {
	var problems []string
	for index, statement := range statements {
		engine := fixture.NewDialect()
		name := fmt.Sprintf(rejectedQueryFormat, index+1)
		filename := name + sqlExtension
		_, result, err := runGenerate(ctx, engine,
			map[string]string{baseMigrationFile: fixture.BaseMigration},
			map[string]string{filename: directiveLine(engine, name, "many") + statement})
		if err != nil {
			problems = append(problems, fmt.Sprintf(generationFatalFormat, err))
			continue
		}
		if !hasDiagnostic(result, filename, diag.CodeParseError) {
			problems = append(problems, fmt.Sprintf("%q must be rejected with a %s parse error, got %s", statement,
				diag.CodeParseError, formatDiagnostics(result.Diagnostics)))
		}
	}
	return outcomeOf(problems)
}
