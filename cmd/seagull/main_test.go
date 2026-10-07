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
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

var stockProgramName = regexp.MustCompile("(?m)(^|[\\s\"`])sgl([\\s:`\"]|$)")

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
	os.Args = append([]string{"seagull"}, arguments...)
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

func TestVersionNamesTheProgramSeagull(t *testing.T) {
	run := runMain(t, "version")

	require.Zero(t, run.status, run.stderr)
	assert.Regexp(t, `^seagull {8}\S`, run.stdout)
	for _, label := range []string{"\nlibrary ", "\ncodegen ", "\nconfig schema ", "\ngo "} {
		assert.Contains(t, run.stdout, label)
	}
	assert.NotRegexp(t, stockProgramName, run.stdout)
}

func TestHelpNamesTheProgramSeagull(t *testing.T) {
	run := runMain(t, "help")

	require.Zero(t, run.status, run.stderr)
	assert.True(t, strings.HasPrefix(run.stdout, "seagull turns plain SQL into type-safe Go.\n"), run.stdout)
	assert.Contains(t, run.stdout, "\n  seagull [global flags] <command> [flags] [args]\n")
	assert.Contains(t, run.stdout, `Run "seagull <command> -h" for a command's flags.`)
	assert.NotRegexp(t, stockProgramName, run.stdout)
}

func TestAnUnknownCommandNamesTheProgramSeagull(t *testing.T) {
	run := runMain(t, "no-such-command")

	assert.Equal(t, 2, run.status)
	assert.Equal(t, "seagull: unknown command \"no-such-command\"; run `seagull help`\n", run.stderr)
}

func TestDialectsListTheSameDriversAsSgl(t *testing.T) {
	if testing.Short() {
		t.Skip("building sgl runs the go command; skipped under -short")
	}

	workspace, err := filepath.Abs(filepath.Join("..", "..", "go.work"))
	require.NoError(t, err)
	stock, err := testkit.GoCommand(t, filepath.Join("..", "sgl"), []string{"GOWORK=" + workspace}, "run", ".", "dialects").Output()
	require.NoError(t, err)

	run := runMain(t, "dialects")

	require.Zero(t, run.status, run.stderr)
	assert.Equal(t, string(stock), run.stdout, "seagull registers a different driver from sgl for some dialect")
}
