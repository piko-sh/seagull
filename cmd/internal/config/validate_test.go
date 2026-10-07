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

package config

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestValidateStaysSmallAndFastForManyDatabases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		databases int
		contains  string
	}{
		{name: "more databases than accepted", databases: 4_000, contains: "4000 databases are more than the 256 one configuration may declare"},
		{name: "every accepted database sharing the default go.out", databases: MaxDatabases, contains: `"d7" and 248 more all write generated code to go.out "internal/store"`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var data strings.Builder
			data.WriteString("databases:\n")
			for index := range testCase.databases {
				fmt.Fprintf(&data, "  - {name: d%d, dialect: sqlite}\n", index)
			}
			var err error
			scaling.Within(t, 500*time.Millisecond, func(context.Context) {
				_, err = Parse([]byte(data.String()), false)
			})

			require.ErrorIs(t, err, ErrInvalid)
			require.ErrorContains(t, err, testCase.contains)
			require.Less(t, len(err.Error()), 10<<10)
		})
	}
}

func TestValidateOutputDirectoriesGrowsLinearly(t *testing.T) {
	nested := func(count int) []Database {
		databases := make([]Database, 0, count)
		for index := range count {
			databases = append(databases, Database{Name: fmt.Sprintf("d%d", index), Go: Go{Out: fmt.Sprintf("gen/%d/%d", index%7, index)}})
		}
		return databases
	}
	smaller, larger := nested(5_000), nested(20_000)

	scaling.Linear(t,
		func() { _ = validateOutputDirectories(smaller, "") },
		func() { _ = validateOutputDirectories(larger, "") },
	)
}

func TestValidateReportsTheFirstProblemsAndCountsTheRest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		problems int
		contains string
		absent   string
	}{
		{name: "within the cap", problems: maxReportedProblems, absent: "more problem"},
		{name: "one past the cap", problems: maxReportedProblems + 1, contains: "and 1 more problem"},
		{name: "far past the cap", problems: 200, contains: "and 150 more problems"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &Config{Version: CurrentVersion, Directives: Directives{Prefixes: []string{"sgl"}, RequireCanonical: false}, Databases: nil, Schema: ""}
			for index := range testCase.problems {
				config.Databases = append(config.Databases, Database{
					Name: fmt.Sprintf("d%d", index), Dialect: "sqlite",
					Go:      Go{Package: "Bad", Driver: "database/sql", Out: fmt.Sprintf("out/%d", index)},
					Migrate: Migrate{Namespace: "sgl"},
				})
			}

			err := config.Validate()

			require.ErrorIs(t, err, ErrInvalid)
			require.Equal(t, min(testCase.problems, maxReportedProblems), strings.Count(err.Error(), `go.package "Bad"`))
			if testCase.contains != "" {
				require.ErrorContains(t, err, testCase.contains)
			}
			if testCase.absent != "" {
				require.NotContains(t, err.Error(), testCase.absent)
			}
		})
	}
}

func TestValidateReportsEachSharedOrNestedOutputDirectoryOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		outs     []string
		problems []string
	}{
		{
			name:     "three databases sharing one directory",
			outs:     []string{"gen", "gen", "./gen"},
			problems: []string{`databases "d0", "d1" and "d2" all write generated code to go.out "gen"`},
		},
		{
			name:     "a sibling that sorts between a directory and its child",
			outs:     []string{"gen", "gen-x", "gen/sub"},
			problems: []string{`database "d2" go.out "gen/sub" is inside database "d0" go.out "gen"`},
		},
		{
			name:     "the nearest enclosing directory is named",
			outs:     []string{"gen", "gen/a", "gen/a/b"},
			problems: []string{`database "d1" go.out "gen/a" is inside database "d0" go.out "gen"`, `database "d2" go.out "gen/a/b" is inside database "d1" go.out "gen/a"`},
		},
		{
			name:     "the current directory holds every relative directory",
			outs:     []string{".", "internal/store"},
			problems: []string{`database "d1" go.out "internal/store" is inside database "d0" go.out "."`},
		},
		{
			name:     "a parent directory is not inside the current one",
			outs:     []string{".", "..", "../other"},
			problems: []string{`database "d2" go.out "../other" is inside database "d1" go.out ".."`},
		},
		{
			name: "an absolute and a relative directory never overlap",
			outs: []string{"/srv/gen", "srv/gen/inner"},
		},
		{
			name:     "an absolute directory inside another",
			outs:     []string{"/srv", "/srv/gen"},
			problems: []string{`database "d1" go.out "/srv/gen" is inside database "d0" go.out "/srv"`},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			databases := make([]Database, 0, len(testCase.outs))
			for index, out := range testCase.outs {
				databases = append(databases, Database{Name: fmt.Sprintf("d%d", index), Go: Go{Out: out}})
			}

			problems := validateOutputDirectories(databases, "")

			require.Len(t, problems, len(testCase.problems))
			for index, problem := range testCase.problems {
				require.Contains(t, problems[index], problem)
			}
		})
	}
}

func TestValidateRefusesGeneratedCodeOptionsItCannotUse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		goBlock string
		problem string
	}{
		{name: "an unknown json case", goBlock: `{ json_tags: { case: kebab } }`, problem: `go.json_tags.case "kebab" must be snake, camel or pascal`},
		{name: "json among the struct tags", goBlock: `{ struct_tags: { json: "{name}" } }`, problem: "go.struct_tags cannot give json"},
		{name: "an interface name that is no identifier", goBlock: `{ interface: { enabled: true, name: "My Querier" } }`, problem: `go.interface.name "My Querier" is not a Go identifier`},
		{name: "struct tags on a SQL type override", goBlock: `{ types: { overrides: [ { sql: uuid, struct_tags: { validate: uuid } } ] } }`, problem: `struct_tags apply to a column override, not to sql "uuid"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			data := "databases:\n  - name: app\n    dialect: postgres\n    go: " + testCase.goBlock + "\n"
			_, err := Parse([]byte(data), false)

			require.ErrorIs(t, err, ErrInvalid)
			require.ErrorContains(t, err, testCase.problem)
		})
	}
}

func TestValidateAcceptsAColumnOverrideGivingOnlyStructTags(t *testing.T) {
	t.Parallel()

	data := `
databases:
  - name: app
    dialect: postgres
    go: { types: { overrides: [ { column: users.email, struct_tags: { validate: email } } ] } }
`
	_, err := Parse([]byte(data), false)

	require.NoError(t, err)
}

func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	data := `
version: 2
directives:
  prefixes: [Bad-Prefix, sgl, sgl]
databases:
  - name: app
    go: { package: Not-A-Package, driver: "Not A Driver", types: { overrides: [ { go: "" }, { sql: a, column: b, go: x } ] } }
    migrate: { lock: sometimes, namespace: "1bad", history_table: "drop table" }
    custom_functions: [ { nullable: maybe, args: [a], min_args: 3 } ]
  - name: app
    dialect: sqlite
`
	_, err := Parse([]byte(data), false)

	require.ErrorIs(t, err, ErrInvalid)
	for _, expected := range []string{
		"version 2 is not supported",
		`"Bad-Prefix" must be lower case`,
		`"sgl" is listed twice`,
		"dialect is required",
		`go.package "Not-A-Package"`,
		`go.driver "Not A Driver"`,
		`migrate.lock "sometimes"`,
		`migrate.namespace "1bad"`,
		`migrate.history_table "drop table"`,
		"overrides[0]: set exactly one of sql and column",
		"overrides[0]: go, non_null_elements or struct_tags is required",
		"overrides[1]: set exactly one of sql and column",
		`column "b" must be table.column`,
		"custom_functions[0]: name is required",
		"custom_functions[0]: returns is required",
		`nullable "maybe"`,
		"min_args is larger",
		`database "app": the name is used more than once`,
	} {
		require.Contains(t, err.Error(), expected)
	}
}

func TestValidateCustomFunctionNullability(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "null_on_null", "never_null", "called_on_null"} {
		data := "databases:\n  - name: app\n    dialect: sqlite\n    custom_functions: [ { name: f, returns: text, nullable: \"" + value + "\" } ]\n"
		_, err := Parse([]byte(data), false)
		require.NoError(t, err, value)
	}
	for _, value := range []string{"never", "always", "maybe"} {
		data := "databases:\n  - name: app\n    dialect: sqlite\n    custom_functions: [ { name: f, returns: text, nullable: " + value + " } ]\n"
		_, err := Parse([]byte(data), false)
		require.ErrorContains(t, err, "must be null_on_null, never_null or called_on_null", value)
	}
}

func TestValidateCustomFunctionDataAccess(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "read_only", "modifies_data"} {
		data := "databases:\n  - name: app\n    dialect: sqlite\n    custom_functions: [ { name: f, returns: text, data_access: \"" + value + "\" } ]\n"
		configuration, err := Parse([]byte(data), false)
		require.NoError(t, err, value)
		require.Equal(t, value, configuration.Databases[0].CustomFunctions[0].DataAccess)
	}
	for _, value := range []string{"volatile", "writes", "READ_ONLY"} {
		data := "databases:\n  - name: app\n    dialect: sqlite\n    custom_functions: [ { name: f, returns: text, data_access: " + value + " } ]\n"
		_, err := Parse([]byte(data), false)
		require.ErrorContains(t, err, "must be read_only or modifies_data", value)
	}
}

func TestValidateRequiresDatabases(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("version: 1\n"), false)

	require.ErrorContains(t, err, "at least one database is required")
}

func TestValidateMissingName(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("databases:\n  - dialect: sqlite\n"), false)

	require.ErrorContains(t, err, "databases[0]: name is required")
}

func TestValidateRejectsOverlappingOutputDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		first   string
		second  string
		problem string
	}{
		{name: "both default", first: "", second: "", problem: `databases "one" and "two" both write generated code to go.out "internal/store"`},
		{name: "same after cleaning", first: "gen", second: "./gen/", problem: `databases "one" and "two" both write generated code to go.out "gen"`},
		{name: "nested", first: "gen", second: "gen/two", problem: `database "two" go.out "gen/two" is inside database "one" go.out "gen"`},
		{name: "nested the other way", first: "gen/one/deeper", second: "gen/one", problem: `database "one" go.out "gen/one/deeper" is inside database "two" go.out "gen/one"`},
		{name: "distinct siblings", first: "gen/one", second: "gen/two"},
		{name: "shared prefix but not nested", first: "gen", second: "generated"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			data := "databases:\n" +
				"  - { name: one, dialect: sqlite, go: { out: \"" + testCase.first + "\" } }\n" +
				"  - { name: two, dialect: sqlite, go: { out: \"" + testCase.second + "\" } }\n"
			_, err := Parse([]byte(data), false)
			if testCase.problem == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalid)
			require.ErrorContains(t, err, testCase.problem)
		})
	}
}
