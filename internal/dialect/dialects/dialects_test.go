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

package dialects_test

import (
	"errors"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNamesListsEveryBuiltInDialectSorted(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"clickhouse", "cockroachdb", "duckdb", "mariadb", "mysql", "postgres", "sqlite", "timescaledb",
	}, dialects.Names())
}

func TestNewReportsNameAndFamily(t *testing.T) {
	t.Parallel()

	standard := ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete
	cases := []struct {
		name      string
		family    string
		returning ast.DataChange
	}{
		{name: "postgres", family: "postgres", returning: standard},
		{name: "cockroachdb", family: "postgres", returning: standard},
		{name: "timescaledb", family: "postgres", returning: standard},
		{name: "mysql", family: "mysql", returning: 0},
		{name: "mariadb", family: "mysql", returning: ast.DataChangeInsert | ast.DataChangeDelete},
		{name: "sqlite", family: "sqlite", returning: standard},
		{name: "duckdb", family: "duckdb", returning: standard},
		{name: "clickhouse", family: "clickhouse", returning: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.name, dialects.Options{})
			require.NoError(t, err)
			info := built.Info()
			assert.Equal(t, testCase.name, info.Name)
			assert.Equal(t, testCase.family, info.Family)
			assert.Equal(t, testCase.returning, info.Returning)

			renamed, err := dialects.New(testCase.name, dialects.Options{Name: "custom"})
			require.NoError(t, err)
			assert.Equal(t, "custom", renamed.Info().Name)
			assert.Equal(t, testCase.family, renamed.Info().Family, "a custom name keeps the family")
		})
	}
}

func TestNewAppliesReturningToTheMySQLFamily(t *testing.T) {
	t.Parallel()

	enabled := true
	mysql, err := dialects.New("mysql", dialects.Options{Returning: &enabled})
	require.NoError(t, err)
	assert.Equal(t, ast.DataChangeInsert|ast.DataChangeUpdate|ast.DataChangeDelete, mysql.Info().Returning)

	disabled := false
	mariadb, err := dialects.New("mariadb", dialects.Options{Returning: &disabled})
	require.NoError(t, err)
	assert.Zero(t, mariadb.Info().Returning)
}

func TestReturningOptionDecidesWhetherTheClauseIsReported(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	insert := "INSERT INTO items (id, name) VALUES (?, ?) RETURNING id;"
	update := "UPDATE items SET name = ? WHERE id = ? RETURNING id;"
	deletion := "DELETE FROM items WHERE id = ? RETURNING id;"
	cases := []struct {
		returning *bool
		name      string
		dialect   string
		query     string
		reported  bool
	}{
		{name: "mysql by default", dialect: "mysql", query: insert, returning: nil, reported: true},
		{name: "mysql with returning on", dialect: "mysql", query: insert, returning: &enabled, reported: false},
		{name: "mysql update with returning on", dialect: "mysql", query: update, returning: &enabled, reported: false},
		{name: "mariadb insert by default", dialect: "mariadb", query: insert, returning: nil, reported: false},
		{name: "mariadb delete by default", dialect: "mariadb", query: deletion, returning: nil, reported: false},
		{name: "mariadb update by default", dialect: "mariadb", query: update, returning: nil, reported: true},
		{name: "mariadb with returning off", dialect: "mariadb", query: insert, returning: &disabled, reported: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.dialect, dialects.Options{Returning: testCase.returning})
			require.NoError(t, err)
			generator, err := app.New(built)
			require.NoError(t, err)

			files := fstest.MapFS{
				"migrations/001_items.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE items (id INT PRIMARY KEY, name VARCHAR(255));")},
				"queries/items.sql":           &fstest.MapFile{Data: []byte("-- sgl.query(ChangeItem, one)\n" + testCase.query)},
			}
			report, err := generator.Vet(t.Context(), app.Input{Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
			require.NoError(t, err)

			reported := slices.ContainsFunc(report.Diagnostics, func(diagnostic diag.SourceError) bool {
				return diagnostic.Code == diag.CodeReturningNotSupported
			})
			assert.Equal(t, testCase.reported, reported, "diagnostics: %+v", report.Diagnostics)
		})
	}
}

func TestNewAcceptsLimits(t *testing.T) {
	t.Parallel()

	for _, name := range dialects.Names() {
		options := dialects.Options{MaxParseDepth: 64, MaxTokensPerStatement: 1000}
		if name == "clickhouse" {
			options.MaxTypeParseDepth = 8
		}
		built, err := dialects.New(name, options)
		require.NoError(t, err, name)
		statements, err := built.ParseStatements(t.Context(), "SELECT 1;")
		require.NoError(t, err, name)
		assert.Len(t, statements, 1, name)
	}
}

func TestNewAcceptsLimitsAtTheirCeilings(t *testing.T) {
	t.Parallel()

	for _, name := range dialects.Names() {
		options := dialects.Options{MaxParseDepth: dialectkit.MaxParseDepthCeiling, MaxTokensPerStatement: dialectkit.MaxTokensPerStatementCeiling}
		if name == "clickhouse" {
			options.MaxTypeParseDepth = sqlmodel.MaxTypeDepth
		}
		_, err := dialects.New(name, options)
		require.NoError(t, err, name)
	}
}

func TestNewPassesJoinUseNullsToClickHouse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		joinUseNulls *bool
		name         string
		wantNullable ast.JoinSides
	}{
		{name: "server default", joinUseNulls: nil, wantNullable: 0},
		{name: "off", joinUseNulls: new(false), wantNullable: 0},
		{name: "on", joinUseNulls: new(true), wantNullable: ast.JoinSideRight},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New("clickhouse", dialects.Options{JoinUseNulls: testCase.joinUseNulls})
			require.NoError(t, err)
			statements, err := built.ParseStatements(t.Context(), "SELECT u.id FROM users u LEFT JOIN accounts a ON a.user_id = u.id")
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := built.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.Len(t, analysis.JoinClauses, 1)
			assert.Equal(t, testCase.wantNullable, analysis.JoinClauses[0].Kind.Nullable)
		})
	}
}

func TestNewPassesGroupByUseNullsToClickHouse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		groupByUseNulls *bool
		name            string
		wantDefaults    bool
	}{
		{name: "server default", groupByUseNulls: nil, wantDefaults: true},
		{name: "off", groupByUseNulls: new(false), wantDefaults: true},
		{name: "on", groupByUseNulls: new(true), wantDefaults: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New("clickhouse", dialects.Options{GroupByUseNulls: testCase.groupByUseNulls})
			require.NoError(t, err)
			statements, err := built.ParseStatements(t.Context(), "SELECT region, count() FROM sales GROUP BY region WITH ROLLUP")
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := built.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.NotNil(t, analysis.GroupingSets)
			assert.Equal(t, testCase.wantDefaults, analysis.GroupingSets.FillsDefaults)
		})
	}
}

func TestNewPassesANSIQuotesToTheMySQLFamily(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		ansiQuotes *bool
		name       string
		dialect    string
		want       sqllex.DoubleQuoteMeaning
	}{
		{name: "mysql server default", dialect: "mysql", ansiQuotes: nil, want: sqllex.DoubleQuoteString},
		{name: "mysql off", dialect: "mysql", ansiQuotes: new(false), want: sqllex.DoubleQuoteString},
		{name: "mysql on", dialect: "mysql", ansiQuotes: new(true), want: sqllex.DoubleQuoteIdentifier},
		{name: "mariadb on", dialect: "mariadb", ansiQuotes: new(true), want: sqllex.DoubleQuoteIdentifier},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.dialect, dialects.Options{ANSIQuotes: testCase.ansiQuotes})
			require.NoError(t, err)
			assert.Equal(t, testCase.want, built.Info().Lexical.Quotes.DoubleQuote)
		})
	}
}

func TestNewPassesTheSearchPathToThePostgresFamily(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		dialect    string
		searchPath []string
		want       []string
	}{
		{name: "postgres default", dialect: "postgres", searchPath: nil, want: []string{"public"}},
		{name: "postgres set", dialect: "postgres", searchPath: []string{"auth", "$user", "public"}, want: []string{"auth", "public"}},
		{name: "cockroachdb set", dialect: "cockroachdb", searchPath: []string{"auth"}, want: []string{"auth"}},
		{name: "timescaledb set", dialect: "timescaledb", searchPath: []string{"metrics", "public"}, want: []string{"metrics", "public"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.dialect, dialects.Options{SearchPath: testCase.searchPath})
			require.NoError(t, err)
			require.Equal(t, testCase.want, built.Info().SchemaSearchPath)
		})
	}
}

func TestNewRejectsUnknownName(t *testing.T) {
	t.Parallel()

	built, err := dialects.New("oracle", dialects.Options{})
	require.Error(t, err)
	assert.Nil(t, built)
	assert.ErrorIs(t, err, dialects.ErrUnknownDialect)
	assert.ErrorIs(t, err, fault.ErrInvalidConfig)
	assert.Contains(t, err.Error(), `"oracle"`)
	assert.Contains(t, err.Error(), "clickhouse, cockroachdb, duckdb, mariadb, mysql, postgres, sqlite, timescaledb")
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	enabled := true
	cases := []struct {
		name    string
		dialect string
		options dialects.Options
		message string
	}{
		{name: "negative parse depth", dialect: "postgres", options: dialects.Options{MaxParseDepth: -1}, message: "MaxParseDepth"},
		{name: "negative token budget", dialect: "sqlite", options: dialects.Options{MaxTokensPerStatement: -5}, message: "MaxTokensPerStatement"},
		{name: "negative type depth", dialect: "clickhouse", options: dialects.Options{MaxTypeParseDepth: -2}, message: "MaxTypeParseDepth"},
		{name: "parse depth above its ceiling", dialect: "mysql", options: dialects.Options{MaxParseDepth: dialectkit.MaxParseDepthCeiling + 1},
			message: "MaxParseDepth must be at most 4096"},
		{name: "token budget above its ceiling", dialect: "duckdb",
			options: dialects.Options{MaxTokensPerStatement: dialectkit.MaxTokensPerStatementCeiling + 1}, message: "MaxTokensPerStatement must be at most 1000000"},
		{name: "type depth above the deepest type", dialect: "clickhouse", options: dialects.Options{MaxTypeParseDepth: sqlmodel.MaxTypeDepth + 1},
			message: "MaxTypeParseDepth must be at most 64"},
		{name: "returning outside mysql", dialect: "postgres", options: dialects.Options{Returning: &enabled}, message: "Returning"},
		{name: "type depth outside clickhouse", dialect: "duckdb", options: dialects.Options{MaxTypeParseDepth: 4}, message: "MaxTypeParseDepth"},
		{name: "join_use_nulls outside clickhouse", dialect: "postgres", options: dialects.Options{JoinUseNulls: &enabled}, message: "JoinUseNulls"},
		{name: "group_by_use_nulls outside clickhouse", dialect: "duckdb", options: dialects.Options{GroupByUseNulls: &enabled}, message: "GroupByUseNulls"},
		{name: "ansi_quotes outside the mysql family", dialect: "postgres", options: dialects.Options{ANSIQuotes: &enabled}, message: "ANSIQuotes"},
		{name: "search_path outside the postgres family", dialect: "mysql", options: dialects.Options{SearchPath: []string{"shop"}},
			message: "SearchPath"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.dialect, testCase.options)
			require.Error(t, err)
			assert.Nil(t, built)
			assert.True(t, errors.Is(err, dialects.ErrInvalidOption))
			assert.ErrorIs(t, err, fault.ErrInvalidConfig)
			assert.Contains(t, err.Error(), testCase.message)
		})
	}
}

func TestNewNamesEveryOptionItRefuses(t *testing.T) {
	t.Parallel()

	enabled := true
	cases := []struct {
		name      string
		dialect   string
		message   string
		described []string
		options   dialects.Options
	}{
		{name: "a negative limit", dialect: "postgres", options: dialects.Options{MaxParseDepth: -1},
			message:   "invalid dialect option: invalid configuration: MaxParseDepth must not be negative, got -1",
			described: []string{"max_parse_depth must not be negative, got -1"}},
		{name: "a limit above its ceiling", dialect: "sqlite", options: dialects.Options{MaxParseDepth: 1 << 20},
			message:   "invalid dialect option: invalid configuration: MaxParseDepth must be at most 4096, got 1048576",
			described: []string{"max_parse_depth must be at most 4096, got 1048576"}},
		{name: "an option of one dialect", dialect: "duckdb", options: dialects.Options{JoinUseNulls: &enabled},
			message:   "invalid dialect option: invalid configuration: JoinUseNulls applies to clickhouse, not duckdb",
			described: []string{"join_use_nulls applies to clickhouse, not duckdb"}},
		{name: "an option of two dialects", dialect: "postgres", options: dialects.Options{Returning: &enabled},
			message:   "invalid dialect option: invalid configuration: Returning applies to mysql and mariadb, not postgres",
			described: []string{"returning applies to mysql and mariadb, not postgres"}},
		{name: "an option of three dialects", dialect: "sqlite", options: dialects.Options{SearchPath: []string{"shop"}},
			message:   "invalid dialect option: invalid configuration: SearchPath applies to postgres, cockroachdb and timescaledb, not sqlite",
			described: []string{"search_path applies to postgres, cockroachdb and timescaledb, not sqlite"}},
		{name: "a name of another family", dialect: "clickhouse", options: dialects.Options{Name: "sqlite"},
			message: "invalid dialect option: invalid configuration: Name must not name a built-in dialect of another family than " +
				`clickhouse, got "sqlite"`,
			described: []string{`name must not name a built-in dialect of another family than clickhouse, got "sqlite"`}},
		{name: "several options", dialect: "postgres", options: dialects.Options{MaxTokensPerStatement: -3, ANSIQuotes: &enabled, GroupByUseNulls: &enabled},
			message: "invalid dialect option: invalid configuration: MaxTokensPerStatement must not be negative, got -3\n" +
				"invalid dialect option: invalid configuration: GroupByUseNulls applies to clickhouse, not postgres\n" +
				"invalid dialect option: invalid configuration: ANSIQuotes applies to mysql and mariadb, not postgres",
			described: []string{"max_tokens_per_statement must not be negative, got -3", "group_by_use_nulls applies to clickhouse, not postgres",
				"ansi_quotes applies to mysql and mariadb, not postgres"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := dialects.New(testCase.dialect, testCase.options)
			require.EqualError(t, err, testCase.message)
			refused := dialects.OptionErrors(err)
			require.Len(t, refused, len(testCase.described))
			for index, optionError := range refused {
				require.ErrorIs(t, optionError, dialects.ErrInvalidOption)
				require.Equal(t, testCase.dialect, optionError.Dialect)
				require.Equal(t, testCase.described[index], optionError.Describe(snakeCase(optionError.Option)))
			}
		})
	}
}

func TestNewRefusesANameThatIsBlankOrAnotherFamilysBuiltInName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		dialect       string
		custom        string
		wantDescribed string
	}{
		{name: "another family's built-in name", dialect: "clickhouse", custom: "postgres",
			wantDescribed: `name must not name a built-in dialect of another family than clickhouse, got "postgres"`},
		{name: "another family's built-in name in another case with space around it", dialect: "duckdb", custom: " MySQL ",
			wantDescribed: `name must not name a built-in dialect of another family than duckdb, got " MySQL "`},
		{name: "a derived dialect's name for its base family", dialect: "sqlite", custom: "mariadb",
			wantDescribed: `name must not name a built-in dialect of another family than sqlite, got "mariadb"`},
		{name: "a blank name", dialect: "postgres", custom: "   ", wantDescribed: "name must not be blank"},
		{name: "a tab alone", dialect: "mysql", custom: "\t", wantDescribed: "name must not be blank"},
		{name: "its own name", dialect: "postgres", custom: "postgres"},
		{name: "a built-in name of its own family", dialect: "cockroachdb", custom: "postgres"},
		{name: "a built-in name of its own family in another case", dialect: "mysql", custom: "MariaDB"},
		{name: "a name of its own", dialect: "clickhouse", custom: "warehouse"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, err := dialects.New(testCase.dialect, dialects.Options{Name: testCase.custom})

			if testCase.wantDescribed == "" {
				require.NoError(t, err)
				require.Equal(t, testCase.custom, built.Info().Name)
				return
			}
			require.Nil(t, built)
			require.ErrorIs(t, err, dialects.ErrInvalidOption)
			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			refused := dialects.OptionErrors(err)
			require.Len(t, refused, 1)
			require.Equal(t, "Name", refused[0].Option)
			require.Equal(t, testCase.custom, refused[0].Text)
			require.Equal(t, testCase.wantDescribed, refused[0].Describe("name"))
		})
	}
}

func TestOptionErrorsFindsNoneInOtherErrors(t *testing.T) {
	t.Parallel()

	_, err := dialects.New("oracle", dialects.Options{})
	require.Empty(t, dialects.OptionErrors(err))
	require.Empty(t, dialects.OptionErrors(nil))
}

func snakeCase(name string) string {
	words := map[string]string{
		"MaxParseDepth": "max_parse_depth", "MaxTokensPerStatement": "max_tokens_per_statement", "MaxTypeParseDepth": "max_type_parse_depth",
		"Returning": "returning", "JoinUseNulls": "join_use_nulls", "GroupByUseNulls": "group_by_use_nulls", "ANSIQuotes": "ansi_quotes",
		"SearchPath": "search_path", "Name": "name",
	}
	return words[name]
}
