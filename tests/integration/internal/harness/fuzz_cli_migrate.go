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
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcli"
)

const (
	// cliCommandDeadline bounds one sgl command of the migrate workflow.
	cliCommandDeadline = time.Minute

	// cliBuildDeadline bounds building the sgl binary.
	cliBuildDeadline = 10 * time.Minute

	// cliDirectoryMode is the mode of a project directory the workflow writes.
	cliDirectoryMode = 0o750

	// cliFileMode is the mode of a project file the workflow writes.
	cliFileMode = 0o600
)

// errCLIDeadline is the cause of an sgl build's or command's context once its time limit
// has passed.
var errCLIDeadline = errors.New("the sgl command did not finish in time")

// cliTableQueries reads the names of the model's tables, m followed by a number, in the
// pool's own schema or database, per engine.
var cliTableQueries = map[string]string{
	enginePostgres:    `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name ~ '^m[0-9]+$'`,
	engineCockroachDB: `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name ~ '^m[0-9]+$'`,
	engineTimescaleDB: `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name ~ '^m[0-9]+$'`,
	engineMySQL:       `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name REGEXP '^m[0-9]+$'`,
	engineMariaDB:     `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name REGEXP '^m[0-9]+$'`,
	engineClickHouse:  `SELECT name FROM system.tables WHERE database = currentDatabase() AND match(name, '^m[0-9]+$')`,
}

// cliWorkspace runs the model's commands through the built binary in a project directory.
type cliWorkspace struct {
	// t owns the project.
	t *testing.T

	// binary is the built sgl.
	binary string

	// directory is the project's root.
	directory string

	// connection is the isolated connection string.
	connection string
}

// Write replaces a project file.
//
// Takes name (string) which is the file, relative to the project.
// Takes content (string) which is its new content.
func (workspace *cliWorkspace) Write(name, content string) {
	workspace.t.Helper()
	path := filepath.Join(workspace.directory, name)
	require.NoError(workspace.t, os.MkdirAll(filepath.Dir(path), cliDirectoryMode))
	require.NoError(workspace.t, os.WriteFile(path, []byte(content), cliFileMode))
}

// Read returns a project file's content.
//
// Takes name (string) which is the file, relative to the project.
//
// Returns string which is its content.
func (workspace *cliWorkspace) Read(name string) string {
	workspace.t.Helper()
	content, err := os.ReadFile(filepath.Join(workspace.directory, name)) //nolint:gosec // a file of the test's own project
	require.NoError(workspace.t, err)
	return string(content)
}

// Remove deletes a project file, which need not exist.
//
// Takes name (string) which is the file, relative to the project.
func (workspace *cliWorkspace) Remove(name string) {
	workspace.t.Helper()
	if err := os.Remove(filepath.Join(workspace.directory, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		require.NoError(workspace.t, err)
	}
}

// Run runs sgl in the project against the isolated database.
//
// Takes arguments (...string) which are the command and its arguments.
//
// Returns fuzzcli.Result which is what the command gave.
func (workspace *cliWorkspace) Run(arguments ...string) fuzzcli.Result {
	workspace.t.Helper()
	return workspace.run(append(arguments, "--dsn", workspace.connection)...)
}

// run runs sgl in the project with exactly the arguments given.
//
// Takes arguments (...string) which are the command line.
//
// Returns fuzzcli.Result which is what the command gave.
func (workspace *cliWorkspace) run(arguments ...string) fuzzcli.Result {
	workspace.t.Helper()
	ctx, cancel := context.WithTimeoutCause(workspace.t.Context(), scaled(cliCommandDeadline), errCLIDeadline)
	defer cancel()
	command := exec.CommandContext(ctx, workspace.binary, arguments...) //nolint:gosec // the binary this test built
	command.Dir = workspace.directory
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if exitErr, exited := errors.AsType[*exec.ExitError](err); exited {
		code = exitErr.ExitCode()
	} else {
		require.NoError(workspace.t, err, "running sgl %v", arguments)
	}
	return fuzzcli.Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// FuzzCLIMigrate runs the sgl command line's migrate and seed commands, through a binary
// built from cmd/sgl, on an isolated schema or database of the suite's server, and checks
// each against fuzzcli's independent model, as cmd/sgl's own FuzzCLIMigrate does on
// SQLite.
//
// The stock binary links no DuckDB driver and the SQLite suites are the command's own
// lane, so only the server engines run it.
//
// Takes f (*testing.F) which receives the seeds.
// Takes suite (NativeFuzzSuite) which supplies the isolated connections.
func FuzzCLIMigrate(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	query, supported := cliTableQueries[suite.Engine]
	require.True(f, supported && suite.OpenNative != nil, "the sgl migrate workflow has no lane for %q", suite.Engine)
	for _, seed := range fuzzcli.MigrateSeeds() {
		f.Add(seed)
	}
	binary := buildSGL(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		database, connection := suite.OpenNative(t)
		workspace := &cliWorkspace{t: t, binary: binary, directory: t.TempDir(), connection: connection}
		result := workspace.run("init", "--dialect", suite.Engine)
		require.Zero(t, result.Code, "sgl init failed: %s", result.Stderr)
		fuzzcli.RunMigrate(t, data, workspace, fuzzcli.DialectFor(suite.Engine), func() fuzzcli.Observation {
			return observeCLITables(t, database, query)
		})
	})
}

// buildSGL builds the sgl binary from the repository's cmd/sgl module into the fuzz
// test's temporary directory, once per process.
//
// Takes f (*testing.F) which owns the binary.
//
// Returns string which is the binary's path.
func buildSGL(f *testing.F) string {
	f.Helper()
	module, found := findCLIModule()
	require.True(f, found, "cmd/sgl is not above the working directory")
	binary := filepath.Join(f.TempDir(), "sgl")
	ctx, cancel := context.WithTimeoutCause(f.Context(), scaled(cliBuildDeadline), errCLIDeadline)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".") //nolint:gosec // builds the repository's own command
	build.Dir = module
	output, err := build.CombinedOutput()
	require.NoError(f, err, "building sgl\n%s", output)
	return binary
}

// findCLIModule finds the cmd/sgl module from the working directory upwards.
//
// Returns string which is the module's directory.
// Returns bool which is false when there is none.
func findCLIModule() (string, bool) {
	directory, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		module := filepath.Join(directory, "cmd", "sgl")
		if _, statErr := os.Stat(filepath.Join(module, "go.mod")); statErr == nil {
			return module, true
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", false
		}
		directory = parent
	}
}

// observeCLITables reads the model's tables and the seed's rows.
//
// Takes database (*sql.DB) which is the isolated pool.
// Takes query (string) which reads the table names.
//
// Returns fuzzcli.Observation which is the state.
func observeCLITables(t *testing.T, database *sql.DB, query string) fuzzcli.Observation {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	require.NoError(t, err, "reading the tables")
	observed := fuzzcli.Observation{Tables: nil, SeedRows: 0}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		observed.Tables = append(observed.Tables, name)
	}
	require.NoError(t, errors.Join(rows.Err(), rows.Close()), "reading the tables")
	if slices.Contains(observed.Tables, "m1") {
		require.NoError(t, database.QueryRowContext(t.Context(), "SELECT count(*) FROM m1").Scan(&observed.SeedRows), "counting the seed's rows")
	}
	return observed
}
