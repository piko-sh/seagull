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

package session_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const (
	shelfConfiguration = "version: 1\n" +
		"databases:\n" +
		"  - name: shelf\n" +
		"    dialect: postgres\n" +
		"    migrations: db/migrations\n" +
		"    queries: db/queries\n" +
		"    go:\n" +
		"      package: shelf\n" +
		"      out: shelf\n" +
		"      types:\n" +
		"        overrides:\n" +
		"          - column: authors.pen_name\n" +
		"            go: example.com/pen-kit.Name\n"
	shelfMigration = "CREATE TABLE authors (\n" +
		"    id BIGSERIAL PRIMARY KEY,\n" +
		"    full_name TEXT NOT NULL,\n" +
		"    pen_name TEXT NOT NULL,\n" +
		"    tags TEXT[] NOT NULL\n" +
		");\n" +
		"COMMENT ON TABLE authors IS 'People who write books.';\n" +
		"COMMENT ON COLUMN authors.full_name IS 'The name on the passport.';\n"
	shelfQueries = "-- ListAuthors lists every author by name.\n" +
		"-- sgl.query(ListAuthors, many)\n" +
		"SELECT * FROM authors ORDER BY full_name;\n" +
		"\n" +
		"-- sgl.query(GetAuthor, one)\n" +
		"SELECT id, full_name FROM authors WHERE id = $1;\n"
)

var generatedCodeOverrides = []struct {
	check    func(t *testing.T, response session.Response)
	name     string
	override session.Override
}{
	{
		name:     "non-null elements",
		override: session.Override{NonNullElements: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "models.go").Content, "Tags     []string")
			require.True(t, response.Config.NonNullElements)
		},
	},
	{
		name:     "stars kept as written",
		override: session.Override{KeepStars: new(true), Driver: "pgx"},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findQuery(t, response, "ListAuthors").SQLSent, "SELECT * FROM authors ORDER BY full_name;")
			require.True(t, response.Config.KeepStars)
		},
	},
	{
		name:     "the interface of the query methods, named",
		override: session.Override{Interface: new(true), InterfaceName: "Shelf"},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "querier_interface.go").Content, "type Shelf interface")
			require.True(t, response.Config.Interface)
			require.Equal(t, "Shelf", response.Config.InterfaceName)
		},
	},
	{
		name:     "struct tags",
		override: session.Override{StructTags: map[string]string{"db": "{name}"}},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "models.go").Content, "`json:\"full_name\" db:\"full_name\"`")
			require.Equal(t, map[string]string{"db": "{name}"}, response.Config.StructTags)
		},
	},
	{
		name:     "json tags in camel case with omitempty",
		override: session.Override{JSONCase: "camel", JSONOmitEmpty: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "models.go").Content, "`json:\"fullName,omitempty\"`")
			require.Equal(t, "camel", response.Config.JSONCase)
			require.True(t, response.Config.JSONOmitEmpty)
		},
	},
	{
		name:     "json tags left out",
		override: session.Override{JSONOmit: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.NotContains(t, findFile(t, response, "models.go").Content, "json:")
			require.True(t, response.Config.JSONOmit)
		},
	},
	{
		name:     "empty slices",
		override: session.Override{EmptySlices: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "authors.sql.go").Content, "results := []ListAuthorsRow{}")
			require.True(t, response.Config.EmptySlices)
		},
	},
	{
		name:     "doc comments",
		override: session.Override{DocComments: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "authors.sql.go").Content, "// ListAuthors lists every author by name.\nfunc (queries *Queries) ListAuthors(")
			require.Contains(t, findFile(t, response, "models.go").Content, "// People who write books.\ntype Authors struct")
			require.True(t, response.Config.DocComments)
		},
	},
	{
		name:     "models reused",
		override: session.Override{ReuseModels: new(true)},
		check: func(t *testing.T, response session.Response) {
			list := findQuery(t, response, "ListAuthors")
			require.Equal(t, "Authors", list.RowType)
			require.Equal(t, "model", list.RowKind)
			require.True(t, response.Config.ReuseModels)
		},
	},
	{
		name:     "errors wrapped",
		override: session.Override{WrapErrors: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "authors.sql.go").Content, "sglWrapError(")
			require.True(t, response.Config.WrapErrors)
		},
	},
	{
		name:     "package names",
		override: session.Override{PackageNames: map[string]string{"example.com/pen-kit": "pens"}},
		check: func(t *testing.T, response session.Response) {
			require.Contains(t, findFile(t, response, "models.go").Content, "pens.Name")
			require.Equal(t, map[string]string{"example.com/pen-kit": "pens"}, response.Config.PackageNames)
		},
	},
}

var dialectOptionOverrides = []struct {
	check    func(t *testing.T, response session.Response)
	files    map[string]string
	name     string
	override session.Override
}{
	{
		name: "group_by_use_nulls on ClickHouse",
		files: map[string]string{
			"db/migrations/0001_init.up.sql": "CREATE TABLE sales (region String, amount UInt64) ENGINE = MergeTree ORDER BY region;\n",
			"db/queries/sales.sql": "-- sgl.query(SalesByRegion, many)\n" +
				"SELECT region, sum(amount) AS total FROM sales GROUP BY ROLLUP(region);\n",
		},
		override: session.Override{Dialect: "clickhouse", GroupByUseNulls: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.True(t, findQuery(t, response, "SalesByRegion").Columns[0].Nullable, "the rolled-up row holds NULL for the region")
			require.Equal(t, new(true), response.Config.GroupByUseNulls)
		},
	},
	{
		name: "ansi_quotes on MySQL",
		files: map[string]string{
			"db/migrations/0001_init.up.sql": "CREATE TABLE notes (id BIGINT PRIMARY KEY, body TEXT NOT NULL);\n",
			"db/queries/notes.sql":           "-- sgl.query(NoteBodies, many)\nSELECT \"body\" FROM notes;\n",
		},
		override: session.Override{Dialect: "mysql", ANSIQuotes: new(true)},
		check: func(t *testing.T, response session.Response) {
			require.Equal(t, "notes.body", findQuery(t, response, "NoteBodies").Columns[0].Source, "a double-quoted name is the column")
			require.Equal(t, new(true), response.Config.ANSIQuotes)
		},
	},
	{
		name: "search_path on PostgreSQL",
		files: map[string]string{
			"db/migrations/0001_init.up.sql": "CREATE SCHEMA library;\nCREATE TABLE books (id BIGINT PRIMARY KEY, title TEXT NOT NULL);\n",
			"db/queries/books.sql":           "-- sgl.query(ListBooks, many)\nSELECT id, title FROM books;\n",
		},
		override: session.Override{Dialect: "postgres", SearchPath: []string{"library", "public"}},
		check: func(t *testing.T, response session.Response) {
			require.Equal(t, []string{"library", "public"}, response.Config.SearchPath)
			require.Equal(t, "library", response.Catalogue.DefaultSchema, "unqualified tables are created in the first schema of the path")
		},
	},
}

func shelfProject() map[string]string {
	return map[string]string{
		"sgl.yaml":                       shelfConfiguration,
		"db/migrations/0001_init.up.sql": shelfMigration,
		"db/queries/authors.sql":         shelfQueries,
	}
}

func fileContents(response session.Response) string {
	var builder strings.Builder
	for _, file := range response.Files {
		builder.WriteString(file.Name + "\n" + file.Content)
	}
	return builder.String()
}

func TestRunAppliesTheGeneratedCodeOverrides(t *testing.T) {
	t.Parallel()

	baseline := run(t, shelfProject(), session.Override{})
	require.True(t, baseline.OK, baseline.Error)
	require.Zero(t, baseline.Counts.Errors, baseline.Diagnostics)

	for _, testCase := range generatedCodeOverrides {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := run(t, shelfProject(), testCase.override)

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			require.Empty(t, response.Config.Warnings)
			require.NotEqual(t, fileContents(baseline), fileContents(response), "the override changes the generated code")
			testCase.check(t, response)
		})
	}
}

func TestRunWrapsErrorsAsTheConfigurationAndTheOverrideSay(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		override session.Override
		name     string
		setting  string
		wrapping bool
	}{
		{name: "neither asks", setting: "", override: session.Override{}, wrapping: false},
		{name: "sgl.yaml asks", setting: "      wrap_errors: true\n", override: session.Override{}, wrapping: true},
		{name: "the override asks", setting: "", override: session.Override{WrapErrors: new(true)}, wrapping: true},
		{name: "the override turns off what sgl.yaml asks", setting: "      wrap_errors: true\n", override: session.Override{WrapErrors: new(false)}, wrapping: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := shelfProject()
			files["sgl.yaml"] = shelfConfiguration + testCase.setting

			response := run(t, files, testCase.override)

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			require.Equal(t, testCase.wrapping, response.Config.WrapErrors)
			require.Equal(t, testCase.wrapping, strings.Contains(findFile(t, response, "authors.sql.go").Content, "sglWrapError("))
		})
	}
}

func TestRunAppliesTheDialectOptionOverrides(t *testing.T) {
	t.Parallel()

	for _, testCase := range dialectOptionOverrides {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := run(t, testCase.files, testCase.override)

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			require.Empty(t, response.Config.Warnings)
			testCase.check(t, response)
		})
	}
}

func TestRunLeavesOutADialectOptionTheDialectDoesNotTake(t *testing.T) {
	t.Parallel()

	response := run(t, starter(t, "sqlite"), session.Override{GroupByUseNulls: new(true), ANSIQuotes: new(false), SearchPath: []string{"library"}})

	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors, response.Diagnostics)
	require.Equal(t, []string{
		"dialect_options.group_by_use_nulls does not apply to sqlite; the playground left it out",
		"dialect_options.ansi_quotes does not apply to sqlite; the playground left it out",
		"dialect_options.search_path does not apply to sqlite; the playground left it out",
	}, response.Config.Warnings)
	require.Nil(t, response.Config.GroupByUseNulls)
	require.Nil(t, response.Config.ANSIQuotes)
	require.Empty(t, response.Config.SearchPath)
}

func TestRunRefusesOverridesTheConfigurationRefuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		override session.Override
		message  string
	}{
		{name: "an unknown json case", override: session.Override{JSONCase: "kebab"}, message: `go.json_tags.case "kebab" must be snake, camel or pascal`},
		{name: "a json struct tag", override: session.Override{StructTags: map[string]string{"json": "{name}"}}, message: "go.struct_tags cannot give json; set go.json_tags instead"},
		{name: "an interface name that is no identifier", override: session.Override{Interface: new(true), InterfaceName: "My Store"}, message: `go.interface.name "My Store" is not a Go identifier`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := run(t, starter(t, "sqlite"), testCase.override)

			require.False(t, response.OK)
			require.False(t, response.Internal)
			require.Contains(t, response.Error, "the playground's settings")
			require.Contains(t, response.Error, testCase.message)
		})
	}
}

func TestRunRefusesAnOverrideLargerThanThePlaygroundTakes(t *testing.T) {
	t.Parallel()

	manyTags, manyPackages := map[string]string{}, map[string]string{}
	for index := range 65 {
		manyTags[fmt.Sprintf("tag%d", index)] = "{name}"
		manyPackages[fmt.Sprintf("example.com/kit%d", index)] = "kit"
	}
	testCases := []struct {
		name     string
		override session.Override
		message  string
	}{
		{
			name:     "too many struct tags",
			override: session.Override{StructTags: manyTags},
			message:  "override.structTags holds 65 entries, more than the playground takes: at most 64",
		},
		{
			name:     "too many package names",
			override: session.Override{PackageNames: manyPackages},
			message:  "override.packageNames holds 65 entries, more than the playground takes: at most 64",
		},
		{
			name:     "too long a search path",
			override: session.Override{SearchPath: make([]string, 65)},
			message:  "override.searchPath holds 65 entries, more than the playground takes: at most 64",
		},
		{
			name:     "a struct tag value too long",
			override: session.Override{StructTags: map[string]string{"db": strings.Repeat("é", 257)}},
			message:  "override.structTags holds a text of 257 characters, more than the playground takes: at most 256",
		},
		{
			name:     "a dialect name too long",
			override: session.Override{Dialect: strings.Repeat("x", 300)},
			message:  "override.dialect holds a text of 300 characters, more than the playground takes: at most 256",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := run(t, starter(t, "sqlite"), testCase.override)

			require.False(t, response.OK)
			require.False(t, response.Internal)
			require.Equal(t, "the playground's settings: "+testCase.message, response.Error)
		})
	}
}

func TestRunTakesAnOverrideAtThePlaygroundsLimits(t *testing.T) {
	t.Parallel()

	tags := map[string]string{}
	for index := range 64 {
		tags[fmt.Sprintf("tag%d", index)] = strings.Repeat("n", 256)
	}

	response := run(t, starter(t, "sqlite"), session.Override{StructTags: tags})

	require.True(t, response.OK, response.Error)
	require.Len(t, response.Config.StructTags, 64)
}

func TestRunHoldsParserLimitsToThePlaygrounds(t *testing.T) {
	t.Parallel()

	nested := "-- sgl.query(Nested, one)\nSELECT " + strings.Repeat("(", 1000) + "1" + strings.Repeat(")", 1000) + " AS deep;\n"
	testCases := []struct {
		name     string
		dialect  string
		options  string
		queries  string
		warnings []string
		refused  bool
	}{
		{
			name:    "parse depth and tokens raised to their ceilings",
			dialect: "sqlite",
			options: "      max_parse_depth: 4096\n      max_tokens_per_statement: 1000000\n",
			queries: nested,
			warnings: []string{
				"dialect_options.max_parse_depth 4096 is above the playground's limit; the playground used 256",
				"dialect_options.max_tokens_per_statement 1000000 is above the playground's limit; the playground used 100000",
			},
			refused: true,
		},
		{
			name:    "parse depth raised past its ceiling",
			dialect: "sqlite",
			options: "      max_parse_depth: 100000000\n",
			queries: nested,
			warnings: []string{
				"dialect_options.max_parse_depth 100000000 is above the playground's limit; the playground used 256",
			},
			refused: true,
		},
		{
			name:     "type depth raised past its ceiling",
			dialect:  "clickhouse",
			options:  "      max_type_parse_depth: 1000\n",
			queries:  "-- sgl.query(One, one)\nSELECT 1 AS one;\n",
			warnings: []string{"dialect_options.max_type_parse_depth 1000 is above the playground's limit; the playground used 64"},
			refused:  false,
		},
		{
			name:     "limits lowered below the playground's",
			dialect:  "sqlite",
			options:  "      max_parse_depth: 100\n      max_tokens_per_statement: 5000\n",
			queries:  "-- sgl.query(One, one)\nSELECT 1 AS one;\n",
			warnings: []string{},
			refused:  false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := starter(t, testCase.dialect)
			files["sgl.yaml"] = "version: 1\ndatabases:\n  - name: app\n    dialect: " + testCase.dialect + "\n    dialect_options:\n" + testCase.options
			files["db/queries/example.sql"] = testCase.queries

			scaling.Within(t, time.Second, func(ctx context.Context) {
				response := session.Run(ctx, session.Request{Files: files, Override: session.Override{}, V: session.ProtocolVersion})

				require.True(t, response.OK, response.Error)
				require.Equal(t, testCase.warnings, response.Config.Warnings)
				if testCase.refused {
					require.Positive(t, response.Counts.Errors, "the nesting is deeper than the playground's limit")
					return
				}
				require.Zero(t, response.Counts.Errors, response.Diagnostics)
			})
		})
	}
}

func TestRunValidatesAConfigurationOfManyDatabasesQuickly(t *testing.T) {
	t.Parallel()

	var configuration strings.Builder
	configuration.WriteString("version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n")
	for index := range 255 {
		fmt.Fprintf(&configuration, "  - {name: d%d, dialect: sqlite, go: {out: gen/d%d}}\n", index, index)
	}
	files := starter(t, "sqlite")
	files["sgl.yaml"] = configuration.String()

	scaling.Within(t, time.Second, func(ctx context.Context) {
		response := session.Run(ctx, session.Request{Files: files, Override: session.Override{JSONCase: "camel"}, V: session.ProtocolVersion})

		require.True(t, response.OK, response.Error)
		require.Equal(t, "camel", response.Config.JSONCase)
	})
}
