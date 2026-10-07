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

package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/internal/config"
)

func TestValidateChecksOnlyTheGeneratedDatabase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		generated string
		message   string
	}{
		{name: "a valid generated database passes though another is invalid", generated: "snake", message: ""},
		{name: "an invalid generated database is refused", generated: "kebab", message: `go.json_tags.case "kebab" must be snake, camel or pascal`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			loaded, err := config.Parse([]byte("version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n  - name: other\n    dialect: sqlite\n    go:\n      out: other\n"), false)
			require.NoError(t, err)
			loaded.Databases[0].Go.JSONTags.Case = testCase.generated
			loaded.Databases[1].Go.JSONTags.Case = "kebab"
			resolved := &project{loaded: loaded, database: &loaded.Databases[0], file: "sgl.yaml", migrations: "", queries: "", warnings: nil, skipFailingQueries: false}

			err = resolved.validate()

			if testCase.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, config.ErrInvalid)
			require.ErrorContains(t, err, testCase.message)
			require.NotContains(t, err.Error(), `"other"`)
		})
	}
}

func TestSourcePathPrefersQueriesThenMigrations(t *testing.T) {
	t.Parallel()

	resolved := &project{loaded: nil, database: nil, file: "sgl.yaml", migrations: "schema", queries: "sql", warnings: nil, skipFailingQueries: false}
	files := map[string]string{
		"schema/0001_init.up.sql": "",
		"schema/shared.sql":       "",
		"sql/shared.sql":          "",
		"sql/authors.sql":         "",
	}
	testCases := []struct {
		name     string
		filename string
		want     string
	}{
		{name: "a migration", filename: "0001_init.up.sql", want: "schema/0001_init.up.sql"},
		{name: "a query file", filename: "authors.sql", want: "sql/authors.sql"},
		{name: "a name in both directories is the query file", filename: "shared.sql", want: "sql/shared.sql"},
		{name: "a file the request does not hold", filename: "missing.sql", want: "missing.sql"},
		{name: "no file name is the configuration file", filename: "", want: "sgl.yaml"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, resolved.sourcePath(files, testCase.filename))
		})
	}
}
