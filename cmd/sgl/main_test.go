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

package main

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"seagull.sh/seagull/internal/testkit"
)

var stockDriverPackages = []string{
	"github.com/ClickHouse/clickhouse-go/v2",
	"github.com/go-sql-driver/mysql",
	"github.com/jackc/pgx/v5/stdlib",
	"modernc.org/sqlite",
}

type mainRun struct {
	stdout string
	stderr string
	status int
}

func runMain(t *testing.T, arguments ...string) mainRun {
	t.Helper()

	stdoutReader, stdoutWriter, err := os.Pipe()
	require.NoError(t, err)
	stderrReader, stderrWriter, err := os.Pipe()
	require.NoError(t, err)
	commandLine, processExit, stdout, stderr := os.Args, exit, os.Stdout, os.Stderr
	t.Cleanup(func() { os.Args, exit, os.Stdout, os.Stderr = commandLine, processExit, stdout, stderr })
	var statuses []int
	os.Args = append([]string{"sgl"}, arguments...)
	exit = func(status int) { statuses = append(statuses, status) }
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter

	main()

	os.Stdout, os.Stderr = stdout, stderr
	require.NoError(t, stdoutWriter.Close())
	require.NoError(t, stderrWriter.Close())
	printed, err := io.ReadAll(stdoutReader)
	require.NoError(t, err)
	warned, err := io.ReadAll(stderrReader)
	require.NoError(t, err)
	require.NoError(t, stdoutReader.Close())
	require.NoError(t, stderrReader.Close())
	require.Len(t, statuses, 1)
	return mainRun{stdout: string(printed), stderr: string(warned), status: statuses[0]}
}

func TestMainExitsWithTheStatusOfTheCommand(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      int
	}{
		{name: "a command that succeeds", arguments: []string{"version"}, want: 0},
		{name: "an unknown command", arguments: []string{"no-such-command"}, want: 2},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, runMain(t, testCase.arguments...).status)
		})
	}
}

func TestMainNamesTheProgramSgl(t *testing.T) {
	version := runMain(t, "version")
	require.Zero(t, version.status, version.stderr)
	assert.Regexp(t, `^sgl {12}\S`, version.stdout)

	help := runMain(t, "help")
	require.Zero(t, help.status, help.stderr)
	assert.True(t, strings.HasPrefix(help.stdout, "sgl turns plain SQL into type-safe Go.\n"), help.stdout)
	assert.Contains(t, help.stdout, "\n  sgl [global flags] <command> [flags] [args]\n")

	unknown := runMain(t, "no-such-command")
	assert.Equal(t, "sgl: unknown command \"no-such-command\"; run `sgl help`\n", unknown.stderr)
}

func dependenciesOf(t *testing.T, pattern string) []string {
	t.Helper()

	loaded, err := packages.Load(&packages.Config{
		Mode:    packages.NeedName | packages.NeedImports | packages.NeedDeps,
		Context: t.Context(),
		Dir:     ".",
		Env:     testkit.ToolchainEnvironment(),
		Tests:   false,
	}, pattern)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	var dependencies []string
	packages.Visit(loaded, nil, func(visited *packages.Package) {
		dependencies = append(dependencies, visited.PkgPath)
	})
	return dependencies
}

func TestOnlyMainAndTheDriversPackageLinkTheStockDrivers(t *testing.T) {
	if testing.Short() {
		t.Skip("loading the dependency graph runs go list; skipped under -short")
	}

	tests := []struct {
		pattern    string
		wantLinked bool
	}{
		{pattern: "seagull.sh/seagull/cmd/internal/assemble", wantLinked: false},
		{pattern: "./cli", wantLinked: false},
		{pattern: "seagull.sh/seagull/cmd/internal/config", wantLinked: false},
		{pattern: "seagull.sh/seagull/cmd/internal/starter", wantLinked: false},
		{pattern: "./internal/drivers", wantLinked: true},
		{pattern: ".", wantLinked: true},
	}
	for _, testCase := range tests {
		dependencies := dependenciesOf(t, testCase.pattern)
		for _, driver := range stockDriverPackages {
			require.Equal(t, testCase.wantLinked, slices.Contains(dependencies, driver), "%s links %s", testCase.pattern, driver)
		}
	}
}
