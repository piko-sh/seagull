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

package sqltemplate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/sqltemplate"
)

type filteredCase struct {
	engine        dialect.DialectPort
	configure     func(input *sqltemplate.Input)
	name          string
	sql           string
	template      string
	count         string
	filtered      string
	filteredCount string
}

func resultFiltered(names ...string) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) {
		input.Query.DynamicRuntime = true
		input.ItemsWrapCount = true
		for _, name := range names {
			input.Query.OutputColumns = append(input.Query.OutputColumns, sqlmodel.OutputColumn{Name: name})
		}
	}
}

func TestBuildWrapsAQueryWhoseConditionsFilterItsResultRows(t *testing.T) {
	t.Parallel()

	testCases := []filteredCase{
		{
			name:     "a window query keeps its own WHERE and ORDER BY inside the derived table",
			engine:   postgres.New(),
			sql:      "SELECT i.id, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = $1 ORDER BY i.id",
			template: "SELECT i.id, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = ⟨p0⟩ ⟨order:ORDER BY i.id⟩⟨pagination⟩⟨arguments p0⟩",
			count:    "SELECT COUNT(*) FROM (SELECT i.id, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = ⟨p0⟩) sub⟨arguments p0⟩",
			filtered: "SELECT * FROM (SELECT i.id, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = ⟨p0⟩ ORDER BY i.id) AS result_rows" +
				"⟨where⟩⟨order⟩⟨pagination⟩⟨arguments p0⟩",
			filteredCount: "SELECT COUNT(*) FROM (SELECT i.id, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = ⟨p0⟩ ORDER BY i.id) AS result_rows" +
				"⟨where⟩⟨arguments p0⟩",
			configure: resultFiltered("id", "rn"),
		},
		{
			name:          "an aggregate query keeps its statement and terminator",
			engine:        postgres.New(),
			sql:           "SELECT kind, count(*) AS n FROM items GROUP BY kind;",
			template:      "SELECT kind, count(*) AS n FROM items GROUP BY kind⟨order⟩⟨pagination⟩;",
			count:         "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind) sub",
			filtered:      "SELECT * FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩;",
			filteredCount: "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind) AS result_rows⟨where⟩",
			configure:     resultFiltered("kind", "n"),
		},
		{
			name:     "MySQL limits an ordered derived table so it keeps the order",
			engine:   mysql.New(),
			sql:      "SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC FOR UPDATE",
			template: "SELECT kind, count(*) AS n FROM items GROUP BY kind ⟨order:ORDER BY n DESC⟩⟨pagination⟩ FOR UPDATE",
			count:    "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind) sub",
			filtered: "SELECT * FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC LIMIT 18446744073709551615 FOR UPDATE) AS result_rows" +
				"⟨where⟩⟨order⟩⟨pagination⟩",
			filteredCount: "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC FOR UPDATE) AS result_rows⟨where⟩",
			configure:     resultFiltered("kind", "n"),
		},
		{
			name:          "ClickHouse keeps its trailing clauses inside the derived table",
			engine:        clickhouse.New(),
			sql:           "SELECT kind, count() AS n FROM items GROUP BY kind SETTINGS max_threads = 1",
			template:      "SELECT kind, count() AS n FROM items GROUP BY kind⟨order⟩⟨pagination⟩ SETTINGS max_threads = 1",
			count:         "SELECT COUNT(*) FROM (SELECT kind, count() AS n FROM items GROUP BY kind) sub SETTINGS max_threads = 1",
			filtered:      "SELECT * FROM (SELECT kind, count() AS n FROM items GROUP BY kind SETTINGS max_threads = 1) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			filteredCount: "SELECT COUNT(*) FROM (SELECT kind, count() AS n FROM items GROUP BY kind SETTINGS max_threads = 1) AS result_rows⟨where⟩",
			configure:     resultFiltered("kind", "n"),
		},
		{
			name:      "output names that collide leave no filtered form and no condition",
			engine:    postgres.New(),
			sql:       "SELECT a.id, b.id, count(*) AS n FROM a JOIN b ON b.a = a.id GROUP BY a.id, b.id",
			template:  "SELECT a.id, b.id, count(*) AS n FROM a JOIN b ON b.a = a.id GROUP BY a.id, b.id⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT a.id, b.id, count(*) AS n FROM a JOIN b ON b.a = a.id GROUP BY a.id, b.id) sub",
			configure: resultFiltered("id", "id", "n"),
		},
		{
			name:      "a compound query keeps its own handling",
			engine:    postgres.New(),
			sql:       "SELECT kind, count(*) AS n FROM a GROUP BY kind UNION ALL SELECT kind, count(*) AS n FROM b GROUP BY kind",
			template:  "SELECT kind, count(*) AS n FROM a GROUP BY kind UNION ALL SELECT kind, count(*) AS n FROM b GROUP BY kind⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM a GROUP BY kind UNION ALL SELECT kind, count(*) AS n FROM b GROUP BY kind) sub",
			configure: resultFiltered("kind", "n"),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, problems := buildTemplate(t, testCase.engine, testCase.sql, testCase.configure)

			require.Empty(t, problems)
			require.Equal(t, testCase.template, compact(t, &built.Template))
			require.Equal(t, testCase.count, compact(t, built.CountTemplate))
			require.Equal(t, testCase.filtered, compact(t, built.FilteredTemplate))
			require.Equal(t, testCase.filteredCount, compact(t, built.FilteredCountTemplate))
		})
	}
}

func runtimeColumns(names ...string) func(input *sqltemplate.Input) {
	return func(input *sqltemplate.Input) {
		input.Query.DynamicRuntime = true
		for _, name := range names {
			input.Query.OutputColumns = append(input.Query.OutputColumns, sqlmodel.OutputColumn{Name: name})
		}
	}
}

type pickedCase struct {
	engine    dialect.DialectPort
	configure func(input *sqltemplate.Input)
	name      string
	sql       string
	template  string
	count     string
	problems  []string
	reads     bool
}

func TestBuildWrapsAQueryThatPicksItsOwnRows(t *testing.T) {
	t.Parallel()

	testCases := []pickedCase{
		{
			name:      "a plain query with its own LIMIT is read and counted as the rows it returns",
			engine:    postgres.New(),
			sql:       "SELECT id, region, score FROM players WHERE score > $1 ORDER BY score DESC LIMIT 10",
			template:  "SELECT * FROM (SELECT id, region, score FROM players WHERE score > ⟨p0⟩ ORDER BY score DESC LIMIT 10) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, region, score FROM players WHERE score > ⟨p0⟩ ORDER BY score DESC LIMIT 10) AS result_rows⟨where⟩⟨arguments p0⟩",
			reads:     true,
			configure: runtimeColumns("id", "region", "score"),
		},
		{
			name:      "OFFSET and FETCH stay inside the derived table",
			engine:    postgres.New(),
			sql:       "SELECT id FROM players ORDER BY id OFFSET 5 ROWS FETCH FIRST 10 ROWS ONLY",
			template:  "SELECT * FROM (SELECT id FROM players ORDER BY id OFFSET 5 ROWS FETCH FIRST 10 ROWS ONLY) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id FROM players ORDER BY id OFFSET 5 ROWS FETCH FIRST 10 ROWS ONLY) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id"),
		},
		{
			name:      "the statement's terminator follows the places of the builder's clauses",
			engine:    postgres.New(),
			sql:       "SELECT id FROM players ORDER BY id LIMIT 10;",
			template:  "SELECT * FROM (SELECT id FROM players ORDER BY id LIMIT 10) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩;",
			count:     "SELECT COUNT(*) FROM (SELECT id FROM players ORDER BY id LIMIT 10) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id"),
		},
		{
			name:      "a MySQL limit with an offset and a parameter needs no limit of its own to keep the order",
			engine:    mysql.New(),
			sql:       "SELECT id, region FROM players ORDER BY id LIMIT 5, ?",
			template:  "SELECT * FROM (SELECT id, region FROM players ORDER BY id LIMIT 5, ⟨p0⟩) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, region FROM players ORDER BY id LIMIT 5, ⟨p0⟩) AS result_rows⟨where⟩⟨arguments p0⟩",
			reads:     true,
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "MySQL keeps an aggregate query's own limit inside the derived table",
			engine:    mysql.New(),
			sql:       "SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC LIMIT 3",
			template:  "SELECT * FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC LIMIT 3) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT kind, count(*) AS n FROM items GROUP BY kind ORDER BY n DESC LIMIT 3) AS result_rows⟨where⟩",
			reads:     true,
			configure: resultFiltered("kind", "n"),
		},
		{
			name:      "ClickHouse LIMIT n BY",
			engine:    clickhouse.New(),
			sql:       "SELECT id, region FROM players ORDER BY score DESC LIMIT 1 BY region",
			template:  "SELECT * FROM (SELECT id, region FROM players ORDER BY score DESC LIMIT 1 BY region) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, region FROM players ORDER BY score DESC LIMIT 1 BY region) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "ClickHouse LIMIT n BY and a LIMIT keep their trailing clauses inside the derived table",
			engine:    clickhouse.New(),
			sql:       "SELECT id, kind FROM items ORDER BY id LIMIT 2 BY kind LIMIT 10 SETTINGS max_threads = 1",
			template:  "SELECT * FROM (SELECT id, kind FROM items ORDER BY id LIMIT 2 BY kind LIMIT 10 SETTINGS max_threads = 1) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, kind FROM items ORDER BY id LIMIT 2 BY kind LIMIT 10 SETTINGS max_threads = 1) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id", "kind"),
		},
		{
			name:      "DuckDB QUALIFY",
			engine:    duckdb.New(),
			sql:       "SELECT id, region FROM players QUALIFY row_number() OVER (PARTITION BY region ORDER BY score DESC) = 1",
			template:  "SELECT * FROM (SELECT id, region FROM players QUALIFY row_number() OVER (PARTITION BY region ORDER BY score DESC) = 1) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT id, region FROM players QUALIFY row_number() OVER (PARTITION BY region ORDER BY score DESC) = 1) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "PostgreSQL DISTINCT ON keeps its own WHERE and the row its order picks",
			engine:    postgres.New(),
			sql:       "SELECT DISTINCT ON (region) id, region FROM players WHERE score > $1 ORDER BY region, score DESC",
			template:  "SELECT * FROM (SELECT DISTINCT ON (region) id, region FROM players WHERE score > ⟨p0⟩ ORDER BY region, score DESC) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM (SELECT DISTINCT ON (region) id, region FROM players WHERE score > ⟨p0⟩ ORDER BY region, score DESC) AS result_rows⟨where⟩⟨arguments p0⟩",
			reads:     true,
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "DuckDB DISTINCT ON",
			engine:    duckdb.New(),
			sql:       "SELECT DISTINCT ON (region) id, region FROM players ORDER BY region, score DESC",
			template:  "SELECT * FROM (SELECT DISTINCT ON (region) id, region FROM players ORDER BY region, score DESC) AS result_rows⟨where⟩⟨order⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT DISTINCT ON (region) id, region FROM players ORDER BY region, score DESC) AS result_rows⟨where⟩",
			reads:     true,
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "a plain DISTINCT picks no rows, so its conditions join its own WHERE",
			engine:    postgres.New(),
			sql:       "SELECT DISTINCT region FROM players WHERE score > $1 ORDER BY region",
			template:  "SELECT DISTINCT region FROM players WHERE ⟨where:score > ⟨p0⟩⟩ ⟨order:ORDER BY region⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM (SELECT DISTINCT region FROM players WHERE ⟨where:score > ⟨p0⟩⟩) sub⟨arguments p0⟩",
			configure: runtimeColumns("region"),
		},
		{
			name:      "output names that collide are reported as an error, since the derived table is read by name",
			engine:    postgres.New(),
			sql:       "SELECT a.id, b.id FROM a JOIN b ON b.a = a.id ORDER BY a.id LIMIT 10",
			template:  "SELECT a.id, b.id FROM a JOIN b ON b.a = a.id ⟨order:ORDER BY a.id⟩ ⟨pagination:LIMIT 10⟩",
			count:     "SELECT COUNT(*) FROM a JOIN b ON b.a = a.id",
			problems:  []string{diag.CodeRuntimeBuilderRowNames},
			configure: runtimeColumns("id", "id"),
		},
		{
			name:      "an output column without a name is reported",
			engine:    postgres.New(),
			sql:       "SELECT DISTINCT ON (region) region, score + 1 FROM players ORDER BY region",
			template:  "SELECT DISTINCT ON (region) region, score + 1 FROM players ⟨order:ORDER BY region⟩⟨pagination⟩",
			count:     "SELECT COUNT(*) FROM (SELECT DISTINCT ON (region) region, score + 1 FROM players) sub",
			problems:  []string{diag.CodeRuntimeBuilderRowNames},
			configure: runtimeColumns("region", ""),
		},
		{
			name:      "a query without pagination keeps its conditions in its own WHERE",
			engine:    postgres.New(),
			sql:       "SELECT id, region FROM players WHERE score > $1 ORDER BY id",
			template:  "SELECT id, region FROM players WHERE ⟨where:score > ⟨p0⟩⟩ ⟨order:ORDER BY id⟩⟨pagination⟩⟨arguments p0⟩",
			count:     "SELECT COUNT(*) FROM players WHERE ⟨where:score > ⟨p0⟩⟩⟨arguments p0⟩",
			configure: runtimeColumns("id", "region"),
		},
		{
			name:      "a dynamic query that is not a runtime builder keeps its place for a condition",
			engine:    postgres.New(),
			sql:       "SELECT id FROM players ORDER BY id LIMIT 10",
			template:  "SELECT id FROM players⟨where⟩ ⟨order:ORDER BY id⟩ ⟨pagination:LIMIT 10⟩",
			configure: func(input *sqltemplate.Input) { input.Query.IsDynamic = true },
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built, problems := buildTemplate(t, testCase.engine, testCase.sql, testCase.configure)

			require.Equal(t, testCase.problems, nilIfEmpty(codesOf(problems)), "%v", problems)
			require.Equal(t, testCase.template, compact(t, &built.Template))
			require.Equal(t, testCase.count, compact(t, built.CountTemplate))
			require.Equal(t, testCase.reads, built.ReadsResultRows)
			if testCase.reads {
				require.True(t, built.CountWrapped)
			}
			require.Nil(t, built.FilteredTemplate)
			require.Nil(t, built.FilteredCountTemplate)
		})
	}
}
