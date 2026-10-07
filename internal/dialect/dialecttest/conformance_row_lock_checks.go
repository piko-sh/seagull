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
)

// rowLockQueriesFile is the query file the row lock check generates from.
const rowLockQueriesFile = "row_locks.sql"

// rowLockShapes place a row-locking clause, written where %s stands, at the end of the
// statement and at the end of a query nested in it: a CTE body, a derived table, an IN
// subquery, an EXISTS subquery and a scalar subquery in the select list.
var rowLockShapes = []string{
	"SELECT id FROM items WHERE id = 1 %s",
	"WITH locked AS (SELECT id FROM items %s) SELECT id FROM locked",
	"SELECT d.id FROM (SELECT id FROM items %s) d",
	"SELECT id FROM items WHERE id IN (SELECT id FROM items %s)",
	"SELECT i.id FROM items i WHERE EXISTS (SELECT 1 FROM items j WHERE j.id = i.id %s)",
	"SELECT i.id, (SELECT j.name FROM items j WHERE j.id = i.id %s) AS other FROM items i",
}

// rowLockQuery is a query the row lock check runs and whether it locks rows.
type rowLockQuery struct {
	// name is the query's name.
	name string

	// sql is the query, without its directive.
	sql string

	// locked reports that the query holds a row-locking clause.
	locked bool
}

// problems reports a locking query analysed as read-only, or a query without a lock
// analysed as writing.
//
// Takes emitter (*recordingEmitter) which holds the analysed queries.
//
// Returns []string which describes each problem found.
func (q rowLockQuery) problems(emitter *recordingEmitter) []string {
	analysed := findQuery(emitter.queries, q.name)
	switch {
	case analysed == nil:
		return []string{fmt.Sprintf("%q was not analysed", q.sql)}
	case q.locked && analysed.ReadOnly:
		return []string{fmt.Sprintf("%q locks rows, so it must not be read-only (it would run on a read replica)", q.sql)}
	case !q.locked && !analysed.ReadOnly:
		return []string{fmt.Sprintf("%q locks no rows and changes none, so it must be read-only", q.sql)}
	}
	return nil
}

// checkRowLocksAnywhereRouteToTheWriter checks that row locks in a query or its nested
// queries require the writer, while the unlocked query remains read-only.
//
// Takes fixture (*Fixture) which supplies the dialect, its base migration and its
// row-locking clauses.
//
// Returns checkOutcome which holds any problem found.
func checkRowLocksAnywhereRouteToTheWriter(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if len(fixture.RowLocks) == 0 {
		return notApplicable("the fixture lists no row-locking clause")
	}
	if fixture.BaseMigration == "" {
		return notApplicable("the fixture sets no BaseMigration")
	}
	engine := fixture.NewDialect()
	var source strings.Builder
	var queries []rowLockQuery
	for _, shape := range rowLockShapes {
		queries = append(queries, rowLockQuery{name: fmt.Sprintf("Unlocked%d", len(queries)+1),
			sql: strings.TrimSpace(fmt.Sprintf(shape, "")), locked: false})
		for _, clause := range fixture.RowLocks {
			queries = append(queries, rowLockQuery{name: fmt.Sprintf("Locked%d", len(queries)+1),
				sql: fmt.Sprintf(shape, clause), locked: true})
		}
	}
	for _, query := range queries {
		source.WriteString(directiveLine(engine, query.name, "many") + query.sql + ";\n\n")
	}
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{rowLockQueriesFile: source.String()})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	var problems []string
	if len(result.Diagnostics) > 0 {
		problems = append(problems, "the row-locking queries must analyse without diagnostics, got "+formatDiagnostics(result.Diagnostics))
	}
	for _, query := range queries {
		problems = append(problems, query.problems(emitter)...)
	}
	return outcomeOf(problems)
}
