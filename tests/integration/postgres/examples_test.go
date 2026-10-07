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

package postgres_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestPostgresExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	harness.RunExamples(t, testConnectionString, "postgres", "postgres", "./...")
}

func TestPostgresTutorial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	command := testkit.GoCommand(t, "../../tutorials", []string{
		"GOWORK=" + os.Getenv("GOWORK"),
		"SGL_EXAMPLE_DSN=" + testConnectionString,
		"SGL_EXAMPLE_REQUIRE_DSN=1",
	}, "test", "-count=1", "./07_postgres")
	output, err := command.CombinedOutput()
	t.Logf("PostgreSQL tutorial checkpoint\n%s", output)
	require.NoError(t, err, "PostgreSQL tutorial failed against the embedded server")
}
