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

package dialecttest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit"
	emit_clickhouse "seagull.sh/seagull/internal/emit/clickhouse"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/pgx"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/pikopreset"
)

var updateCompat = flag.Bool("update-piko-compat", false, "Re-record the frozen Piko compatibility corpus; a diff there changes output Piko already ships, so it is never part of the golden update lane")

const (
	compatDirectory       = "testdata/piko_compat"
	compatManifest        = "manifest.json"
	compatFullList        = "full.txt"
	compatFullDirectory   = "full"
	compatInputsDirectory = "inputs"
)

type compatEmitter struct {
	newEmitter func(engine dialect.DialectPort) emit.EmitterPort
	name       string
}

type compatEngine struct {
	newEngine func() dialect.DialectPort
	name      string
	emitters  []compatEmitter
}

type compatSpec struct {
	CustomFunctions []sqlmodel.CustomFunctionConfig `json:"customFunctions"`
	ShouldError     bool                            `json:"shouldError"`
}

type compatOutput struct {
	files       map[string][]byte
	diagnostics []byte
	err         string
}

type compatRecord struct {
	Files       map[string]string `json:"files,omitempty"`
	Diagnostics string            `json:"diagnostics,omitempty"`
	Error       string            `json:"error,omitempty"`
}

var databaseSQLEmitter = compatEmitter{
	name: "database_sql",
	newEmitter: func(engine dialect.DialectPort) emit.EmitterPort {
		return databasesql.New()
	},
}

var pgxEmitter = compatEmitter{
	name: "pgx",
	newEmitter: func(dialect.DialectPort) emit.EmitterPort {
		return pgx.New()
	},
}

var clickHouseNativeEmitter = compatEmitter{
	name: "clickhouse_native",
	newEmitter: func(dialect.DialectPort) emit.EmitterPort {
		return emit_clickhouse.New()
	},
}

var compatEngines = []compatEngine{
	{name: "postgres", newEngine: func() dialect.DialectPort { return postgres.New() },
		emitters: []compatEmitter{databaseSQLEmitter, pgxEmitter}},
	{name: "cockroachdb", newEngine: func() dialect.DialectPort { return cockroachdb.New() },
		emitters: []compatEmitter{databaseSQLEmitter, pgxEmitter}},
	{name: "mysql", newEngine: func() dialect.DialectPort { return mysql.New() },
		emitters: []compatEmitter{databaseSQLEmitter}},
	{name: "mariadb", newEngine: func() dialect.DialectPort { return mariadb.New() },
		emitters: []compatEmitter{databaseSQLEmitter}},
	{name: "sqlite", newEngine: func() dialect.DialectPort { return sqlite.New() },
		emitters: []compatEmitter{databaseSQLEmitter}},
	{name: "duckdb", newEngine: func() dialect.DialectPort { return duckdb.New() },
		emitters: []compatEmitter{databaseSQLEmitter}},
	{name: "clickhouse", newEngine: func() dialect.DialectPort { return clickhouse.New() },
		emitters: []compatEmitter{databaseSQLEmitter, clickHouseNativeEmitter}},
}

type compatDiagnostic struct {
	Filename   string
	Message    string
	Code       string
	Suggestion string
	Line       int
	Column     int
	EndLine    int
	EndColumn  int
	Severity   uint8
}

func TestPikoCompatibilityCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("the compatibility corpus generates every engine case; skipped under -short")
	}

	fullCases := loadFullCaseList(t)
	manifest := make(map[string]compatRecord)

	for _, engine := range compatEngines {
		caseDirectories := engineCaseDirectories(t, engine.name)
		for _, caseDirectory := range caseDirectories {
			caseName := filepath.Base(caseDirectory)
			for _, emitter := range engine.emitters {
				key := engine.name + "/" + caseName + "/" + emitter.name
				output := generateCompatCase(t, engine, emitter, caseDirectory)
				manifest[key] = recordOf(output)
				if slices.Contains(fullCases, engine.name+"/"+caseName) {
					checkFullCase(t, key, output)
				}
			}
		}
	}

	manifestPath := filepath.Join(compatDirectory, compatManifest)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	if *updateCompat {
		require.NoError(t, os.MkdirAll(compatDirectory, 0o755))
		require.NoError(t, os.WriteFile(manifestPath, encoded, 0o644))
		return
	}

	expected, err := os.ReadFile(manifestPath)
	require.NoError(t, err, "missing compatibility manifest; run with -update-piko-compat")

	var expectedManifest map[string]compatRecord
	require.NoError(t, json.Unmarshal(expected, &expectedManifest))
	for key, record := range expectedManifest {
		record.Files = withoutEmptyFiles(record.Files)
		actual, ok := manifest[key]
		require.Truef(t, ok, "compatibility case %s is no longer generated", key)
		require.Equalf(t, record, actual, "compatibility case %s changed its output", key)
	}
	require.Len(t, manifest, len(expectedManifest), "the set of compatibility cases changed")
}

func engineCaseDirectories(t *testing.T, engineName string) []string {
	t.Helper()

	root := filepath.Join(compatDirectory, compatInputsDirectory, engineName)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		if _, statErr := os.Stat(filepath.Join(directory, "migrations")); statErr != nil {
			continue
		}
		directories = append(directories, directory)
	}
	return directories
}

func generateCompatCase(t *testing.T, engine compatEngine, emitter compatEmitter, caseDirectory string) compatOutput {
	t.Helper()

	specification := loadCompatSpec(t, caseDirectory)
	engineInstance := engine.newEngine()

	generator, err := app.New(engineInstance,
		app.WithEmitter(emitter.newEmitter(engineInstance)),
		app.WithPreset(pikopreset.Preset()),
		app.WithTypeOverrides(pikopreset.TypeOverrides(new(engineInstance.Info()))...),
		app.WithCustomFunctions(specification.CustomFunctions...),
	)
	require.NoError(t, err)

	result, generateErr := generator.Generate(t.Context(), app.Input{
		Package:      "generated",
		Migrations:   os.DirFS(caseDirectory),
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})

	output := compatOutput{files: make(map[string][]byte)}
	if generateErr != nil {
		output.err = relativiseError(generateErr.Error(), caseDirectory)
		return output
	}

	for _, file := range result.Files {
		output.files[file.Name] = file.Content
	}
	output.diagnostics = renderDiagnostics(t, result.Diagnostics, caseDirectory)
	return output
}

func relativiseError(message, caseDirectory string) string {
	absolute, err := filepath.Abs(caseDirectory)
	if err != nil {
		return message
	}
	return strings.ReplaceAll(message, absolute+string(filepath.Separator), "")
}

func renderDiagnostics(t *testing.T, diagnostics []diag.SourceError, caseDirectory string) []byte {
	t.Helper()

	if len(diagnostics) == 0 {
		return nil
	}
	absolute, err := filepath.Abs(caseDirectory)
	require.NoError(t, err)

	records := make([]compatDiagnostic, len(diagnostics))
	for index, diagnostic := range diagnostics {
		records[index] = compatDiagnostic{Filename: diagnostic.Filename, Message: diagnostic.Message, Code: diagnostic.Code,
			Suggestion: diagnostic.Suggestion, Line: diagnostic.Line, Column: diagnostic.Column, EndLine: diagnostic.EndLine,
			EndColumn: diagnostic.EndColumn, Severity: uint8(diagnostic.Severity)}
	}
	encoded, err := json.MarshalIndent(records, "", "  ")
	require.NoError(t, err)
	return []byte(strings.ReplaceAll(string(encoded), absolute+string(filepath.Separator), ""))
}

func recordOf(output compatOutput) compatRecord {
	record := compatRecord{Error: output.err}
	if len(output.files) > 0 {
		record.Files = make(map[string]string, len(output.files))
		for name, content := range output.files {
			record.Files[name] = digest(content)
		}
	}
	if len(output.diagnostics) > 0 {
		record.Diagnostics = digest(output.diagnostics)
	}
	return record
}

func withoutEmptyFiles(files map[string]string) map[string]string {
	emptyDigest := digest(nil)
	for name, fileDigest := range files {
		if fileDigest == emptyDigest {
			delete(files, name)
		}
	}
	if len(files) == 0 {
		return nil
	}
	return files
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func checkFullCase(t *testing.T, key string, output compatOutput) {
	t.Helper()

	directory := filepath.Join(compatDirectory, compatFullDirectory, filepath.FromSlash(key))
	expected := make(map[string][]byte, len(output.files)+2)
	for name, content := range output.files {
		expected[name+".golden"] = content
	}
	if len(output.diagnostics) > 0 {
		expected["diagnostics.json"] = output.diagnostics
	}
	if output.err != "" {
		expected["error.txt"] = []byte(output.err + "\n")
	}

	if *updateCompat {
		require.NoError(t, os.RemoveAll(directory))
		require.NoError(t, os.MkdirAll(directory, 0o755))
		for name, content := range expected {
			require.NoError(t, os.WriteFile(filepath.Join(directory, name), content, 0o644))
		}
		return
	}

	entries, err := os.ReadDir(directory)
	require.NoErrorf(t, err, "missing full compatibility case %s; run with -update-piko-compat", key)
	recordedNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		require.NoError(t, infoErr)
		if info.Size() > 0 {
			recordedNames = append(recordedNames, entry.Name())
		}
	}
	require.Lenf(t, recordedNames, len(expected), "full compatibility case %s has a different file set", key)
	for name, content := range expected {
		recorded, readErr := os.ReadFile(filepath.Join(directory, name))
		require.NoErrorf(t, readErr, "full compatibility case %s is missing %s", key, name)
		require.Equalf(t, string(recorded), string(content), "full compatibility case %s changed %s", key, name)
	}
}

func loadFullCaseList(t *testing.T) []string {
	t.Helper()

	file, err := os.Open(filepath.Join(compatDirectory, compatFullList))
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	var cases []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cases = append(cases, line)
	}
	require.NoError(t, scanner.Err())
	return cases
}

func loadCompatSpec(t *testing.T, caseDirectory string) compatSpec {
	t.Helper()

	var specification compatSpec
	data, err := os.ReadFile(filepath.Join(caseDirectory, "testspec.json"))
	if os.IsNotExist(err) {
		return specification
	}
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &specification))
	return specification
}
