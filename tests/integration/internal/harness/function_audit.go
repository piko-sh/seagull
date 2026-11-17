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
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

const (
	// auditZero is the zero the audit writes for numbers.
	auditZero = "0"

	// functionAuditCombinations bounds how many argument combinations one function is called
	// with.
	functionAuditCombinations = 48
)

var (
	// functionAuditSkippedFlags lists the spec flags of functions the audit does not call.
	// These are aggregates, set-returning and window functions, and functions that change
	// data.
	functionAuditSkippedFlags = []string{"aggregate", "returns-set", "modifies-data", "window"}

	// functionAuditVolatile lists functions whose results vary or that wait, which the audit
	// does not call.
	functionAuditVolatile = map[string]bool{
		"random": true, "rand": true, "sleep": true, "pg_sleep": true, "nextval": true, "setval": true, "uuid": true,
		"gen_random_uuid": true, "randomblob": true, "load_extension": true, "benchmark": true, "get_lock": true,
		"release_lock": true, "sleepeachrow": true, "throwif": true,
	}
)

// auditValueRules lists, in order, how the audit writes each kind of type.
var auditValueRules = []auditValueRule{
	{matches: func(lower string) bool {
		return strings.Contains(lower, "[]") || strings.HasPrefix(lower, "array") || strings.HasPrefix(lower, "map") ||
			strings.HasPrefix(lower, "tuple") || strings.Contains(lower, "list") || strings.Contains(lower, "struct")
	}, values: nil},
	{matches: func(lower string) bool { return strings.HasPrefix(lower, "uint") }, values: []string{auditZero, "1", "255"}},
	{matches: func(lower string) bool { return strings.Contains(lower, "int") }, values: []string{auditZero, "1", "-1", "100"}},
	{matches: func(lower string) bool {
		return strings.Contains(lower, "float") || strings.Contains(lower, "double") || strings.Contains(lower, "real") ||
			strings.Contains(lower, "numeric") || strings.Contains(lower, "decimal")
	}, values: []string{auditZero, "1.5", "-1", "100"}},
	{matches: func(lower string) bool { return strings.Contains(lower, "bool") }, values: []string{"true", "false"}},
	{matches: func(lower string) bool { return strings.Contains(lower, "json") }, values: []string{`'{"a":1}'`, "'[1,2]'", `'"a"'`}},
	{matches: func(lower string) bool {
		return strings.Contains(lower, "timestamp") || strings.Contains(lower, "datetime")
	},
		values: []string{"'2024-01-31 12:00:00'"}},
	{matches: func(lower string) bool { return lower == "date" || strings.HasPrefix(lower, "date32") }, values: []string{"'2024-01-31'"}},
	{matches: func(lower string) bool { return lower == "time" }, values: []string{"'12:00:00'"}},
	{matches: func(lower string) bool { return strings.Contains(lower, "interval") }, values: []string{"'1 day'"}},
	{matches: func(lower string) bool { return strings.Contains(lower, "uuid") }, values: []string{"'00000000-0000-0000-0000-000000000001'"}},
	{matches: func(lower string) bool {
		return strings.Contains(lower, "text") || strings.Contains(lower, "char") || strings.Contains(lower, "string") || lower == "name"
	}, values: []string{"''", "'a'", "'1'", "'2024-01-31'"}},
}

// specFunction is one signature a builtin_functions.spec line declares.
type specFunction struct {
	// name is the function's name.
	name string

	// line is the spec line, for the report.
	line string

	// arguments holds each argument's declared type.
	arguments []string

	// calledOnNull marks a function that may return NULL for arguments that are not NULL.
	calledOnNull bool

	// neverNull marks a function that returns a value whatever its arguments.
	neverNull bool

	// emptyTextNulls is the one-based argument whose empty string makes the result NULL, or
	// zero.
	emptyTextNulls int

	// nonNumericNulls is the one-based argument whose text that is no number makes the
	// result NULL, or zero.
	nonNumericNulls int

	// zeroNulls is the one-based argument whose zero makes the result NULL, or zero.
	zeroNulls int
}

// functionAudit holds what auditing one spec needs.
type functionAudit struct {
	// database is connected to the engine.
	database *sql.DB

	// generator types each call the way generated code would.
	generator *seagull.Generator

	// engine names the engine.
	engine string

	// report receives the findings.
	report functionAuditReport
}

// functionAuditReport holds what a function audit found.
type functionAuditReport struct {
	// StrictNulls lists the strict functions that returned NULL for arguments that are not
	// NULL.
	StrictNulls []string

	// NeverNullNulls lists the never-null functions that returned NULL.
	NeverNullNulls []string

	// Called counts the functions called at least once without an error.
	Called int
}

// auditValueRule gives the values of the types a test picks out.
type auditValueRule struct {
	// matches reports whether a lower-cased type name is of the rule's kind.
	matches func(lower string) bool

	// values holds the values, written before the cast, or nil for a kind the audit does not
	// write.
	values []string
}

// check calls one function with each combination of argument values and records the first
// combination that breaks what its spec claims.
//
// A NULL is a finding only when Seagull, generating the same call, types its result as
// never NULL, so a dialect resolver that types a function itself is judged by what it
// generates rather than by the spec.
//
// Takes function (*specFunction) which is the signature.
func (audit *functionAudit) check(ctx context.Context, function *specFunction) {
	if function.calledOnNull && !function.neverNull {
		return
	}
	argumentValues, ok := functionArgumentValues(audit.engine, function)
	if !ok {
		return
	}
	called := false
	for _, combination := range combinations(argumentValues, functionAuditCombinations) {
		call := function.name + "(" + strings.Join(combination, ", ") + ")"
		isNull, err := audit.callIsNull(ctx, call)
		if err != nil {

			continue
		}
		called = true
		if isNull && !audit.typesNullable(ctx, call) && recordAuditFinding(function, call, &audit.report) {
			break
		}
	}
	if called {
		audit.report.Called++
	}
}

// callIsNull runs one call and reports whether it returned NULL.
//
// Takes call (string) which is the call.
//
// Returns bool which is true when the call returned NULL.
// Returns error when the engine refuses the call.
func (audit *functionAudit) callIsNull(ctx context.Context, call string) (bool, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	var isNull bool
	err := audit.database.QueryRowContext(ctx, "SELECT ("+call+") IS NULL").Scan(&isNull)
	return isNull, err
}

// typesNullable generates a query selecting a call and reports whether Seagull types its
// result as possibly NULL. A call Seagull refuses or cannot type counts as nullable,
// since its generated code never scans a NULL into a value.
//
// Takes call (string) which is the call.
//
// Returns bool which is true unless Seagull types the result as never NULL.
func (audit *functionAudit) typesNullable(ctx context.Context, call string) bool {
	files := fstest.MapFS{
		"migrations/0001_empty.up.sql": {Data: []byte("-- The function audit reads no table.\n")},
		"queries/audit.sql":            {Data: []byte("-- sgl.query(name: Audit, command: one)\nSELECT " + call + " AS result;\n")},
	}
	details, err := audit.generator.GenerateDetailed(ctx, seagull.Input{Package: "audit", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil || details.Report == nil || len(details.Report.Queries) != 1 || len(details.Report.Queries[0].OutputColumns) != 1 {
		return true
	}
	return details.Report.Queries[0].OutputColumns[0].Nullable
}

// applyFlag records one spec flag the audit reads.
//
// Takes flag (string) which is the flag.
func (function *specFunction) applyFlag(flag string) {
	function.calledOnNull = function.calledOnNull || flag == "called-on-null"
	function.neverNull = function.neverNull || flag == "never-null"
	var position int
	if _, err := fmt.Sscanf(flag, "empty-text-nulls=%d", &position); err == nil {
		function.emptyTextNulls = position + 1
	}
	if _, err := fmt.Sscanf(flag, "zero-nulls=%d", &position); err == nil {
		function.zeroNulls = position + 1
	}
	if _, err := fmt.Sscanf(flag, "non-numeric-nulls=%d", &position); err == nil {
		function.nonNumericNulls = position + 1
	}
}

// CheckFunctionSpec checks declared scalar function nullability against the live engine.
// A strict function returning NULL for non-NULL arguments or a never-null function
// returning NULL would produce a result that generated code cannot scan.
//
// Takes t (*testing.T) which reports the findings.
// Takes suite (FuzzSuite) which supplies a database.
// Takes specPath (string) which is the spec file.
func CheckFunctionSpec(t *testing.T, suite FuzzSuite, specPath string) {
	t.Helper()
	functions, err := readFunctionSpec(specPath)
	require.NoError(t, err)
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	audit := &functionAudit{database: suite.Open(t), generator: generator, engine: suite.Engine, report: functionAuditReport{}}
	for index := range functions {
		audit.check(t.Context(), &functions[index])
	}
	report := &audit.report
	t.Logf("function audit: %d signatures read, %d called, %d strict functions returned NULL, %d never-null functions returned NULL",
		len(functions), report.Called, len(report.StrictNulls), len(report.NeverNullNulls))
	for _, finding := range report.StrictNulls {
		assert.Failf(t, "strict function returned NULL", "the engine returns NULL for %s, a call Seagull types as never NULL", finding)
	}
	for _, finding := range report.NeverNullNulls {
		assert.Failf(t, "never-null function returned NULL", "the engine returns NULL for %s, which the spec declares never NULL", finding)
	}
}

// functionArgumentValues lists the values the audit calls each argument with.
//
// Takes engine (string) which names the engine.
// Takes function (*specFunction) which is the signature.
//
// Returns [][]string which holds each argument's values.
// Returns bool which is false when an argument has a type the audit does not write.
func functionArgumentValues(engine string, function *specFunction) ([][]string, bool) {
	argumentValues := make([][]string, 0, len(function.arguments))
	for position, argument := range function.arguments {
		values := excludeDeclaredNulls(function, position, auditValues(engine, argument))
		if len(values) == 0 {
			return nil, false
		}
		if function.neverNull {
			values = append(values, "NULL")
		}
		argumentValues = append(argumentValues, values)
	}
	return argumentValues, true
}

// recordAuditFinding records a NULL Seagull types as never NULL against what the spec
// claims.
//
// Takes function (*specFunction) which is the signature.
// Takes call (string) which is the call that returned NULL.
// Takes report (*functionAuditReport) which receives the finding.
//
// Returns bool which is true when the NULL breaks the spec's claim.
func recordAuditFinding(function *specFunction, call string, report *functionAuditReport) bool {
	finding := call + "  [spec: " + function.line + "]"
	switch {
	case function.neverNull:
		report.NeverNullNulls = append(report.NeverNullNulls, finding)
	case !function.calledOnNull && !strings.Contains(call, "NULL"):
		report.StrictNulls = append(report.StrictNulls, finding)
	default:
		return false
	}
	return true
}

// excludeDeclaredNulls drops the values of an argument the spec already says make the
// result NULL. That is the empty string where it declares empty-text-nulls, text where it
// declares non-numeric-nulls, and zero where it declares zero-nulls.
//
// Takes function (*specFunction) which is the signature.
// Takes position (int) which is the zero-based argument.
// Takes values ([]string) which are the argument's values.
//
// Returns []string which holds the values left.
func excludeDeclaredNulls(function *specFunction, position int, values []string) []string {
	kept := values[:0:0]
	for _, value := range values {
		switch {
		case function.emptyTextNulls == position+1 && strings.Contains(value, "''"):
		case function.nonNumericNulls == position+1 && strings.Contains(value, "'"):
		case function.zeroNulls == position+1 && (value == auditZero || strings.HasPrefix(value, "CAST("+auditZero+" ")):
		default:
			kept = append(kept, value)
		}
	}
	return kept
}

// combinations lists the argument lists formed by taking one value for each argument, at
// most limit of them, varying the last argument fastest.
//
// Takes values ([][]string) which holds each argument's values.
// Takes limit (int) which bounds the count.
//
// Returns [][]string which holds the argument lists.
func combinations(values [][]string, limit int) [][]string {
	result := [][]string{{}}
	for _, choices := range values {
		next := make([][]string, 0, len(result)*len(choices))
		for _, prefix := range result {
			for _, choice := range choices {
				if len(next) >= limit {
					break
				}
				next = append(next, append(slices.Clone(prefix), choice))
			}
		}
		result = next
	}
	return result
}

// readFunctionSpec reads the scalar function signatures of a builtin_functions.spec,
// leaving out aggregates, set-returning and data-changing functions, volatile functions
// and variadic tails.
//
// Takes path (string) which is the spec file.
//
// Returns []specFunction which holds the signatures.
// Returns error when the file cannot be read.
func readFunctionSpec(path string) ([]specFunction, error) {
	lines, err := readSpecLines(path)
	if err != nil {
		return nil, err
	}
	var functions []specFunction
	for _, line := range lines {
		if function, ok := parseSpecLine(line); ok {
			functions = append(functions, function)
		}
	}
	return functions, nil
}

// readSpecLines reads the trimmed lines of a builtin_functions.spec.
//
// Takes path (string) which is the spec file.
//
// Returns []string which holds the lines in order.
// Returns error when the file cannot be read.
func readSpecLines(path string) ([]string, error) {
	file, err := os.Open(path) //nolint:gosec // the path names a spec in the repository
	if err != nil {
		return nil, fmt.Errorf("opening the function spec: %w", err)
	}
	defer func() { _ = file.Close() }()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading the function spec %s: %w", path, err)
	}
	return lines, nil
}

// parseSpecLine reads one spec line.
//
// Takes line (string) which is the line.
//
// Returns specFunction which is the signature.
// Returns bool which is false for a comment, a blank line or a function the audit skips.
func parseSpecLine(line string) (specFunction, bool) {
	open, closeAt := strings.Index(line, "("), strings.LastIndex(line, ")")
	if line == "" || strings.HasPrefix(line, "#") || open <= 0 || closeAt < open {
		return specFunction{}, false
	}
	flags := strings.Fields(line[closeAt+1:])
	name := strings.TrimSpace(line[:open])
	if skippedSpecFunction(name, flags) {
		return specFunction{}, false
	}
	function := specFunction{name: name, line: line, arguments: nil, calledOnNull: false, neverNull: false,
		emptyTextNulls: 0, nonNumericNulls: 0, zeroNulls: 0}
	for _, flag := range flags {
		function.applyFlag(flag)
	}
	arguments := strings.TrimSpace(line[open+1 : closeAt])
	if arguments == "" {
		return function, true
	}
	for _, argument := range splitSpecArguments(arguments) {
		typeName, ok := argumentType(argument)
		if !ok {
			return specFunction{}, false
		}
		if strings.HasSuffix(typeName, "?") {
			break
		}
		function.arguments = append(function.arguments, typeName)
	}
	return function, true
}

// skippedSpecFunction reports whether the audit leaves a function out. It leaves out an
// aggregate, a set-returning or data-changing function, a volatile one, and one named
// with a schema.
//
// Takes name (string) which is the function's name.
// Takes flags ([]string) which are its spec flags.
//
// Returns bool which is true for a function the audit does not call.
func skippedSpecFunction(name string, flags []string) bool {
	for _, flag := range flags {
		if slices.Contains(functionAuditSkippedFlags, flag) {
			return true
		}
	}
	return functionAuditVolatile[strings.ToLower(name)] || strings.Contains(name, ".")
}

// argumentType reads the type of one spec argument, written as a name and a type or as a
// type alone.
//
// Takes argument (string) which is the argument.
//
// Returns string which is the type.
// Returns bool which is false for an empty argument.
func argumentType(argument string) (string, bool) {
	fields := strings.Fields(argument)
	switch len(fields) {
	case 0:
		return "", false
	case 1:
		return fields[0], true
	default:
		return strings.Join(fields[1:], " "), true
	}
}

// splitSpecArguments splits an argument list at its top-level commas, so a type such as
// Array(Tuple(UInt8, String)) stays whole.
//
// Takes arguments (string) which is the list.
//
// Returns []string which holds each argument.
func splitSpecArguments(arguments string) []string {
	var parts []string
	depth, start := 0, 0
	for index, character := range arguments {
		switch character {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, arguments[start:index])
				start = index + 1
			}
		}
	}
	return append(parts, arguments[start:])
}

// auditValues writes representative values of a declared type as SQL, each cast to the
// type, or nothing for a type the audit does not write.
//
// Takes engine (string) which names the engine.
// Takes typeName (string) which is the declared type.
//
// Returns []string which holds the values.
func auditValues(engine, typeName string) []string {
	if inner, nullable := strings.CutPrefix(typeName, "Nullable("); nullable {
		typeName = strings.TrimSuffix(inner, ")")
	}
	lower := strings.ToLower(strings.TrimSpace(typeName))
	switch {
	case lower == "any":
		return []string{"1", "'a'", auditZero}
	case lower == "blob" || lower == "bytea" || strings.Contains(lower, "binary"):
		return auditBlobs(engine)
	}
	for _, rule := range auditValueRules {
		if !rule.matches(lower) {
			continue
		}
		values := make([]string, 0, len(rule.values))
		for _, value := range rule.values {
			values = append(values, fmt.Sprintf("CAST(%s AS %s)", value, typeName))
		}
		return values
	}
	return nil
}

// auditBlobs writes representative byte strings in the engine's spelling.
//
// Takes engine (string) which names the engine.
//
// Returns []string which holds the values.
func auditBlobs(engine string) []string {
	if isPostgresFamily(engine) {
		return []string{`'\x00'::bytea`, `'\x6162'::bytea`}
	}
	return []string{"X'00'", "X'6162'"}
}
