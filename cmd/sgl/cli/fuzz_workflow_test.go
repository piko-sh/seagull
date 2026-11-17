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

package cli_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	cliWorkflowOperations   = 12
	cliWorkflowOutput       = "internal/store"
	cliWorkflowQueries      = "queries/read.sql"
	cliWorkflowPayloadBytes = 64
)

var cliWorkflowEngines = []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"}

type cliFileObservation struct {
	content  string
	modified time.Time
	mode     fs.FileMode
}

type cliFailingWriter struct{}

func (cliFailingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func FuzzCLIWorkflow(f *testing.F) {
	for engine := range len(cliWorkflowEngines) {
		for operation := range cliWorkflowOperations {
			for _, choices := range []byte{0, 1, 2} {
				f.Add([]byte{fuzzcase.InvalidVersion, byte(engine), byte(operation), choices})
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 || len(data) > fuzzcase.ScenarioMaxBytes || data[0] != fuzzcase.InvalidVersion {
			t.Skip("unsupported CLI workflow envelope")
		}
		engine := cliWorkflowEngines[int(data[1])%len(cliWorkflowEngines)]
		var choices byte
		if len(data) > 3 {
			choices = data[3]
		}
		project := cliWorkflowProject(t, engine, choices)
		requireCode(t, project.Run("generate"), 0)
		requireCode(t, project.Run("generate", "--check"), 0)
		before := cliSnapshot(t, project)
		requireCode(t, project.Run("generate"), 0)
		require.Equal(t, before, cliSnapshot(t, project), "idempotent generation changed content or modification times")
		payload := hex.EncodeToString(data[3:min(len(data), 3+cliWorkflowPayloadBytes)])
		project.Write(cliWorkflowOutput+"/handwritten.go", "package store\n// User text "+payload+".\n")
		project.Write(cliWorkflowQueries, fmt.Sprintf("-- sgl.query(ReadBoats, many)\nSELECT id, label FROM boats WHERE id >= %d ORDER BY id DESC;\n-- %s\n", int(choices)+1, payload))
		operations := []func(*testing.T, *clitest.Project){cliReadOnlyWorkflow, cliInvalidConfiguration, cliInvalidMigration, cliPartialQueries,
			cliMissingQueries, cliDirectoryCollision, cliCancelledGeneration, cliStaleFiles, cliNearHeader, cliUnwritableOutput, cliSymlinkOutput, cliBrokenOutput}
		operations[int(data[2])%len(operations)](t, project)
		require.Contains(t, project.Read(cliWorkflowOutput+"/handwritten.go"), "User text "+payload+".")
	})
}

func cliWorkflowProject(t *testing.T, engine string, choices byte) *clitest.Project {
	t.Helper()
	project := clitest.NewProject(t)
	project.Write("sgl.yaml", fmt.Sprintf("version: 1\ndatabases:\n  - name: app\n    dialect: %s\n    migrations: migrations\n    queries: queries\n    go:\n      package: store\n      out: internal/store\n", engine))
	settings := ""
	if choices&1 != 0 && fuzzcase.RuntimeNative(engine) {
		driver := "pgx"
		if engine == "clickhouse" {
			driver = "clickhouse"
		}
		settings = "      driver: " + driver + "\n"
	} else if choices&2 != 0 && engine != "clickhouse" {
		settings = "      prepared: true\n"
	}
	project.Write("sgl.yaml", project.Read("sgl.yaml")+settings)
	schema := "CREATE TABLE boats (id BIGINT NOT NULL, label TEXT NOT NULL);\n"
	if engine == "clickhouse" {
		schema = "CREATE TABLE boats (id Int64, label String) ENGINE = MergeTree ORDER BY id;\n"
	}
	project.Write("migrations/0001_boats.up.sql", schema)
	project.Write(cliWorkflowQueries, "-- sgl.query(ReadBoats, many)\nSELECT id, label FROM boats ORDER BY id;\n")
	return project
}

func cliSnapshot(t *testing.T, project *clitest.Project) map[string]cliFileObservation {
	t.Helper()
	root, err := os.OpenRoot(project.Path(cliWorkflowOutput))
	require.NoError(t, err)
	defer func() { require.NoError(t, root.Close()) }()
	observed := make(map[string]cliFileObservation)
	require.NoError(t, fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := root.Readlink(name)
			if err != nil {
				return err
			}
			observed[name] = cliFileObservation{content: target, mode: info.Mode(), modified: info.ModTime()}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular output %s", name)
		}
		content, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		observed[name] = cliFileObservation{content: string(content), mode: info.Mode(), modified: info.ModTime()}
		return nil
	}))
	return observed
}

func cliReadOnlyWorkflow(t *testing.T, project *clitest.Project) {
	t.Helper()
	before := cliSnapshot(t, project)
	for _, arguments := range [][]string{{"generate", "--check"}, {"generate", "--dry-run"}, {"generate", "--diff"}, {"generate", "--check", "--diff"}} {
		result := project.Run(arguments...)
		want := 0
		if strings.Contains(strings.Join(arguments, " "), "--check") {
			want = 1
		}
		requireCode(t, result, want)
		require.Equal(t, before, cliSnapshot(t, project), "read-only operation changed generated files")
	}
	requireCode(t, project.Run("generate"), 0)
	requireCode(t, project.Run("generate", "--check"), 0)
	cliParseGenerated(t, project)
}

func cliInvalidConfiguration(t *testing.T, project *clitest.Project) {
	t.Helper()
	project.Write("sgl.yaml", project.Read("sgl.yaml")+"      driver: unknown-driver\n")
	cliRejectedGeneration(t, project, 2)
}

func cliInvalidMigration(t *testing.T, project *clitest.Project) {
	t.Helper()
	project.Write("migrations/0002_broken.up.sql", "CREATE TABLE ;\n")
	cliRejectedGeneration(t, project, 1)
}

func cliPartialQueries(t *testing.T, project *clitest.Project) {
	t.Helper()
	project.Write("queries/broken.sql", "-- sgl.query(Broken, many)\nSELECT id FROM boats WHERE label = 'unterminated;\n")
	requireCode(t, project.Run("generate"), 1)
	require.False(t, project.Exists(cliWorkflowOutput+"/broken.sql.go"))
	require.Contains(t, project.Read(cliWorkflowOutput+"/read.sql.go"), "ORDER BY id DESC")
	before := cliSnapshot(t, project)
	requireCode(t, project.Run("generate", "--check"), 1)
	require.Equal(t, before, cliSnapshot(t, project))
	cliParseGenerated(t, project)
}

func cliMissingQueries(t *testing.T, project *clitest.Project) {
	t.Helper()
	project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "queries: queries", "queries: missing", 1))
	cliRejectedGeneration(t, project, 2)
}

func cliDirectoryCollision(t *testing.T, project *clitest.Project) {
	t.Helper()
	name := project.Path(cliWorkflowOutput + "/read.sql.go")
	require.NoError(t, os.Remove(name))
	require.NoError(t, os.Mkdir(name, 0o700))
	cliRejectedGeneration(t, project, 2)
	info, err := os.Stat(name)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func cliCancelledGeneration(t *testing.T, project *clitest.Project) {
	t.Helper()
	before := cliSnapshot(t, project)
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("the workflow cancels generation before it starts"))
	requireCode(t, project.RunWith(ctx, nil, "generate"), 130)
	require.Equal(t, before, cliSnapshot(t, project))
}

func cliStaleFiles(t *testing.T, project *clitest.Project) {
	t.Helper()
	project.Write(cliWorkflowOutput+"/stale.go", "// Code generated by sgl. DO NOT EDIT.\npackage store\n")
	cliReadOnlyWorkflow(t, project)
	require.False(t, project.Exists(cliWorkflowOutput+"/stale.go"))
}

func cliNearHeader(t *testing.T, project *clitest.Project) {
	t.Helper()
	name := cliWorkflowOutput + "/near.go"
	original := "// Code generated by sgl. DO NOT EDIT. Additional user text.\npackage store\n"
	project.Write(name, original)
	cliReadOnlyWorkflow(t, project)
	require.Equal(t, original, project.Read(name))
}

func cliRejectedGeneration(t *testing.T, project *clitest.Project, code int) {
	t.Helper()
	before := cliSnapshot(t, project)
	result := project.Run("generate")
	requireCode(t, result, code)
	require.NotContains(t, result.Stderr, "recovered a panic")
	require.Equal(t, before, cliSnapshot(t, project), "rejected generation changed existing output")
}

func cliParseGenerated(t *testing.T, project *clitest.Project) {
	t.Helper()
	for name, file := range cliSnapshot(t, project) {
		if filepath.Ext(name) != ".go" {
			continue
		}
		_, err := parser.ParseFile(token.NewFileSet(), name, file.content, parser.AllErrors)
		require.NoError(t, err, "generated output was not parseable Go")
	}
}

func cliUnwritableOutput(t *testing.T, project *clitest.Project) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("permission faults require an unprivileged filesystem user")
	}
	directory := project.Path(cliWorkflowOutput)
	require.NoError(t, os.Chmod(directory, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(directory, 0o700)) })
	cliRejectedGeneration(t, project, 3)
}

func cliSymlinkOutput(t *testing.T, project *clitest.Project) {
	t.Helper()
	target := "outside.go"
	content := "package protected\n"
	project.Write(target, content)
	link := project.Path(cliWorkflowOutput + "/read.sql.go")
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(project.Path(target), link))
	cliRejectedGeneration(t, project, 3)
	require.Equal(t, content, project.Read(target))
}

func cliBrokenOutput(t *testing.T, project *clitest.Project) {
	t.Helper()
	before := cliSnapshot(t, project)
	for _, mode := range []string{"--diff", "--dry-run"} {
		var diagnostics bytes.Buffer
		result := project.RunWith(t.Context(), []cli.Option{cli.WithIO(nil, cliFailingWriter{}, &diagnostics)}, "generate", mode)
		require.Equal(t, 3, result.Code, "failed output writer was ignored for %s: %s", mode, diagnostics.String())
		require.Equal(t, before, cliSnapshot(t, project))
	}
	result := project.RunWith(t.Context(), []cli.Option{cli.WithIO(nil, io.Discard, cliFailingWriter{})}, "generate", "--dry-run", "--timings")
	require.Equal(t, 3, result.Code, "failed diagnostic writer was ignored")
	require.Equal(t, before, cliSnapshot(t, project))
}
