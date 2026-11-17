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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// nativeModule names a helper module allowed to import the internal protocol package.
	nativeModule = "seagull.sh/seagull/nativefuzz"

	// nativeCompileBound limits one generated compilation after dependencies are prepared.
	nativeCompileBound = time.Minute

	// nativeProgramBytes bounds all retained compiled native helpers.
	nativeProgramBytes = 512 << 20

	// nativeProgramEntries bounds retained compiled shapes independently of byte size.
	nativeProgramEntries = 8

	// nativeMethodsPerType requires every insert, read, stream and builder method.
	nativeMethodsPerType = 4

	// nativePreparedShape separates prepared and ordinary compiled catalogues.
	nativePreparedShape = 4

	// nativeDriverShape selects a dedicated native emitter catalogue.
	nativeDriverShape = 8
)

// nativeLibraries names the helper libraries that affect native execution.
var nativeLibraries = []string{"internal/testkit/fuzznative", "internal/testkit/fuzzcase"}

// helperProgram describes one helper module whose generated packages are written.
type helperProgram struct {
	// directory holds the helper module.
	directory string

	// name names the executable.
	name string

	// main is the helper's entry point.
	main string

	// files holds the generated sources, named as the cache key records them.
	files []emit.GeneratedFile

	// sources holds further source names and contents the cache key covers.
	sources [][]byte
}

// errNativeCompileDeadline is the cause of a generated compilation's context once
// nativeCompileBound has passed.
var errNativeCompileDeadline = errors.New("the generated native helper did not compile in time")

// buildNativeProgram compiles actual generated Go against the suite's pinned driver.
//
// Takes t (*testing.T) which reports generation and build failures.
// Takes suite (NativeFuzzSuite) which supplies dialect and native driver configuration.
// Takes cache (*fuzzruntime.ProgramCache) which owns bounded persistent executables.
// Takes shape (int) which selects an independently equivalent query catalogue.
//
// Returns string which names a verified cached executable.
func buildNativeProgram(t *testing.T, suite NativeFuzzSuite, cache *fuzzruntime.ProgramCache, shape int) string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), nativeCompileBound, errNativeCompileDeadline)
	defer cancel()
	files := nativeGeneratedFiles(ctx, t, suite, shape)
	directory := t.TempDir()
	for _, file := range files {
		writeFile(t, filepath.Join(directory, "generated", file.Name), file.Content)
	}
	return compileHelper(ctx, t, suite, cache, &helperProgram{directory: directory, name: "native-program",
		main: nativeMain(suite.Engine, suite.Driver.Name, shape), files: files, sources: nil})
}

// compileHelper writes a helper module's manifest, entry point and driver beside its
// generated packages, and builds it unless the cache holds the same build.
//
// Takes t (*testing.T) which reports build failures.
// Takes suite (NativeFuzzSuite) which supplies the driver and build environment.
// Takes cache (*fuzzruntime.ProgramCache) which retains compiled helpers.
// Takes helper (*helperProgram) which describes the helper.
//
// Returns string which names a verified cached executable.
func compileHelper(ctx context.Context, t *testing.T, suite NativeFuzzSuite, cache *fuzzruntime.ProgramCache, helper *helperProgram) string {
	t.Helper()
	root := nativeRepositoryRoot(t)
	manifest := RunnerModuleFile(nativeModule, nativeRequirements(t, root, suite.Driver.Module)) + "\nreplace seagull.sh/seagull => " + strconv.Quote(root) + "\n"
	writeFile(t, filepath.Join(helper.directory, moduleFile), []byte(manifest))
	writeFile(t, filepath.Join(helper.directory, "main.go"), []byte(helper.main))
	writeFile(t, filepath.Join(helper.directory, "driver.go"), []byte(DriverSource(suite.Driver)))
	environment := append([]string{"GOPROXY=off"}, suite.BuildEnvironment...)
	TidyModule(ctx, t, helper.directory, environment...)
	key := nativeProgramKey(ctx, t, root, helper.directory, environment, helper.files, helper.main)
	if len(helper.sources) > 0 {
		key = fuzzruntime.ProgramKey(append(slices.Clone(helper.sources), []byte(key))...)
	}
	cached, err := cache.Find(key)
	require.NoError(t, err)
	if cached != "" {
		return cached
	}
	binary := filepath.Join(helper.directory, helper.name)
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	RunGo(ctx, t, helper.directory, environment, "build", "-p=1", "-o", binary, ".")
	retained, err := cache.Retain(key, binary)
	require.NoError(t, err)
	return retained
}

// nativeGeneratedFiles requires every admitted catalogue query to reach generation.
//
// Takes t (*testing.T) which reports failures.
// Takes suite (NativeFuzzSuite) which supplies the exact native driver profile.
// Takes shape (int) which selects equivalent projection syntax.
//
// Returns []emit.GeneratedFile which contains the actual generated query API.
func nativeGeneratedFiles(ctx context.Context, t *testing.T, suite NativeFuzzSuite, shape int) []emit.GeneratedFile {
	t.Helper()
	generator, err := seagull.New(suite.Dialect(), seagull.WithEmitter(modeEmitter(suite, shapeMode(shape))))
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations/001_native.up.sql": &fstest.MapFile{Data: []byte(strings.Join(fuzzcase.RuntimeSchema(suite.Engine), "\n"))},
		"queries/native.sql":           &fstest.MapFile{Data: []byte(fuzzcase.RuntimeQueries(suite.Engine, shape%nativePreparedShape))},
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "generated", PackagePath: "", Queries: nil,
		Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	require.NoError(t, details.Result.Err(), "positive native catalogue shape %d", shape)
	require.Len(t, details.Report.Queries, nativeMethodsPerType*len(fuzzcase.RuntimeSchema(suite.Engine)),
		"native catalogue did not reach every required generated method")
	require.NotEmpty(t, details.Result.Files, "native catalogue did not reach every required generated method")
	return details.Result.Files
}

// shapeMode names the execution mode a compiled shape selects.
//
// Takes shape (int) which is the compiled shape.
//
// Returns string which is the native, prepared or direct database/sql mode.
func shapeMode(shape int) string {
	switch {
	case shape >= nativeDriverShape:
		return modeNative
	case shape%nativeDriverShape >= nativePreparedShape:
		return modePrepared
	default:
		return modeSQL
	}
}

// openProgramCache opens the program cache under the campaign's managed root, or under a
// temporary directory outside managed campaigns.
//
// Takes t (*testing.T) which reports an unusable cache.
// Takes temporary (string) which is the worker's own temporary directory.
//
// Returns *fuzzruntime.ProgramCache which the worker closes.
func openProgramCache(t *testing.T, temporary string) *fuzzruntime.ProgramCache {
	t.Helper()
	root := cmp.Or(os.Getenv(fuzzManagedRootVariable), temporary)
	cache, err := fuzzruntime.OpenProgramCache(filepath.Join(root, "programs"), nativeProgramBytes, nativeProgramEntries)
	require.NoError(t, err)
	return cache
}

// nativeProgramKey fingerprints source bytes, effective modules and toolchain settings.
//
// Takes t (*testing.T) which reports unsafe or unreadable inputs.
// Takes root (string) which names the repository containing helper library sources.
// Takes directory (string) which contains the resolved standalone module.
// Takes environment ([]string) which supplies the exact build environment.
// Takes files ([]emit.GeneratedFile) which contains the generated sources.
// Takes main (string) which contains the complete helper entry point.
//
// Returns string which invalidates the cache when any effective build input changes.
func nativeProgramKey(ctx context.Context, t *testing.T, root, directory string, environment []string, files []emit.GeneratedFile, main string) string {
	t.Helper()
	parts := make([][]byte, 0, 1+2*len(files))
	parts = append(parts, []byte(main))
	for _, name := range []string{moduleFile, "go.sum", "driver.go"} {
		content, err := nativeSource(directory, name)
		require.NoError(t, err)
		parts = append(parts, []byte(name), content)
	}
	for _, file := range files {
		parts = append(parts, []byte(file.Name), file.Content)
	}
	parts = append(parts, moduleSources(t, root, nativeLibraries)...)
	command := testkit.GoCommandContext(ctx, directory, environment, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED",
		"GOAMD64", "GOARM64", "GOEXPERIMENT", "GOFLAGS", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS")
	settings, err := command.Output()
	require.NoError(t, err)
	parts = append(parts, settings)
	return fuzzruntime.ProgramKey(parts...)
}

// nativeRepositoryRoot finds the checkout's root module without reading unrelated files.
//
// Takes t (*testing.T) which reports a missing root module.
//
// Returns string which names the directory declaring seagull.sh/seagull.
func nativeRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	for {
		content, err := nativeSource(directory, moduleFile)
		if err == nil && strings.HasPrefix(string(content), "module seagull.sh/seagull\n") {
			return directory
		}
		parent := filepath.Dir(directory)
		require.NotEqual(t, directory, parent, "native fuzzing could not find the root module")
		directory = parent
	}
}

// nativeRequirements pins generated value dependencies to the compilation suite.
//
// Takes t (*testing.T) which reports missing dependency versions.
// Takes root (string) which contains the compilation suite manifest.
// Takes driver (string) which names the current native driver module.
//
// Returns map[string]string which prevents network version discovery during mutations.
func nativeRequirements(t *testing.T, root, driver string) map[string]string {
	t.Helper()
	manifest, err := nativeSource(root, filepath.Join("tests", "integration", "compile", moduleFile))
	require.NoError(t, err)
	requirements := map[string]string{"seagull.sh/seagull": "v0.0.0", driver: ModuleVersion(t, driver)}
	for _, module := range []string{"github.com/cockroachdb/apd/v3", "github.com/google/uuid"} {
		version, found, err := RequiredVersion(manifest, module)
		require.NoError(t, err, "reading the pinned generated value dependency %s", module)
		require.True(t, found, "missing pinned generated value dependency %s", module)
		requirements[module] = version
	}
	return requirements
}

// nativeSource confines source reads to the selected module directory.
//
// Takes directory (string) which names a trusted checkout or owned temporary module.
// Takes name (string) which is relative to that module.
//
// Returns []byte which supplies exact source or manifest bytes.
// Returns error when a path escapes its module or cannot be read.
func nativeSource(directory, name string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", directory, err)
	}
	content, err := root.ReadFile(name)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("reading %s: %w", name, err), root.Close())
	}
	return content, root.Close()
}

// moduleSources fingerprints the non-test Go sources of packages in the repository.
//
// Takes t (*testing.T) which reports missing or unsafe source paths.
// Takes root (string) which is the repository root.
// Takes directories ([]string) which name the packages relative to the root.
//
// Returns [][]byte which pairs stable source names with their exact contents.
func moduleSources(t *testing.T, root string, directories []string) [][]byte {
	t.Helper()
	var parts [][]byte
	for _, subdirectory := range directories {
		names, err := fs.Glob(os.DirFS(root), subdirectory+"/*.go")
		require.NoError(t, err)
		slices.Sort(names)
		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			content, err := nativeSource(root, name)
			require.NoError(t, err)
			parts = append(parts, []byte(name), content)
		}
	}
	return parts
}
