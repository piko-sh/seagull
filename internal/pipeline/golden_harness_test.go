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

package pipeline_test

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/pipeline"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

var (
	updateGoldenFiles = flag.Bool("update", false, "Update golden files")
)

type testSpec struct {
	Description             string               `json:"description"`
	Engine                  string               `json:"engine"`
	ShouldError             bool                 `json:"shouldError"`
	ExpectedQueryCount      *int                 `json:"expectedQueryCount"`
	ExpectedDiagnosticCount *int                 `json:"expectedDiagnosticCount"`
	Diagnostics             []expectedDiagnostic `json:"diagnostics"`
}

type caseRequest struct {
	files fs.FS
}

func (r caseRequest) MigrationSource() (fs.FS, string) {
	return r.files, "migrations"
}

func (r caseRequest) QuerySource() (fs.FS, string) {
	return nil, "queries"
}

func (caseRequest) PackageName() string {
	return "test"
}

func (caseRequest) PackageImportPath() string {
	return ""
}

type expectedDiagnostic struct {
	Severity        string `json:"severity"`
	Code            string `json:"code"`
	MessageContains string `json:"messageContains"`
	Filename        string `json:"filename"`
	OnLine          *int   `json:"onLine"`
}

func runTestCase(t *testing.T, testCaseDirectory string) {
	t.Helper()
	ctx := t.Context()

	spec := loadTestSpec(t, testCaseDirectory)
	engineConfig := loadEngineConfig(t, testCaseDirectory)

	engine := newMockEngine(engineConfig)
	emitter := &recordingCodeEmitter{}

	generator, err := pipeline.New(pipeline.Config{Dialect: engine, Emitter: emitter})
	require.NoError(t, err)

	generation, generateError := generator.Generate(ctx, caseRequest{files: os.DirFS(testCaseDirectory)})
	diagnostics := generation.Diagnostics

	if spec.ShouldError {
		assert.Error(t, generateError, "expected Generate to return an error")
		return
	}
	require.NoError(t, generateError)

	catalogue := engine.recordedCatalogue
	if catalogue == nil && emitter.catalogue != nil {
		catalogue = emitter.catalogue
	}

	goldenDirectory := filepath.Join(testCaseDirectory, "golden")

	assertOutputGolden(t, filepath.Join(goldenDirectory, "catalogue.json"), catalogue, catalogue != nil)
	assertOutputGolden(t, filepath.Join(goldenDirectory, "queries.json"), emitter.queries, len(emitter.queries) > 0)
	assertOutputGolden(t, filepath.Join(goldenDirectory, "diagnostics.json"), diagnostics,
		len(diagnostics) > 0)

	if spec.ExpectedQueryCount != nil {
		assert.Equal(t, *spec.ExpectedQueryCount, len(emitter.queries),
			"unexpected query count")
	}

	if spec.ExpectedDiagnosticCount != nil {
		assert.Equal(t, *spec.ExpectedDiagnosticCount, len(diagnostics),
			"unexpected diagnostic count")
	}

	for _, expected := range spec.Diagnostics {
		found := false
		for _, actual := range diagnostics {
			if matchesDiagnostic(actual, expected) {
				found = true
				break
			}
		}
		assert.True(t, found,
			"expected diagnostic not found: severity=%s code=%s message containing %q",
			expected.Severity, expected.Code, expected.MessageContains)
	}
}

func loadTestSpec(t *testing.T, testCaseDirectory string) testSpec {
	t.Helper()
	specPath := filepath.Join(testCaseDirectory, "testspec.json")
	specBytes, err := os.ReadFile(specPath)
	require.NoError(t, err, "failed to read testspec.json")

	var spec testSpec
	require.NoError(t, json.Unmarshal(specBytes, &spec), "failed to parse testspec.json")
	return spec
}

func loadEngineConfig(t *testing.T, testCaseDirectory string) mockEngineConfig {
	t.Helper()
	enginePath := filepath.Join(testCaseDirectory, "engine.json")
	engineBytes, err := os.ReadFile(enginePath)
	require.NoError(t, err, "failed to read engine.json")

	var config mockEngineConfig
	require.NoError(t, json.Unmarshal(engineBytes, &config), "failed to parse engine.json")
	return config
}

func assertOutputGolden(t *testing.T, goldenPath string, value any, hasData bool) {
	t.Helper()
	if !hasData && !goldenFileExists(goldenPath) {
		return
	}
	actual := serialiseDeterministic(t, value)
	assertGoldenJSON(t, goldenPath, actual)
}

func goldenFileExists(goldenPath string) bool {
	_, statError := os.Stat(goldenPath)
	return statError == nil
}

func assertGoldenJSON(t *testing.T, goldenPath string, actual []byte) {
	t.Helper()

	if *updateGoldenFiles {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, actual, 0o644))
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	expectedBytes, err := os.ReadFile(goldenPath)
	require.NotErrorIs(t, err, fs.ErrNotExist, "golden file not found at %s (run with -update to generate)", goldenPath)
	require.NoError(t, err)

	assert.JSONEq(t, string(expectedBytes), string(actual),
		"golden file mismatch: %s", goldenPath)
}

func serialiseDeterministic(t *testing.T, value any) []byte {
	t.Helper()
	result, err := json.MarshalIndent(goldenjson.Project(value), "", "  ")
	require.NoError(t, err, "failed to serialise to JSON")
	return append(result, '\n')
}

func matchesDiagnostic(actual diag.SourceError, expected expectedDiagnostic) bool {
	if expected.Code != "" && actual.Code != expected.Code {
		return false
	}
	if expected.Severity != "" && !matchesSeverity(actual.Severity, expected.Severity) {
		return false
	}
	if expected.MessageContains != "" && !strings.Contains(actual.Message, expected.MessageContains) {
		return false
	}
	if expected.Filename != "" && !strings.HasSuffix(actual.Filename, expected.Filename) {
		return false
	}
	if expected.OnLine != nil && actual.Line != *expected.OnLine {
		return false
	}
	return true
}

func matchesSeverity(actual diag.ErrorSeverity, expected string) bool {
	switch strings.ToLower(expected) {
	case "error":
		return actual == diag.SeverityError
	case "warning":
		return actual == diag.SeverityWarning
	case "hint":
		return actual == diag.SeverityHint
	default:
		return false
	}
}
