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
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigSchemaAndValidate(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	result := project.Run("config", "schema")
	requireCode(t, result, 0)
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &schema))
	assert.Equal(t, "https://seagull.sh/sgl.schema.json", schema["$id"])

	result = project.Run("config", "validate")
	requireCode(t, result, 0)
	assert.Equal(t, "sgl.yaml is valid (1 database(s))\n", result.Stdout)

	project.Write("other/sgl.yml", "databases:\n  - name: app\n    dialect: sqlite\n    go: { driver: pgx }\n")
	result = project.Run("config", "validate", "other/sgl.yml")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "pgx needs a dialect that runs on pgx, such as postgres")

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    migratons: db\n")
	result = project.Run("config", "validate")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, `did you mean "migrations"?`)

	requireCode(t, project.Run("config"), 2)
	requireCode(t, project.Run("config", "edit"), 2)
	requireCode(t, project.Run("config", "schema", "extra"), 2)
	requireCode(t, project.Run("config", "validate", "a", "b"), 2)
}

func TestConfigErrorsNameTheFileRelatively(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("bad.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    querys: db/queries\n    go:\n      pakage: store\n")
	result := project.Run("config", "validate", "bad.yaml")
	requireCode(t, result, 2)
	require.True(t, strings.HasPrefix(result.Stderr, "sgl: bad.yaml: invalid configuration: "), result.Stderr)
	require.Contains(t, result.Stderr, `field querys not found in type config.Database; did you mean "queries"?`)
	require.Contains(t, result.Stderr, `field pakage not found in type config.Go; did you mean "package"?`)
	require.NotContains(t, result.Stderr, project.Dir)

	project.Write("other/sgl.yml", "databases:\n  - name: app\n    dialect: sqlite\n    go: { driver: pgx }\n")
	result = project.Run("config", "validate", "other/sgl.yml")
	requireCode(t, result, 2)
	require.Equal(t, "sgl: other/sgl.yml: database \"app\": go.driver pgx needs a dialect that runs on pgx, such as postgres, not sqlite\n", result.Stderr)

	project.Write("overlap.yaml", "databases:\n  - { name: one, dialect: sqlite, go: { out: gen } }\n  - { name: two, dialect: sqlite, go: { out: gen/two } }\n")
	result = project.Run("config", "validate", "overlap.yaml")
	requireCode(t, result, 2)
	require.Contains(t, result.Stderr, `database "two" go.out "gen/two" is inside database "one" go.out "gen"`)
	require.NotContains(t, result.Stderr, project.Dir)

	for _, arguments := range [][]string{{"config", "validate", "missing.yaml"}, {"-c", "missing.yaml", "vet"}, {"-c", "missing/sgl.yaml", "generate"}} {
		result = project.Run(arguments...)
		requireCode(t, result, 2)
		require.True(t, strings.HasPrefix(result.Stderr, "sgl: missing"), "%v: %s", arguments, result.Stderr)
		require.NotContains(t, result.Stderr, "statat", arguments)
		require.NotContains(t, result.Stderr, project.Dir, arguments)
	}
}

func TestConfigValidateRefusesWhatGenerateRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings string
		message  string
	}{
		{name: "an option of another dialect", settings: "    dialect_options:\n      returning: true\n",
			message: "sgl: sgl.yaml: database \"app\": dialect_options.returning applies to mysql and mariadb, not sqlite\n"},
		{name: "an interface named after the Queries type", settings: "    go:\n      interface:\n        enabled: true\n        name: Queries\n",
			message: "sgl: sgl.yaml: database \"app\": the interface name \"Queries\" is a name the generated package already declares or imports; " +
				"choose another name: invalid configuration\n"},
		{name: "an interface named after an import", settings: "    go:\n      interface:\n        enabled: true\n        name: DBTX\n",
			message: "sgl: sgl.yaml: database \"app\": the interface name \"DBTX\" is a name the generated package already declares or imports; " +
				"choose another name: invalid configuration\n"},
		{name: "a query-names function named after the Queries type",
			settings: "    go:\n      query_names:\n        enabled: true\n        func: Queries\n",
			message: "sgl: sgl.yaml: database \"app\": the query-names function \"Queries\" is a name the generated package already declares or imports; " +
				"choose another name: invalid configuration\n"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := sqliteProject(t)
			project.Write("sgl.yaml", "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: db/migrations\n"+
				"    queries: db/queries\n"+testCase.settings)
			for _, command := range []string{"config validate", "generate", "vet"} {
				result := project.Run(strings.Fields(command)...)
				requireCode(t, result, 2)
				require.Equal(t, testCase.message, result.Stderr, command)
				require.NotContains(t, result.Stderr, "this sgl knows", command)
			}
		})
	}
}
