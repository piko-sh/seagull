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
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// typeProbeColumn is the name every probe query gives its single column.
	typeProbeColumn = "v"

	// typeQueryFile is the query file holding the probe queries.
	typeQueryFile = "probes.sql"

	// numericFixtureTable is the table every numeric fixture creates.
	numericFixtureTable = "numbers"

	// typeAllowlistSeparator joins a probe query to one of its differences in an allowlist
	// key, chosen because no probe query contains it.
	typeAllowlistSeparator = " | "
)

// ReportedType is a result type as a database reports it, reduced to what the type
// comparison reads.
type ReportedType struct {
	// Width is an integer's or a float's width in bits, or 0 when the name gives none.
	Width int

	// Category is the type's category.
	Category dialect.SQLTypeCategory

	// Unsigned reports an integer type that holds no negative values.
	Unsigned bool
}

// TypeAllowlist holds the differences between inferred and reported types that a suite
// accepts, each keyed "query | difference" and mapped to the reason.
type TypeAllowlist map[string]string

// TypeSuite compares the type the generator infers for the single column of each probe
// query with the type the database reports for it.
type TypeSuite struct {
	// Dialect builds the dialect the generator analyses the queries with.
	Dialect func() dialect.Dialect

	// Database is the database the probe queries run on.
	Database *sql.DB

	// Probe returns the name the database gives the type of the single column a query
	// returns.
	Probe func(ctx context.Context, database *sql.DB, query string) (string, error)

	// Parse reads a name Probe returned, reporting false for a name it does not know.
	Parse func(name string) (ReportedType, bool)

	// Allowlist holds the differences the suite accepts.
	Allowlist TypeAllowlist

	// Schema holds the statements creating the fixture tables. They run on the database and
	// form the generator's single migration.
	Schema []string

	// Rows holds the statements filling the fixture tables, run on the database only.
	Rows []string

	// Queries holds the probe queries, each selecting one column named v.
	Queries []string

	// Width compares the widths and signedness of integers and floats as well as their
	// categories.
	Width bool
}

// inferredType is the type the generator gives a probe query's column, or the reason it
// gives none.
type inferredType struct {
	// problem says why the generator typed no column, or is empty.
	problem string

	// sqlType is the column's type when problem is empty.
	sqlType dialect.SQLType
}

// CheckTypes creates and fills the suite's fixture tables, then fails the test for each
// probe query whose inferred type differs from the reported one in a way the allowlist
// does not hold, and for each allowlisted difference that no longer occurs. A query the
// database rejects is skipped.
//
// Takes suite (*TypeSuite) which describes the database, the dialect and the probes.
func CheckTypes(t *testing.T, suite *TypeSuite) {
	t.Helper()

	ctx := t.Context()
	for _, statement := range slices.Concat(suite.Schema, suite.Rows) {
		_, err := suite.Database.ExecContext(ctx, statement)
		require.NoError(t, err, "running the fixture statement %q", statement)
	}

	inferred := inferTypes(t, suite)
	checked := make(map[string]bool, len(suite.Queries))
	used := make(map[string]bool)
	for index, query := range suite.Queries {
		name, err := suite.Probe(ctx, suite.Database, query)
		if err != nil {
			t.Logf("skipped, the database rejects %q: %v", query, err)
			continue
		}
		reported, known := suite.Parse(name)
		require.Truef(t, known, "the database reports a type the suite cannot read, %q, for %q", name, query)
		checked[query] = true
		for _, difference := range compareTypes(inferred[index], name, reported, suite.Width) {
			key := query + typeAllowlistSeparator + difference
			if _, allowed := suite.Allowlist[key]; allowed {
				used[key] = true
				continue
			}
			assert.Fail(t, "the inferred type differs from the database's: "+key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(suite.Allowlist)) {
		query, _, _ := strings.Cut(key, typeAllowlistSeparator)
		if checked[query] && !used[key] {
			assert.Fail(t, "allowlisted type difference no longer occurs, remove it: "+key)
		}
	}
}

// ColumnTypeProbe reports the driver's name for the type of a query's single column.
//
// Takes database (*sql.DB) which runs the query.
// Takes query (string) which is the probe query.
//
// Returns string which is the driver's name for the column's type.
// Returns error when the query fails.
func ColumnTypeProbe(ctx context.Context, database *sql.DB, query string) (string, error) {
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.ColumnTypes()
	if err != nil {
		return "", err
	}
	if len(columns) != 1 {
		return "", fmt.Errorf("the query returns %d columns, want 1", len(columns))
	}
	return columns[0].DatabaseTypeName(), nil
}

// ValueTypeProbe reports the type of a query's single column by asking the database to
// name the type of its first value with a function such as pg_typeof or typeof.
//
// Takes typeFunction (string) which is the function that names a value's type, written so
// its result reads as text.
//
// Returns func which probes a query.
func ValueTypeProbe(typeFunction string) func(ctx context.Context, database *sql.DB, query string) (string, error) {
	return func(ctx context.Context, database *sql.DB, query string) (string, error) {
		var name string
		probe := fmt.Sprintf("SELECT %s FROM (%s) AS probe LIMIT 1", fmt.Sprintf(typeFunction, typeProbeColumn), query) //nolint:gosec // wraps the harness's own probe queries, not input
		if err := database.QueryRowContext(ctx, probe).Scan(&name); err != nil {
			return "", err
		}
		return name, nil
	}
}

// NumericProbes builds probe queries over one table. It applies each function template to
// each column and combines each ordered pair of different columns by every operator
// template.
//
// Takes table (string) which is the fixture table.
// Takes columns ([]string) which are its numeric columns.
// Takes functions ([]string) which are function templates with %[1]s for the column, such
// as "abs(%[1]s)" or "power(%[1]s, %[1]s)".
// Takes pairs ([]string) which are templates with %[1]s and %[2]s for the two columns,
// such as "%[1]s + %[2]s" or "COALESCE(%[1]s, %[2]s)".
// Takes union (bool) which adds a UNION ALL of each ordered pair.
//
// Returns []string which holds the probe queries.
func NumericProbes(table string, columns, functions, pairs []string, union bool) []string {
	var queries []string
	for _, function := range functions {
		for _, column := range columns {
			queries = append(queries, fmt.Sprintf("SELECT %s AS %s FROM %s", fmt.Sprintf(function, column), typeProbeColumn, table))
		}
	}
	for _, left := range columns {
		for _, right := range columns {
			if left == right {
				continue
			}
			for _, pair := range pairs {
				queries = append(queries, fmt.Sprintf("SELECT %s AS %s FROM %s", fmt.Sprintf(pair, left, right), typeProbeColumn, table))
			}
			if union {
				queries = append(queries, fmt.Sprintf("SELECT %[1]s AS %[3]s FROM %[4]s UNION ALL SELECT %[2]s FROM %[4]s",
					left, right, typeProbeColumn, table))
			}
		}
	}
	return queries
}

// analyseProbes analyses every probe query against the fixture schema, and returns the
// report with the diagnostics that fall within each query, in query order.
//
// Takes suite (*TypeSuite) which holds the schema, the queries and the dialect.
//
// Returns *seagull.Report which holds the analysed queries.
// Returns [][]string which holds, per query, its diagnostics' codes and messages.
func analyseProbes(t *testing.T, suite *TypeSuite) (*seagull.Report, [][]string) {
	t.Helper()

	var queries strings.Builder
	firstLines := make([]int, len(suite.Queries))
	line := 1
	for index, query := range suite.Queries {
		firstLines[index] = line
		text := fmt.Sprintf("-- sgl.query(name: %s, command: one)\n%s;\n\n", probeName(index), query)
		queries.WriteString(text)
		line += strings.Count(text, "\n")
	}
	files := fstest.MapFS{
		migrationDirectory + "/0001_fixture.up.sql": {Data: []byte(strings.Join(suite.Schema, ";\n") + ";\n")},
		queryDirectory + "/" + typeQueryFile:        {Data: []byte(queries.String())},
	}

	generator, err := seagull.New(suite.Dialect())
	require.NoError(t, err)
	report, err := generator.Vet(t.Context(), seagull.Input{
		Migrations:   files,
		Queries:      files,
		Package:      "",
		PackagePath:  "",
		MigrationDir: migrationDirectory,
		QueryDir:     queryDirectory,
	})
	require.NoError(t, err)

	messages := make([][]string, len(suite.Queries))
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Filename != typeQueryFile {
			continue
		}
		index, _ := slices.BinarySearch(firstLines, diagnostic.Line+1)
		if index > 0 {
			messages[index-1] = append(messages[index-1], diagnostic.Code+" "+diagnostic.Message)
		}
	}
	return report, messages
}

// inferTypes analyses every probe query against the fixture schema and returns, in query
// order, the type the generator gives each one's column.
//
// Takes suite (*TypeSuite) which holds the schema, the queries and the dialect.
//
// Returns []inferredType which holds one entry per probe query.
func inferTypes(t *testing.T, suite *TypeSuite) []inferredType {
	t.Helper()

	report, messages := analyseProbes(t, suite)
	analysed := make(map[string]dialect.SQLType, len(report.Queries))
	for _, query := range report.Queries {
		if len(query.OutputColumns) == 1 && query.OutputColumns[0].Name == typeProbeColumn {
			analysed[query.Name] = query.OutputColumns[0].SQLType
		}
	}

	inferred := make([]inferredType, len(suite.Queries))
	for index := range suite.Queries {
		sqlType, found := analysed[probeName(index)]
		if !found {
			problem := "the generator gives the query no single column v"
			if len(messages[index]) > 0 {
				problem += " (" + strings.Join(messages[index], "; ") + ")"
			}
			inferred[index] = inferredType{problem: problem, sqlType: dialect.SQLType{}}
			continue
		}
		if sqlType.Category == dialect.TypeCategoryUnknown && len(messages[index]) > 0 {
			inferred[index] = inferredType{problem: "inferred unknown (" + strings.Join(messages[index], "; ") + ")", sqlType: sqlType}
			continue
		}
		inferred[index] = inferredType{problem: "", sqlType: sqlType}
	}
	return inferred
}

// probeName names the probe query at an index.
//
// Takes index (int) which is the query's position.
//
// Returns string which is the query's name.
func probeName(index int) string {
	return fmt.Sprintf("Probe%03d", index+1)
}

// compareTypes lists how an inferred type differs from the type the database reports.
//
// Takes inferred (inferredType) which is the generator's type for the column.
// Takes name (string) which is the database's name for the reported type.
// Takes reported (ReportedType) which is that type, read.
// Takes width (bool) which compares widths and signedness as well as categories.
//
// Returns []string which holds the differences, empty when the types agree.
func compareTypes(inferred inferredType, name string, reported ReportedType, width bool) []string {
	if inferred.problem != "" {
		return []string{inferred.problem + "; the database reports " + name}
	}
	got := inferred.sqlType
	if got.Category != reported.Category {
		return []string{fmt.Sprintf("inferred %s, the database reports %s", describeInferredType(got), name)}
	}
	if !width || (got.Category != dialect.TypeCategoryInteger && got.Category != dialect.TypeCategoryFloat) {
		return nil
	}
	if reported.Width != 0 && (got.Width != reported.Width || got.Unsigned != reported.Unsigned) {
		return []string{fmt.Sprintf("inferred %s, the database reports %s", describeInferredType(got), name)}
	}
	return nil
}

// describeInferredType names an inferred type by its category, width and signedness.
//
// Takes sqlType (dialect.SQLType) which is the type.
//
// Returns string which is its description, such as "integer(32)" or "decimal".
func describeInferredType(sqlType dialect.SQLType) string {
	description := sqlType.Category.String()
	if sqlType.Width != 0 {
		description += fmt.Sprintf("(%d)", sqlType.Width)
	}
	if sqlType.Unsigned {
		description = "unsigned " + description
	}
	return description
}
