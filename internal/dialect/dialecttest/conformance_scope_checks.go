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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// scopeQueriesFile is the query file the scope checks generate from.
	scopeQueriesFile = "scopes.sql"

	// joinAliasTable names the second table the aliased-join check derives from the base
	// migration.
	joinAliasTable = "item_tags"
)

var (
	// idAndName are the columns a query projecting the base migration's items table gives.
	idAndName = []expectedColumn{
		{name: "id", category: sqlmodel.TypeCategoryInteger, nullable: false},
		{name: "name", category: sqlmodel.TypeCategoryText, nullable: false},
	}

	// nestedScopeQueries read the items table through nested scopes: CTE bodies holding
	// derived tables and WITH clauses, CTEs read by later CTEs, derived tables and set
	// operation branches reading the CTEs of an enclosing WITH clause, and a derived table
	// in the second arm of a set operation.
	nestedScopeQueries = []scopeQuery{
		{name: "StarOverAnEarlierCTE", columns: idAndName,
			sql: "WITH a AS (SELECT id, name FROM items), b AS (SELECT * FROM a) SELECT * FROM b"},
		{name: "EarlierCTEJoinedInALaterOne", columns: idAndName,
			sql: "WITH a AS (SELECT id, name FROM items), b AS (SELECT x.id, y.name FROM a x JOIN a y ON x.id = y.id) " +
				"SELECT id, name FROM b"},
		{name: "DerivedTableInACTEBody", columns: idAndName,
			sql: "WITH a AS (SELECT d.id, d.name FROM (SELECT id, name FROM items) d) SELECT id, name FROM a"},
		{name: "WithClauseInACTEBody", columns: idAndName,
			sql: "WITH a AS (WITH inner_rows AS (SELECT id, name FROM items) SELECT id, name FROM inner_rows) SELECT id, name FROM a"},
		{name: "CTEInADerivedTable", columns: idAndName,
			sql: "WITH a AS (SELECT id, name FROM items) SELECT d.id, d.name FROM (SELECT id, name FROM a) d"},
		{name: "CTEInSetOperationBranches", columns: idAndName,
			sql: "WITH a AS (SELECT id, name FROM items) SELECT u.id, u.name FROM (SELECT id, name FROM a UNION ALL SELECT id, name FROM a) u"},
		{name: "DerivedTableInALaterArm", columns: idAndName,
			sql: "SELECT id, name FROM items UNION ALL SELECT d.id, d.name FROM (SELECT id, name FROM items) d"},
	}

	// correlatedInsertQueries insert the rows a query selects, each filtering them with a
	// subquery in its WHERE clause that reads the selecting query's relation by its alias.
	correlatedInsertQueries = []string{
		"INSERT INTO items (id, name) SELECT i.id, i.name FROM items i WHERE NOT EXISTS (SELECT 1 FROM items j WHERE j.id = i.id)",
		"INSERT INTO items (id, name) SELECT i.id, i.name FROM items i WHERE EXISTS (SELECT 1 FROM items j WHERE j.name = i.name)",
		"INSERT INTO items (id, name) SELECT i.id, i.name FROM items i WHERE (SELECT count(*) FROM items j WHERE j.id = i.id) = 0",
	}

	// scalarSubqueryQueries project scalar subqueries: one reading a column of the outer
	// query, and one without an alias, which takes its column's name.
	scalarSubqueryQueries = []scopeQuery{
		{name: "ScalarSubqueries",
			sql: "SELECT id, (SELECT i.name FROM items i WHERE i.id = items.id) AS other, " +
				"(SELECT i.name FROM items i ORDER BY i.id LIMIT 1) FROM items",
			columns: []expectedColumn{
				{name: "id", category: sqlmodel.TypeCategoryInteger, nullable: false},
				{name: "other", category: sqlmodel.TypeCategoryText, nullable: true},
				{name: "name", category: sqlmodel.TypeCategoryText, nullable: true},
			}},
	}

	// renamedIDAndName are the columns of the items table renamed a and b.
	renamedIDAndName = []expectedColumn{
		{name: "a", category: sqlmodel.TypeCategoryInteger, nullable: false},
		{name: "b", category: sqlmodel.TypeCategoryText, nullable: false},
	}

	// derivedColumnListQuery renames the columns of a derived table through its column list.
	derivedColumnListQuery = scopeQuery{name: "DerivedColumnList", columns: renamedIDAndName,
		sql: "SELECT d.a, d.b FROM (SELECT id, name FROM items) AS d (a, b)"}

	// cteColumnListQuery renames the columns of a CTE whose body selects a star through its
	// column list.
	cteColumnListQuery = scopeQuery{name: "CTEColumnList", columns: renamedIDAndName,
		sql: "WITH t (a, b) AS (SELECT * FROM items) SELECT a, b FROM t"}

	// tableColumnListQuery renames the columns of a table through the column alias list
	// after its alias.
	tableColumnListQuery = scopeQuery{name: "TableColumnList", columns: renamedIDAndName,
		sql: "SELECT i.a, i.b FROM items AS i (a, b)"}

	// valuesArmQueries write a VALUES list where a query stands: as the arm of a set
	// operation after one that names the columns, as the body of a CTE that names them, and
	// as the anchor of a recursive CTE, so the names are the same on every engine.
	valuesArmQueries = []scopeQuery{
		{name: "ValuesArm", columns: idAndName, sql: "SELECT id, name FROM items UNION ALL VALUES (0, 'none')"},
		{name: "ValuesBody", columns: idAndName,
			sql: "WITH pairs (id, name) AS (VALUES (1, 'one'), (2, 'two')) SELECT id, name FROM pairs"},
		{name: "ValuesAnchor", columns: []expectedColumn{{name: "n", category: sqlmodel.TypeCategoryInteger, nullable: false}},
			sql: "WITH RECURSIVE c (n) AS (VALUES (1) UNION ALL SELECT n + 1 FROM c WHERE n < 3) SELECT n FROM c"},
	}

	// joinAliasQueries name a parenthesised join by an alias.
	joinAliasQueries = []scopeQuery{
		{name: "AliasedJoinStar", sql: "SELECT r.* FROM (items JOIN " + joinAliasTable + " USING (id)) AS r",
			columns: []expectedColumn{
				{name: "id", category: sqlmodel.TypeCategoryInteger, nullable: false},
				{name: "name", category: sqlmodel.TypeCategoryText, nullable: false},
				{name: "name", category: sqlmodel.TypeCategoryText, nullable: false},
			}},
		{name: "AliasedJoinColumn", sql: "SELECT r.id FROM (items i JOIN " + joinAliasTable + " t ON i.id = t.id) AS r (id, name, tag_id, tag)",
			columns: []expectedColumn{
				{name: "id", category: sqlmodel.TypeCategoryInteger, nullable: false},
			}},
	}
)

// scopeQuery is a query a scope check runs and the columns it must give.
type scopeQuery struct {
	// name is the query's name.
	name string

	// sql is the query, without its directive.
	sql string

	// columns lists the columns the query must give, in order.
	columns []expectedColumn
}

// expectedColumn is a column a scope check expects: its name and the category of its
// type.
type expectedColumn struct {
	// name is the column's name, compared without regard to case.
	name string

	// category is the category of the column's type.
	category sqlmodel.SQLTypeCategory

	// nullable reports that the column must be nullable; false leaves its nullability
	// unchecked.
	nullable bool
}

// aliasOperandProbe is a query comparing a parameter with a select-list alias in one
// clause.
type aliasOperandProbe struct {
	// clause names the clause.
	clause string

	// sql is the query, without its directive.
	sql string
}

// checkNestedQueriesSeeEnclosingRelations checks that nested queries can resolve
// enclosing relations without leaking local relations between set-operation branches.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkNestedQueriesSeeEnclosingRelations(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	outcome := runScopeQueries(ctx, fixture, fixture.BaseMigration, nestedScopeQueries)
	if outcome.NotApplicable != "" {
		return outcome
	}
	return outcomeOf(append(outcome.Problems, correlatedInsertProblems(ctx, fixture)...))
}

// correlatedInsertProblems generates the correlated INSERT ... SELECT queries over the
// base migration and reports any diagnostic they draw, since each subquery's reference to
// the selecting query's alias resolves.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns []string which describes each problem found.
func correlatedInsertProblems(ctx context.Context, fixture *Fixture) []string {
	engine := fixture.NewDialect()
	var source strings.Builder
	for index, query := range correlatedInsertQueries {
		source.WriteString(directiveLine(engine, fmt.Sprintf("CorrelatedInsert%d", index+1), "exec") + query + ";\n\n")
	}
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{scopeQueriesFile: source.String()})
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	var problems []string
	if len(result.Diagnostics) > 0 {
		problems = append(problems, "an INSERT whose query's WHERE subquery reads the query's alias must analyse without "+
			"diagnostics, got "+formatDiagnostics(result.Diagnostics))
	}
	for index, query := range correlatedInsertQueries {
		if findQuery(emitter.queries, fmt.Sprintf("CorrelatedInsert%d", index+1)) == nil {
			problems = append(problems, fmt.Sprintf("%q was not analysed", query))
		}
	}
	return problems
}

// checkScalarSubqueriesTakeTheirColumnsType checks that a scalar subquery is typed from
// the one column it selects, nullable since it may find no row, whether it reads a column
// of the outer query or not, and named after that column when it has no alias.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkScalarSubqueriesTakeTheirColumnsType(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runScopeQueries(ctx, fixture, fixture.BaseMigration, scalarSubqueryQueries)
}

// checkParametersComparedWithScalarSubqueriesAreTyped checks that a parameter a predicate
// compares with a scalar subquery takes the type of the subquery's column.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkParametersComparedWithScalarSubqueriesAreTyped(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.BaseMigration == "" {
		return notApplicable("the fixture sets no BaseMigration")
	}
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("Info.Placeholders lists no placeholder form")
	}
	placeholder := info.Placeholders.Sigils[0].Render(1, info.Placeholders.BoundName("largest"), placeholderFormType)
	query := directiveLine(engine, "ComparedWithAScalarSubquery", "many") +
		"SELECT id FROM items WHERE (SELECT max(i.id) FROM items i) = " + placeholder + ";\n"
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{scopeQueriesFile: query})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("a parameter compared with a scalar subquery must analyse without diagnostics, got %s",
			formatDiagnostics(result.Diagnostics))
	}
	analysed := findQuery(emitter.queries, "ComparedWithAScalarSubquery")
	if analysed == nil || len(analysed.Parameters) != 1 {
		return failed("the query ComparedWithAScalarSubquery was not analysed into one parameter")
	}
	if parameter := analysed.Parameters[0]; parameter.SQLType.Category != sqlmodel.TypeCategoryInteger {
		return failed("a parameter compared with (SELECT max(i.id) FROM items i) is %s, but must take the subquery's integer type",
			describeType(&parameter.SQLType))
	}
	return passed
}

// checkColumnListsNameTheColumns checks that the column list a derived table, a CTE or a
// table's alias writes renames the relation's columns in order, after a star in its query
// is expanded.
//
// Takes fixture (*Fixture) which supplies the dialect, its base migration and which
// column lists it accepts.
//
// Returns checkOutcome which holds any problem found.
func checkColumnListsNameTheColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	var queries []scopeQuery
	if fixture.DerivedColumnLists {
		queries = append(queries, derivedColumnListQuery)
	}
	if fixture.CTEColumnLists {
		queries = append(queries, cteColumnListQuery)
	}
	if fixture.TableColumnLists {
		queries = append(queries, tableColumnListQuery)
	}
	if len(queries) == 0 {
		return notApplicable("the fixture accepts no column list after a derived table, a CTE or a table")
	}
	return runScopeQueries(ctx, fixture, fixture.BaseMigration, queries)
}

// checkValuesListsStandWhereQueriesDo checks that a dialect that accepts a VALUES list
// where a query stands analyses one as the arm of a set operation, the body of a CTE and
// the anchor of a recursive CTE, typing its columns from its rows.
//
// Takes fixture (*Fixture) which supplies the dialect, its base migration and whether it
// accepts the form.
//
// Returns checkOutcome which holds any problem found.
func checkValuesListsStandWhereQueriesDo(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !fixture.ValuesArms {
		return notApplicable("the fixture does not accept a VALUES list where a query stands")
	}
	return runScopeQueries(ctx, fixture, fixture.BaseMigration, valuesArmQueries)
}

// checkAliasedJoinsAreRelations checks that an alias on a parenthesised join names a
// relation holding every column of the join, whose star lists them and whose column list
// renames them.
//
// Takes fixture (*Fixture) which supplies the dialect, its base migration and whether it
// accepts the alias.
//
// Returns checkOutcome which holds any problem found.
func checkAliasedJoinsAreRelations(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !fixture.JoinAliases {
		return notApplicable("the fixture does not accept an alias on a parenthesised join")
	}
	if !strings.Contains(fixture.BaseMigration, "items") {
		return notApplicable("the fixture sets no BaseMigration creating items")
	}
	migration := fixture.BaseMigration + "\n" + strings.Replace(fixture.BaseMigration, "items", joinAliasTable, 1)
	return runScopeQueries(ctx, fixture, migration, joinAliasQueries)
}

// checkSelectAliasOperandsTypeParameters checks that, in each clause
// Info.SelectAliasOperands lists, a parameter compared with a select-list alias takes the
// type of the item the alias names, without the alias being reported.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkSelectAliasOperandsTypeParameters(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if info.SelectAliasOperands == 0 {
		return notApplicable("Info.SelectAliasOperands lists no clause")
	}
	if fixture.BaseMigration == "" || len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the fixture sets no BaseMigration or the dialect lists no placeholder form")
	}
	var problems []string
	for _, probe := range aliasOperandProbes(&info) {
		problems = append(problems, aliasOperandProblems(ctx, engine, fixture, probe)...)
	}
	return outcomeOf(problems)
}

// aliasOperandProbes returns a probe for each of WHERE and HAVING that
// Info.SelectAliasOperands lists.
//
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns []aliasOperandProbe which holds the probes.
func aliasOperandProbes(info *dialect.Info) []aliasOperandProbe {
	placeholder := info.Placeholders.Sigils[0].Render(1, info.Placeholders.BoundName("least"), placeholderFormType)
	var probes []aliasOperandProbe
	if info.SelectAliasOperands.Includes(ast.ClauseWhere) {
		probes = append(probes, aliasOperandProbe{clause: "WHERE",
			sql: "SELECT id + 1 AS bumped FROM items WHERE bumped > " + placeholder})
	}
	if info.SelectAliasOperands.Includes(ast.ClauseHaving) {
		probes = append(probes, aliasOperandProbe{clause: "HAVING",
			sql: "SELECT name, count(*) AS total FROM items GROUP BY name HAVING total > " + placeholder})
	}
	return probes
}

// aliasOperandProblems runs one alias operand probe.
//
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes fixture (*Fixture) which supplies the base migration.
// Takes probe (aliasOperandProbe) which is the probe.
//
// Returns []string which describes each problem found.
func aliasOperandProblems(ctx context.Context, engine dialect.DialectPort, fixture *Fixture, probe aliasOperandProbe) []string {
	query := directiveLine(engine, "AliasOperand", "many") + probe.sql + ";\n"
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{scopeQueriesFile: query})
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	if len(result.Diagnostics) > 0 {
		return []string{fmt.Sprintf("%q must analyse without diagnostics where Info.SelectAliasOperands holds %s, got %s",
			probe.sql, probe.clause, formatDiagnostics(result.Diagnostics))}
	}
	analysed := findQuery(emitter.queries, "AliasOperand")
	if analysed == nil || len(analysed.Parameters) != 1 || len(analysed.OutputColumns) == 0 {
		return []string{fmt.Sprintf("%q was not analysed into one parameter", probe.sql)}
	}
	aliased := analysed.OutputColumns[len(analysed.OutputColumns)-1]
	if parameter := analysed.Parameters[0]; parameter.SQLType.Category != aliased.SQLType.Category {
		return []string{fmt.Sprintf("in %q the parameter compared with the alias is %s, but must take the aliased item's type, %s",
			probe.sql, describeType(&parameter.SQLType), describeType(&aliased.SQLType))}
	}
	return nil
}

// runScopeQueries generates a set of queries over a migration and checks that each
// analyses without diagnostics into the columns it must give.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes migration (string) which is the migration the queries read.
// Takes queries ([]scopeQuery) which are the queries.
//
// Returns checkOutcome which holds any problem found.
func runScopeQueries(ctx context.Context, fixture *Fixture, migration string, queries []scopeQuery) checkOutcome {
	if migration == "" {
		return notApplicable("the fixture sets no BaseMigration")
	}
	engine := fixture.NewDialect()
	var source strings.Builder
	for _, query := range queries {
		source.WriteString(directiveLine(engine, query.name, "many") + query.sql + ";\n\n")
	}
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: migration},
		map[string]string{scopeQueriesFile: source.String()})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	var problems []string
	if len(result.Diagnostics) > 0 {
		problems = append(problems, "the queries must analyse without diagnostics, got "+formatDiagnostics(result.Diagnostics))
	}
	for _, query := range queries {
		problems = append(problems, scopeQueryProblems(findQuery(emitter.queries, query.name), query)...)
	}
	return outcomeOf(problems)
}

// scopeQueryProblems compares an analysed query's columns with those it must give.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is the analysed query, or nil.
// Takes query (scopeQuery) which is the query and its expected columns.
//
// Returns []string which describes each problem found.
func scopeQueryProblems(analysed *sqlmodel.AnalysedQuery, query scopeQuery) []string {
	if analysed == nil {
		return []string{fmt.Sprintf("%q was not analysed", query.sql)}
	}
	if len(analysed.OutputColumns) != len(query.columns) {
		return []string{fmt.Sprintf("%q must give %d columns, got %d", query.sql, len(query.columns), len(analysed.OutputColumns))}
	}
	var problems []string
	for index, expected := range query.columns {
		column := &analysed.OutputColumns[index]
		if !strings.EqualFold(column.Name, expected.name) || column.SQLType.Category != expected.category ||
			(expected.nullable && !column.Nullable) {
			problems = append(problems, fmt.Sprintf("column %d of %q must be %s of the %s category%s, got %s %s, nullable %v",
				index+1, query.sql, expected.name, expected.category, nullableNote(expected.nullable), column.Name,
				describeType(&column.SQLType), column.Nullable))
		}
	}
	return problems
}

// nullableNote describes an expected column's nullability for a problem.
//
// Takes nullable (bool) which is true when the column must be nullable.
//
// Returns string which is the note, or empty.
func nullableNote(nullable bool) string {
	if nullable {
		return ", nullable"
	}
	return ""
}
