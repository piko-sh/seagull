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

package intake

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const logic = `statement ok
CREATE TABLE t1(a INTEGER, b TEXT)

statement ok
INSERT INTO t1 VALUES(1, 'x')

statement error
CREATE TABLE broken(

query IT rowsort
SELECT a, b FROM t1
----
1
x

onlyif sqlite
query I nosort
SELECT typeof(a) FROM t1
----
integer

skipif postgresql
query I nosort
SELECT a FROM t1 WHERE b = 'x'
----
1
`

func writeManifest(t *testing.T, directory string, content string) string {
	t.Helper()

	path := filepath.Join(directory, "manifest.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestRunAdmitsSeedsAndRefusesUnsafeFiles(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	source := filepath.Join(work, "suite")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "nested"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "select1.test"), []byte(logic), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "nested", "copy.test"), []byte(logic), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "huge.test"), []byte(strings.Repeat("x", maxFileBytes+1)), 0o600))
	outside := filepath.Join(work, "outside.test")
	require.NoError(t, os.WriteFile(outside, []byte(logic), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(source, "escape.test")))
	require.NoError(t, os.Symlink(work, filepath.Join(source, "linked-directory")))
	hasFIFO := makeFIFO(t, filepath.Join(source, "pipe.test"))
	manifestPath := writeManifest(t, work, `{"name": "fixture", "licence": "public domain", "revision": "abc123",
		"upstream": "https://example.invalid/fixture", "format": "sqllogictest",
		"dialects": ["sqlite", "postgres"], "aliases": {"postgres": ["postgresql"]}}`)
	manifest, err := ReadManifest(manifestPath)
	require.NoError(t, err)
	output := filepath.Join(work, "out")

	report, err := Run(source, manifest, output)

	require.NoError(t, err)
	var record map[string]any
	content, err := os.ReadFile(filepath.Join(output, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &record))
	reasons := map[string]string{}
	for _, rejected := range report.Rejected {
		reasons[rejected.Path] = rejected.Reason
	}
	wantReasons := map[string]string{
		"escape.test": "a link or a path outside the source",
		"huge.test":   "larger than the file bound",
	}
	if hasFIFO {
		wantReasons["pipe.test"] = "not a regular file"
	}
	require.Equal(t, wantReasons, reasons)
	require.Zero(t, report.BeyondFileLimit)
	require.Len(t, report.Admitted, 2)
	require.Equal(t, "select1.test", report.Admitted[0].Path)
	require.Equal(t, "nested/copy.test", report.Admitted[1].Path)
	require.Equal(t, 3, report.Seeds)
	require.Equal(t, 3, report.Duplicates)
	require.Equal(t, "public domain", record["licence"])
	require.Equal(t, "abc123", record["revision"])
	require.InDelta(t, 3, record["seeds"], 0)

	file, err := os.Open(filepath.Join(output, "seeds.jsonl"))
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	var seeds []seed
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line seed
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		seeds = append(seeds, line)
	}
	require.Len(t, seeds, 3)
	require.Equal(t, "CREATE TABLE t1(a INTEGER, b TEXT);\n", seeds[0].Migration, "only schema statements form the migration")
	require.Equal(t, "-- sgl.query(External, many)\nSELECT a, b FROM t1;\n", seeds[0].Query)
	require.Equal(t, []string{"sqlite", "postgres"}, seeds[0].Dialects)
	require.Equal(t, origin{Path: "select1.test", Line: 11}, seeds[0].Origin)
	require.Equal(t, []string{"sqlite"}, seeds[1].Dialects, "onlyif keeps only the named engine")
	require.Equal(t, []string{"sqlite"}, seeds[2].Dialects, "skipif drops the named engine, through its alias")
	require.FileExists(t, filepath.Join(output, "originals", "select1.test"), "originals are kept")

	_, err = Run(source, manifest, output)
	require.ErrorIs(t, err, ErrOutputExists, "an existing output is refused")
}

func TestRunSplitsPlainSQLFiles(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	source := filepath.Join(work, "suite")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "a.sql"), []byte("CREATE TABLE t (a int);\n\n  SELECT a FROM t;\nDELETE FROM t;"), 0o600))
	manifest, err := ReadManifest(writeManifest(t, work, `{"name": "plain", "licence": "MIT", "revision": "1",
		"upstream": "u", "format": "sql", "dialects": ["sqlite"]}`))
	require.NoError(t, err)

	report, err := Run(source, manifest, filepath.Join(work, "out"))

	require.NoError(t, err)
	require.Equal(t, 1, report.Seeds)
	content, err := os.ReadFile(filepath.Join(work, "out", "seeds.jsonl"))
	require.NoError(t, err)
	var line seed
	require.NoError(t, json.Unmarshal(content, &line))
	require.Equal(t, "CREATE TABLE t (a int);\n", line.Migration)
	require.Equal(t, 3, line.Origin.Line)
}

func TestReadManifestRefusesIncompleteManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "not JSON", content: `{`, want: "not readable JSON"},
		{name: "not an object", content: `[]`, want: "name must be"},
		{name: "a bad name", content: `{"name": "Bad Name"}`, want: "name must be"},
		{name: "missing fields", content: `{"name": "a", "licence": "", "dialects": []}`, want: "lacks licence, revision, upstream, format, dialects"},
		{
			name:    "an unknown format",
			content: `{"name": "a", "licence": "l", "revision": "r", "upstream": "u", "format": "csv", "dialects": ["sqlite"]}`,
			want:    "format must be",
		},
		{
			name:    "dialects that are not names",
			content: `{"name": "a", "licence": "l", "revision": "r", "upstream": "u", "format": "sql", "dialects": [1]}`,
			want:    "dialects must be",
		},
		{
			name:    "malformed aliases",
			content: `{"name": "a", "licence": "l", "revision": "r", "upstream": "u", "format": "sql", "dialects": ["sqlite"], "aliases": {"sqlite": "x"}}`,
			want:    "aliases must map",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := ReadManifest(writeManifest(t, t.TempDir(), testCase.content))
			require.ErrorIs(t, err, ErrManifest)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}
