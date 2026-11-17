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

package emittest

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// emitInputDeadline bounds generation for one input.
	emitInputDeadline = 10 * time.Second

	// emitWatchdogDeadline stops workers that ignore cancellation.
	emitWatchdogDeadline = 20 * time.Second

	// emitFlagsByte locates the model's flags byte in a fuzz input.
	emitFlagsByte = 2

	// emitStyleFlag selects empty slices and doc comments.
	emitStyleFlag = 2

	// emitInterfaceFlag selects interface generation.
	emitInterfaceFlag = 4

	// emitTypeOverrideFlag selects a supported nullable integer mapping.
	emitTypeOverrideFlag = 8

	// NoPreparedFlag is the bit of the flags byte FuzzEmit hands its factory that turns the
	// prepared wrapper off.
	NoPreparedFlag = 16

	// PreparedFlag is the bit of the flags byte FuzzEmit hands its factory that turns the
	// prepared wrapper on where the driver supports it.
	PreparedFlag = 32

	// emitCollisionFlag exercises reserved names and folded column names.
	emitCollisionFlag = 64

	// emitFilenameFlag exercises names interpreted as Go platform constraints.
	emitFilenameFlag = 128

	// emitCombinedFlags exercises every style and naming option.
	emitCombinedFlags = emitStyleFlag | emitInterfaceFlag | emitTypeOverrideFlag | NoPreparedFlag | PreparedFlag |
		emitCollisionFlag | emitFilenameFlag
)

// FuzzEmit checks generated Go for valid models and bounded style combinations.
//
// Takes f (*testing.F) which registers inputs and failures.
// Takes engines ([]string) which lists supported engine names.
// Takes factory (func(*dialect.Facts, byte) emit.EmitterPort) which creates a fresh
// emitter.
//
// Panics in the watchdog callback if generation ignores cancellation.
func FuzzEmit(f *testing.F, engines []string, factory func(*dialect.Facts, byte) emit.EmitterPort) {
	f.Helper()
	for _, engine := range engines {
		for _, flags := range []byte{NoPreparedFlag, PreparedFlag, emitCollisionFlag, emitFilenameFlag, emitCombinedFlags} {
			seed := slices.Clone(fuzzcase.Seeds(engine)[0])
			seed[emitFlagsByte] = flags
			f.Add(engine, seed)
		}
		for _, seed := range fuzzcase.Seeds(engine) {
			f.Add(engine, seed)
			static := slices.Clone(seed)
			static[emitFlagsByte] |= PreparedFlag
			f.Add(engine, static)
		}
	}
	f.Fuzz(func(t *testing.T, engine string, data []byte) {
		if !slices.Contains(engines, engine) {
			t.Skip("unsupported emitter and engine combination")
		}
		model, ok := fuzzcase.Decode(engine, data)
		if !ok {
			t.Skip("unsupported model version, shape or size")
		}
		ctx, cancel := context.WithTimeout(t.Context(), emitInputDeadline)
		defer cancel()
		watchdog := time.AfterFunc(emitWatchdogDeadline, func() { panic("emitter ignored cancellation: " + t.Name()) })
		defer watchdog.Stop()
		first := fuzzEmission(ctx, t, factory, model)
		second := fuzzEmission(ctx, t, factory, model)
		if !reflect.DeepEqual(first.Result.Files, second.Result.Files) || !reflect.DeepEqual(first.Result.Methods, second.Result.Methods) {
			t.Fatal("emission is not deterministic")
		}
		checked := typeCheck(first.Result.Files)
		for _, problem := range checked.problems {
			t.Error(problem)
		}
		checkFuzzStaticOutput(t, first, checked, model)
		checkFuzzFilenames(t, first.Result)
	})
}

// checkFuzzStaticOutput applies existing binding and output checks to each model.
//
// Takes t (*testing.T) which reports generated package defects.
// Takes generated (*app.DetailedResult) which contains emitted files and analysis.
// Takes checked (*checkedPackage) which holds parsed and type-checked output.
// Takes model (fuzzcase.Model) which supplies the dialect and style choices.
func checkFuzzStaticOutput(t *testing.T, generated *app.DetailedResult, checked *checkedPackage, model fuzzcase.Model) {
	t.Helper()
	engine, err := dialects.New(model.Engine, dialects.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info := engine.Info()
	problems := fileProblems("fuzz", generated.Result)
	problems = append(problems, bindingProblems(checked)...)
	problems = append(problems, rowProblems(checked)...)
	problems = append(problems, fuzzScanOrderProblems(checked)...)
	problems = append(problems, fuzzStaticMappingProblems(checked, &info, generated)...)
	problems = append(problems, placeholderProblems(checked, &info)...)
	if model.Flags&emitInterfaceFlag != 0 {
		problems = append(problems, interfaceProblems(generated.Result, checked)...)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

// fuzzEmission generates one model with a fresh engine and emitter.
//
// Takes t (*testing.T) which reports generation failures.
// Takes factory (func(*dialect.Facts, byte) emit.EmitterPort) which supplies the emitter.
// Takes model (fuzzcase.Model) which supplies SQL and style choices.
//
// Returns *app.DetailedResult which holds generated files and their analysis.
func fuzzEmission(ctx context.Context, t *testing.T, factory func(*dialect.Facts, byte) emit.EmitterPort, model fuzzcase.Model) *app.DetailedResult {
	t.Helper()
	engine, err := dialects.New(model.Engine, dialects.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info := engine.Info()
	facts := info.Facts()
	options := []app.Option{app.WithEmitter(factory(&facts, model.Flags))}
	preset := app.DefaultPreset()
	if model.Flags&emitStyleFlag != 0 {
		preset.EmptySlices = true
		preset.DocComments = true
	}
	if model.Flags&emitInterfaceFlag != 0 {
		preset.Interface.Enabled = true
		preset.Interface.Name = "Querier"
	}
	options = append(options, app.WithPreset(preset))
	if model.Flags&emitTypeOverrideFlag != 0 {
		options = append(options, app.WithTypeOverrides(app.TypeOverride{
			Column: "fuzz_items.score", GoName: "int32", GoPackage: "", SQLTypeName: "", NonNullElements: nil,
			StructTags: map[string]string{"db": "model_score"},
		}))
	}
	generator, err := app.New(engine, options...)
	if err != nil {
		t.Fatal(err)
	}
	query := model.Query().SQL
	if model.Flags&PreparedFlag != 0 {
		query = strings.Replace(query, ", dynamic: runtime", "", 1)
	}
	queryCount := 1
	if model.Flags&emitCollisionFlag != 0 {
		query = strings.Replace(query, "name: FuzzQuery", "name: New", 1)
		query += "\n-- sgl.query(Context, many, dynamic: runtime)\nSELECT id AS user_id, category AS user__id FROM fuzz_items;"
		queryCount++
	}
	queryFile := "queries/fuzz.sql"
	if model.Flags&emitFilenameFlag != 0 {
		queryFile = "queries/fuzz_linux.sql"
	}
	files := fstest.MapFS{
		"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(strings.Join(model.Migrations(), "\n"))},
		queryFile:                    &fstest.MapFile{Data: []byte(query)},
	}
	result, err := generator.GenerateDetailed(ctx, app.Input{Package: "generated", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Result.Err(); err != nil {
		t.Fatalf("positive emitter model: %v\n%s", err, model.Query().SQL)
	}
	if len(result.Result.Files) == 0 || result.Result.QueryCount != queryCount {
		t.Fatalf("positive model emitted %d files and %d queries", len(result.Result.Files), result.Result.QueryCount)
	}
	return result
}

// checkFuzzFilenames rejects duplicate names and paths outside the generated package.
//
// Takes t (*testing.T) which reports invalid files.
// Takes result (*app.Result) which holds generated output.
func checkFuzzFilenames(t *testing.T, result *app.Result) {
	t.Helper()
	seen := make(map[string]bool)
	for _, file := range result.Files {
		if file.Name == "" || seen[file.Name] || strings.Contains(file.Name, "/") || strings.Contains(file.Name, "\\") {
			t.Fatalf("invalid generated filename %q", file.Name)
		}
		seen[file.Name] = true
	}
}
