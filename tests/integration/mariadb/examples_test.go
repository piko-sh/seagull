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

package mariadb_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/examples/catalogue"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestMariaDBExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("examples/mariadb", func(t *testing.T) {
		harness.RunExamples(t, testConnectionString, "mariadb", "mariadb", "./...")
	})
	t.Run("mysql twins", func(t *testing.T) {
		runTwinExamples(t, "mariadb")
	})
}

func runTwinExamples(t *testing.T, engine string) {
	t.Helper()

	curriculum, err := catalogue.Load(harness.ExamplesDirectory())
	require.NoError(t, err)

	var modules []string
	packages := map[string][]string{}
	for _, cell := range curriculum.EquivalentsFor(engine) {
		if _, seen := packages[cell.Module()]; !seen {
			modules = append(modules, cell.Module())
		}
		packages[cell.Module()] = append(packages[cell.Module()], cell.Package())
	}
	require.NotEmpty(t, modules, "concepts.yaml marks no %s cell equivalent", engine)

	for _, module := range modules {
		harness.RunExamples(t, testConnectionString, module, engine, packages[module]...)
	}
}
