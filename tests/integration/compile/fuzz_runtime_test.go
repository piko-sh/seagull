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

package compile_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	runtimeSequenceLimit     = 32
	runtimeExchangeDeadline  = 10 * time.Second
	runtimeWaitDelay         = 5 * time.Second
	runtimeFileMode          = 0o600
	runtimeDirectoryMode     = 0o755
	preparedRecorder         = "databasesql_prepared"
	databaseSQLRecorder      = "databasesql"
	clickHouseTarget         = "clickhouse"
	runtimeBaseCondition     = "id >= 1001"
	runtimeBaseSelect        = "SELECT id, score FROM fuzz_items WHERE " + runtimeBaseCondition
	runtimeOperationKinds    = 12
	runtimeListKinds         = 9
	runtimeLimitKinds        = 8
	runtimeOffsetLimit       = 9
	runtimeFilterBase        = 2000
	runtimeListBase          = 3000
	runtimeBatchIDBase       = 4000
	runtimeBatchCategoryBase = 5000
)

const (
	operationReset = iota
	operationFilter
	operationNullFilter
	operationOrder
	operationLimit
	operationOffset
	operationAll
	operationCount
	operationPlain
	operationList
	operationBatch
	operationOptional
)

var (
	errRuntimeDeadline = errors.New("the generated runtime helper did not finish in time")
	runtimeRecorders   = []string{databaseSQLRecorder, preparedRecorder, "pgx", clickHouseTarget}
	runtimeSeeds       = [][]byte{{252}, {253}, {48, 8, 8, 8, 0, 8}, {48, 6, 7, 8, 21}, {96, 6, 7, 8, 21}, {144, 6, 7, 8, 21}, {192, 6, 7, 8, 21}, {6},
		{1, 2, 3, 4, 5, 6, 7, 11, 0, 6}, {8, 9, 21, 33, 10, 22, 34}, {11, 23, 35, 47, 11}, {1, 6, 47, 4, 7, 11, 6}}
	runtimePlaceholders = map[string][2]string{
		"sqlite":         {"?1", "?2"},
		"mysql":          {"?", "?"},
		"mariadb":        {"?", "?"},
		clickHouseTarget: {"{id:Int64}", "{category:Int64}"},
	}
	packageClause = regexp.MustCompile(`(?m)^package \w+$`)
)

type runtimeChecker struct {
	t          *testing.T
	facts      *dialect.Facts
	response   *fuzzruntime.Response
	condition  string
	order      string
	pagination string
	data       []byte
	limit      int
	offset     int
	callIndex  int
}

func FuzzGeneratedRuntime(f *testing.F) {
	for _, target := range targets() {
		for _, emitter := range target.emitters {
			if !slices.Contains(runtimeRecorders, emitter.name) {
				continue
			}
			for _, sequence := range runtimeSeeds {
				f.Add(target.name, emitter.name, sequence)
			}
		}
	}
	root := f.TempDir()
	processes := make(map[string]*fuzzruntime.Process)
	f.Cleanup(func() {
		for name, process := range processes {
			assert.NoError(f, process.Close(), "%s", name)
		}
	})
	f.Fuzz(func(t *testing.T, engine, driver string, data []byte) {
		if len(data) > runtimeSequenceLimit {
			t.Skipf("runtime sequence exceeds %d operations", runtimeSequenceLimit)
		}
		if !slices.Contains(runtimeRecorders, driver) {
			t.Skip("unsupported recorder")
		}
		target, emitter, ok := fuzzCompileTarget(engine, driver)
		if !ok {
			t.Skip("unsupported emitter and engine combination")
		}
		key := engine + "-" + driver
		process := processes[key]
		if process == nil {
			process = buildFuzzRuntime(t, filepath.Join(root, key), target, emitter)
			processes[key] = process
		}
		ctx, cancel := context.WithTimeoutCause(t.Context(), runtimeExchangeDeadline, errRuntimeDeadline)
		defer cancel()
		response, err := process.Exchange(ctx, data)
		require.NoError(t, err)
		require.Empty(t, response.Panic, "generated code panicked")
		for _, problem := range fuzzruntime.PreparedProblems(driver == preparedRecorder, data, response) {
			assert.Fail(t, problem)
		}
		if response.PreparationError != "" {
			return
		}
		facts := target.newDialect().Info()
		checker := &runtimeChecker{t: t, facts: &facts, response: &response, condition: runtimeBaseCondition, order: "", pagination: "",
			data: data, limit: 0, offset: 0, callIndex: 0}
		checker.check()
	})
}

func (checker *runtimeChecker) check() {
	checker.t.Helper()
	for step, operation := range checker.data {
		checker.apply(step, operation)
		checker.pagination = ""
		if checker.limit > 0 {
			checker.pagination = fmt.Sprintf(" LIMIT %d", checker.limit)
		}
		if checker.offset > 0 {
			checker.pagination += fmt.Sprintf(" OFFSET %d", checker.offset)
		}
	}
	checker.terminal(len(checker.data), "SELECT id, score FROM fuzz_items WHERE "+checker.condition+checker.order+checker.pagination)
	require.Len(checker.t, checker.response.Calls, checker.callIndex, "generated code executed extra terminal calls")
}

func (checker *runtimeChecker) apply(step int, operation byte) {
	checker.t.Helper()
	switch operation % runtimeOperationKinds {
	case operationReset:
		checker.condition, checker.order, checker.pagination, checker.limit, checker.offset = runtimeBaseCondition, "", "", 0, 0
	case operationFilter:
		checker.condition += " AND id >= " + strconv.Itoa(runtimeFilterBase+int(operation))
	case operationNullFilter:
		checker.condition += " AND score IS NULL"
	case operationOrder:
		checker.appendOrder(operation)
	case operationLimit:
		checker.limit = int(operation%runtimeLimitKinds) + 1
	case operationOffset:
		checker.limit, checker.offset = runtimeOffsetLimit, int(operation%runtimeLimitKinds)
	case operationAll:
		checker.terminal(step, "SELECT id, score FROM fuzz_items WHERE "+checker.condition+checker.order+checker.pagination)
	case operationCount:
		checker.terminal(step, "SELECT COUNT(*) FROM fuzz_items WHERE "+checker.condition)
	case operationPlain:
		checker.terminal(step, runtimeBaseSelect)
	case operationList:
		checker.terminal(step, "SELECT id FROM fuzz_items WHERE id "+checker.listPredicate(operation))
	case operationBatch:
		checker.batch(step, operation)
	case operationOptional:
		checker.terminal(step, runtimeBaseSelect)
		checker.terminal(step, fuzzruntime.OptionalSQL(operation, false))
		checker.terminal(step, fuzzruntime.OptionalSQL(operation, true))
	}
}

func (checker *runtimeChecker) appendOrder(operation byte) {
	direction := "ASC"
	if operation&1 != 0 {
		direction = "DESC"
	}
	if checker.order == "" {
		checker.order = " ORDER BY id " + direction
		return
	}
	checker.order += ", id " + direction
}

func (checker *runtimeChecker) listPredicate(operation byte) string {
	count := int(operation/runtimeOperationKinds) % runtimeListKinds
	if count == 0 {
		return checker.facts.Lists.EmptyMembership
	}
	values := make([]string, 0, count)
	for index := range count {
		values = append(values, strconv.Itoa(runtimeListBase+index))
	}
	return "IN (" + strings.Join(values, ", ") + ")"
}

func (checker *runtimeChecker) next(missing string) fuzzruntime.Call {
	checker.t.Helper()
	require.Less(checker.t, checker.callIndex, len(checker.response.Calls), "generated code omitted a %s call", missing)
	call := checker.response.Calls[checker.callIndex]
	checker.callIndex++
	return call
}

func (checker *runtimeChecker) terminal(step int, expected string) {
	checker.t.Helper()
	call := checker.next("terminal")
	require.True(checker.t, call.Step == step && len(call.Statements) == 1, "unexpected terminal result %+v", call)
	for _, problem := range fuzzruntime.RowProblems(checker.data, expected, call) {
		assert.Fail(checker.t, problem)
	}
	want, err := fuzzruntime.SQLValues(checker.facts, fuzzruntime.Statement{SQL: expected})
	require.NoError(checker.t, err)
	got, err := fuzzruntime.SQLValues(checker.facts, call.Statements[0])
	require.NoError(checker.t, err)
	require.True(checker.t, slices.Equal(want, got), "step %d changed SQL or bindings\nwant: %s (%q)\ngot: %+v (%q)", step, expected, want, call.Statements[0], got)
}

func (checker *runtimeChecker) batch(step int, operation byte) {
	checker.t.Helper()
	call := checker.next("batch")
	require.True(checker.t, call.Step == step && call.Error == "", "batch failed %+v", call)
	var got []string
	for _, statement := range call.Statements {
		require.Contains(checker.t, strings.ToLower(statement.SQL), "fuzz_items", "batch changed its destination")
		for _, argument := range statement.Arguments {
			got = append(got, argument.Values...)
		}
	}
	count := int(operation/runtimeOperationKinds) % runtimeListKinds
	want := make([]string, 0, 2*count)
	for index := range count {
		want = append(want, strconv.Itoa(runtimeBatchIDBase+index), strconv.Itoa(runtimeBatchCategoryBase+index))
	}
	require.True(checker.t, slices.Equal(want, got), "batch reordered rows or columns: want %v, got %v", want, got)
}

func buildFuzzRuntime(t *testing.T, directory string, target dialectTarget, emitter emitterTarget) *fuzzruntime.Process {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), compileInputDeadline, errRuntimeDeadline)
	defer cancel()
	generateRuntimePackage(ctx, t, directory, target, emitter)
	source, modules := writeRuntimeRecorder(t, directory, target, emitter)
	manifest := "module " + targetModule + "\n\ngo 1.27.0\n" + writeRuntimeStubs(t, directory, modules)
	for name, content := range map[string]string{"go.mod": manifest, "recorder.go": source, "main.go": fuzzruntime.Program} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(content), runtimeFileMode))
	}
	binary := filepath.Join(directory, "runtime")
	command := testkit.GoCommandContext(ctx, directory, []string{proxyOff, cgoOff}, "build", "-o", binary, ".")
	command.WaitDelay = runtimeWaitDelay
	output, err := command.CombinedOutput()
	require.NoError(t, err, "building runtime helper\n%s", output)
	process, err := fuzzruntime.Start(binary, os.Stderr)
	require.NoError(t, err)
	return process
}

func generateRuntimePackage(ctx context.Context, t *testing.T, directory string, target dialectTarget, emitter emitterTarget) {
	t.Helper()
	model, ok := fuzzcase.Decode(target.name, []byte{1, 0})
	require.True(t, ok, "unsupported runtime engine")
	engine := target.newDialect()
	generator, err := seagull.New(engine, seagull.WithEmitter(emitter.newEmitter(engine.Info())))
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(strings.Join(model.Migrations(), "\n"))},
		"queries/fuzz.sql":           &fstest.MapFile{Data: []byte(runtimeQueries(target.name))},
	}
	result, err := generator.Generate(ctx, seagull.Input{Package: generatedPackage, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	require.NoError(t, result.Err())
	writePackage(t, filepath.Join(directory, generatedPackage), result.Files)
}

func runtimeQueries(engine string) string {
	placeholders, known := runtimePlaceholders[engine]
	if !known {
		placeholders = [2]string{"$1", "$2"}
	}
	placeholder, second := placeholders[0], placeholders[1]
	anchor, secondAnchor := placeholder, second
	if placeholder == "?" {
		anchor, secondAnchor = "?1", "?2"
	}
	optionalFirst, optionalSecond := anchor, secondAnchor
	if engine == clickHouseTarget {
		optionalFirst, optionalSecond = "{minimum:Nullable(Int64)}", "{category:Nullable(Int64)}"
	}
	return "-- sgl.query(FuzzBuilder, many, dynamic: runtime)\nSELECT id, score FROM fuzz_items WHERE id >= " + placeholder + ";\n" +
		"-- sgl.query(FuzzPlain, many)\nSELECT id, score FROM fuzz_items WHERE id >= " + placeholder + ";\n" +
		"-- sgl.query(FuzzOther, many)\nSELECT id, score FROM fuzz_items WHERE id < " + placeholder + ";\n" +
		"-- sgl.query(FuzzOptional, many, dynamic: runtime)\n-- " + optionalFirst + " as sgl.param(minimum, optional: true)\n-- " +
		optionalSecond + " as sgl.param(category, optional: true)\n" +
		"SELECT id, score FROM fuzz_items WHERE id > 0 AND (id >= " + optionalFirst + " OR " + optionalFirst + " IS NULL) AND (category = " +
		optionalSecond + " OR " + optionalSecond + " IS NULL);\n" +
		"-- sgl.query(FuzzList, many)\n-- " + anchor + " as sgl.param(ids, kind: slice)\nSELECT id FROM fuzz_items WHERE id IN (" + placeholder + ");\n" +
		"-- sgl.query(FuzzInsert, batch)\nINSERT INTO fuzz_items (id, category) VALUES (" + placeholder + ", " + second + ");\n"
}

func writeRuntimeRecorder(t *testing.T, directory string, target dialectTarget, emitter emitterTarget) (string, map[string]fs.FS) {
	t.Helper()
	recorderName := emitter.name
	if recorderName == preparedRecorder {
		recorderName = databaseSQLRecorder
	}
	recorder := fuzzruntime.Driver(recorderName)
	populated, err := fuzzruntime.PopulateRecorder(recorder.Source)
	require.NoError(t, err)
	if emitter.name == preparedRecorder {
		populated, err = fuzzruntime.InstrumentPreparedRecorder(populated)
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "fuzz_prepared.go"), []byte(fuzzruntime.PreparedSource(emitter.name)), runtimeFileMode))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "fuzz_rows.go"), []byte(fuzzruntime.RowsSource(recorderName)), runtimeFileMode))
	modules := recorder.Modules
	if target.name == clickHouseTarget && emitter.name == databaseSQLRecorder {
		modules = fuzzruntime.Driver(clickHouseTarget).Modules
	}
	return packageClause.ReplaceAllString(populated, "package main"), modules
}

func writeRuntimeStubs(t *testing.T, directory string, modules map[string]fs.FS) string {
	t.Helper()
	var manifest strings.Builder
	for index, module := range slices.Sorted(maps.Keys(modules)) {
		relative := fmt.Sprintf("stub%d", index)
		version := "v0.0.0"
		switch {
		case strings.HasSuffix(module, "/v5"):
			version = "v5.0.0"
		case strings.HasSuffix(module, "/v2"):
			version = "v2.0.0"
		}
		manifest.WriteString("\nrequire " + module + " " + version + "\nreplace " + module + " => ./" + relative + "\n")
		require.NoError(t, copyModule(modules[module], filepath.Join(directory, relative)))
		require.NoError(t, os.WriteFile(filepath.Join(directory, relative, "go.mod"), []byte("module "+module+"\n\ngo 1.27.0\n"), runtimeFileMode))
	}
	return manifest.String()
}

func copyModule(files fs.FS, directory string) error {
	return fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), runtimeDirectoryMode); err != nil {
			return fmt.Errorf("creating the directory of %s: %w", name, err)
		}
		return os.WriteFile(path, content, runtimeFileMode)
	})
}
