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

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func TestIntrospectionFamilies(t *testing.T) {
	t.Parallel()

	assert.True(t, hasIntrospection(dialect_engine_postgres.Family))
	assert.True(t, hasIntrospection(dialect_engine_sqlite.Family))
	assert.False(t, hasIntrospection(dialect_engine_duckdb.Family), "DuckDB has no introspection provider")
}

func TestPackageImportPathFollowsTheNearestModule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "store"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("// the application\nmodule \"example.com/app\" // with a comment\n\ngo 1.27\n"), 0o600))
	nested := filepath.Join(root, "tools")
	require.NoError(t, os.MkdirAll(filepath.Join(nested, "gen"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/app/tools\n"), 0o600))

	require.Equal(t, "example.com/app/internal/store", packageImportPath(filepath.Join(root, "internal", "store")))
	require.Equal(t, "example.com/app", packageImportPath(root))
	require.Equal(t, "example.com/app/tools/gen", packageImportPath(filepath.Join(nested, "gen")))
	require.Equal(t, "example.com/app/missing", packageImportPath(filepath.Join(root, "missing")))
}

func TestDeclaredModuleReadsTheModuleLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "plain", content: "module example.com/app\n", want: "example.com/app"},
		{name: "quoted", content: "module \"example.com/app\"\n", want: "example.com/app"},
		{name: "after a comment", content: "// module example.com/other\nmodule example.com/app // trailing\n", want: "example.com/app"},
		{name: "none", content: "go 1.27\n", want: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, declaredModule([]byte(testCase.content)))
		})
	}
}

func TestPackageImportPathReadsOnlyTheStartOfAGoMod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goMod string
		want  string
	}{
		{name: "a module directive at the start", goMod: "module example.com/app\n" + strings.Repeat("// padding\n", maxGoModPrefixBytes), want: "example.com/app"},
		{name: "a module directive past the part read", goMod: strings.Repeat("// padding\n", maxGoModPrefixBytes) + "module example.com/app\n", want: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(testCase.goMod), 0o600))

			require.Equal(t, testCase.want, packageImportPath(root))
		})
	}
}

func TestPackageImportPathSkipsAGoModThatIsNotAFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o600))
	nested := filepath.Join(root, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(nested, "go.mod"), 0o750))

	require.Equal(t, "example.com/app/nested", packageImportPath(nested))
}
