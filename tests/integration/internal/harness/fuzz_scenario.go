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

package harness

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// scenarioPositionChoices preserves the decoder's stable position envelope.
	scenarioPositionChoices = 8

	// scenarioBooleanFeature selects the independent Boolean declaration contract.
	scenarioBooleanFeature = "type/boolean"

	// scenarioWrapperChoices is how many optional wrappers the decoder chooses between.
	// Choice 0 is parentheses.
	scenarioWrapperChoices = 4

	// scenarioWrapperFirst is the first input byte that chooses a wrapper.
	scenarioWrapperFirst = 5

	// scenarioWrapperLast is the input byte after the last one that chooses a wrapper.
	scenarioWrapperLast = 8

	// relationPartition says the later queries split the first query's rows.
	relationPartition = "partition"

	// relationCountTruth says the first query counts the rows the second marks true.
	relationCountTruth = "count-truth"

	// relationEqual says two queries return the same rows.
	relationEqual = "equal"

	// relationConstantSubstitution says a scalar subquery and its observed value agree.
	relationConstantSubstitution = "constant-substitution"

	// scenarioConstantQuery reads the scalar a constant substitution replaces.
	scenarioConstantQuery = "SELECT MAX(category) FROM fuzz_items"

	// scenarioConstantProbe is the scalar subquery a constant substitution replaces.
	scenarioConstantProbe = "(" + scenarioConstantQuery + ")"
)

// scenarioExecution keeps a rendered statement with its bound arguments.
type scenarioExecution struct {
	// statement is the native SQL spelling.
	statement string

	// arguments retains binding order.
	arguments []any
}

// runFuzzScenario compares generated execution with independently bound SQL.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes oracle (FuzzOracle) which selects the required comparison.
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes metadata (*sync.Once) which records native engine metadata once.
//
// Panics if native work ignores the cancellation deadline.
func runFuzzScenario(t *testing.T, suite FuzzSuite, oracle FuzzOracle, scenario fuzzcase.Scenario, metadata *sync.Once) {
	t.Helper()
	defer func() {
		if t.Failed() {
			t.Logf("scenario %v features %v queries %+v", scenario.Data, scenario.Features, scenario.Queries)
		}
	}()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(databaseWatchdogDeadline), func() { panic("scenario ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, database, suite.Engine) })
	fuzzApply(ctx, t, database, scenario.Schema)
	fuzzApply(ctx, t, database, scenario.Inserts)
	prepareScenarioConstants(ctx, t, database, &scenario)
	observeFuzzScenario(ctx, t, database, suite, oracle, scenario)
}

// observeFuzzScenario executes each query and checks its independent relationships.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes oracle (FuzzOracle) which selects the required comparison.
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
//
// Returns [][][]any which retains rendered results after every required check.
func observeFuzzScenario(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, oracle FuzzOracle, scenario fuzzcase.Scenario) [][][]any {
	t.Helper()
	details := analyseFuzzScenario(ctx, t, suite, scenario, oracle == OracleCount)
	reference := make([][][]any, len(scenario.Queries))
	generated := make([][][]any, len(scenario.Queries))
	for index := range scenario.Queries {
		expected := scenario.Queries[index]
		query := details.Report.Queries[index]
		reference[index] = scenarioRows(ctx, t, database, suite, expected.Reference, scenarioArguments(t, suite.Engine, expected), expected.Ordered)
		rendered, arguments := renderScenario(t, suite, expected, query, query.Template)
		generated[index] = scenarioRows(ctx, t, database, suite, rendered, arguments, expected.Ordered)
		require.True(t, fuzzExactResultsEqual(reference[index], generated[index], expected.Ordered),
			"scenario result mismatch for %s input %v\nreference %s\nrendered %s\nwant %#v\ngot %#v",
			expected.Name, scenario.Data, expected.Reference, rendered, reference[index], generated[index])
		execution := scenarioExecution{statement: rendered, arguments: arguments}
		checkFuzzNulls(t, generated[index], query)
		checkScenarioNullability(t, expected, generated[index], query)
		checkScenarioOrder(ctx, t, database, suite.Engine, expected, execution)
		switch oracle {
		case OracleTypes:
			checkScenarioTypes(ctx, t, database, suite, scenario, execution, query)
		case OracleCount:
			checkScenarioCount(ctx, t, database, suite, expected, query, reference[index])
		case OracleQuery, OracleMigration:
		}
	}
	checkScenarioRelations(t, scenario.Relations, reference, generated)
	return generated
}

// checkScenarioCount compares the generated count query with the reference row count.
//
// Takes t (*testing.T) which reports a different count.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes expected (fuzzcase.ScenarioQuery) which holds the independent values.
// Takes query (*emit.AnalysedQuery) which holds the count template.
// Takes reference ([][]any) which holds the reference rows.
func checkScenarioCount(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, expected fuzzcase.ScenarioQuery, query *emit.AnalysedQuery, reference [][]any) {
	t.Helper()
	require.NotNil(t, query.CountTemplate, "scenario has no count template: %s", expected.Name)
	countSQL, countArguments := renderScenario(t, suite, expected, query, *query.CountTemplate)
	require.Empty(t, fuzzCountProblem(reference, fuzzRows(ctx, t, database, countSQL, countArguments)))
}

// checkScenarioOrder compares the row sequence of the rendered query with the reference.
//
// scenarioRows reorders rows by their first column, so a query whose own ORDER BY sorts a
// rewritten column differently from the reference would pass it. This reads both queries
// directly instead, for scenarios that make nullability claims and so come from a model
// that states its own total order.
//
// Takes t (*testing.T) which reports a different sequence.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes engine (string) which selects the native binding rules.
// Takes expected (fuzzcase.ScenarioQuery) which holds the reference.
// Takes execution (scenarioExecution) which holds the rendered statement.
func checkScenarioOrder(ctx context.Context, t *testing.T, database *sql.DB, engine string, expected fuzzcase.ScenarioQuery, execution scenarioExecution) {
	t.Helper()
	if !expected.Ordered || expected.MayBeNull == nil {
		return
	}
	want := fuzzRows(ctx, t, database, expected.Reference, scenarioArguments(t, engine, expected))
	got := fuzzRows(ctx, t, database, execution.statement, execution.arguments)
	require.True(t, fuzzExactResultsEqual(want, got, true), "row order differs from the reference\nreference %s\nrendered %s\nwant %v\ngot %v",
		expected.Reference, execution.statement, fuzzExactSequence(want), fuzzExactSequence(got))
}

// checkScenarioNullability compares observed NULLs and Seagull's nullability with the
// model's independent rule table.
//
// A NULL in a column the rule table proves never NULL is a model mistake and fails. A
// column the rule table proves never NULL that Seagull types nullable is imprecision, and
// a column the rule table admits NULL in that Seagull types non-nullable while the data
// held no NULL is unconfirmed. Both are logged for review rather than failed.
//
// Takes t (*testing.T) which reports model mistakes and logs the rest.
// Takes expected (fuzzcase.ScenarioQuery) which holds the rule table's claims.
// Takes rows ([][]any) which holds the rendered query's results.
// Takes query (*emit.AnalysedQuery) which holds Seagull's output columns.
func checkScenarioNullability(t *testing.T, expected fuzzcase.ScenarioQuery, rows [][]any, query *emit.AnalysedQuery) {
	t.Helper()
	if len(expected.MayBeNull) == 0 {
		return
	}
	require.Len(t, expected.MayBeNull, len(query.OutputColumns), "the model claims nullability for %d columns of %d", len(expected.MayBeNull), len(query.OutputColumns))
	observed := make([]bool, len(query.OutputColumns))
	for _, row := range rows {
		for index, value := range row[:min(len(row), len(observed))] {
			observed[index] = observed[index] || value == nil
		}
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		switch {
		case observed[index] && !expected.MayBeNull[index]:
			assert.Failf(t, "model mistake", "column %s returned NULL, but the rule table proves it is never NULL\n%s", column.Name, expected.Reference)
		case !expected.MayBeNull[index] && column.Nullable:
			t.Logf("nullability imprecision: column %s is never NULL, but Seagull types it nullable\n%s", column.Name, expected.Reference)
		case expected.MayBeNull[index] && !column.Nullable && !observed[index]:
			t.Logf("nullability unconfirmed: the rule table admits NULL in column %s, which Seagull types non-nullable\n%s", column.Name, expected.Reference)
		}
	}
}

// checkScenarioRelations applies each independent comparison to both execution paths.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes relations ([]fuzzcase.Relation) which lists the independent query relationships.
// Takes reference ([][][]any) which holds results from independently bound SQL.
// Takes generated ([][][]any) which holds results from rendered SQL.
func checkScenarioRelations(t *testing.T, relations []fuzzcase.Relation, reference, generated [][][]any) {
	t.Helper()
	for _, relation := range relations {
		require.Empty(t, scenarioRelationProblem(relation, reference), "native relation or model failure")
		require.Empty(t, scenarioRelationProblem(relation, generated), "generated relation failure")
	}
}

// analyseFuzzScenario requires all source queries to produce generated methods.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes dynamic (bool) which requests runtime count templates.
//
// Returns *seagull.DetailedResult which includes generated methods and their analysis.
func analyseFuzzScenario(ctx context.Context, t *testing.T, suite FuzzSuite, scenario fuzzcase.Scenario, dynamic bool) *seagull.DetailedResult {
	t.Helper()
	generator, err := seagull.New(suite.Dialect())
	require.NoError(t, err)
	sources := make([]string, 0, len(scenario.Queries))
	for index := range scenario.Queries {
		source := scenario.Queries[index].SQL
		if dynamic {
			source = strings.Replace(source, ", many)", ", many, dynamic: runtime)", 1)
		}
		sources = append(sources, source)
	}
	files := fstest.MapFS{
		"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(strings.Join(scenario.Schema, ";\n"))},
		"queries/scenario.sql":       &fstest.MapFile{Data: []byte(strings.Join(sources, "\n"))},
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "generated", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	require.NoError(t, details.Result.Err(), "positive scenario %v", scenario.Data)
	require.Len(t, details.Report.Queries, len(scenario.Queries), "scenario did not reach generation")
	require.NotEmpty(t, details.Result.Files, "scenario did not reach generation")
	return details
}

// scenarioArguments binds native occurrences independently of template rendering.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes engine (string) which selects native binding and metadata rules.
// Takes query (fuzzcase.ScenarioQuery) which retains parameter identities and inferred
// output facts.
//
// Returns []any which preserves independent native binding identities.
func scenarioArguments(t *testing.T, engine string, query fuzzcase.ScenarioQuery) []any {
	t.Helper()
	arguments := make([]any, len(query.Bindings))
	for index, identity := range query.Bindings {
		require.True(t, identity >= 0 && identity < len(query.Values), "invalid independent binding identity")
		value, err := query.Values[identity].Binding(engine)
		require.NoError(t, err)
		if engine == engineClickHouse {
			value = sql.Named("p"+strconv.Itoa(identity+1), value)
		}
		arguments[index] = value
	}
	return arguments
}

// renderScenario checks generated binding identities against independent values.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes expected (fuzzcase.ScenarioQuery) which holds independently expected
// observations.
// Takes query (*emit.AnalysedQuery) which retains parameter identities and inferred
// output facts.
// Takes template (emit.SQLTemplate) which selects the rendered query or count.
//
// Returns string which contains rendered SQL.
// Returns []any which preserves rendered parameter order.
func renderScenario(t *testing.T, suite FuzzSuite, expected fuzzcase.ScenarioQuery, query *emit.AnalysedQuery, template emit.SQLTemplate) (string, []any) {
	t.Helper()
	rendered := fuzzRenderTemplate(t, suite, query, template)
	arguments := make([]any, len(rendered.Arguments))
	for index, argument := range rendered.Arguments {
		require.True(t, argument.Parameter >= 0 && argument.Parameter < len(query.Parameters), "rendered binding index is invalid")
		identity := query.Parameters[argument.Parameter].Number - 1
		require.True(t, identity >= 0 && identity < len(expected.Values), "rendered binding is outside the independent model")
		value, err := expected.Values[identity].PositionalBinding(suite.Engine)
		if argument.Name != "" {
			value, err = expected.Values[identity].Binding(suite.Engine)
		}
		require.NoError(t, err)
		if argument.Name != "" {
			value = sql.Named(argument.Name, value)
		}
		arguments[index] = value
	}
	return rendered.SQL, arguments
}

// scenarioTypeHandled checks independently declared alias contracts.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes inferred (dialect.SQLType) which records the inferred SQL representation.
// Takes observed (string) which holds the actual native observation.
// Takes index (int) which identifies the current observation.
//
// Returns bool which reports whether a declared alias contract handled the observation.
func scenarioTypeHandled(t *testing.T, scenario fuzzcase.Scenario, inferred dialect.SQLType, observed string, index int) bool {
	t.Helper()
	if len(scenario.Data) > 0 && scenario.Data[0] == fuzzcase.QueryGrammarVersion {
		return scenarioGrammarTypeHandled(t, scenario, inferred, observed, index)
	}
	if index != 1 || len(scenario.Features) == 0 {
		return false
	}
	handled, problem := scenarioAliasedType(scenario, inferred, observed)
	assert.Empty(t, problem, problem)
	return handled
}

// scenarioGrammarTypeHandled applies the declared alias contracts to the converted column
// of a version 14 wrapped-column scenario.
//
// SQLite stores CAST(x AS DECIMAL(12, 2)) with numeric affinity, so a whole value comes
// back as an integer and a fraction as a real, whatever the declared decimal type.
// MariaDB's JSON is an alias of LONGTEXT, so JSON_ARRAY reports a text type.
//
// Takes t (*testing.T) which reports a broken contract.
// Takes scenario (fuzzcase.Scenario) which supplies the engine and features.
// Takes inferred (dialect.SQLType) which records Seagull's SQL type.
// Takes observed (string) which holds the storage class SQLite reports.
// Takes index (int) which identifies the output column.
//
// Returns bool which reports whether the contract handled the observation.
func scenarioGrammarTypeHandled(t *testing.T, scenario fuzzcase.Scenario, inferred dialect.SQLType, observed string, index int) bool {
	t.Helper()
	if scenario.Engine == engineMariaDB && index == 0 && slices.Contains(scenario.Features, "wrapped/json") {
		native, ok := fuzzScalarType(scenario.Engine, observed)
		if inferred.Category != dialect.TypeCategoryJSON || !ok || native.Category != dialect.TypeCategoryText {
			assert.Failf(t, "MariaDB JSON conversion", "Seagull infers %s, MariaDB reports %s", inferred.Category, observed)
		}
		return true
	}
	if scenario.Engine != engineSQLite || index != 0 || !slices.Contains(scenario.Features, "wrapped/decimal") {
		return false
	}
	if inferred.Category != dialect.TypeCategoryDecimal || (observed != "integer" && observed != "real") {
		assert.Failf(t, "SQLite decimal conversion", "Seagull infers %s, SQLite stores %s", inferred.Category, observed)
	}
	return true
}

// checkScenarioTypes compares each bound result with native metadata.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes execution (scenarioExecution) which keeps the native statement with its bound
// arguments.
// Takes query (*emit.AnalysedQuery) which retains parameter identities and inferred
// output facts.
func checkScenarioTypes(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, scenario fuzzcase.Scenario, execution scenarioExecution, query *emit.AnalysedQuery) {
	t.Helper()
	types := fuzzColumnTypes(ctx, t, database, execution.statement, execution.arguments)
	require.Len(t, types, len(query.OutputColumns), "native result width changed")
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		name := fuzzObservedType(ctx, t, database, suite, execution.statement, execution.arguments, types[index])
		if name == "" && suite.Engine == engineSQLite {
			continue
		}
		if scenarioTypeHandled(t, scenario, column.SQLType, name, index) {
			continue
		}
		name = fuzzResultTypeName(suite, column.SQLType, len(query.Parameters) > 0, name)
		checkFuzzType(t, suite, column.SQLType, name, column.Name)
	}
}

// prepareScenarioConstants substitutes an observed scalar under unchanged data.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes scenario (*fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
func prepareScenarioConstants(ctx context.Context, t *testing.T, database *sql.DB, scenario *fuzzcase.Scenario) {
	t.Helper()
	for _, relation := range scenario.Relations {
		if relation.Kind != relationConstantSubstitution {
			continue
		}
		require.Len(t, relation.Queries, 2, "a constant substitution relates two queries")
		require.True(t, relation.Queries[1] >= 0 && relation.Queries[1] < len(scenario.Queries), "constant substitution names a missing query")
		rows := fuzzRows(ctx, t, database, scenarioConstantQuery, nil)
		require.True(t, len(rows) == 1 && len(rows[0]) == 1, "constant probe returned an invalid shape")
		literal := fuzzNullText
		if rows[0][0] != nil {
			number, err := strconv.ParseInt(fuzzValue(rows[0][0]), 10, 64)
			require.NoError(t, err)
			literal = strconv.FormatInt(number, 10)
		}
		query := &scenario.Queries[relation.Queries[1]]
		query.SQL = strings.ReplaceAll(query.SQL, scenarioConstantProbe, literal)
		query.Reference = strings.ReplaceAll(query.Reference, scenarioConstantProbe, literal)
	}
}

// scenarioRelationProblem checks bags and predicate counts without compiler output.
//
// Takes relation (fuzzcase.Relation) which selects participating observations and the
// comparison rule.
// Takes observations ([][][]any) which retains all executed query results.
//
// Returns string which is empty when the relationship holds.
func scenarioRelationProblem(relation fuzzcase.Relation, observations [][][]any) string {
	for _, index := range relation.Queries {
		if index < 0 || index >= len(observations) {
			return "relation references a missing observation"
		}
	}
	if len(relation.Queries) < 2 {
		return "relation needs at least two observations"
	}
	want := observations[relation.Queries[0]]
	switch relation.Kind {
	case relationPartition:
		var combined [][]any
		for _, index := range relation.Queries[1:] {
			combined = append(combined, observations[index]...)
		}
		if !fuzzExactResultsEqual(want, combined, false) {
			return "partition changed values or row multiplicities"
		}
	case relationCountTruth:
		return scenarioTruthProblem(want, observations[relation.Queries[1]])
	case relationEqual, relationConstantSubstitution:
		if !fuzzExactResultsEqual(want, observations[relation.Queries[1]], false) {
			return "equivalent expressions changed results"
		}
	default:
		return fmt.Sprintf("unknown relation %q", relation.Kind)
	}
	return ""
}

// scenarioTruthProblem compares selected counts with projected three-valued truth.
//
// Takes want ([][]any) which holds the independently observed reference result.
// Takes truthRows ([][]any) which holds projected predicate truth values.
//
// Returns string which is empty when filtered counts equal projected truth.
func scenarioTruthProblem(want, truthRows [][]any) string {
	count := 0
	for _, row := range truthRows {
		if len(row) != 1 {
			return "truth projection has the wrong width"
		}
		truth := fuzzValue(row[0])
		if truth != "0" && truth != "1" {
			return "truth projection is not Boolean"
		}
		if truth == "1" {
			count++
		}
	}
	if len(want) != 1 || len(want[0]) != 1 || fuzzValue(want[0][0]) != strconv.Itoa(count) {
		return "filtered count differs from projected truth"
	}
	return ""
}

// scenarioSQLiteType separates numeric affinity from each observed storage class.
//
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes inferred (dialect.SQLType) which records the inferred SQL representation.
// Takes observed (string) which holds the actual native observation.
//
// Returns bool which selects an independently declared affinity contract.
// Returns string which is empty when the observation matches.
func scenarioSQLiteType(scenario fuzzcase.Scenario, inferred dialect.SQLType, observed string) (bool, string) {
	if len(scenario.Features) == 0 {
		return false, ""
	}
	feature := scenario.Features[0]
	if feature != "type/decimal" && feature != scenarioBooleanFeature {
		return false, ""
	}
	expected := dialect.TypeCategoryDecimal
	rawColumn := feature == scenarioBooleanFeature && len(scenario.Data) > 2 && scenario.Data[2]%scenarioPositionChoices == 1
	wrapped := rawColumn && scenarioWrapsConverting(scenario.Data)
	if rawColumn && !wrapped {
		expected = dialect.TypeCategoryBoolean
	}

	mixed := wrapped && inferred.Category == dialect.TypeCategoryUnknown
	if inferred.Category != expected && !mixed {
		return true, "SQLite declared numeric affinity differs from the independent scenario"
	}
	if observed != "integer" && observed != "real" {
		return true, "SQLite numeric conversion returned a non-numeric storage class"
	}
	if feature == scenarioBooleanFeature && observed != "integer" {
		return true, "SQLite Boolean fixture returned a non-integer storage class"
	}
	return true, ""
}

// scenarioWrapsConverting reports whether the decoder wraps a scenario's expression in
// anything other than parentheses, such as a CAST, COALESCE or CASE.
//
// Takes data ([]byte) which is the scenario's input.
//
// Returns bool which is true when any wrapper converts the expression.
func scenarioWrapsConverting(data []byte) bool {
	if len(data) <= scenarioWrapperFirst {
		return false
	}
	for _, choice := range data[scenarioWrapperFirst:min(len(data), scenarioWrapperLast)] {
		if choice%scenarioWrapperChoices != 0 {
			return true
		}
	}
	return false
}

// scenarioAliasedType checks declared logical types against known native aliases.
//
// Takes scenario (fuzzcase.Scenario) which supplies independently constructed SQL and
// expectations.
// Takes inferred (dialect.SQLType) which records the inferred SQL representation.
// Takes observed (string) which holds the actual native observation.
//
// Returns bool which selects an independently declared alias contract.
// Returns string which is empty when the observation matches.
func scenarioAliasedType(scenario fuzzcase.Scenario, inferred dialect.SQLType, observed string) (bool, string) {
	if scenario.Engine == engineSQLite {
		return scenarioSQLiteType(scenario, inferred, observed)
	}
	if len(scenario.Data) <= 2 || len(scenario.Features) == 0 || scenario.Data[0] != fuzzcase.TypedVersion || scenario.Data[2]%scenarioPositionChoices != 1 {
		return false, ""
	}
	feature := scenario.Features[0]
	if (scenario.Engine == engineMySQL || scenario.Engine == engineMariaDB) && feature == scenarioBooleanFeature {
		if inferred.Category != dialect.TypeCategoryBoolean || !strings.EqualFold(observed, "TINYINT") {
			return true, "MySQL Boolean alias differs from the independent column declaration"
		}
		return true, ""
	}
	if scenario.Engine == engineMariaDB && feature == "type/json" {
		native, ok := fuzzScalarType(scenario.Engine, observed)
		if inferred.Category != dialect.TypeCategoryJSON || !ok || native.Category != dialect.TypeCategoryText {
			return true, "MariaDB JSON alias differs from the independent column declaration"
		}
		return true, ""
	}
	return false, ""
}
