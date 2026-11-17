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
	"testing"

	"github.com/stretchr/testify/require"
)

// CheckFuzzUserVariables verifies user variable names against their native databases
// under the default SQL mode, where double quotes delimit strings.
//
// Takes t (*testing.T) which reports analysis and result differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzUserVariables(t *testing.T, suite FuzzSuite) {
	t.Helper()
	checkFuzzUserVariables(t, suite, "", `@"quote\"name"`)
}

// CheckFuzzUserVariablesWithANSIQuotes verifies user variable names against their native
// databases under ANSI_QUOTES, where double quotes delimit identifiers.
//
// Takes t (*testing.T) which reports analysis and result differences.
// Takes suite (FuzzSuite) which supplies a dialect set up for ANSI_QUOTES.
func CheckFuzzUserVariablesWithANSIQuotes(t *testing.T, suite FuzzSuite) {
	t.Helper()
	checkFuzzUserVariables(t, suite, "ANSI_QUOTES", `@"price\"`)
}

// checkFuzzUserVariables verifies user variable names under one SQL mode.
//
// Takes t (*testing.T) which reports analysis and result differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
// Takes mode (string) which is the session's sql_mode.
// Takes quoted (string) which is a variable whose double quotes the mode reads its own
// way.
func checkFuzzUserVariables(t *testing.T, suite FuzzSuite, mode, quoted string) {
	t.Helper()
	if suite.Engine != engineMySQL && suite.Engine != engineMariaDB {
		t.Skip("the dialect does not use MySQL user variables")
	}
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	_, err := database.ExecContext(t.Context(), "SET SESSION sql_mode = '"+mode+"'")
	require.NoError(t, err)
	variables := []string{
		"@price$usd", "@price$", "@$price", "@price.usd", "@42", "@'price usd'", "@\"price usd\"",
		"@`price usd`", "@'quo''te'", "@'semi;colon'", "@`quo``te`", "@'quote\\'name'", "@\"quo\"\"te\"", quoted,
	}
	const firstValue = 42
	for index, variable := range variables {
		t.Run(variable, func(t *testing.T) {
			_, err := database.ExecContext(t.Context(), "SET "+variable+" = ?", index+firstValue)
			require.NoError(t, err)
			checkFuzzVariableQuery(t, suite, database, "SELECT CAST("+variable+" AS CHAR) AS value;")
		})
	}
	for _, variable := range []string{"@@version", "@@SESSION.sql_mode", "@@SESSION.`sql_mode`", "@@`version`", "@@global.`time_zone`"} {
		t.Run(variable, func(t *testing.T) {
			checkFuzzVariableQuery(t, suite, database, "SELECT CAST("+variable+" AS CHAR) AS value;")
		})
	}
}

// checkFuzzVariableQuery compares native and generated queries for a session variable.
//
// Takes t (*testing.T) which reports generation and result differences.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes database (*sql.DB) which holds the configured session.
// Takes source (string) which reads one user or system variable.
func checkFuzzVariableQuery(t *testing.T, suite FuzzSuite, database *sql.DB, source string) {
	t.Helper()
	want := fuzzRows(t.Context(), t, database, source, nil)
	details := fuzzGenerate(t.Context(), t, suite, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Variable, one)\n"+source)
	query := details.Report.Queries[0]
	require.True(t, len(query.Parameters) == 0 && len(query.OutputColumns) == 1 && query.OutputColumns[0].Name == "value",
		"the session variable changed parameter or column identities")
	rendered := fuzzRenderTemplate(t, suite, query, query.Template)
	got := fuzzRows(t.Context(), t, database, rendered.SQL, nil)
	require.True(t, fuzzResultsEqual(want, got, true), "rendering changed variable results, want %#v, got %#v", want, got)
}
