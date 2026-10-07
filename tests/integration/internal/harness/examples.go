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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

const (
	// examplesDirectory is the repository's examples directory, from a suite's module.
	examplesDirectory = "../../../examples"

	// workspaceVariable selects the go.work file a go command reads.
	workspaceVariable = "GOWORK"
)

// RunExamples runs go test on packages of one examples module, pointed at a suite's
// server, and fails with go test's output when any package fails. A variant that finds no
// server fails rather than skips. The go command reads the workspace the suite runs in,
// as make test-examples does, so the examples build against this checkout.
//
// Takes connectionString (string) which is the server's connection string.
// Takes module (string) which is the examples module, such as postgres.
// Takes engine (string) which names the engine the variants run against.
// Takes packages (...string) which are the packages to test, such as ./....
func RunExamples(t *testing.T, connectionString, module, engine string, packages ...string) {
	t.Helper()

	arguments := append([]string{"test", "-count=1"}, packages...)
	command := testkit.GoCommand(t, filepath.Join(examplesDirectory, module), []string{
		workspaceVariable + "=" + os.Getenv(workspaceVariable),
		"SGL_EXAMPLE_DSN=" + connectionString,
		"SGL_EXAMPLE_ENGINE=" + engine,
		"SGL_EXAMPLE_REQUIRE_DSN=1",
	}, arguments...)
	command.WaitDelay = commandWaitDelay

	output, err := command.CombinedOutput()
	t.Logf("go %s (examples/%s, SGL_EXAMPLE_ENGINE=%s)\n%s", strings.Join(arguments, " "), module, engine, output)
	require.NoError(t, err, "examples/%s failed against %s", module, engine)
}

// ExamplesDirectory returns the repository's examples directory, from a suite's module.
//
// Returns string which is the directory.
func ExamplesDirectory() string {
	return examplesDirectory
}
