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

package sqlite_cgo_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	functionSpec       = "../../../internal/dialect/sqlite/builtin_functions.spec"
	mattnMathFunctions = "github.com/mattn/go-sqlite3 builds SQLite without the math functions unless built with the sqlite_math_functions tag"
	mattnFTS5          = "github.com/mattn/go-sqlite3 builds SQLite without FTS5 unless built with the sqlite_fts5 tag"
	mattnDriver        = "sqlite3"
)

func fuzzSuite() harness.FuzzSuite {
	return harness.FuzzSuite{
		Engine:  "sqlite",
		Dialect: func() dialect.Dialect { return dialect_engine_sqlite.New() },
		Open: func(t *testing.T) *sql.DB {
			t.Helper()
			database, err := sql.Open(mattnDriver, filepath.Join(t.TempDir(), "fuzz.db"))
			require.NoError(t, err)
			database.SetMaxOpenConns(1)
			t.Cleanup(func() {
				assert.NoError(t, database.Close())
			})
			return database
		},
		ParseType:    harness.SQLiteTypeName,
		TypeFunction: "typeof(%s)",
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
		Driver:           harness.Driver{Module: "github.com/mattn/go-sqlite3", Import: "github.com/mattn/go-sqlite3", Name: mattnDriver},
		BuildEnvironment: []string{"CGO_ENABLED=1"},
		OpenNative: func(t *testing.T) (*sql.DB, string) {
			t.Helper()
			connection := filepath.Join(t.TempDir(), "native.db")
			database, err := sql.Open(mattnDriver, connection)
			require.NoError(t, err)
			t.Cleanup(func() {
				assert.NoError(t, database.Close())
			})
			return database, connection
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
	harness.CheckGoldenNullSweep(t, fuzzSuite(), "../../../internal/dialect/sqlite/testdata")
}

func TestConcurrentMigrators(t *testing.T) { harness.CheckConcurrentMigrators(t, nativeFuzzSuite()) }

func TestMigrationKill(t *testing.T) {
	harness.CheckMigrationKill(t, nativeFuzzSuite(), "TestMigrationKillChild")
}

func TestMigrationKillChild(t *testing.T) { harness.RunMigrationKillChild(t, nativeFuzzSuite()) }

func TestSeedOutsideTransaction(t *testing.T) {
	harness.CheckSeedOutsideTransaction(t, nativeFuzzSuite())
}

func TestForeignKeyRebuild(t *testing.T) { harness.CheckForeignKeyRebuild(t, nativeFuzzSuite()) }

func TestFileEncodings(t *testing.T) { harness.CheckFileEncodings(t, nativeFuzzSuite()) }

func TestKeywordAliasParity(t *testing.T) { harness.CheckKeywordAliasParity(t, fuzzSuite()) }

func TestKeywordIdentifierParity(t *testing.T) { harness.CheckKeywordIdentifierParity(t, fuzzSuite()) }

func TestFunctionSpecAudit(t *testing.T) {
	harness.CheckFunctionSpec(t, fuzzSuite(), functionSpec)
}

func TestSplitterConstructs(t *testing.T) { harness.CheckSplitterConstructs(t, fuzzSuite()) }

func TestFunctionNameAudit(t *testing.T) {
	harness.CheckFunctionNames(t, fuzzSuite(), functionSpec, map[string]string{
		"acos":      mattnMathFunctions,
		"acosh":     mattnMathFunctions,
		"asin":      mattnMathFunctions,
		"asinh":     mattnMathFunctions,
		"atan":      mattnMathFunctions,
		"atan2":     mattnMathFunctions,
		"atanh":     mattnMathFunctions,
		"ceil":      mattnMathFunctions,
		"ceiling":   mattnMathFunctions,
		"cos":       mattnMathFunctions,
		"cosh":      mattnMathFunctions,
		"degrees":   mattnMathFunctions,
		"exp":       mattnMathFunctions,
		"floor":     mattnMathFunctions,
		"ln":        mattnMathFunctions,
		"log":       mattnMathFunctions,
		"log10":     mattnMathFunctions,
		"log2":      mattnMathFunctions,
		"mod":       mattnMathFunctions,
		"pi":        mattnMathFunctions,
		"pow":       mattnMathFunctions,
		"power":     mattnMathFunctions,
		"radians":   mattnMathFunctions,
		"sin":       mattnMathFunctions,
		"sinh":      mattnMathFunctions,
		"sqrt":      mattnMathFunctions,
		"tan":       mattnMathFunctions,
		"tanh":      mattnMathFunctions,
		"trunc":     mattnMathFunctions,
		"bm25":      mattnFTS5,
		"highlight": mattnFTS5,
		"soundex":   "github.com/mattn/go-sqlite3 builds SQLite without SQLITE_SOUNDEX",
	})
}

func FuzzMigrationLifecycle(f *testing.F) { harness.FuzzMigrationLifecycle(f, nativeFuzzSuite()) }

func FuzzNativeModes(f *testing.F) { harness.FuzzNativeModes(f, nativeFuzzSuite()) }

func FuzzQueryModes(f *testing.F) { harness.FuzzQueryModes(f, nativeFuzzSuite()) }

func FuzzDMLModel(f *testing.F) { harness.FuzzDMLModel(f, nativeFuzzSuite()) }

func FuzzSchemaHistory(f *testing.F) { harness.FuzzSchemaHistory(f, nativeFuzzSuite()) }
