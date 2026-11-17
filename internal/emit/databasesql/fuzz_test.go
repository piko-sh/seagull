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

package databasesql_test

import (
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/emittest"
)

func FuzzEmit(f *testing.F) {
	engines := []string{"postgres", "cockroachdb", "timescaledb", "mysql", "mariadb", "sqlite", "duckdb", "clickhouse"}
	emittest.FuzzEmit(f, engines, func(facts *dialect.Facts, flags byte) emit.EmitterPort {
		if flags&emittest.NoPreparedFlag != 0 {
			return databasesql.New(databasesql.WithPrepared(false))
		}
		if flags&emittest.PreparedFlag != 0 && databasesql.PreparesStatements(facts, "") {
			return databasesql.New(databasesql.WithPrepared(true))
		}
		return databasesql.New()
	})
}
