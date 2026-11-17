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
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

const (
	// typeAuditCombinations caps the argument combinations tried before a function's result
	// type is read.
	typeAuditCombinations = 8

	// typeAuditView is the view a MySQL-family engine reports a call's result type through.
	typeAuditView = "sgl_type_probe"
)

// resultFieldPattern reads a generated row struct's query name and the Go type of its one
// field, result.
var resultFieldPattern = regexp.MustCompile("type (\\w+)Row struct \\{\\n\\tResult\\s+(.+?)\\s*`")

// typeMismatch is a function whose result Seagull types other than the engine does.
type typeMismatch struct {
	// line is the spec line that declares the function.
	line string

	// call is the call the engine typed.
	call string

	// engineType is the engine's type of the call's result.
	engineType string

	// seagull is the Go type Seagull generates for the call.
	seagull string

	// engine is the Go type Seagull generates for a column of the engine's type.
	engine string

	// breaking is true when the difference fails a scan. That is a sign difference, or a 128
	// or 256-bit engine integer read into a 64-bit Go one, not a narrower width the value
	// still fits or a type left as any.
	breaking bool
}

// typedCall is a call whose result type the engine reported.
type typedCall struct {
	// line is the spec line of the signature called.
	line string

	// call is the call.
	call string

	// engineType is the engine's type of its result.
	engineType string
}

// CheckFunctionTypes compares generated function result types with live engine types.
// CheckFunctionSpec checks nullability separately.
//
// A mismatch that breaks a scan fails the test. Any other mismatch is only logged.
//
// Takes t (*testing.T) which reports the mismatches.
// Takes suite (FuzzSuite) which supplies a database and the dialect.
// Takes specPath (string) which is the spec file.
func CheckFunctionTypes(t *testing.T, suite FuzzSuite, specPath string) {
	t.Helper()
	functions, err := readFunctionSpec(specPath)
	require.NoError(t, err)
	database := suite.Open(t)
	calls := make([]typedCall, 0, len(functions))
	for index := range functions {
		if typed, ok := engineTypedCall(t.Context(), database, suite.Engine, &functions[index]); ok {
			calls = append(calls, typed)
		}
	}
	mismatches, err := compareFunctionTypes(t.Context(), suite, calls)
	require.NoError(t, err)
	t.Logf("function type audit: %d signatures read, %d typed by the engine, %d typed otherwise by Seagull",
		len(functions), len(calls), len(mismatches))
	for _, mismatch := range mismatches {
		message := fmt.Sprintf("%s | %s: the engine types it %s, read as %s, but Seagull generates %s",
			mismatch.line, mismatch.call, mismatch.engineType, mismatch.engine, mismatch.seagull)
		if mismatch.breaking {
			assert.Fail(t, message)
			continue
		}
		t.Log(message)
	}
}

// engineTypedCall finds a call of a function the engine runs and reads its result type.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes function (*specFunction) which is the signature.
//
// Returns typedCall which is the call and its type.
// Returns bool which is false when no call ran or its type says nothing.
func engineTypedCall(ctx context.Context, database *sql.DB, engine string, function *specFunction) (typedCall, bool) {
	argumentValues, ok := functionArgumentValues(engine, function)
	if !ok {
		return typedCall{}, false
	}
	for position := range argumentValues {
		argumentValues[position] = withoutNull(argumentValues[position])
	}
	for _, combination := range combinations(argumentValues, typeAuditCombinations) {
		call := function.name + "(" + strings.Join(combination, ", ") + ")"
		engineType, err := engineResultType(ctx, database, engine, call)
		if err != nil {
			continue
		}
		if informativeType(engineType) {
			return typedCall{line: function.line, call: call, engineType: engineType}, true
		}
		return typedCall{}, false
	}
	return typedCall{}, false
}

// withoutNull returns an argument's values without NULL, whose type the engine may read
// as no type at all.
//
// Takes values ([]string) which are the values.
//
// Returns []string which holds the values but NULL.
func withoutNull(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != "NULL" {
			kept = append(kept, value)
		}
	}
	return kept
}

// engineResultType asks the engine for the type of a call's result.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes call (string) which is the call.
//
// Returns string which is the type as the engine spells it in a column definition.
// Returns error when the engine refuses the call.
func engineResultType(ctx context.Context, database *sql.DB, engine, call string) (string, error) {
	var engineType string
	switch engine {
	case engineClickHouse:
		err := database.QueryRowContext(ctx, "SELECT toTypeName("+call+")").Scan(&engineType)
		return engineType, err
	case enginePostgres, engineTimescaleDB, engineCockroachDB:
		err := database.QueryRowContext(ctx, "SELECT pg_typeof("+call+")::text").Scan(&engineType)
		return engineType, err
	case engineDuckDB:
		err := database.QueryRowContext(ctx, "SELECT typeof("+call+")").Scan(&engineType)
		return engineType, err
	case engineMySQL, engineMariaDB:
		if _, err := database.ExecContext(ctx, "CREATE OR REPLACE VIEW "+typeAuditView+" AS SELECT ("+call+") AS r"); err != nil {
			return "", err
		}
		err := database.QueryRowContext(ctx, "SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
			typeAuditView).Scan(&engineType)
		return engineType, err
	default:
		return "", fmt.Errorf("the type audit does not read %s types", engine)
	}
}

// informativeType reports whether an engine type says what a result holds, unlike the
// type of a NULL or of a polymorphic placeholder.
//
// Takes engineType (string) which is the engine's type.
//
// Returns bool which is true for a type a column can be declared with.
func informativeType(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "", "unknown", "nothing", "nullable(nothing)", "null", "void", "record", "anyelement", `"char"`:
		return false
	default:
		return true
	}
}

// compareFunctionTypes generates each call, and a column of the engine's type for it, in
// one package, and compares the Go types of the two.
//
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes calls ([]typedCall) which are the calls and their engine types.
//
// Returns []typeMismatch which holds the calls typed otherwise.
// Returns error when Seagull cannot be set up or does not generate the probe package.
func compareFunctionTypes(ctx context.Context, suite FuzzSuite, calls []typedCall) ([]typeMismatch, error) {
	var migration, queries strings.Builder
	for index, typed := range calls {
		fmt.Fprintf(&migration, "CREATE TABLE sgl_probe_%d (r %s)%s;\n", index, typed.engineType, tableSuffix(suite.Engine))
		fmt.Fprintf(&queries, "-- sgl.query(name: Call%d, command: one)\nSELECT %s AS result;\n\n", index, typed.call)
		fmt.Fprintf(&queries, "-- sgl.query(name: Column%d, command: one)\nSELECT r AS result FROM sgl_probe_%d;\n\n", index, index)
	}
	files := fstest.MapFS{
		"migrations/0001_probe.up.sql": {Data: []byte(migration.String())},
		"queries/audit.sql":            {Data: []byte(queries.String())},
	}
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	if err != nil {
		return nil, fmt.Errorf("creating the type audit generator: %w", err)
	}
	result, err := generator.Generate(ctx, seagull.Input{Package: "audit", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil {
		return nil, fmt.Errorf("generating the type audit probes: %w", err)
	}
	returned := make(map[string]string)
	for _, file := range result.Files {
		for _, match := range resultFieldPattern.FindAllStringSubmatch(string(file.Content), -1) {
			returned[match[1]] = baseGoType(match[2])
		}
	}
	var mismatches []typeMismatch
	for index, typed := range calls {
		seagullType, called := returned[fmt.Sprintf("Call%d", index)]
		engineGoType, declared := returned[fmt.Sprintf("Column%d", index)]
		if !called || !declared || seagullType == engineGoType || engineGoType == "any" {
			continue
		}
		mismatches = append(mismatches, typeMismatch{line: typed.line, call: typed.call, engineType: typed.engineType, seagull: seagullType, engine: engineGoType,
			breaking: breaksScan(seagullType, engineGoType)})
	}
	return mismatches, nil
}

// tableSuffix is the clause a probe table needs after its columns.
//
// Takes engine (string) which names the engine.
//
// Returns string which is the clause, or empty.
func tableSuffix(engine string) string {
	if engine == engineClickHouse {
		return " ENGINE = Memory"
	}
	return ""
}

// baseGoType strips a Go type of the nullability wrapper or pointer a nullable value has,
// so a nullable and a NOT NULL value of one type compare equal.
//
// Takes goType (string) which is the generated type.
//
// Returns string which is the type without its wrapper.
func baseGoType(goType string) string {
	goType = strings.TrimSpace(goType)
	if inner, ok := strings.CutPrefix(goType, "Null["); ok {
		return strings.TrimSuffix(inner, "]")
	}
	return strings.TrimPrefix(goType, "*")
}

// breaksScan reports whether a value of the engine's Go type fails to scan into the Go
// type Seagull generates. That happens for integers of different signedness, whose
// negative or high-bit values do not fit, and for an integer wider than 64 bits read into
// a 64-bit one.
//
// Takes seagullType (string) which is the generated Go type.
// Takes engineType (string) which is the Go type of the engine's type.
//
// Returns bool which is true when some value fails to scan.
func breaksScan(seagullType, engineType string) bool {
	seagullSigned, seagullInteger := goIntegerSign(seagullType)
	engineSigned, engineInteger := goIntegerSign(engineType)
	if seagullInteger && engineInteger && seagullSigned != engineSigned {
		return true
	}
	return engineType == "big.Int" && seagullInteger
}

// goIntegerSign reports whether a Go type is an integer and whether it is signed.
//
// Takes goType (string) which is the Go type.
//
// Returns signed (bool) which is true for a signed integer.
// Returns integer (bool) which is true for any integer.
func goIntegerSign(goType string) (signed bool, integer bool) {
	switch goType {
	case "int", "int8", "int16", "int32", "int64":
		return true, true
	case "uint", "uint8", "uint16", "uint32", "uint64":
		return false, true
	default:
		return false, false
	}
}
