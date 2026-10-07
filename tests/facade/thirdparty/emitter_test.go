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

package thirdparty_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_testkit"
	"seagull.sh/seagull/tests/facade/thirdparty"
)

func TestTheThirdPartyEmitterConforms(t *testing.T) {
	t.Parallel()

	emit_testkit.Run(t, emit_testkit.Suite{
		Name:          "thirdparty",
		New:           func(*dialect.Facts) emit.EmitterPort { return thirdparty.New() },
		Dialects:      []dialect.Dialect{dialect_engine_sqlite.New()},
		Harness:       emit_testkit.DatabaseSQL(),
		Corpora:       sqliteCorpora(t),
		KnownFailures: map[string]string{},
	})
}

func sqliteTestdata() []string {
	directories := []string{"internal/dialect/sqlite/testdata", "tests/integration/sqlite/testdata"}
	roots := make([]string, 0, len(directories))
	for _, directory := range directories {
		roots = append(roots, filepath.Join("..", "..", "..", filepath.FromSlash(directory)))
	}
	return roots
}

func sqliteCorpora(t *testing.T) []emit_testkit.Corpus {
	t.Helper()

	roots := sqliteTestdata()
	corpora := make([]emit_testkit.Corpus, 0, len(roots))
	for _, root := range roots {
		cases, err := filepath.Glob(filepath.Join(root, "*", "testspec.json"))
		require.NoError(t, err)
		require.NotEmpty(t, cases, "%s holds no case", root)
		corpora = append(corpora, emit_testkit.Corpus{Cases: os.DirFS(root), Dialect: "sqlite"})
	}
	return corpora
}
