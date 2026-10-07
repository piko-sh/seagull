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

package syntax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/namespace"
)

const fuzzMaxDirectiveBytes = 2048

func addDirectiveSeeds(f *testing.F) {
	f.Helper()

	patterns := []string{
		filepath.Join("..", "..", "dialect", "*", "testdata", "*", "queries", "*.sql"),
		filepath.Join("..", "..", "dialect", "*", "testdata", "*", "migrations", "*.sql"),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		require.NoError(f, err, "globbing %s", pattern)
		require.NotEmpty(f, paths, "the seed pattern %s matched no file", pattern)
		for _, path := range paths {
			content, readErr := os.ReadFile(path)
			require.NoError(f, readErr, "reading %s", path)
			f.Add(string(content))
			for line := range strings.SplitSeq(string(content), "\n") {
				if body, ok := strings.CutPrefix(strings.TrimSpace(line), "-- "); ok && strings.Contains(body, "(") {
					f.Add(body)
				}
			}
		}
	}
	for _, seed := range []string{
		"",
		"sgl.query(name: GetUser, command: one)",
		"(a, b: [x, y], c: 'it''s', d: -1, e: f.g)",
		"(unclosed: [1, 2",
		"$1 as sgl.param(type: text)",
		"{name} as sgl.slice",
		"'unterminated",
	} {
		f.Add(seed)
	}
}

func FuzzDirective(f *testing.F) {
	addDirectiveSeeds(f)

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > fuzzMaxDirectiveBytes {
			return
		}
		lexer := lexerFromContent(input)
		reachedEnd := false
		for range len(input) + 2 {
			if lexer.Next().Kind == TokenEOF {
				reachedEnd = true
				break
			}
		}
		require.True(t, reachedEnd, "lexer did not reach EOF within %d tokens", len(input)+2)

		_, _ = ParseCallArguments(lexerFromContent(input), diagbuild.NewErrorBuilder("fuzz.sql"), "fuzz")

		stripped := StripDirectives(input, NeutralLexicon(), namespace.Default())
		require.Equal(t, stripped, StripDirectives(stripped, NeutralLexicon(), namespace.Default()), "stripping the stripped text changed it")

		_ = NoTransactionRequired(input, NeutralLexicon(), namespace.Default())
		_ = ScanMigration(input, NeutralLexicon(), namespace.Default())
	})
}
