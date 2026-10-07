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

package catalogue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnchorOf(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"Command kinds":                  "command-kinds",
		"Header directive: `sgl.embed`":  "header-directive-sglembed",
		"Type override: `sgl.column`":    "type-override-sglcolumn",
		"Q010":                           "q010",
		"What `prepared.go` gives you":   "what-preparedgo-gives-you",
		"snake_case and kebab-case kept": "snake_case-and-kebab-case-kept",
	}
	for heading, want := range testCases {
		require.Equal(t, want, anchorOf(heading), heading)
	}
}

func TestHeadingAnchorsSkipFencedCode(t *testing.T) {
	t.Parallel()

	anchors := headingAnchors("# Title\n\n## Real heading\n\n```sh\n# not a heading\n```\n\n#not-a-heading\n")
	require.True(t, anchors["title"])
	require.True(t, anchors["real-heading"])
	require.False(t, anchors["not-a-heading"])
}

func TestCheckDocsReportsMissingPagesAndHeadings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	examples := filepath.Join(root, "examples")
	require.NoError(t, os.MkdirAll(examples, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "dialects"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "directives.md"), []byte("# Directives\n\n### Command kinds\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "dialects", "sqlite.md"), []byte("# SQLite\n"), 0o600))

	curriculum := &Curriculum{Concepts: []Concept{{Number: 1, Slug: "first-query", Docs: []string{
		"directives#command-kinds",
		"dialects/sqlite",
		"directives#no-such-heading",
		"missing-page",
	}}}}
	problems := curriculum.checkDocs(examples)
	require.Len(t, problems, 2)
	require.ErrorContains(t, problems[0], `"directives#no-such-heading" names no heading`)
	require.ErrorContains(t, problems[1], `"missing-page" names no page`)
}
