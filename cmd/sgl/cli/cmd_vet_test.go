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
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestVetCleanProject(t *testing.T) {
	t.Parallel()

	result := sqliteProject(t).Run("vet")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stderr, "0 error(s), 0 warning(s), 0 hint(s)")
}

func warningProject(t *testing.T) *clitest.Project {
	t.Helper()
	project := sqliteProject(t)
	project.Write("db/queries/typo.sql", "-- sgl.query(Typo, many)\nSELECT id, nmae FROM authors;\n")
	project.Write("db/queries/keyword.sql", "-- sgl.query(W, one, nullabel: true)\nSELECT id FROM authors;\n")
	return project
}

func TestVetTextShowsSourceCaretAndHelp(t *testing.T) {
	t.Parallel()

	project := warningProject(t)
	result := project.Run("vet")
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "db/queries/typo.sql:2:12: warning[Q001]: unknown column \"nmae\"\n")
	assert.Contains(t, result.Stdout, " 2 | SELECT id, nmae FROM authors;\n   |            ^^^^\n")
	assert.Contains(t, result.Stdout, "help: did you mean \"name\"?\n")
	assert.Contains(t, result.Stdout, "db/queries/keyword.sql:1:22: error[Q027]: unknown keyword argument \"nullabel\" on sgl.query\n"+
		"   |\n"+
		" 1 | -- sgl.query(W, one, nullabel: true)\n"+
		"   |                      ^^^^^^^^\n"+
		"help: did you mean \"nullable\"?\n\n")
	assert.Contains(t, result.Stderr, "1 error(s), 1 warning(s)")

	project.Write("db/queries/keyword.sql", "-- sgl.query(W, one, nullable: true)\nSELECT id FROM authors;\n")
	requireCode(t, project.Run("vet"), 0)
	requireCode(t, project.Run("vet", "--strict"), 1)
}

func TestVetReportsAQueryTheEmitterWouldRefuse(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	requireCode(t, project.Run("init", "--dialect", "postgres"), 0)
	project.Write("db/migrations/0002_pairs.up.sql", "CREATE TYPE pair AS (a integer, b integer);\n"+
		"CREATE TABLE pairs (id integer PRIMARY KEY, value pair);\n")
	project.Write("db/queries/pairs.sql", "-- sgl.query(FindPairs, many)\nSELECT id FROM pairs WHERE value = ANY($1::pair[]);\n")

	result := project.Run("vet")
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "db/queries/pairs.sql:1:1: error[Q059]: parameter value is an array of the composite type pair")
	requireCode(t, project.Run("vet", "--strict"), 1)
	requireCode(t, project.Run("generate"), 1)
}

func TestVetMachineFormats(t *testing.T) {
	t.Parallel()

	project := warningProject(t)

	result := project.Run("vet", "--format", "json")
	requireCode(t, result, 1)
	scanner := bufio.NewScanner(strings.NewReader(result.Stdout))
	require.True(t, scanner.Scan())
	var finding map[string]any
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &finding))
	assert.Equal(t, "Q027", finding["code"])
	assert.Equal(t, "error", finding["severity"])
	assert.Equal(t, "db/queries/keyword.sql", finding["file"])
	assert.Equal(t, "app", finding["database"])
	assert.Equal(t, "nullable", finding["suggestion"])
	assert.InDelta(t, 22, finding["column"], 0)

	result = project.Run("vet", "--format", "github")
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "::error file=db/queries/keyword.sql,line=1,col=22,title=Q027::unknown keyword argument")
	assert.Contains(t, result.Stdout, "%0Ahelp: did you mean")
	assert.Contains(t, result.Stdout, "::warning file=db/queries/typo.sql,line=2,col=12,title=Q001::unknown column \"nmae\"%0Ahelp: did you mean \"name\"?")

	result = project.Run("vet", "--format", "sarif")
	requireCode(t, result, 1)
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &log))
	assert.Equal(t, "2.1.0", log.Version)
	require.Len(t, log.Runs, 1)
	assert.Equal(t, "sgl", log.Runs[0].Tool.Driver.Name)
	require.NotEmpty(t, log.Runs[0].Tool.Driver.Rules)
	assert.Equal(t, "Q001", log.Runs[0].Tool.Driver.Rules[0].ID)
	assert.Equal(t, "Q027", log.Runs[0].Tool.Driver.Rules[1].ID)
	require.Len(t, log.Runs[0].Results, 2)
	assert.Equal(t, "error", log.Runs[0].Results[0].Level)
	assert.Equal(t, "db/queries/keyword.sql", log.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 1, log.Runs[0].Results[0].Locations[0].PhysicalLocation.Region.StartLine)
	assert.Equal(t, "warning", log.Runs[0].Results[1].Level)

	requireCode(t, project.Run("vet", "--format", "xml"), 2)
	requireCode(t, project.Run("vet", "extra"), 2)
}

func TestVetErrorsAreFindings(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/broken.sql", "-- sgl.query(Broken, one)\nSELECT id FROM authors WHERE name = 'unterminated;\n")
	result := project.Run("vet")
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "error[Q")
}

func TestVetReportsConfigurationDiagnosticsAgainstTheConfigFile(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    go:\n      types:\n        overrides: [ { column: authors.nmae, go: example.com/x.Name } ]\n")
	result := project.Run("vet")
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "sgl.yaml: error[Q037]: type override references unknown column")
	assert.Contains(t, result.Stdout, `help: did you mean "authors.name"?`)
}

func TestVetAcceptsAForeignPrefixListedInTheConfiguration(t *testing.T) {
	t.Parallel()

	foreign := seagull.DefaultPreset().Directives.Foreign[0]
	project := sqliteProject(t)
	project.Write("db/queries/imported.sql", "-- "+foreign+".query(ListBios, many)\nSELECT bio FROM authors;\n")
	result := project.Run("vet")
	require.Contains(t, result.Stdout, "warning[Q051]")

	accepting := strings.Replace(project.Read("sgl.yaml"), "version: 1\n", "version: 1\n\ndirectives:\n  prefixes: [sgl, seagull, "+foreign+"]\n", 1)
	require.Contains(t, accepting, "prefixes: [sgl, seagull, "+foreign+"]")
	project.Write("sgl.yaml", accepting)

	result = project.Run("vet")
	requireCode(t, result, 0)
	require.NotContains(t, result.Stdout, "Q051")
	require.Contains(t, result.Stderr, "0 error(s), 0 warning(s), 0 hint(s)")

	requireCode(t, project.Run("generate"), 0)
	require.Contains(t, project.Read("internal/store/imported.sql.go"), "ListBios")
}
