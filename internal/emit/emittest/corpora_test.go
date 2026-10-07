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
	"bytes"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/logging"
)

func TestReadCorpusSpecPicksTheCasesToGenerate(t *testing.T) {
	t.Parallel()

	cases := fstest.MapFS{
		"plain/testspec.json":          {Data: []byte(`{"description": "plain"}`)},
		"plain/queries":                {Mode: fs.ModeDir},
		"failing/testspec.json":        {Data: []byte(`{"shouldError": true}`)},
		"failing/queries":              {Mode: fs.ModeDir},
		"skipped/testspec.json":        {Data: []byte(`{"skip": true}`)},
		"skipped/queries":              {Mode: fs.ModeDir},
		"migrationsonly/testspec.json": {Data: []byte(`{}`)},
		"unspecified/queries/q.sql":    {Data: []byte("SELECT 1;")},
		"functions/testspec.json":      {Data: []byte(`{"customFunctions": [{"name": "score", "returns": "integer"}]}`)},
		"functions/queries/q.sql":      {Data: []byte("SELECT 1;")},
		"loose.sql":                    {Data: []byte("SELECT 1;")},
	}
	entries, err := fs.ReadDir(cases, ".")
	require.NoError(t, err)

	var picked []string
	for _, entry := range entries {
		if specification, ok := readCorpusSpec(cases, entry); ok {
			picked = append(picked, entry.Name())
			if entry.Name() == "functions" {
				require.Len(t, specification.CustomFunctions, 1)
			}
		}
	}

	require.Equal(t, []string{"functions", "plain"}, picked)
}

func TestCorpusChecksDoNotApplyWithoutCorpora(t *testing.T) {
	t.Parallel()

	current := &subject{suite: &Suite{}, corpora: &corpusRun{}}

	require.NotEmpty(t, checkCorpusGenerates(t.Context(), current).NotApplicable)
	require.NotEmpty(t, checkCorpusIsDeterministic(t.Context(), current).NotApplicable)
	require.NotEmpty(t, corpusCheck(func(string, *checkedPackage, *subject) []string { return nil })(t.Context(), current).NotApplicable)
}

func TestCorpusGenerationsRecordAPanicAsTheCasesError(t *testing.T) {
	t.Parallel()

	cases := fstest.MapFS{
		"plain/testspec.json":           {Data: []byte(`{}`)},
		"plain/migrations/001_init.sql": {Data: []byte("CREATE TABLE items (id integer PRIMARY KEY);\n")},
		"plain/queries/items.sql":       {Data: []byte("-- sgl.query name=CountItems command=one\nSELECT count(*) FROM items;\n")},
	}
	testCases := []struct {
		name     string
		emitter  func(*dialect.Facts) emit.EmitterPort
		expected []string
	}{
		{
			name:    "a panicking emitter constructor fails the case",
			emitter: func(*dialect.Facts) emit.EmitterPort { panic("corpus boom") },
			expected: []string{"corpus 0 case plain: Generate failed instead of reporting a diagnostic: " +
				"generating the case: panic: corpus boom"},
		},
		{name: "a working emitter generates the case", emitter: newSilentEmitter, expected: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			suite := &Suite{Name: "corpus", New: testCase.emitter, Corpora: []Corpus{{Cases: cases, Dialect: "postgres"}}}
			current := newSubject(suite, postgres.New(), &runtimeLane{})
			var output bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&output, nil)))

			result := checkCorpusGenerates(ctx, current)

			require.Empty(t, result.NotApplicable)
			require.Equal(t, testCase.expected, result.Problems)
			require.Equal(t, 2*len(testCase.expected), strings.Count(output.String(), "recovered a panic"),
				"each of the case's two generations logs its panic once")
		})
	}
}
