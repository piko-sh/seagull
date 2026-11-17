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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// fuzzBindingParameterType names the integer type used in typed placeholders.
	fuzzBindingParameterType = "Int64"

	// fuzzBindingOrder gives binding regressions a deterministic result order.
	fuzzBindingOrder = " ORDER BY id;"
)

// CheckFuzzDollarIdentifiers verifies dollar signs inside unquoted column names.
//
// Takes t (*testing.T) which reports database or analysis differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzDollarIdentifiers(t *testing.T, suite FuzzSuite) {
	t.Helper()
	database := suite.Open(t)
	for _, source := range []string{"SELECT 42 AS price$usd", "SELECT 42 AS price$", "SELECT 42 AS price$$usd", "SELECT 42 AS price$1"} {
		column := strings.TrimPrefix(source, "SELECT 42 AS ")
		t.Run(column, func(t *testing.T) {
			require.Equal(t, []string{column}, fuzzColumnNames(t.Context(), t, database, source), "the engine names the column otherwise")
			checkFuzzDollarAnalysis(t, suite, column)
		})
	}
}

// checkFuzzDollarAnalysis checks column identity through schema and query generation.
//
// Takes t (*testing.T) which reports generation failures.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes column (string) which names an unquoted column containing dollar signs.
func checkFuzzDollarAnalysis(t *testing.T, suite FuzzSuite, column string) {
	t.Helper()
	details := fuzzGenerate(t.Context(), t, suite, "CREATE TABLE prices ("+column+" BIGINT);", "-- sgl.query(Price, many)\nSELECT "+column+" FROM prices;")
	query := details.Report.Queries[0]
	require.True(t, len(query.OutputColumns) == 1 && query.OutputColumns[0].Name == column && len(query.Parameters) == 0,
		"analysis did not preserve the dollar identifier %q", column)
}

// CheckFuzzDollarSliceBounds checks slicing with dollar signs in bound identifiers.
//
// Takes t (*testing.T) which reports database or analysis differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzDollarSliceBounds(t *testing.T, suite FuzzSuite) {
	t.Helper()
	if suite.Engine != enginePostgres && suite.Engine != engineTimescaleDB && suite.Engine != engineDuckDB {
		t.Skip("the dialect does not support this array slice form")
	}
	const source = "SELECT (ARRAY[1,2,3])[lo$:hi$] AS sliced FROM (SELECT 1 AS lo$, 2 AS hi$) bounds;"
	const reference = "SELECT (ARRAY[1,2,3])[1:2] AS sliced;"
	database := suite.Open(t)
	want := fuzzRows(t.Context(), t, database, reference, nil)
	got := fuzzRows(t.Context(), t, database, source, nil)
	require.True(t, fuzzResultsEqual(want, got, true), "dollar identifiers changed native slice results, want %#v, got %#v", want, got)
	details := fuzzGenerate(t.Context(), t, suite, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Slice, one)\n"+source)
	query := details.Report.Queries[0]
	require.Empty(t, query.Parameters, "array slice bounds were mistaken for a named parameter")
	rendered := fuzzRenderTemplate(t, suite, query, query.Template)
	got = fuzzRows(t.Context(), t, database, rendered.SQL, nil)
	require.True(t, fuzzResultsEqual(want, got, true), "rendering changed slice results, want %#v, got %#v", want, got)
}

// CheckFuzzDollarParameterNames verifies dollar signs in named parameter identities.
//
// Takes t (*testing.T) which reports binding or result differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzDollarParameterNames(t *testing.T, suite FuzzSuite) {
	t.Helper()
	info := suite.Dialect().Info()
	for _, form := range info.Placeholders.Sigils {
		if form.Kind != dialect.PlaceholderNamed {
			continue
		}
		t.Run(form.Prefix, func(t *testing.T) {
			if form.Prefix == "$" && info.Lexical.Quotes.DollarQuoting {
				t.Skip("a second dollar sign opens a dollar-quoted string")
			}
			first, second := form.Render(1, "a$one", fuzzBindingParameterType), form.Render(2, "b$two", fuzzBindingParameterType)
			source := "-- sgl.query(Bindings, many)\nSELECT id FROM fuzz_items WHERE id = " + first + " OR id = " + second + " OR id = " + first + fuzzBindingOrder
			checkFuzzBindingValues(t, suite, source)
		})
	}
}

// CheckFuzzMixedPositionalBindings verifies mixed numbered and anonymous argument values.
//
// Takes t (*testing.T) which reports database result differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzMixedPositionalBindings(t *testing.T, suite FuzzSuite) {
	t.Helper()
	info := suite.Dialect().Info()
	if !info.Lexical.Parameters.QuestionMark || !info.Lexical.Parameters.NumberedQuestionMark {
		t.Skip("the dialect does not accept both question-mark forms")
	}
	for _, predicate := range []string{
		"id = ?1 OR id = ? OR id = ?1",
		"id = ? OR id = ?1 OR id = ?",
	} {
		t.Run(predicate, func(t *testing.T) {
			source := "-- sgl.query(Bindings, many)\nSELECT id FROM fuzz_items WHERE " + predicate + fuzzBindingOrder
			checkFuzzBindingValues(t, suite, source)
		})
	}
}

// CheckFuzzParameterNameCollisions verifies binding values after parameter renaming.
//
// Takes t (*testing.T) which reports database result differences.
// Takes suite (FuzzSuite) which supplies the dialect and isolated database.
func CheckFuzzParameterNameCollisions(t *testing.T, suite FuzzSuite) {
	t.Helper()
	const alternative = " OR id = "
	info := suite.Dialect().Info()
	for _, named := range info.Placeholders.Sigils {
		if named.Kind != dialect.PlaceholderNamed {
			continue
		}
		first, second := named.Render(1, "a", fuzzBindingParameterType), named.Render(2, "id", fuzzBindingParameterType)
		t.Run("renamed/"+named.Prefix, func(t *testing.T) {
			source := "-- sgl.query(Bindings, many)\n-- " + first + " as sgl.param(id)\nSELECT id FROM fuzz_items WHERE id = " + first + alternative + second + alternative + second + fuzzBindingOrder
			checkFuzzBindingValues(t, suite, source)
		})
		for _, positional := range info.Placeholders.Sigils {
			if positional.Kind == dialect.PlaceholderNamed {
				continue
			}
			t.Run(fmt.Sprintf("inferred/%d/%s/%s", positional.Kind, positional.Prefix, named.Prefix), func(t *testing.T) {
				source := "-- sgl.query(Bindings, many)\nSELECT id FROM fuzz_items WHERE id = " + positional.Render(1, "", "") + alternative + second + alternative + second + fuzzBindingOrder
				checkFuzzBindingValues(t, suite, source)
			})
		}
	}
}

// checkFuzzBindingValues compares two distinct bound values with literal SQL.
//
// Takes t (*testing.T) which reports generation and execution failures.
// Takes suite (FuzzSuite) which supplies the engine and database.
// Takes source (string) which contains the query and its binding directives.
func checkFuzzBindingValues(t *testing.T, suite FuzzSuite, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	const rows = 3
	model, ok := fuzzcase.Decode(suite.Engine, []byte{fuzzcase.Version, 0, 0, rows})
	require.True(t, ok, "binding regression model was rejected")
	details := fuzzGenerate(ctx, t, suite, strings.Join(model.Migrations(), "\n"), source)
	query := details.Report.Queries[0]
	rendered := fuzzRenderTemplate(t, suite, query, query.Template)
	arguments := make([]any, 0, len(rendered.Arguments))
	for _, argument := range rendered.Arguments {
		require.True(t, argument.Parameter >= 0 && argument.Parameter < len(query.Parameters), "rendered parameter index is out of bounds")
		number := query.Parameters[argument.Parameter].Number
		require.True(t, number == 1 || number == 2, "unexpected parameter number %d", number)
		var value any = int64(number)
		if argument.Name != "" {
			value = sql.Named(argument.Name, value)
		}
		arguments = append(arguments, value)
	}
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	fuzzApply(ctx, t, database, append(model.Migrations(), model.Inserts()...))
	want := fuzzRows(ctx, t, database, "SELECT id FROM fuzz_items WHERE id IN (1, 2) ORDER BY id", nil)
	got := fuzzRows(ctx, t, database, rendered.SQL, arguments)
	require.Len(t, want, 2, "binding regression needs two distinct reference rows, got %#v", want)
	require.True(t, fuzzResultsEqual(want, got, true), "binding name collision changed results\nsource: %s\nrendered: %s\narguments: %#v\nwant: %#v\ngot: %#v",
		source, rendered.SQL, arguments, want, got)
}
