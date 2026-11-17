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

package dialecttest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadExternalSeedsReadsEachLineAndSkipsBlankOnes(t *testing.T) {
	t.Parallel()

	content := `{"dialects":["sqlite"],"migration":"CREATE TABLE t (a INTEGER);\n","query":"-- sgl.query(External, many)\nSELECT a FROM t;\n"}` +
		"\n\n" + `{"dialects":["postgres","sqlite"],"migration":"","query":"SELECT 1;"}` + "\n"

	seeds, err := readExternalSeeds(strings.NewReader(content))

	require.NoError(t, err)
	require.Len(t, seeds, 2)
	assert.Equal(t, []string{"sqlite"}, seeds[0].Dialects)
	assert.Equal(t, "CREATE TABLE t (a INTEGER);\n", seeds[0].Migration)
	assert.Equal(t, "SELECT 1;", seeds[1].Query)
}

func TestReadExternalSeedsRefusesAMalformedLine(t *testing.T) {
	t.Parallel()

	_, err := readExternalSeeds(strings.NewReader("{\"dialects\":[\"sqlite\"]}\nnot json\n"))

	require.ErrorContains(t, err, "line 2", "a malformed seed fails rather than being dropped unnoticed")
}

func TestReadExternalSeedsRefusesTooManySeeds(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("{}\n", maxExternalSeeds+1)

	_, err := readExternalSeeds(strings.NewReader(content))

	require.ErrorIs(t, err, errTooManyExternalSeeds)
}

func TestReadExternalSeedsRefusesAnOverlongLine(t *testing.T) {
	t.Parallel()

	content := `{"query":"` + strings.Repeat("x", maxExternalSeedLine) + `"}`

	_, err := readExternalSeeds(strings.NewReader(content))

	require.Error(t, err)
}

func TestOpenExternalSeedsReadsASuiteFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "suite"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "suite", externalSeedsFile), []byte(`{"query":"SELECT 1;"}`), 0o600))
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	seeds, err := openExternalSeeds(root, "suite")

	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "SELECT 1;", seeds[0].Query)
}

func TestOpenExternalSeedsReportsAMissingFile(t *testing.T) {
	t.Parallel()

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	_, err = openExternalSeeds(root, "suite")

	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestModuleRootFindsTheGoModule(t *testing.T) {
	t.Parallel()

	root, err := moduleRoot()

	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(root, "go.mod"))
}
