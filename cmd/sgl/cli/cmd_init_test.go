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
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func TestInit(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	result := project.Run("init")
	requireCode(t, result, 0)
	assert.Contains(t, project.Read("sgl.yaml"), "dialect: postgres")
	assert.Contains(t, project.Read("db/queries/example.sql"), "WHERE id = $1;")
	assert.Contains(t, result.Stdout, "created db/migrations/0001_init.up.sql")

	result = project.Run("init", "--dialect", "sqlite")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "sgl.yaml already exists")

	for _, name := range []string{"mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		dialectProject := clitest.NewProject(t)
		requireCode(t, dialectProject.Run("init", "--dialect", name), 0)
		vet := dialectProject.Run("vet", "--strict")
		requireCode(t, vet, 0)
	}

	requireCode(t, clitest.NewProject(t).Run("init", "--dialect", "oracle"), 2)
	requireCode(t, clitest.NewProject(t).Run("init", "extra"), 2)

	custom := clitest.NewProject(t)
	result = custom.RunWith(t.Context(), []cli.Option{
		cli.WithDialect("exotic", func(dialect_registry.Options) (dialect.Dialect, error) {
			return exoticFamilyDialect(), nil
		}),
	}, "init", "--dialect", "exotic")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no starter files")
}
