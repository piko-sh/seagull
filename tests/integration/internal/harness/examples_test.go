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

package harness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

const exampleTest = `package demo

import (
	"os"
	"testing"
)

func TestTheExampleRunsAgainstTheSuitesServer(t *testing.T) {
	for variable, want := range map[string]string{
		"SGL_EXAMPLE_DSN":         "fake://server/postgres",
		"SGL_EXAMPLE_ENGINE":      "cockroachdb",
		"SGL_EXAMPLE_REQUIRE_DSN": "1",
	} {
		if got := os.Getenv(variable); got != want {
			t.Fatalf("%s = %q, want %q", variable, got, want)
		}
	}
}
`

func TestRunExamplesTestsTheExamplesModuleAgainstTheSuitesServer(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in an examples module")
	}
	root := t.TempDir()
	suiteDirectory := filepath.Join(root, "tests", "integration", "postgres")
	module := filepath.Join(root, "examples", "demo")
	require.NoError(t, os.MkdirAll(suiteDirectory, 0o750))
	require.NoError(t, os.MkdirAll(module, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(module, "go.mod"), []byte("module demo\n\ngo 1.27.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(module, "demo_test.go"), []byte(exampleTest), 0o600))
	t.Chdir(suiteDirectory)
	t.Setenv("GOWORK", "off")

	tests := []struct {
		name     string
		engine   string
		packages []string
	}{
		{name: "every package of the module", engine: "cockroachdb", packages: []string{"./..."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, filepath.Join(root, "examples"), filepath.Join(suiteDirectory, harness.ExamplesDirectory()))
			harness.RunExamples(t, "fake://server/postgres", "demo", test.engine, test.packages...)
		})
	}
}
