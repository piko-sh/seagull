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

//go:build integration

package timescaledb_test

import (
	"database/sql"
	"testing"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_timescaledb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const functionSpec = "../../../internal/dialect/postgres/builtin_functions.spec"

func fuzzSuite() harness.FuzzSuite {
	return harness.FuzzSuite{
		Engine:  "timescaledb",
		Dialect: func() dialect.Dialect { return dialect_engine_timescaledb.New() },
		Open: func(t *testing.T) *sql.DB {
			t.Helper()
			database, _ := harness.IsolatedSchema(t, "pgx", testConnectionString)
			return database
		},
		ParseType:    harness.PostgresTypeName,
		TypeFunction: "pg_typeof(%s)::text",
		Width:        true,
	}
}

func FuzzQueryEquivalence(f *testing.F) {
	harness.FuzzDatabase(f, fuzzSuite(), harness.OracleQuery)
}

func FuzzCount(f *testing.F) {
	harness.FuzzDatabase(f, fuzzSuite(), harness.OracleCount)
}

func FuzzResultTypes(f *testing.F) {
	harness.FuzzDatabase(f, fuzzSuite(), harness.OracleTypes)
}

func FuzzMigrationReplay(f *testing.F) {
	harness.FuzzDatabase(f, fuzzSuite(), harness.OracleMigration)
}

func TestFuzzCountOracle(t *testing.T) {
	harness.CheckFuzzCountOracle(t, fuzzSuite())
}

func TestFuzzParameterNameCollisions(t *testing.T) {
	harness.CheckFuzzParameterNameCollisions(t, fuzzSuite())
}

func TestFuzzMixedPositionalBindings(t *testing.T) {
	harness.CheckFuzzMixedPositionalBindings(t, fuzzSuite())
}

func TestFuzzDollarIdentifiers(t *testing.T) {
	harness.CheckFuzzDollarIdentifiers(t, fuzzSuite())
}

func TestFuzzDollarSliceBounds(t *testing.T) {
	harness.CheckFuzzDollarSliceBounds(t, fuzzSuite())
}

func TestFuzzDollarParameterNames(t *testing.T) {
	harness.CheckFuzzDollarParameterNames(t, fuzzSuite())
}

func TestFuzzUserVariables(t *testing.T) {
	harness.CheckFuzzUserVariables(t, fuzzSuite())
}

func FuzzSchemaEquivalence(f *testing.F) {
	harness.FuzzSchemaEquivalence(f, fuzzSuite())
}

func TestFuzzTypeAliases(t *testing.T) { harness.CheckFuzzTypeAliases(t, fuzzSuite()) }

func FuzzDMLSequence(f *testing.F) { harness.FuzzDMLSequence(f, fuzzSuite()) }

func nativeFuzzSuite() harness.NativeFuzzSuite {
	return harness.NativeFuzzSuite{FuzzSuite: fuzzSuite(),
		Driver:           harness.Driver{Module: "github.com/jackc/pgx/v5", Import: "github.com/jackc/pgx/v5/stdlib", Name: "pgx"},
		BuildEnvironment: nil,
		OpenNative: func(t *testing.T) (*sql.DB, string) {
			t.Helper()
			return harness.IsolatedSchema(t, "pgx", testConnectionString)
		},
	}
}

func FuzzGeneratedNativeValues(f *testing.F) {
	harness.FuzzGeneratedNativeValues(f, nativeFuzzSuite())
}
func FuzzGeneratedNativeShapes(f *testing.F) { harness.FuzzGeneratedNativeShapes(f, nativeFuzzSuite()) }

func FuzzMigrationExecution(f *testing.F) { harness.FuzzMigrationExecution(f, nativeFuzzSuite()) }

func TestFuzzWorkflowSoak(t *testing.T) { harness.CheckFuzzWorkflowSoak(t, nativeFuzzSuite()) }

func TestGoldenNullSweep(t *testing.T) {
	harness.CheckGoldenNullSweep(t, fuzzSuite(), "../../../internal/dialect/timescaledb/testdata")
}

func TestConcurrentMigrators(t *testing.T) { harness.CheckConcurrentMigrators(t, nativeFuzzSuite()) }

func TestMigrationKill(t *testing.T) {
	harness.CheckMigrationKill(t, nativeFuzzSuite(), "TestMigrationKillChild")
}

func TestMigrationKillChild(t *testing.T) { harness.RunMigrationKillChild(t, nativeFuzzSuite()) }

func TestSeedOutsideTransaction(t *testing.T) {
	harness.CheckSeedOutsideTransaction(t, nativeFuzzSuite())
}

func TestMigrationInterruption(t *testing.T) {
	harness.CheckMigrationInterruption(t, nativeFuzzSuite())
}

func TestFileEncodings(t *testing.T) { harness.CheckFileEncodings(t, nativeFuzzSuite()) }

func TestKeywordAliasParity(t *testing.T) { harness.CheckKeywordAliasParity(t, fuzzSuite()) }

func TestKeywordIdentifierParity(t *testing.T) { harness.CheckKeywordIdentifierParity(t, fuzzSuite()) }

func TestFunctionSpecAudit(t *testing.T) {
	harness.CheckFunctionSpec(t, fuzzSuite(), functionSpec)
}

func TestFunctionTypeAudit(t *testing.T) {
	harness.CheckFunctionTypes(t, fuzzSuite(), functionSpec)
}

func TestSplitterConstructs(t *testing.T) { harness.CheckSplitterConstructs(t, fuzzSuite()) }

func TestFunctionNameAudit(t *testing.T) {
	harness.CheckFunctionNames(t, fuzzSuite(), functionSpec, map[string]string{
		"similarity":             "a pg_trgm function, present once a migration creates the extension",
		"strict_word_similarity": "a pg_trgm function, present once a migration creates the extension",
		"word_similarity":        "a pg_trgm function, present once a migration creates the extension",
	})
}

func FuzzCLIMigrate(f *testing.F) { harness.FuzzCLIMigrate(f, nativeFuzzSuite()) }

func TestLimitedUserMigrations(t *testing.T) {
	harness.CheckLimitedUserMigrations(t, nativeFuzzSuite())
}

func FuzzMigrationLifecycle(f *testing.F) { harness.FuzzMigrationLifecycle(f, nativeFuzzSuite()) }

func FuzzNativeModes(f *testing.F) { harness.FuzzNativeModes(f, nativeFuzzSuite()) }

func FuzzQueryModes(f *testing.F) { harness.FuzzQueryModes(f, nativeFuzzSuite()) }

func FuzzDMLModel(f *testing.F) { harness.FuzzDMLModel(f, nativeFuzzSuite()) }

func FuzzSchemaHistory(f *testing.F) { harness.FuzzSchemaHistory(f, nativeFuzzSuite()) }
