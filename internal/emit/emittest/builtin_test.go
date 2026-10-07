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

package emittest_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/emit"
	emitclickhouse "seagull.sh/seagull/internal/emit/clickhouse"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/emittest"
	"seagull.sh/seagull/internal/emit/pgx"
)

const clickHouseNamedStub = `package clickhouse

import "database/sql/driver"

func Named(name string, value any) driver.NamedValue {
	return driver.NamedValue{Name: name, Value: value}
}
`

var sharedKnownFailures = map[string]string{}

type builtinDialect struct {
	engine   dialect.DialectPort
	testdata []string
}

func builtinDialects() map[string]builtinDialect {
	return map[string]builtinDialect{
		"postgres":    {engine: postgres.New(), testdata: []string{"internal/dialect/postgres/testdata", "tests/integration/postgres/testdata"}},
		"cockroachdb": {engine: cockroachdb.New(), testdata: []string{"internal/dialect/cockroachdb/testdata", "tests/integration/cockroachdb/testdata"}},
		"timescaledb": {engine: timescaledb.New(), testdata: []string{"tests/integration/timescaledb/testdata"}},
		"mysql":       {engine: mysql.New(), testdata: []string{"internal/dialect/mysql/testdata", "tests/integration/mysql/testdata"}},
		"mariadb":     {engine: mariadb.New(), testdata: []string{"internal/dialect/mariadb/testdata", "tests/integration/mariadb/testdata"}},
		"sqlite":      {engine: sqlite.New(), testdata: []string{"internal/dialect/sqlite/testdata", "tests/integration/sqlite/testdata"}},
		"duckdb":      {engine: duckdb.New(), testdata: []string{"internal/dialect/duckdb/testdata", "tests/integration/duckdb/testdata"}},
		"clickhouse":  {engine: clickhouse.New(), testdata: []string{"internal/dialect/clickhouse/testdata", "tests/integration/clickhouse/testdata"}},
		"martian":     {engine: martian.New(), testdata: nil},
	}
}

func suiteDialects(t *testing.T, names ...string) ([]dialect.Dialect, []emittest.Corpus) {
	t.Helper()

	available := builtinDialects()
	dialects := make([]dialect.Dialect, 0, len(names))
	var corpora []emittest.Corpus
	for _, name := range names {
		chosen := available[name]
		dialects = append(dialects, dialect.Wrap(chosen.engine))
		for _, directory := range chosen.testdata {
			root := filepath.Join("..", "..", "..", filepath.FromSlash(directory))
			require.DirExists(t, root, "the %s corpus has moved; update builtinDialects", name)
			corpora = append(corpora, emittest.Corpus{Cases: os.DirFS(root), Dialect: name})
		}
	}
	return dialects, corpora
}

func databaseSQLHarness() *emittest.Harness {
	harness := emittest.DatabaseSQL()
	harness.Modules = map[string]fs.FS{
		"github.com/ClickHouse/clickhouse-go/v2": fstest.MapFS{"named.go": &fstest.MapFile{Data: []byte(clickHouseNamedStub)}},
	}
	return harness
}

func TestDatabaseSQLEmitterConforms(t *testing.T) {
	t.Parallel()

	dialects, corpora := suiteDialects(t, "postgres", "cockroachdb", "timescaledb", "mysql", "mariadb", "sqlite", "duckdb", "clickhouse", "martian")
	emittest.Run(t, emittest.Suite{
		Name:          "database/sql",
		New:           func(*dialect.Facts) emit.EmitterPort { return databasesql.New() },
		Dialects:      dialects,
		Corpora:       corpora,
		Harness:       databaseSQLHarness(),
		KnownFailures: sharedKnownFailures,
	})
}

func TestPgxEmitterConforms(t *testing.T) {
	t.Parallel()

	dialects, corpora := suiteDialects(t, "postgres", "cockroachdb", "timescaledb", "martian")
	emittest.Run(t, emittest.Suite{
		Name:          "pgx",
		New:           func(*dialect.Facts) emit.EmitterPort { return pgx.New() },
		Dialects:      dialects,
		Corpora:       corpora,
		Harness:       emittest.Pgx(),
		KnownFailures: sharedKnownFailures,
	})
}

func TestClickHouseEmitterConforms(t *testing.T) {
	t.Parallel()

	dialects, corpora := suiteDialects(t, "clickhouse", "martian")
	emittest.Run(t, emittest.Suite{
		Name:          "clickhouse",
		New:           func(*dialect.Facts) emit.EmitterPort { return emitclickhouse.New() },
		Dialects:      dialects,
		Corpora:       corpora,
		Harness:       emittest.ClickHouseNative(),
		KnownFailures: sharedKnownFailures,
	})
}
