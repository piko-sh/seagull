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

package dialecttest_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const parityMigration = `CREATE TABLE t (id bigint PRIMARY KEY, n bigint NOT NULL, small smallint NOT NULL,
	r real NOT NULL, d numeric NOT NULL, s text NOT NULL, ts timestamptz NOT NULL, tags bigint[] NOT NULL);`

const parityQuery = `-- sgl.query(name: SharedBuiltins, command: one)
SELECT SUM(n) AS sum_big, SUM(small) AS sum_small, SUM(r) AS sum_real, SUM(d) AS sum_numeric,
	SUM(n) OVER (ORDER BY id) AS running_sum, AVG(n) AS average, AVG(r) AS average_real,
	MIN(s) AS lowest, MAX(ts) AS latest, COUNT(*) AS total, array_agg(n) AS all_n,
	coalesce(s, 'x') AS label, lag(ts) OVER (ORDER BY id) AS previous_ts
FROM t
GROUP BY id;
`

type parityColumn struct {
	category   sqlmodel.SQLTypeCategory
	engineName string
	nullable   bool
}

type parityRecorder struct {
	queries []*sqlmodel.AnalysedQuery
}

func (recorder *parityRecorder) Emit(_ context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	recorder.queries = request.Queries
	return nil, nil
}

func parityColumns(t *testing.T, engine dialect.DialectPort) map[string]parityColumn {
	t.Helper()

	recorder := &parityRecorder{}
	generator, err := app.New(engine, app.WithEmitter(recorder))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Migrations: fstest.MapFS{
			"migrations/001_t.up.sql": &fstest.MapFile{Data: []byte(parityMigration)},
			"queries/shared.sql":      &fstest.MapFile{Data: []byte(parityQuery)},
		},
		Package:      "parity",
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	require.NoError(t, result.Err())
	require.Len(t, recorder.queries, 1)

	columns := make(map[string]parityColumn)
	for _, column := range recorder.queries[0].OutputColumns {
		columns[column.Name] = parityColumn{category: column.SQLType.Category, engineName: column.SQLType.EngineName, nullable: column.Nullable}
	}
	return columns
}

func TestPostgresFlavoursTypeSharedBuiltinsLikePostgres(t *testing.T) {
	t.Parallel()

	want := parityColumns(t, postgres.New())
	require.Equal(t, parityColumn{category: sqlmodel.TypeCategoryDecimal, engineName: "numeric", nullable: true}, want["sum_big"],
		"SUM over no rows is NULL, and SUM(bigint) is numeric")

	for name, newFlavour := range map[string]func(...postgres.Option) *postgres.Dialect{
		"timescaledb": timescaledb.New,
		"cockroachdb": cockroachdb.New,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, want, parityColumns(t, newFlavour()),
				"a flavour's own registrations must not shadow the base dialect's built-ins")
		})
	}
}
