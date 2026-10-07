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

package facade_test

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

const workspaceAuto = "GOWORK=auto"

func TestGeneratedRuntimePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated runtime fixtures")
	}
	for _, target := range []struct {
		emitter emit.EmitterPort
		name    string
	}{{name: "pgx", emitter: emit_driver_pgx.New()}, {name: "sql", emitter: emit_driver_databasesql.New()}} {
		t.Run(target.name, func(t *testing.T) {
			files := fstest.MapFS{
				"migrations/001_schema.up.sql": {Data: []byte(`CREATE TABLE users (id BIGINT NOT NULL, name TEXT NOT NULL, age INTEGER NOT NULL);
CREATE TABLE custom_values (id BIGINT NOT NULL);
CREATE TABLE orders (id BIGINT NOT NULL, customer TEXT NOT NULL, total INTEGER NOT NULL);
CREATE TABLE order_items (id BIGINT NOT NULL, order_id BIGINT NOT NULL, product TEXT NOT NULL, quantity INTEGER NOT NULL);`)},
				"queries/users.sql": {Data: []byte(`-- sgl.query(ListUsers, many)
SELECT id, name, age FROM users;
-- sgl.query(StreamUsers, stream)
SELECT id, name, age FROM users;
-- sgl.query(SearchUsers, many, dynamic: runtime)
SELECT id, name, age FROM users;
-- sgl.query(GetUser, one)
SELECT id, name, age FROM users WHERE id = $1;
-- sgl.query(DeleteUser, exec)
DELETE FROM users WHERE id = $1;
-- sgl.query(CopyUsers, copyfrom)
INSERT INTO users(id, name, age) VALUES ($1, $2, $3);
-- sgl.query(BatchUsers, batch)
INSERT INTO users(id, name, age) VALUES ($1, $2, $3);
-- sgl.query(ListCustom, many)
SELECT id FROM custom_values;
-- sgl.query(name: ListOrdersWithItems, command: many, group_by: orders.id)
-- sgl.embed(orders, from: o)
-- sgl.embed(order_items, from: i)
SELECT o.id, o.customer, o.total, i.id, i.product, i.quantity FROM orders o LEFT JOIN order_items i ON i.order_id = o.id;
-- sgl.query(ListOrderRows, many)
-- sgl.embed(orders, from: o)
-- sgl.embed(order_items, from: i)
SELECT o.id, o.customer, o.total, i.id, i.product, i.quantity FROM orders o LEFT JOIN order_items i ON i.order_id = o.id;
-- sgl.query(StreamOrderRows, stream)
-- sgl.embed(orders, from: o)
-- sgl.embed(order_items, from: i)
SELECT o.id, o.customer, o.total, i.id, i.product, i.quantity FROM orders o LEFT JOIN order_items i ON i.order_id = o.id;

`)},
			}
			generator, err := seagull.New(dialect_engine_postgres.New(), seagull.WithEmitter(target.emitter), seagull.WithTypeOverrides(seagull.TypeOverride{Column: "custom_values.id", GoName: "SelfReferencing"}))
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), seagull.Input{Migrations: files, Queries: files, MigrationDir: "migrations", QueryDir: "queries", Package: "store"})
			require.NoError(t, err)
			require.NoError(t, result.Err())
			directory := t.TempDir()
			arguments := []string{"test", "-count=1"}
			if build, ok := debug.ReadBuildInfo(); ok {
				for _, setting := range build.Settings {
					if setting.Key == "-race" && setting.Value == "true" {
						arguments = append(arguments, "-race")
					}
				}
			}
			if os.Getenv("SGL_PERFORMANCE_BENCH") != "" {
				arguments = append(arguments, "-bench=.", "-benchmem", "-benchtime=250ms")
			}
			for _, file := range result.Files {
				name := filepath.Join(directory, file.Name)
				require.NoError(t, os.WriteFile(name, file.Content, 0o600))
				arguments = append(arguments, name)
			}
			fixture, err := os.ReadFile(filepath.Join("testdata", "performance", target.name+"_test.go.txt"))
			require.NoError(t, err)
			name := filepath.Join(directory, "runtime_test.go")
			require.NoError(t, os.WriteFile(name, fixture, 0o600))
			arguments = append(arguments, name)
			output, err := testkit.GoCommand(t, "", []string{workspaceAuto}, arguments...).CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Logf("%s", output)
		})
	}
}
