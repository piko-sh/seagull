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
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/emit"
)

const (
	compileInputDeadline    = time.Minute
	compileTypeOverrideFlag = 8
	proxyOff                = "GOPROXY=off"
)

var errCompileDeadline = errors.New("the generated package did not compile in time")

func FuzzGeneratedCompile(f *testing.F) {
	for _, target := range targets() {
		for _, emitter := range target.emitters {
			f.Add(target.name, emitter.name, []byte{1, 1, 0, 8, 3, 7})
			f.Add(target.name, emitter.name, []byte{2, 1, 9, 8, 3, 7, 1, 2, 0, 0, 3, 0, 1, 1, 2, 0, 1, 0, 3, 2, 0, 0, 0, 1, 2, 1})
			for _, seed := range [][]byte{
				{3, 0, 1, 8, 3, 7, 1, 1, 0}, {3, 1, 1, 8, 3, 7, 1, 1, 2}, {3, 2, 1, 8, 3, 7, 1, 1, 3}, {3, 3, 1, 8, 3, 7},
				{5, 0, 0, 8, 0, 0, 0, 0, 0, 255}, {5, 1, 6, 8}, {5, 2, 0, 8},
			} {
				if _, ok := fuzzcase.Decode(target.name, seed); ok {
					f.Add(target.name, emitter.name, seed)
				}
			}
		}
	}
	f.Fuzz(func(t *testing.T, engine, driver string, data []byte) {
		model, ok := fuzzcase.Decode(engine, data)
		if !ok {
			t.Skip("unsupported model version, engine or shape")
		}
		target, emitter, ok := fuzzCompileTarget(engine, driver)
		if !ok {
			t.Skip("unsupported engine and emitter combination")
		}
		ctx, cancel := context.WithTimeoutCause(t.Context(), compileInputDeadline, errCompileDeadline)
		defer cancel()
		files := fuzzCompileFiles(ctx, t, target, emitter, model)
		checksums, err := os.ReadFile("go.sum")
		require.NoError(t, err)
		output, err := buildGeneratedPackage(ctx, t, target, files, targetRequirements(t), checksums)
		require.NoError(t, err, "generated package failed to compile for %s/%s\n%s\nSQL: %s", engine, driver, output, model.Query().SQL)
	})
}

func fuzzCompileTarget(engine, driver string) (dialectTarget, emitterTarget, bool) {
	for _, target := range targets() {
		if target.name != engine {
			continue
		}
		for _, emitter := range target.emitters {
			if emitter.name == driver {
				return target, emitter, true
			}
		}
	}
	return dialectTarget{}, emitterTarget{}, false
}

func fuzzCompileFiles(ctx context.Context, t *testing.T, target dialectTarget, emitter emitterTarget, model fuzzcase.Model) []emit.GeneratedFile {
	t.Helper()
	var options []seagull.Option
	if model.Flags&compileTypeOverrideFlag != 0 {
		options = append(options, seagull.WithTypeOverrides(seagull.TypeOverride{
			Column: "fuzz_items.score", GoName: "int32", StructTags: map[string]string{"db": "model_score"},
		}))
	}
	return generateTargetPackage(ctx, t, target, emitter, strings.Join(model.Migrations(), "\n"), model.Query().SQL, options...)
}

func generateTargetPackage(ctx context.Context, t *testing.T, target dialectTarget, emitter emitterTarget, migration, queries string,
	options ...seagull.Option) []emit.GeneratedFile {
	t.Helper()
	engine := target.newDialect()
	options = append([]seagull.Option{seagull.WithEmitter(emitter.newEmitter(engine.Info())), seagull.WithPreset(emitter.preset())}, options...)
	generator, err := seagull.New(engine, options...)
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(migration)},
		"queries/fuzz.sql":           &fstest.MapFile{Data: []byte(queries)},
	}
	result, err := generator.Generate(ctx, seagull.Input{Package: generatedPackage, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	require.NoError(t, result.Err())
	require.NotEmpty(t, result.Files, "generation produced no code")
	require.Equal(t, 1, result.QueryCount, "generation produced another number of queries")
	return result.Files
}

func buildGeneratedPackage(ctx context.Context, t *testing.T, target dialectTarget, files []emit.GeneratedFile, requirements []string,
	checksums []byte) ([]byte, error) {
	t.Helper()
	directory := t.TempDir()
	writePackage(t, directory, files)
	writeTargetModule(t, directory, requirements, checksums)
	environment := append([]string{moduleMode, proxyOff, cgoOff}, target.buildEnvironment...)
	command := testkit.GoCommandContext(ctx, directory, environment, "build", "./...")
	command.WaitDelay = commandWaitDelay
	return command.CombinedOutput()
}
