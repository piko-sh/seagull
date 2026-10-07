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

package emittest

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestTypeCheckReportsRealErrorsOnly(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("type-checking loads the standard library's export data through the go command; skipped under -short")
	}

	testCases := []struct {
		name           string
		source         string
		expectProblems bool
		expectExternal []string
	}{
		{
			name:           "a selector into a stand-in package is not a problem",
			source:         "package store\n\nimport \"github.com/example/thing/v2\"\n\nvar Value thing.Kind\n",
			expectProblems: false,
			expectExternal: []string{"github.com/example/thing/v2"},
		},
		{
			name:           "an aliased stand-in is not a problem either",
			source:         "package store\n\nimport other \"example.com/driver-go\"\n\nvar Value other.Kind\n",
			expectProblems: false,
			expectExternal: []string{"example.com/driver-go"},
		},
		{
			name:           "a package of a local module without a dot is a stand-in",
			source:         "package store\n\nimport \"case_runner/kinds\"\n\nvar Value kinds.Label\n",
			expectProblems: false,
			expectExternal: []string{"case_runner/kinds"},
		},
		{
			name:           "a standard library path that does not exist is a problem",
			source:         "package store\n\nimport \"stringz\"\n\nvar Value = stringz.ToUpper(\"x\")\n",
			expectProblems: true,
			expectExternal: nil,
		},
		{
			name:           "a standard library misuse is a problem",
			source:         "package store\n\nimport \"strings\"\n\nvar Value int = strings.ToUpper(\"x\")\n",
			expectProblems: true,
			expectExternal: nil,
		},
		{
			name:           "a redeclaration is a problem",
			source:         "package store\n\ntype OrderDirection string\n\ntype OrderDirection string\n",
			expectProblems: true,
			expectExternal: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			checked := typeCheck([]sqlmodel.GeneratedFile{
				{Name: "value.go", Content: []byte(testCase.source)},
				{Name: "schema.sql", Content: []byte("not go")},
			})
			require.Equal(t, testCase.expectProblems, len(checked.problems) > 0, "problems: %v", checked.problems)
			require.ElementsMatch(t, testCase.expectExternal, slices.Sorted(maps.Keys(checked.external)))
		})
	}
}

func TestStubPackageNameFollowsTheImportPath(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"github.com/jackc/pgx/v5":                           "pgx",
		"github.com/jackc/pgx/v5/pgconn":                    "pgconn",
		"github.com/ClickHouse/clickhouse-go/v2":            "clickhouse",
		"github.com/google/uuid":                            "uuid",
		"github.com/ClickHouse/clickhouse-go/v2/lib/driver": "driver",
	}
	for importPath, expected := range testCases {
		require.Equal(t, expected, stubPackageName(importPath), importPath)
	}
}

func TestIsStandardLibraryReadsTheFirstElement(t *testing.T) {
	t.Parallel()

	require.True(t, isStandardLibrary("database/sql/driver"))
	require.True(t, isStandardLibrary("context"))
	require.False(t, isStandardLibrary("github.com/jackc/pgx/v5"))
	require.False(t, isStandardLibrary("golang.org/x/tools/go/packages"))
}
