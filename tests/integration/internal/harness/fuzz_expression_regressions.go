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
	"cmp"
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// constantIntegerTypes spells a signed 64-bit integer cast where BIGINT does not fit.
var constantIntegerTypes = map[string]string{engineMySQL: "SIGNED", engineMariaDB: "SIGNED", engineClickHouse: "Int64"}

// CheckFuzzExpressionRegressions replays numeric and predicate regressions.
//
// Takes t (*testing.T) which reports semantic and metadata mismatches.
// Takes suite (FuzzSuite) which supplies the native engine and isolated database.
func CheckFuzzExpressionRegressions(t *testing.T, suite FuzzSuite) {
	t.Helper()
	for name, data := range map[string][]byte{
		"negative_cast":         []byte("\x0200000000071"),
		"positive_parameter":    []byte("\x02\x00\x00\x01\x09\x00\x00\x00\x00\x00\x07\x02\x00"),
		"negative_parameter":    []byte("\x02\x00\x00\x01\x00\x00\x00\x00\x00\x00\x07\x02\x00"),
		"membership_predicate":  []byte("\x02000000000007"),
		"membership_projection": []byte("\x02000000000&7"),
		"membership_nested":     []byte("\x0221000000000111000017000"),
		"repeated_boolean":      []byte("\x0200000000000120"),
	} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(suite.Engine, data)
			require.True(t, ok, "invalid probe model")
			t.Log(model.Query().Reference)
			for _, oracle := range []FuzzOracle{OracleQuery, OracleCount, OracleTypes} {
				t.Run(string(oracle), func(t *testing.T) {
					runFuzzDatabaseInput(t, suite, oracle, data, &sync.Once{})
				})
			}
		})
	}
}

// CheckFuzzConstantNegation compares native metadata for constant and variable negation.
//
// Takes t (*testing.T) which reports incorrect inferred numeric categories.
// Takes suite (FuzzSuite) which supplies the MySQL-family engine and isolated database.
func CheckFuzzConstantNegation(t *testing.T, suite FuzzSuite) {
	t.Helper()
	expressions := []string{
		"-CAST(-8.0 AS SIGNED)", "-CAST(-8e0 AS SIGNED)", "-CAST(-8.5 AS SIGNED)",
		"-CAST(-0.49 AS SIGNED)", "-CAST(-0.5 AS SIGNED)", "-CAST(-0.5e0 AS SIGNED)",
		"-CAST(0.5 AS SIGNED)", "-CAST(0.5e0 AS SIGNED)", "-CAST(-8.5e0 AS SIGNED)",
		"-CAST(-8.5 AS UNSIGNED)", "-CAST(-8.5e0 AS UNSIGNED)", "-CAST(-0.49 AS UNSIGNED)",
		"-CAST((-8.5+0.1) AS SIGNED)", "-CAST((-8.5e0+0.1) AS SIGNED)",
		"-COALESCE(NULL,CAST(-8 AS SIGNED))", "-COALESCE(NULL,CAST(-8 AS SIGNED),id)",
		"-CASE WHEN TRUE THEN CAST(-8 AS SIGNED) ELSE 1 END",
		"-CASE WHEN TRUE THEN CAST(-8 AS SIGNED) END",
		"-CASE WHEN FALSE THEN NULL ELSE CAST(-8 AS SIGNED) END",
		"-8", "-(-8)", "-CAST(-8 AS SIGNED)", "-CAST(8 AS SIGNED)", "-(1-2)", "-(2-1)",
		"-COALESCE(CAST(-8 AS SIGNED), 1)", "-CASE WHEN 1=1 THEN CAST(-8 AS SIGNED) ELSE 1 END",
		"-CAST(-9223372036854775808 AS SIGNED)", "-(-9223372036854775808)", "-CAST(18446744073709551615 AS UNSIGNED)",
		"-CAST(8 AS UNSIGNED)", "-CAST(-8 AS UNSIGNED)", "-CAST(8 AS DECIMAL(20,0))", "-CAST(-8 AS DECIMAL(20,0))",
		"-(CAST(1 AS SIGNED)-CAST(2 AS SIGNED))", "-COALESCE(CAST(-8 AS SIGNED), id)",
		"-CASE WHEN id=1 THEN CAST(-8 AS SIGNED) ELSE 1 END",
	}
	queries := make([]string, 0, len(expressions))
	for _, expression := range expressions {
		queries = append(queries, "SELECT "+expression+" AS v FROM constant_values")
	}
	CheckTypes(t, &TypeSuite{
		Dialect: suite.Dialect, Database: suite.Open(t), Parse: suite.ParseType,
		Probe: func(ctx context.Context, database *sql.DB, query string) (string, error) {
			name, err := ColumnTypeProbe(ctx, database, query)
			require.NoError(t, err, query)
			return name, err
		},
		Allowlist: TypeAllowlist{}, Schema: []string{"CREATE TABLE constant_values (id BIGINT NOT NULL)"},
		Rows: []string{"INSERT INTO constant_values (id) VALUES (1)"}, Queries: queries, Width: false,
	})
}

// CheckFuzzMigrationDependencies replays dependent and unrelated index alterations.
//
// Takes t (*testing.T) which reports native execution or catalogue differences.
// Takes suite (FuzzSuite) which supplies the supported database adapter.
func CheckFuzzMigrationDependencies(t *testing.T, suite FuzzSuite) {
	t.Helper()
	if suite.Engine == engineClickHouse {
		t.Skip("The Memory fixture does not support ordinary secondary indexes.")
	}
	for name, data := range map[string][]byte{
		"drop_unrelated_column":   []byte("\x06\x02\x01\x06\x06\x01\x01\x01"),
		"rename_unrelated_column": []byte("\x06\x01\x00\x01\x02\x02"),
		"rename_then_drop_key":    []byte("\x06\x00\x13\x01"),
	} {
		t.Run(name, func(t *testing.T) {
			runFuzzDatabaseInput(t, suite, OracleMigration, data, &sync.Once{})
		})
	}
}

// CheckFuzzBitwisePrefix compares native literal complement types across engines.
//
// Takes t (*testing.T) which reports incorrect widths or signedness.
// Takes suite (FuzzSuite) which supplies native type observation.
func CheckFuzzBitwisePrefix(t *testing.T, suite FuzzSuite) {
	t.Helper()
	expression := "~8"
	if suite.Engine == engineClickHouse {
		expression = "bitNot(CAST(8 AS Int64))"
	}
	CheckTypes(t, &TypeSuite{
		Dialect: suite.Dialect, Database: suite.Open(t), Parse: suite.ParseType, Probe: requiredTypeProbe(t, suite),
		Allowlist: TypeAllowlist{}, Schema: nil,
		Queries: []string{"SELECT " + expression + " AS v"}, Rows: nil, Width: true,
	})
}

// CheckFuzzConstantForms compares equivalent numeric constants across engines.
//
// Takes t (*testing.T) which reports native type differences.
// Takes suite (FuzzSuite) which supplies native syntax and metadata adapters.
func CheckFuzzConstantForms(t *testing.T, suite FuzzSuite) {
	t.Helper()
	integer := cmp.Or(constantIntegerTypes[suite.Engine], "BIGINT")
	queries := []string{
		"SELECT -CAST(-8.0 AS " + integer + ") AS v",
		"SELECT -CAST(-8e0 AS " + integer + ") AS v",
		"SELECT -CAST(-8.5 AS " + integer + ") AS v",
		"SELECT -COALESCE(NULL,CAST(-8 AS " + integer + ")) AS v",
		"SELECT -CASE WHEN TRUE THEN CAST(-8 AS " + integer + ") ELSE 1 END AS v",
	}
	CheckTypes(t, &TypeSuite{Dialect: suite.Dialect, Database: suite.Open(t), Parse: suite.ParseType, Probe: requiredTypeProbe(t, suite),
		Allowlist: TypeAllowlist{}, Schema: nil, Queries: queries, Rows: nil, Width: false})
}

// requiredTypeProbe observes result types through the suite's type function when it has
// one, and fails the test when the engine refuses a probe.
//
// Takes t (*testing.T) which reports a refused probe.
// Takes suite (FuzzSuite) which supplies the type function.
//
// Returns func(context.Context, *sql.DB, string) (string, error) which is the probe.
func requiredTypeProbe(t *testing.T, suite FuzzSuite) func(context.Context, *sql.DB, string) (string, error) {
	t.Helper()
	probe := ColumnTypeProbe
	if suite.TypeFunction != "" {
		probe = ValueTypeProbe(suite.TypeFunction)
	}
	return func(ctx context.Context, database *sql.DB, query string) (string, error) {
		name, err := probe(ctx, database, query)
		require.NoError(t, err, query)
		return name, err
	}
}
