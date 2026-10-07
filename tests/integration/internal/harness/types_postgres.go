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
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

var (
	// postgresNumericColumns are the numeric columns of the PostgreSQL-family fixture.
	postgresNumericColumns = []string{"i2", "i4", "i8", "f4", "f8", "n"}

	// postgresNumericFunctions are the maths functions the PostgreSQL-family probes apply to
	// each column.
	postgresNumericFunctions = []string{
		"abs(%[1]s)", "ceil(%[1]s)", "ceiling(%[1]s)", "floor(%[1]s)", "round(%[1]s)", "trunc(%[1]s)",
		"sign(%[1]s)", "sqrt(%[1]s)", "cbrt(%[1]s)", "exp(%[1]s)", "ln(%[1]s)", "log(%[1]s)", "log10(%[1]s)",
		"degrees(%[1]s)", "radians(%[1]s)", "power(%[1]s, %[1]s)", "pow(%[1]s, %[1]s)", "mod(%[1]s, %[1]s)",
		"gcd(%[1]s, %[1]s)", "lcm(%[1]s, %[1]s)",
	}

	// postgresNumericPairs combine two columns of the PostgreSQL-family fixture.
	postgresNumericPairs = []string{
		"%[1]s + %[2]s",
		"COALESCE(%[1]s, %[2]s)",
		"CASE WHEN %[1]s > %[2]s THEN %[1]s ELSE %[2]s END",
		"GREATEST(%[1]s, %[2]s)",
	}

	// postgresTypeNames reads the names pg_typeof gives the fixture's numeric results.
	postgresTypeNames = map[string]ReportedType{
		"smallint":         {Category: dialect.TypeCategoryInteger, Width: 16, Unsigned: false},
		"integer":          {Category: dialect.TypeCategoryInteger, Width: 32, Unsigned: false},
		"bigint":           {Category: dialect.TypeCategoryInteger, Width: 64, Unsigned: false},
		"real":             {Category: dialect.TypeCategoryFloat, Width: 32, Unsigned: false},
		"double precision": {Category: dialect.TypeCategoryFloat, Width: 64, Unsigned: false},
		"numeric":          {Category: dialect.TypeCategoryDecimal, Width: 0, Unsigned: false},
	}
)

// PostgresNumericSchema returns the statements creating the PostgreSQL-family fixture,
// with one non-null column of each numeric type.
//
// Returns []string which holds the statements.
func PostgresNumericSchema() []string {
	return []string{"CREATE TABLE numbers (i2 SMALLINT NOT NULL, i4 INTEGER NOT NULL, i8 BIGINT NOT NULL, " +
		"f4 REAL NOT NULL, f8 DOUBLE PRECISION NOT NULL, n NUMERIC(12, 3) NOT NULL)"}
}

// PostgresNumericRows returns the statements filling the PostgreSQL-family fixture with
// one row of positive values, so logarithms and square roots are defined.
//
// Returns []string which holds the statements.
func PostgresNumericRows() []string {
	return []string{"INSERT INTO numbers VALUES (2, 4, 8, 1.5, 2.5, 3.25)"}
}

// PostgresNumericProbes returns the probe queries over the PostgreSQL-family fixture.
//
// Returns []string which holds the queries.
func PostgresNumericProbes() []string {
	return NumericProbes(numericFixtureTable, postgresNumericColumns, postgresNumericFunctions, postgresNumericPairs, true)
}

// PostgresTypeName reads a type name pg_typeof gives a numeric result.
//
// Takes name (string) which is the name.
//
// Returns ReportedType which is the type.
// Returns bool which is false for a name it does not know.
func PostgresTypeName(name string) (ReportedType, bool) {
	reported, known := postgresTypeNames[strings.ToLower(strings.TrimSpace(name))]
	return reported, known
}

// PostgresParameterProbes returns probe queries over the PostgreSQL-family fixture whose
// placeholders take their type from the call or operator around them.
//
// Returns []string which holds the queries.
func PostgresParameterProbes() []string {
	return []string{
		"SELECT abs($1) AS v FROM numbers",
		"SELECT ceil($1) AS v FROM numbers",
		"SELECT round($1) AS v FROM numbers",
		"SELECT round($1, 2) AS v FROM numbers",
		"SELECT sqrt($1) AS v FROM numbers",
		"SELECT power($1, 2) AS v FROM numbers",
		"SELECT mod($1, i4) AS v FROM numbers",
		"SELECT gcd(i8, $1) AS v FROM numbers",
		"SELECT i4 + $1 AS v FROM numbers",
		"SELECT n * $1 AS v FROM numbers",
		"SELECT f8 + $1 AS v FROM numbers",
		"SELECT GREATEST(i4, $1) AS v FROM numbers",
	}
}

// CheckPostgresParameterTypes creates the suite's fixture tables and fails the test for
// each placeholder whose inferred type differs from the type PostgreSQL prepares it with,
// unless the allowlist holds the difference. A query the server rejects is skipped.
//
// Takes suite (*TypeSuite) which describes the database, the dialect and the probes; its
// Probe is not used.
func CheckPostgresParameterTypes(t *testing.T, suite *TypeSuite) {
	t.Helper()

	ctx := t.Context()
	connection, err := suite.Database.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = connection.Close() }()
	for _, statement := range slices.Concat(suite.Schema, suite.Rows) {
		_, err := connection.ExecContext(ctx, statement)
		require.NoError(t, err, "running the fixture statement %q", statement)
	}

	report, messages := analyseProbes(t, suite)
	parameters := make(map[string][]dialect.SQLType, len(report.Queries))
	for _, query := range report.Queries {
		types := make([]dialect.SQLType, len(query.Parameters))
		for index := range query.Parameters {
			types[index] = query.Parameters[index].SQLType
		}
		parameters[query.Name] = types
	}

	for index, query := range suite.Queries {
		names, prepared := preparedParameterTypes(t, connection, index, query)
		if !prepared {
			continue
		}
		comparePostgresParameters(t, suite, query, parameters[probeName(index)], strings.Split(names, ","), messages[index])
	}
}

// preparedParameterTypes prepares a probe query and reads its placeholders' types from
// pg_prepared_statements, logging a query the server rejects.
//
// Takes connection (*sql.Conn) which is the connection holding the fixture.
// Takes index (int) which is the query's position among the probes.
// Takes query (string) which is the probe query.
//
// Returns string which holds the placeholders' type names, separated by commas.
// Returns bool which is false when the server rejects the query.
func preparedParameterTypes(t *testing.T, connection *sql.Conn, index int, query string) (string, bool) {
	t.Helper()

	ctx := t.Context()
	statement := fmt.Sprintf("sgl_parameter_probe_%d", index+1)
	if _, err := connection.ExecContext(ctx, "PREPARE "+statement+" AS "+query); err != nil {
		t.Logf("skipped, the server rejects %q: %v", query, err)
		return "", false
	}
	var names string
	err := connection.QueryRowContext(ctx,
		"SELECT array_to_string(parameter_types::text[], ',') FROM pg_prepared_statements WHERE name = $1",
		statement).Scan(&names)
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, "DEALLOCATE "+statement)
	require.NoError(t, err)
	return names, true
}

// comparePostgresParameters fails the test for each placeholder of a probe query whose
// inferred type differs from the server's and is not allowlisted.
//
// Takes suite (*TypeSuite) which holds the allowlist and whether widths are compared.
// Takes query (string) which is the probe query.
// Takes inferred ([]dialect.SQLType) which holds the placeholders' inferred types.
// Takes reported ([]string) which holds the type names the server reports for them.
// Takes messages ([]string) which holds the analysis diagnostics for the query.
func comparePostgresParameters(t *testing.T, suite *TypeSuite, query string, inferred []dialect.SQLType, reported, messages []string) {
	t.Helper()

	if len(inferred) != len(reported) {
		assert.Failf(t, "the inferred placeholders differ from the server's",
			"%s: inferred %d placeholders, the server reports %d (%s) %v", query, len(inferred), len(reported), strings.Join(reported, ","), messages)
		return
	}
	for position, name := range reported {
		expected, known := PostgresTypeName(name)
		require.Truef(t, known, "the server reports a type the suite cannot read, %q, for %q", name, query)
		for _, difference := range compareTypes(inferredType{problem: "", sqlType: inferred[position]}, name, expected, suite.Width) {
			key := fmt.Sprintf("%s $%d%s%s", query, position+1, typeAllowlistSeparator, difference)
			if _, allowed := suite.Allowlist[key]; allowed {
				continue
			}
			assert.Fail(t, "the inferred placeholder type differs from the server's: "+key)
		}
	}
}
